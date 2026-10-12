package user

import (
	"errors"
	"io"
)

// MaxAvatarSize 是现有 core 接口允许的头像字节数上限（16 MiB）。
const MaxAvatarSize = 16 << 20

func readAvatar(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("头像 reader 不能为空")
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxAvatarSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > MaxAvatarSize {
		return nil, errors.New("头像数据必须为 1 字节至 16 MiB")
	}
	return data, nil
}
