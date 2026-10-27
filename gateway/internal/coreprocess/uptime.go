// 主要功能：读取当前 core 监听进程的启动时间，不调用微信业务接口。
package coreprocess

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var socketPID = regexp.MustCompile(`pid=(\d+)`)

func CoreStartedAt(ctx context.Context, address string) (time.Time, error) {
	host, port, err := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	n, numberErr := strconv.Atoi(port)
	if err != nil || numberErr != nil || n < 1 || n > 65535 || host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return time.Time{}, fmt.Errorf("core 必须是本机有效监听地址")
	}
	if runtime.GOOS == "windows" {
		// 端口为已校验的整数；不接收用户提供的脚本、路径或进程号。
		script := fmt.Sprintf("$ids=@(Get-NetTCPConnection -LocalPort %d -State Listen -ErrorAction Stop | Select-Object -ExpandProperty OwningProcess -Unique); if($ids.Count -ne 1){exit 1}; (Get-Process -Id $ids[0] -ErrorAction Stop).StartTime.ToUniversalTime().ToString('o')", n)
		out, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
		if err != nil {
			return time.Time{}, fmt.Errorf("无法读取 core 监听进程")
		}
		return time.Parse(time.RFC3339Nano, strings.TrimSpace(string(out)))
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return time.Time{}, fmt.Errorf("当前平台无法读取 core 进程指标")
	}
	pids := map[int]bool{}
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-t", "-iTCP:"+port, "-sTCP:LISTEN").Output()
	if err == nil {
		for _, field := range strings.Fields(string(out)) {
			pid, e := strconv.Atoi(field)
			if e == nil && pid > 0 {
				pids[pid] = true
			}
		}
	} else if runtime.GOOS == "linux" {
		// Linux 安装可能只有 iproute2，不依赖额外安装 lsof。
		out, err = exec.CommandContext(ctx, "ss", "-Hlnpt", "sport = :"+port).Output()
		if err == nil {
			for _, match := range socketPID.FindAllStringSubmatch(string(out), -1) {
				pid, _ := strconv.Atoi(match[1])
				if pid > 0 {
					pids[pid] = true
				}
			}
		}
	}
	if len(pids) != 1 {
		return time.Time{}, fmt.Errorf("无法确定唯一的 core 监听进程")
	}
	var pid int
	for id := range pids {
		pid = id
	}
	out, err = exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "etime=").Output()
	if err != nil {
		return time.Time{}, fmt.Errorf("无法读取 core 进程运行时间")
	}
	elapsed, err := processElapsed(strings.TrimSpace(string(out)))
	if err != nil {
		return time.Time{}, err
	}
	return time.Now().Add(-elapsed), nil
}

func processElapsed(raw string) (time.Duration, error) {
	var days int64
	if before, after, ok := strings.Cut(raw, "-"); ok {
		v, err := strconv.ParseInt(before, 10, 32)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("无效的 core 进程运行时间")
		}
		days, raw = v, after
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("无效的 core 进程运行时间")
	}
	seconds := int64(0)
	for i, part := range parts {
		n, err := strconv.ParseInt(part, 10, 32)
		if err != nil || n < 0 || i > 0 && n > 59 {
			return 0, fmt.Errorf("无效的 core 进程运行时间")
		}
		seconds = seconds*60 + n
	}
	seconds += days * 86400
	if seconds > int64((time.Duration(1<<63-1))/time.Second) {
		return 0, fmt.Errorf("core 进程运行时间溢出")
	}
	return time.Duration(seconds) * time.Second, nil
}
