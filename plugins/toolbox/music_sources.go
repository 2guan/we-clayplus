package toolbox

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	DouyinMusicEndpoint = "https://109a.cn/API/dyyy/api.php"
	KuwoMusicEndpoint   = "https://109a.cn/API/kwyy/api.php"
)

// MusicAudioMIME 额外识别抖音实际返回的 M4A；不相信服务声明的 Content-Type。
func MusicAudioMIME(data []byte) string {
	mime := http.DetectContentType(data)
	if strings.HasPrefix(mime, "audio/") {
		return mime
	}
	if mp3Frames(data) {
		return "audio/mpeg"
	}
	if mime == "video/ogg" {
		return "audio/ogg"
	}
	if len(data) >= 12 && string(data[4:8]) == "ftyp" && string(data[8:12]) == "M4A " {
		return "audio/mp4"
	}
	return ""
}

// 没有 ID3 的 MP3 不在标准嗅探表中；验证连续两帧，避免仅凭声明的 MIME 放行。
func mp3Frames(data []byte) bool {
	frame := func(b []byte) (size, version, rate int) {
		if len(b) < 4 || b[0] != 0xff || b[1]&0xe0 != 0xe0 || b[1]&6 != 2 {
			return
		}
		version = int(b[1]>>3) & 3
		bitrate, sample := int(b[2]>>4), int(b[2]>>2)&3
		if version == 1 || bitrate == 0 || bitrate == 15 || sample == 3 || b[3]&3 == 2 {
			return 0, 0, 0
		}
		rate = []int{44100, 48000, 32000}[sample]
		kbps := []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}[bitrate]
		factor := 144
		if version != 3 {
			rate /= 2
			if version == 0 {
				rate /= 2
			}
			kbps = []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}[bitrate]
			factor = 72
		}
		return factor*kbps*1000/rate + int(b[2]>>1&1), version, rate
	}
	n, v, r := frame(data)
	if n < 4 || n+4 > len(data) {
		return false
	}
	n2, v2, r2 := frame(data[n:])
	return n2 >= 4 && n+n2 <= len(data) && v == v2 && r == r2
}

func musicNormal(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, text)
}

func musicMatchesQuery(item Item, query string) bool {
	// 翻唱括号里的原唱不等于当前演唱者。
	title := item.Title
	if i := strings.IndexAny(title, "（([【"); i >= 0 {
		title = title[:i]
	}
	q, t, a := musicNormal(query), musicNormal(title), musicNormal(item.Artist)
	if q == "" {
		return false
	}
	if strings.Contains(t, q) || strings.Contains(a, q) || a != "" && (strings.Contains(a+t, q) || strings.Contains(t+a, q)) {
		return true
	}
	// 组合查询必须先核实歌手，才放宽连接词和长歌名的一字差异，避免串到其它歌手。
	if a != "" && strings.HasPrefix(q, a) {
		wanted := strings.TrimPrefix(q, a)
		if strings.HasPrefix(wanted, "的") {
			wanted = strings.TrimPrefix(wanted, "的")
		}
		return wanted != "" && (wanted == t || musicTitleNear(wanted, t))
	}
	return false
}

func musicTitleNear(a, b string) bool {
	x, y := []rune(a), []rune(b)
	if min(len(x), len(y)) < 5 || len(x)-len(y) > 1 || len(y)-len(x) > 1 {
		return false
	}
	i, j, changes := 0, 0, 0
	for i < len(x) && j < len(y) {
		if x[i] == y[j] {
			i++
			j++
			continue
		}
		changes++
		if changes > 1 {
			return false
		}
		switch {
		case len(x) > len(y):
			i++
		case len(y) > len(x):
			j++
		default:
			i++
			j++
		}
	}
	return changes+len(x)-i+len(y)-j <= 1
}

func musicAudioHead(ctx context.Context, client *http.Client, raw string) ([]byte, error) {
	if PublicURL(raw) != nil || len(raw) > 4096 {
		return nil, fmt.Errorf("音乐播放地址无效")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if e != nil {
		return nil, fmt.Errorf("音乐播放地址无效")
	}
	req.Header.Set("Range", "bytes=0-65535")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	r, e := client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("音乐播放地址暂时无法读取")
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK && r.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("音乐播放地址返回 HTTP %d", r.StatusCode)
	}
	head, e := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if e != nil || MusicAudioMIME(head) == "" {
		return nil, fmt.Errorf("播放地址未返回可识别的音频")
	}
	return head, nil
}

