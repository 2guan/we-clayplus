package coreprocess

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func processStamp(pid int) (string, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	// comm 可能有空格或括号；后缀第 20 项是进程启动 tick（stat 第 22 字段）。
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return "", errors.New("invalid process stat")
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" {
		return "", errors.New("process exited")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(boot)) + ":" + fields[19], nil
}
