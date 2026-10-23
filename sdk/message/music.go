package message

import (
	"encoding/xml"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Music 是受限的音乐卡片数据；调用方不需要提交任意应用 XML。
type Music struct {
	AppID    string `json:"app_id"`
	Title    string `json:"title"`
	Artist   string `json:"artist,omitempty"`
	URL      string `json:"url"`
	AudioURL string `json:"audio_url"`
	Cover    string `json:"cover,omitempty"`
	Lyric    string `json:"lyric,omitempty"`
}

var musicAppID = regexp.MustCompile(`^wx[0-9a-fA-F]{16}$`)

func (m Music) Validate() error {
	if strings.TrimSpace(m.Title) == "" || len(m.Title) > 1000 || len(m.Artist) > 500 || len(m.Lyric) > 64<<10 || m.AppID != "" && !musicAppID.MatchString(m.AppID) {
		return fmt.Errorf("音乐名称、歌词或 AppID 无效")
	}
	for _, raw := range []string{m.URL, m.AudioURL, m.Cover} {
		if raw == "" {
			continue
		}
		u, e := url.Parse(raw)
		if e != nil || len(raw) > 4096 || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Scheme != "https" && u.Scheme != "http" {
			return fmt.Errorf("音乐链接无效")
		}
		if ip := net.ParseIP(u.Hostname()); strings.EqualFold(u.Hostname(), "localhost") || ip != nil && (!ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
			return fmt.Errorf("音乐链接不能使用本机或内网地址")
		}
	}
	if m.AudioURL == "" {
		return fmt.Errorf("音乐缺少播放链接")
	}
	return nil
}

// MusicXML 保持音乐 XML 类型 3；封面同时提供专辑图和聊天卡片缩略图。
func (m Music) XML() (string, error) {
	if e := m.Validate(); e != nil {
		return "", e
	}
	v := struct {
		XMLName    xml.Name `xml:"appmsg"`
		AppID      string   `xml:"appid,attr"`
		SDKVersion string   `xml:"sdkver,attr"`
		Title      string   `xml:"title"`
		Artist     string   `xml:"des"`
		Action     string   `xml:"action"`
		Type       int      `xml:"type"`
		URL        string   `xml:"url"`
		AudioURL   string   `xml:"dataurl"`
		Cover      string   `xml:"songalbumurl"`
		Thumbnail  string   `xml:"thumburl"`
		Lyric      string   `xml:"songlyric"`
	}{AppID: m.AppID, SDKVersion: "0", Title: m.Title, Artist: m.Artist, Action: "view", Type: 3, URL: m.URL, AudioURL: m.AudioURL, Cover: m.Cover, Thumbnail: m.Cover, Lyric: m.Lyric}
	b, e := xml.Marshal(v)
	return string(b), e
}

// ParseMusicXML 只读取音乐字段，不将其它应用能力透传给 core。
func ParseMusicXML(raw string) (Music, error) {
	var v struct {
		XMLName   xml.Name `xml:"appmsg"`
		AppID     string   `xml:"appid,attr"`
		Title     string   `xml:"title"`
		Artist    string   `xml:"des"`
		Type      int      `xml:"type"`
		URL       string   `xml:"url"`
		AudioURL  string   `xml:"dataurl"`
		Cover     string   `xml:"songalbumurl"`
		Thumbnail string   `xml:"thumburl"`
		Lyric     string   `xml:"songlyric"`
	}
	if len(raw) > 128<<10 || xml.Unmarshal([]byte(raw), &v) != nil || v.Type != 3 {
		return Music{}, fmt.Errorf("音乐卡片 XML 无效")
	}
	// 旧卡片与标准音乐卡片可能只提供缩略图；统一为受校验的封面。
	if v.Cover == "" {
		v.Cover = v.Thumbnail
	}
	m := Music{AppID: v.AppID, Title: v.Title, Artist: v.Artist, URL: v.URL, AudioURL: v.AudioURL, Cover: v.Cover, Lyric: v.Lyric}
	return m, m.Validate()
}
