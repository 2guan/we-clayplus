// 文件名：render.go
// 主要功能：用本机中文字体生成词云、文字表情和日报 PNG；不内嵌有版权限制的系统字体。
// 作者：2guan
package toolbox

import (
	"bytes"
	"fmt"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"strings"
)

func loadFont(path string) (*opentype.Font, error) {
	paths := []string{path}
	if path == "" {
		paths = []string{"/System/Library/Fonts/Supplemental/Arial Unicode.ttf", "/System/Library/Fonts/Supplemental/Songti.ttc", "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc", "C:/Windows/Fonts/msyh.ttc"}
	}
	for _, p := range paths {
		info, e := os.Stat(p)
		if e != nil || info.Size() > 128<<20 {
			continue
		}
		raw, e := os.ReadFile(p)
		if e != nil {
			continue
		}
		coll, e := opentype.ParseCollection(raw)
		if e == nil {
			f, e := coll.Font(0)
			if e == nil {
				if glyph, err := f.GlyphIndex(nil, '中'); err == nil && glyph != 0 {
					return f, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("未找到中文字体，请配置可读取的 TTF / TTC 字体路径")
}

// RenderCloud 沿确定性的中心螺旋放置词语，检测边界和碰撞，保证导出 PNG 无重叠。
func RenderCloud(words []Count, path string) ([]byte, error) {
	if len(words) == 0 {
		return nil, fmt.Errorf("所选范围没有可用于词云的文字")
	}
	f, e := loadFont(path)
	if e != nil {
		return nil, e
	}
	img := image.NewRGBA(image.Rect(0, 0, 1000, 650))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{246, 248, 247, 255}), image.Point{}, draw.Src)
	palette := []color.Color{color.RGBA{7, 139, 78, 255}, color.RGBA{51, 77, 69, 255}, color.RGBA{62, 111, 147, 255}, color.RGBA{125, 98, 61, 255}}
	boxes := []image.Rectangle{}
	for i, w := range words {
		size := 22 + 42*float64(w.Count)/float64(words[0].Count)
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72})
		if err != nil {
			return nil, err
		}
		width := font.MeasureString(face, w.Name).Ceil()
		height := face.Metrics().Height.Ceil()
		ascent := face.Metrics().Ascent.Ceil()
		for step := 0; step < 2400; step++ {
			angle := float64(step) * 0.28
			radius := 3 * math.Sqrt(float64(step))
			x := 500 + int(radius*3.2*math.Cos(angle)) - width/2
			y := 325 + int(radius*1.9*math.Sin(angle)) - height/2
			box := image.Rect(x-6, y-4, x+width+6, y+height+4)
			if !box.In(image.Rect(30, 25, 970, 625)) {
				continue
			}
			fits := true
			for _, old := range boxes {
				if box.Overlaps(old) {
					fits = false
					break
				}
			}
			if !fits {
				continue
			}
			d := font.Drawer{Dst: img, Src: image.NewUniform(palette[i%len(palette)]), Face: face, Dot: fixed.P(x, y+ascent)}
			d.DrawString(w.Name)
			boxes = append(boxes, box)
			break
		}
		face.Close()
	}
	var b bytes.Buffer
	e = png.Encode(&b, img)
	return b.Bytes(), e
}

func RenderCard(title, text, path string, background []byte) ([]byte, error) {
	if len([]rune(text)) > 600 {
		return nil, fmt.Errorf("图片文字最多 600 字")
	}
	f, e := loadFont(path)
	if e != nil {
		return nil, e
	}
	img := image.NewRGBA(image.Rect(0, 0, 900, 700))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{247, 249, 247, 255}), image.Point{}, draw.Src)
	if len(background) > 0 {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(background))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 5000 || cfg.Height > 5000 || cfg.Width*cfg.Height > 20_000_000 {
			return nil, fmt.Errorf("背景图无效或像素过大")
		}
		src, _, err := image.Decode(bytes.NewReader(background))
		if err != nil {
			return nil, err
		} // 等比缩略图保留在顶部，不让文字覆盖原始图片。
		for y := 0; y < 300; y++ {
			for x := 0; x < 900; x++ {
				img.Set(x, y, src.At(x*cfg.Width/900, y*cfg.Height/300))
			}
		}
	}
	face, e := opentype.NewFace(f, &opentype.FaceOptions{Size: 30, DPI: 72})
	if e != nil {
		return nil, e
	}
	defer face.Close()
	y := 70
	if len(background) > 0 {
		y = 350
	}
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.RGBA{35, 57, 46, 255}), Face: face}
	for _, line := range strings.Split(title+"\n\n"+text, "\n") {
		part := ""
		for _, r := range line {
			if font.MeasureString(face, part+string(r)).Ceil() > 800 {
				d.Dot = fixed.P(45, y)
				d.DrawString(part)
				y += 43
				part = ""
			}
			if y > 660 {
				return nil, fmt.Errorf("文字超过图片容量，请缩短内容")
			}
			part += string(r)
		}
		d.Dot = fixed.P(45, y)
		d.DrawString(part)
		y += 43
	}
	var b bytes.Buffer
	e = png.Encode(&b, img)
	return b.Bytes(), e
}
