// 点歌解析和 QQ 音乐接口兼容 Golem Music（作者 ovo）；保留其公开服务地址。
package toolbox

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

const DefaultMusicEndpoint = "https://109a.cn/API/qqyy/api.php"

//go:embed music_providers.json
var musicProvidersJSON []byte

func MusicAppID(value string) (string, error) {
	if value == "random" {
		var entries map[string]string
		if e := json.Unmarshal(musicProvidersJSON, &entries); e != nil {
			return "", e
		}
		n, e := rand.Int(rand.Reader, big.NewInt(int64(len(entries))))
		if e != nil {
			return "", e
		}
		for _, id := range entries {
			if n.Sign() == 0 {
				return id, nil
			}
			n.Sub(n, big.NewInt(1))
		}
	}
	if value != "" {
		if len(value) != 18 || !strings.HasPrefix(value, "wx") {
			return "", fmt.Errorf("AppID 应为 random、空值或 wx 开头的 16 位十六进制编号")
		}
		if _, e := hex.DecodeString(value[2:]); e != nil {
			return "", fmt.Errorf("AppID 格式无效")
		}
	}
	return value, nil
}

var musicPrefixes = []string{
	"发给我一首歌", "给我发一首歌", "给我放一首歌", "给我点一首歌", "给我来一首歌",
	"发给我一首", "给我发一首", "给我放一首", "给我点一首", "给我来一首",
	"发给我首歌", "给我发首歌", "发给我首", "给我发首", "发给我个歌", "给我发个歌",
	"发我一首歌", "来一首歌", "放一首歌", "点一首歌", "听一首歌", "搜一首歌",
	"发我一首", "发我首", "给我一首", "给我来首", "给我放首", "给我点首", "发一首", "来一首",
	"点一首", "放一首", "听一首", "搜一首", "发首歌", "来首歌", "放首歌", "点首歌", "听首歌", "搜首歌",
	"发个歌", "来个歌", "放个歌", "点个歌", "听个歌", "搜个歌", "发首", "来首", "点首", "放首", "听首", "搜首",
	"我要听一首", "我想听一首", "我要听首歌", "我想听首歌", "我要听歌", "我想听歌", "我要听", "我想听",
	"点歌", "放歌", "搜歌", "播放", "点播", "音乐", "music",
}

func init() {
	sort.SliceStable(musicPrefixes, func(i, j int) bool { return len(musicPrefixes[i]) > len(musicPrefixes[j]) })
}

func MusicQuery(text string) string {
	text = CommandText(text)
	for _, p := range musicPrefixes {
		if !strings.HasPrefix(text, p) {
			continue
		}
		q := strings.TrimLeftFunc(strings.TrimPrefix(text, p), func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("，,：:、；;。!！?？", r) })
		if p == "music" && len(text) > len(p) && q == text[len(p):] {
			return ""
		}
		// 自然听歌句式仍排除明显非音乐请求；明确“点歌/搜歌”等允许同名歌曲。
		if strings.Contains(p, "想听") || strings.Contains(p, "要听") {
			for _, s := range []string{"你的声音", "你说话", "你讲", "你说", "你叫", "声音", "语音", "故事", "八卦", "实话", "真话", "解释", "建议", "意见", "秘密", "看看", "瞧瞧", "照片", "这个", "那个", "什么", "啥"} {
				if strings.HasPrefix(q, s) {
					return ""
				}
			}
		}
		if strings.Contains(p, "想听") || strings.Contains(p, "要听") {
			for _, s := range []string{"这首歌", "这歌", "的歌", "听听", "听下", "一下"} {
				q = strings.TrimSpace(strings.TrimSuffix(q, s))
			}
		}
		return strings.TrimSpace(q)
	}
	return ""
}

func SearchMusic(ctx context.Context, client *http.Client, endpoint, key, query, format string, limit int) ([]Item, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("请填写歌名或歌手")
	}
	if format == "items" {
		return Search(ctx, client, endpoint, key, query, limit)
	}
	if endpoint == "" {
		endpoint = DefaultMusicEndpoint
	}
	u, e := url.Parse(endpoint)
	if e != nil {
		return nil, fmt.Errorf("音乐服务地址无效")
	}
	q := u.Query()
	q.Set("msg", query)
	u.RawQuery = q.Encode()
	b, _, e := ReadMusicURL(ctx, client, u.String(), key, 2<<20)
	if e != nil {
		return nil, e
	}
	return ParseMusicResponse(b, limit)
}

func ParseMusicResponse(raw []byte, limit int) ([]Item, error) {
	var body struct {
		Code int `json:"code"`
		Data []struct {
			Song   string `json:"song"`
			Singer string `json:"singer"`
			URL    string `json:"url"`
			Cover  string `json:"cover"`
			Lyric  string `json:"lyric"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil, fmt.Errorf("音乐服务返回格式无效")
	}
	if body.Code != 200 || len(body.Data) == 0 {
		return nil, fmt.Errorf("未找到歌曲或音乐接口返回错误（code=%d）", body.Code)
	}
	if limit < 1 || limit > 20 {
		limit = 5
	}
	if len(body.Data) > limit {
		body.Data = body.Data[:limit]
	}
	out := make([]Item, 0, len(body.Data))
	for _, v := range body.Data {
		if strings.TrimSpace(v.Song) == "" || len(v.Song) > 1000 || len(v.Singer) > 500 || len(v.Lyric) > 64<<10 || len(v.URL) > 4096 || v.URL == "" || PublicURL(v.URL) != nil || v.Cover != "" && (len(v.Cover) > 4096 || PublicURL(v.Cover) != nil) {
			return nil, fmt.Errorf("音乐服务返回了无效歌曲或链接")
		}
		out = append(out, Item{Title: strings.TrimSpace(v.Song), Artist: strings.TrimSpace(v.Singer), URL: v.URL, MediaURL: v.URL, Cover: v.Cover, Lyric: v.Lyric})
	}
	return out, nil
}