func musicStatus(raw json.RawMessage, want string) bool {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		s = string(raw)
	}
	return s == want
}

func parseDouyinMusic(raw []byte, query string, limit int) ([]Item, error) {
	var body struct {
		Code json.RawMessage
		Data []struct {
			Title string
			URL   string
			Image string `json:"imgurl"`
		}
	}
	if json.Unmarshal(raw, &body) != nil || !musicStatus(body.Code, "0") || len(body.Data) == 0 {
		return nil, fmt.Errorf("抖音音乐未返回搜索结果")
	}
	items := make([]Item, 0, len(body.Data))
	for _, v := range body.Data {
		title, artist := strings.TrimSpace(v.Title), ""
		if i := strings.LastIndex(title, "---"); i >= 0 {
			title, artist = strings.TrimSpace(title[:i]), strings.TrimSpace(title[i+3:])
		}
		if title == "" || len(title) > 1000 || len(artist) > 500 || v.URL == "" || len(v.URL) > 4096 || PublicURL(v.URL) != nil || v.Image != "" && (len(v.Image) > 4096 || PublicURL(v.Image) != nil) {
			continue
		}
		item := Item{Title: title, Artist: artist, URL: v.URL, MediaURL: v.URL, Cover: v.Image}
		if musicMatchesQuery(item, query) {
			items = append(items, item)
		}
	}
	// 歌名精确匹配优先，避免把名叫“晴天”的歌手排在歌曲“晴天”前面。
	q := musicNormal(query)
	score := func(v Item) int {
		t := musicNormal(v.Title)
		if t == q {
			return 0
		}
		if strings.Contains(t, q) {
			return 1
		}
		return 2
	}
	sort.SliceStable(items, func(i, j int) bool { return score(items[i]) < score(items[j]) })
	if limit < 1 || limit > 20 {
		limit = 5
	}
	if len(items) > limit {
		items = items[:limit]
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("抖音音乐没有匹配所填歌名、歌手的可用结果")
	}
	return items, nil
}

