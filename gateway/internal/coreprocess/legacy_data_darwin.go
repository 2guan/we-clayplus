package coreprocess

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func legacyProcessDataDir(pid int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// ps -ww 读取 argv，不读取或输出目标进程的环境变量。
	out, err := exec.CommandContext(ctx, "/bin/ps", "-ww", "-p", strconv.Itoa(pid), "-o", "args=").Output()
	if err != nil {
		return "", err
	}
	return dataDirFromPSArgs(string(out))
}

func dataDirFromPSArgs(command string) (string, error) {
	const before = " -data "
	const after = " -device-type mac -update-version=false "
	start := strings.Index(command, before)
	if start < 0 || strings.Count(command, before) != 1 {
		return "", errors.New("core 启动参数不含唯一数据目录")
	}
	start += len(before)
	end := strings.Index(command[start:], after)
	if end < 0 {
		return "", errors.New("core 启动参数格式无法确认")
	}
	dir := command[start : start+end]
	if !filepath.IsAbs(dir) {
		return "", errors.New("core 启动数据目录不是绝对路径")
	}
	return dir, nil
}
