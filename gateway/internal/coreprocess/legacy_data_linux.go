package coreprocess

import (
	"bytes"
	"fmt"
	"os"
)

func legacyProcessDataDir(pid int) (string, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return "", err
	}
	parts := bytes.Split(bytes.TrimSuffix(raw, []byte{0}), []byte{0})
	args := make([]string, len(parts))
	for i, part := range parts {
		args[i] = string(part)
	}
	return dataDirFromArgs(args)
}
