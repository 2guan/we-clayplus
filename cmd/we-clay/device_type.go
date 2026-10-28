package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 安装器选定的设备平台绑定该安装目录；只限制设备类型，不改变其他 CLI 参数的优先级。
// 没有安装器类型文件时，保留独立开发者运行的默认值与显式选择。
func installedDeviceType(dataDir, requested string, explicit bool) (string, error) {
	if explicit && requested != "mac" && requested != "win" {
		return "", errors.New("--device-type 必须为 mac 或 win")
	}
	path := filepath.Join(dataDir, "device-type.txt")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return requested, nil
	}
	if err != nil {
		return "", fmt.Errorf("读取安装器设备类型配置失败: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 64 {
		return "", errors.New("安装器设备类型配置必须为 mac 或 win")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取安装器设备类型配置失败: %w", err)
	}
	saved := strings.TrimRight(string(raw), "\r\n")
	if saved != "mac" && saved != "win" {
		return "", errors.New("安装器设备类型配置必须为 mac 或 win")
	}
	if explicit && requested != saved {
		return "", errors.New("已有安装不能切换设备类型；请使用原类型，或为新设备选择独立安装目录和账号目录")
	}
	return saved, nil
}
