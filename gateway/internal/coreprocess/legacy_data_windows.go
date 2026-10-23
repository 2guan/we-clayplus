package coreprocess

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

func legacyProcessDataDir(pid int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// 只读取指定 PID 的 CommandLine；PID 被格式化为十进制数字，不执行外部输入。
	script := fmt.Sprintf(`[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false); $p = Get-CimInstance Win32_Process -Filter 'ProcessId=%d'; if ($null -eq $p) { exit 1 }; [Console]::Out.Write($p.CommandLine)`, pid)
	out, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return "", err
	}
	args, err := windows.DecomposeCommandLine(strings.TrimSpace(string(out)))
	if err != nil {
		return "", err
	}
	if len(args) == 0 {
		return "", errors.New("core 启动参数为空")
	}
	return dataDirFromArgs(args)
}
