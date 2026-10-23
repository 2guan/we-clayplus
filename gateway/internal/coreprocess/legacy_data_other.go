//go:build !darwin && !linux && !windows

package coreprocess

import "errors"

func legacyProcessDataDir(pid int) (string, error) {
	return "", errors.New("无法在此平台确认旧版 core 的启动数据目录")
}
