//go:build !darwin && !linux && !windows

package coreprocess

import "errors"

func processStamp(pid int) (string, error) {
	return "", errors.New("独立 core 仅支持 macOS、Linux、Windows")
}
