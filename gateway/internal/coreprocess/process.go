package coreprocess

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/we-clay/we-clay/gateway/internal/config"
)

type Process struct {
	cmd      *exec.Cmd
	done     chan struct{}
	err      error // 在关闭 done 前赋值，只在 done 关闭后读取。
	logPath  string
	stopOnce sync.Once
}

// Start 启动本机 core；KeepAlive 时子进程脱离当前终端和 Run 的生命周期。
func Start(ctx context.Context, binary string, cfg config.Config) (*Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c := cfg.Core
	for _, address := range []string{cfg.CoreAddress, c.LoginAddress} {
		if address == "-" {
			continue
		}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return nil, fmt.Errorf("core 地址 %s 已被占用或不可监听；连接现有 core 请省略 Config.Core: %w", address, err)
		}
		listener.Close()
	}
	stage, err := loginStage(*c)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(c.DataDir, 0700); err != nil {
		return nil, err
	}
	loginAddress := c.LoginAddress
	if loginAddress == "-" {
		loginAddress = ""
	}
	args := []string{"-stage", stage, "-data", c.DataDir, "-device-type", "mac", "-update-version=false", "-listen", loginAddress, "-grpc-listen", cfg.CoreAddress, "-api-token-env", "WE_CLAY_GATEWAY_CORE_TOKEN"}
	if c.DeviceName != "" {
		args = append(args, "-device-name", c.DeviceName)
	}
	if c.StorageEncrypted {
		args = append(args, "-storage-encrypt")
	}
	p := &Process{cmd: exec.Command(binary, args...), done: make(chan struct{}), logPath: filepath.Join(c.DataDir, "core.log")}
	if c.KeepAlive {
		detach(p.cmd)
	}
	log, err := os.OpenFile(p.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	p.cmd.Stdout, p.cmd.Stderr = log, log
	p.cmd.Env = childEnv(os.Environ(), cfg.CoreToken, c.GitHubTokenEnv)
	if err = p.cmd.Start(); err != nil {
		log.Close()
		return nil, fmt.Errorf("启动 core 失败: %w", err)
	}
	go func() { p.err = p.cmd.Wait(); log.Close(); close(p.done) }()
	go func() {
		if c.KeepAlive {
			return
		}
		select {
		case <-ctx.Done():
			p.Stop()
		case <-p.done:
		}
	}()
	return p, nil
}

func loginStage(c config.CoreConfig) (string, error) {
	if c.Stage != "auto" {
		return c.Stage, nil
	}
	for _, name := range []string{"session.json", "session.info"} {
		info, err := os.Stat(filepath.Join(c.DataDir, name))
		if err == nil {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("core 会话路径不是普通文件")
			}
			return "resume", nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "login", nil
}

func childEnv(env []string, token, githubEnv string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "OSS_ACCESS_KEY_ID") || strings.EqualFold(key, "OSS_ACCESS_KEY_SECRET") || strings.EqualFold(key, "OSS_SESSION_TOKEN") || strings.EqualFold(key, "SDK_SOURCE_TOKEN") || strings.EqualFold(key, "WE_CLAY_GATEWAY_CORE_TOKEN") || strings.EqualFold(key, githubEnv) || strings.EqualFold(key, "GH_TOKEN") || strings.EqualFold(key, "GITHUB_TOKEN") {
			continue
		}
		out = append(out, entry)
	}
	return append(out, "WE_CLAY_GATEWAY_CORE_TOKEN="+token)
}

func (p *Process) Done() <-chan struct{} { return p.done }

// ExitError 只能在 Done 关闭后调用；即便退出码为 0，运行中的 core 消失也属于错误。
func (p *Process) ExitError() error {
	return fmt.Errorf("core 已退出（%v）；本地日志：%s", p.err, p.logPath)
}
func (p *Process) Stop() {
	p.stopOnce.Do(func() {
		select {
		case <-p.done:
			return
		default:
		}
		_ = terminate(p.cmd.Process)
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-p.done:
		case <-timer.C:
			_ = p.cmd.Process.Kill()
			<-p.done
		}
	})
}
