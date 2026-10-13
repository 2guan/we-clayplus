package coreprocess

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/config"
)

const independentFile = "gateway-core.json"

// 位于账号私有目录；token 从不输出到日志或命令参数。
type independentRecord struct {
	PID          int    `json:"pid"`
	Stamp        string `json:"process_stamp"`
	Token        string `json:"token"`
	Repository   string `json:"repository"`
	Version      string `json:"version"`
	Address      string `json:"address"`
	LoginAddress string `json:"login_address"`
	Binary       string `json:"binary"`
	Encrypted    bool   `json:"storage_encrypted"`
}

func readIndependent(dir string) (*independentRecord, error) {
	path := filepath.Join(dir, independentFile)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16384 {
		return nil, errors.New("独立 core 进程记录无效")
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r independentRecord
	if json.Unmarshal(b, &r) != nil || r.PID <= 0 || r.Stamp == "" || len(r.Token) < 16 {
		return nil, errors.New("独立 core 进程记录无效；请核对本机账号目录")
	}
	return &r, nil
}

func writeIndependent(dir string, r independentRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".gateway-core-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, independentFile))
}

func lockIndependent(ctx context.Context, dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "gateway-core.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err = tryLock(f); err == nil {
			return f, nil
		}
		if !lockBusy(err) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func reachable(address string) bool {
	if address == "-" {
		return false
	}
	c, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func probe(ctx context.Context, cfg config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c, err := api.NewConfiguredClient(ctx, &cfg)
	if err != nil {
		return err
	}
	defer c.Close()
	var view struct {
		Online bool   `json:"online"`
		Status string `json:"status"`
	}
	return c.Get("/api/login/view").WithContext(ctx).DoJson(&view)
}

func matchIndependent(r *independentRecord, cfg config.Config) error {
	if r.Repository != cfg.Core.Repository || r.Version != cfg.Core.Version || r.Address != cfg.CoreAddress || r.LoginAddress != cfg.Core.LoginAddress || r.Encrypted != cfg.Core.StorageEncrypted {
		return errors.New("已有独立 core 的版本或地址配置不同；请先用原配置显式 StopCore，再启动新配置")
	}
	if cfg.CoreToken != "" && cfg.CoreToken != r.Token {
		return errors.New("独立 core 令牌与调用方配置不同")
	}
	return nil
}

// EnsureIndependent 按固定账号目录复用进程；未知监听者绝不自动接管。
func EnsureIndependent(ctx context.Context, cfg config.Config) (config.Config, error) {
	f, err := lockIndependent(ctx, cfg.Core.DataDir)
	if err != nil {
		return cfg, err
	}
	defer unlockFile(f)
	r, err := readIndependent(cfg.Core.DataDir)
	if err != nil {
		return cfg, err
	}
	if r != nil && sameProcess(r) {
		if err = matchIndependent(r, cfg); err != nil {
			return cfg, err
		}
		cfg.CoreToken = r.Token
		if err = probe(ctx, cfg); err != nil {
			return cfg, errors.New("已有独立 core 尚不可连接或鉴权失败；保留进程，请检查 core.log")
		}
		return cfg, nil
	}
	if reachable(cfg.CoreAddress) || reachable(cfg.Core.LoginAddress) {
		return cfg, errors.New("core 端口已被其他程序使用；不会接管或关闭该进程")
	}
	binary, err := Ensure(ctx, *cfg.Core)
	if err != nil {
		return cfg, err
	}
	if cfg.CoreToken == "" {
		var key [32]byte
		if _, err = rand.Read(key[:]); err != nil {
			return cfg, err
		}
		cfg.CoreToken = hex.EncodeToString(key[:])
	}
	if len(cfg.CoreToken) < 16 {
		return cfg, errors.New("独立 core 令牌至少需要 16 个字符")
	}
	p, err := Start(ctx, binary, cfg)
	if err != nil {
		return cfg, err
	}
	stamp, err := processStamp(p.cmd.Process.Pid)
	if err != nil {
		p.Stop()
		return cfg, errors.New("无法确认新 core 的进程身份，已停止新进程")
	}
	r = &independentRecord{Stamp: stamp, PID: p.cmd.Process.Pid, Token: cfg.CoreToken, Repository: cfg.Core.Repository, Version: cfg.Core.Version, Address: cfg.CoreAddress, LoginAddress: cfg.Core.LoginAddress, Binary: binary, Encrypted: cfg.Core.StorageEncrypted}
	if err = writeIndependent(cfg.Core.DataDir, *r); err != nil {
		p.Stop()
		return cfg, errors.New("保存独立 core 进程记录失败，已停止新进程")
	}
	// gRPC 尚未监听时保留启动锁，防止第二个调用方误认为进程已失效。
	ready, cancel := context.WithTimeout(ctx, cfg.Core.StartupTimeout)
	defer cancel()
	for {
		if err = probe(ready, cfg); err == nil {
			return cfg, nil
		}
		select {
		case <-p.Done():
			return cfg, p.ExitError()
		case <-ready.Done():
			return cfg, fmt.Errorf("等待独立 core 启动超时或取消；进程记录已保留，可重新连接或显式停止: %w (最近探测: %v)", ready.Err(), err)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// StopIndependent 使用随机令牌确认端口归属后才按记录 PID 停止。
func StopIndependent(ctx context.Context, cfg config.Config) error {
	f, err := lockIndependent(ctx, cfg.Core.DataDir)
	if err != nil {
		return err
	}
	defer unlockFile(f)
	r, err := readIndependent(cfg.Core.DataDir)
	if err != nil || r == nil {
		return err
	}
	if !sameProcess(r) {
		return os.Remove(filepath.Join(cfg.Core.DataDir, independentFile))
	}
	if err = matchIndependent(r, cfg); err != nil {
		return err
	}
	cfg.CoreToken = r.Token
	if err = probe(ctx, cfg); err != nil {
		return errors.New("无法通过 gRPC 确认独立 core 身份，未停止任何进程；请核对本机 core.log")
	}
	p, err := os.FindProcess(r.PID)
	if err != nil {
		return err
	}
	defer p.Release()
	if err = terminate(p); err != nil {
		return err
	}
	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for sameProcess(r) {
		select {
		case <-deadline.Done():
			return errors.New("已发出 core 停止信号，等待退出超时；请检查进程状态")
		case <-time.After(50 * time.Millisecond):
		}
	}
	return os.Remove(filepath.Join(cfg.Core.DataDir, independentFile))
}

func sameProcess(r *independentRecord) bool {
	if !processAlive(r.PID) {
		return false
	}
	stamp, err := processStamp(r.PID)
	return err == nil && stamp == r.Stamp
}

// WaitIndependent 仅观察进程退出；取消观察不会停止 core。
func WaitIndependent(ctx context.Context, cfg config.Config) error {
	r, err := readIndependent(cfg.Core.DataDir)
	if err != nil {
		return err
	}
	if r == nil {
		return errors.New("独立 core 进程记录缺失")
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if !sameProcess(r) {
				return errors.New("独立 core 已退出，请检查 core.log 后重新连接")
			}
		}
	}
}
