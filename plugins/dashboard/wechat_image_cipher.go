// 文件名：wechat_image_cipher.go
// 主要功能：按朋友圈媒体密钥生成 ISAAC-64 字节流，还原当前账号可读取的加密图片。
// 作者：2guan
package dashboard

// ISAAC-64 算法由 Bob Jenkins 公开： https://burtleburtle.net/bob/rand/isaacafa.html
// 这里只实现微信图片所需的单种播种和大端输出，不承担通用密码接口。
func decryptWechatImage(data []byte, key uint64) {
	var memory, results [256]uint64
	var a, b, counter uint64
	var mix [8]uint64
	for i := range mix {
		mix[i] = 0x9e3779b97f4a7c13
	}
	shuffle := func() {
		shifts := [8]uint{9, 9, 23, 15, 14, 20, 17, 14}
		for i, shift := range shifts {
			mix[i] -= mix[(i+4)%8]
			value := mix[(i+7)%8]
			if i%2 == 0 {
				value >>= shift
			} else {
				value <<= shift
			}
			mix[(i+5)%8] ^= value
			mix[(i+7)%8] += mix[i]
		}
	}
	for i := 0; i < 4; i++ {
		shuffle()
	}
	results[0] = key
	// 两遍混合先吸收种子，再扩散到完整内部状态。
	for pass := 0; pass < 2; pass++ {
		for offset := 0; offset < 256; offset += 8 {
			for j := 0; j < 8; j++ {
				if pass == 0 {
					mix[j] += results[offset+j]
				} else {
					mix[j] += memory[offset+j]
				}
			}
			shuffle()
			copy(memory[offset:offset+8], mix[:])
		}
	}
	generate := func() {
		counter++
		b += counter
		for i := range memory {
			x := memory[i]
			switch i % 4 {
			case 0:
				a = ^(a ^ (a << 21))
			case 1:
				a ^= a >> 5
			case 2:
				a ^= a << 12
			case 3:
				a ^= a >> 33
			}
			a += memory[(i+128)%256]
			y := memory[(x>>3)%256] + a + b
			memory[i] = y
			b = memory[(y>>11)%256] + x
			results[i] = b
		}
	}
	// 每批倒序取 256 个数，每个数按大端字节序与图片异或。
	index := -1
	for offset := 0; offset < len(data); offset += 8 {
		if index < 0 {
			generate()
			index = 255
		}
		value := results[index]
		index--
		for j := 0; j < 8 && offset+j < len(data); j++ {
			data[offset+j] ^= byte(value >> uint(56-j*8))
		}
	}
}
