// 文件名：wechat_image_cipher_test.go
// 主要功能：验证 ISAAC-64 已知输出、尾部字节与重复解码还原，防止图片解密退化。
// 作者：2guan
package dashboard

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestWechatImageCipher(t *testing.T) {
	expected, _ := hex.DecodeString("9e2b4f671b4aade2916ff0d035894e2c8b41c55ddd8918b507ad22dddfb8f138d7b8bc98379aefc3")
	data := make([]byte, len(expected))
	decryptWechatImage(data, 123456)
	if !bytes.Equal(data, expected) {
		t.Fatalf("字节流不匹配: %x", data)
	}
	for _, size := range []int{1, 7, 8, 2049, 5000} {
		data := bytes.Repeat([]byte{0x5a}, size)
		original := bytes.Clone(data)
		decryptWechatImage(data, 123456)
		decryptWechatImage(data, 123456)
		if !bytes.Equal(data, original) {
			t.Fatal("跨块或尾部还原错误")
		}
	}
}