// SearchMusicSources 返回首个已验证音频的匹配；源内与源间切换都不产生微信写操作。
func SearchMusicSources(ctx context.Context, client *http.Client, source, query string, limit int) ([]Item, string, error) {
	if strings.TrimSpace(query) == "" {
		return nil, "", fmt.Errorf("请填写歌名或歌手")
	}
	sources := []string{source}
	if source == "auto" || source == "" {
		sources = []string{"kuwo", "douyin"}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	failures := []string{}
	for _, id := range sources {
		if ctx.Err() != nil {
			break
		}
		name, endpoint := map[string]string{"kuwo": "酷我音乐", "douyin": "抖音音乐"}[id], map[string]string{"kuwo": KuwoMusicEndpoint, "douyin": DouyinMusicEndpoint}[id]
		if endpoint == "" {
			return nil, "", fmt.Errorf("音乐音源无效")
		}
		attempt, stop := context.WithTimeout(ctx, 12*time.Second)
		item, e := searchMusicSource(attempt, client, id, endpoint, query, limit)
		stop()
		if e == nil {
			return []Item{item}, name, nil
		}
		failures = append(failures, name+"："+e.Error())
	}
	if len(failures) == 0 {
		return nil, "", fmt.Errorf("音乐搜索已取消或超时")
	}
	return nil, "", fmt.Errorf("%s", strings.Join(failures, "；"))
}

func searchMusicSource(ctx context.Context, client *http.Client, id, endpoint, query string, limit int) (Item, error) {
	q := url.Values{"msg": {query}}
	if id == "kuwo" {
		q.Set("a", "1")
		q.Set("n", "1")
	}
	raw, _, e := ReadMusicURL(ctx, client, endpoint+"?"+q.Encode(), "", 2<<20)
	if e != nil {
		return Item{}, e
	}
	if id == "kuwo" {
		var body struct {
			Code json.RawMessage
			Name string
			URL  string
		}
		if json.Unmarshal(raw, &body) != nil || !musicStatus(body.Code, "200") || body.URL == "" {
			return Item{}, fmt.Errorf("酷我音乐未返回搜索结果")
		}
		head, e := musicAudioHead(ctx, client, body.URL)
		if e != nil {
			return Item{}, e
		}
		title, artist := musicID3(head)
		album, catalogArtist := kuwoCatalog(body.Name)
		if artist == "" {
			artist = catalogArtist
		}
		if title != "" && !musicMatchesQuery(Item{Title: title, Artist: artist}, query) {
			return Item{}, fmt.Errorf("酷我音乐返回的歌名、歌手与查询不匹配")
		}
		metadata, metadataErr := readKuwoMetadata(ctx, client, query, title, artist, album)
		if metadataErr != nil && title == "" {
			return Item{}, metadataErr
		}
		if metadataErr == nil {
			title, artist = metadata.Title, metadata.Artist
		}
		if len(title) > 1000 || len(artist) > 500 {
			return Item{}, fmt.Errorf("歌曲标签过长")
		}
		return Item{Title: title, Artist: artist, URL: body.URL, MediaURL: body.URL, Cover: metadata.Cover}, nil
	}
	items, e := parseDouyinMusic(raw, query, limit)
	if e != nil {
		return Item{}, e
	}
	for _, item := range items {
		if _, e = musicAudioHead(ctx, client, item.MediaURL); e == nil {
			return item, nil
		}
	}
	return Item{}, fmt.Errorf("抖音音乐未返回可读取的音频")
}

// 只读取有界头部中的 ID3v2.3/2.4 歌名、歌手；不解释图片、链接或其它可执行字段。
func musicID3(data []byte) (title, artist string) {
	if len(data) < 10 || string(data[:3]) != "ID3" || data[3] != 3 && data[3] != 4 || data[5]&0x80 != 0 {
		return
	}
	syncsize := func(b []byte) int {
		n := 0
		for _, v := range b {
			if v&0x80 != 0 {
				return -1
			}
			n = n<<7 | int(v)
		}
		return n
	}
	n := syncsize(data[6:10])
	if n < 0 {
		return
	}
	end := min(len(data), 10+n)
	pos := 10
	if data[5]&0x40 != 0 {
		if end < 14 {
			return
		}
		n = int(binary.BigEndian.Uint32(data[10:14])) + 4
		if data[3] == 4 {
			n = syncsize(data[10:14])
		}
		if n < 4 || n > end-10 {
			return
		}
		pos += n
	}
	for pos+10 <= end {
		id := string(data[pos : pos+4])
		n = int(binary.BigEndian.Uint32(data[pos+4 : pos+8]))
		if data[3] == 4 {
			n = syncsize(data[pos+4 : pos+8])
		}
		if n <= 0 || n > end-pos-10 {
			break
		}
		if (id == "TIT2" || id == "TPE1") && data[pos+8] == 0 && data[pos+9] == 0 && n <= 4096 {
			text := id3Text(data[pos+10 : pos+10+n])
			if id == "TIT2" {
				title = text
			} else {
				artist = text
			}
		}
		pos += 10 + n
	}
	return
}

func id3Text(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	kind, b := b[0], b[1:]
	var s string
	switch kind {
	case 0:
		var out []rune
		for _, v := range b {
			out = append(out, rune(v))
		}
		s = string(out)
	case 3:
		if !utf8.Valid(b) {
			return ""
		}
		s = string(b)
	case 1, 2:
		var order binary.ByteOrder = binary.BigEndian
		if kind == 1 {
			if bytes.HasPrefix(b, []byte{0xff, 0xfe}) {
				order = binary.LittleEndian
			} else if !bytes.HasPrefix(b, []byte{0xfe, 0xff}) {
				return ""
			}
			b = b[2:]
		}
		if len(b)%2 != 0 {
			return ""
		}
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = order.Uint16(b[i*2:])
		}
		s = string(utf16.Decode(u))
	default:
		return ""
	}
	return strings.TrimSpace(strings.Trim(s, "\x00"))
}
