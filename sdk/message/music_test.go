package message

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestMusicXMLRoundTrip(t *testing.T) {
	m := Music{AppID: "wx1111111111111111", Title: `歌名<&"`, Artist: "甲 & 乙", URL: "https://example.com/song?a=1&b=2", AudioURL: "https://example.com/audio.mp3?a=1&b=2", Cover: "https://example.com/cover.jpg", Lyric: "第一行<&>\n第二行"}
	raw, err := m.XML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "<type>3</type>") || strings.Contains(raw, "歌名<") {
		t.Fatal("音乐协议或 XML 转义错误", raw)
	}
	var images struct {
		Cover string `xml:"songalbumurl"`
		Thumb string `xml:"thumburl"`
	}
	if xml.Unmarshal([]byte(raw), &images) != nil || images.Cover != m.Cover || images.Thumb != m.Cover {
		t.Fatal("手机卡片缩略图与专辑封面未完整输出")
	}
	got, err := ParseMusicXML(raw)
	if err != nil || got != m {
		t.Fatal("卡片字段丢失", got, err)
	}
	for _, invalid := range []string{strings.Replace(raw, "<type>3</type>", "<type>57</type>", 1), strings.ReplaceAll(raw, "appmsg", "msg"), raw[:len(raw)/2]} {
		if _, err := ParseMusicXML(invalid); err == nil {
			t.Fatal("接受了非音乐 XML")
		}
	}
}

func TestMusicValidation(t *testing.T) {
	valid := Music{Title: "歌", AudioURL: "https://example.com/song.mp3"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []Music{{AudioURL: valid.AudioURL}, {Title: "歌"}, {Title: "歌", AudioURL: "file:///tmp/audio"}, {Title: "歌", AudioURL: "https://user:pass@example.com/a"}, {Title: "歌", AudioURL: "http://127.0.0.1/a"}, {Title: "歌", AudioURL: valid.AudioURL, AppID: "<inject>"}, {Title: "歌", AudioURL: valid.AudioURL, Lyric: strings.Repeat("x", 65537)}} {
		if _, err := tc.XML(); err == nil {
			t.Fatal("接受了无效卡片")
		}
	}
}

func TestMusicThumbnailCompatibilityAndValidation(t *testing.T) {
	const raw = `<appmsg><type>3</type><title>测试歌曲</title><dataurl>https://example.com/audio.mp3</dataurl><thumburl>https://example.com/cover.jpg?a=1&amp;b=2</thumburl></appmsg>`
	m, err := ParseMusicXML(raw)
	if err != nil || m.Cover != "https://example.com/cover.jpg?a=1&b=2" {
		t.Fatal("未读取只有缩略图的音乐卡片", err)
	}
	out, err := m.XML()
	if err != nil || !strings.Contains(out, "<thumburl>https://example.com/cover.jpg?a=1&amp;b=2</thumburl>") {
		t.Fatal("缩略图未安全转义并保留", err)
	}
	if _, err := ParseMusicXML(strings.Replace(raw, "https://example.com/cover.jpg?a=1&amp;b=2", "http://127.0.0.1/private", 1)); err == nil {
		t.Fatal("缩略图绕过公网链接校验")
	}
}
