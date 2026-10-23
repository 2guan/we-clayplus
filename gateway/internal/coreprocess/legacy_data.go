package coreprocess

import (
	"errors"
	"path/filepath"
)

// 仅从 argv 中提取 core 启动时的 -data 参数；不存在或重复时拒绝旧记录迁移。
func dataDirFromArgs(args []string) (string, error) {
	var dir string
	for i, arg := range args {
		if arg != "-data" {
			continue
		}
		if dir != "" || i+1 >= len(args) || !filepath.IsAbs(args[i+1]) {
			return "", errors.New("core 启动数据目录参数无效")
		}
		dir = args[i+1]
	}
	if dir == "" {
		return "", errors.New("core 启动数据目录参数缺失")
	}
	return dir, nil
}
