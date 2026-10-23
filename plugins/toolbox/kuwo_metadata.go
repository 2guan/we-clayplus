package toolbox

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const kuwoSearchEndpoint = "https://search.kuwo.cn/r.s"

func kuwoText(s string) string { return strings.TrimSpace(html.UnescapeString(s)) }
func kuwoEqual(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(kuwoText(a)), ""), strings.Join(strings.Fields(kuwoText(b)), ""))
}

func kuwoCatalog(name string) (album, artist string) {
	line := strings.SplitN(name, "\n", 2)[0]
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "1."))
	if i := strings.LastIndex(line, "---"); i >= 0 {
		return kuwoText(line[:i]), kuwoText(line[i+3:])
	}
	return "", ""
}

// 使用音频标签或明确查询词，并同时核对同一音源的歌手、专辑；不拿首条搜索结果随意补图。
func readKuwoMetadata(ctx context.Context, client *http.Client, query, knownTitle, artist, album string) (Item, error) {
	if artist == "" || album == "" {
		return Item{}, fmt.Errorf("酷我结果缺少用于核对歌曲的歌手或专辑")
	}
	title := knownTitle
	if title == "" {
		title = strings.TrimSpace(query)
		if strings.HasPrefix(title, artist) {
			title = strings.TrimSpace(strings.TrimPrefix(title, artist))
		} else if strings.HasSuffix(title, artist) {
			title = strings.TrimSpace(strings.TrimSuffix(title, artist))
		}
	}
	if title == "" {
		return Item{}, fmt.Errorf("请同时提供歌名，以确认酷我歌曲资料")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	// 使用已验证可返回标准 JSON 与完整歌曲资料的公开搜索参数。
	q := url.Values{"all": {title}, "ft": {"music"}, "client": {"kt"}, "pn": {"0"}, "rn": {"20"}, "rformat": {"json"}, "encoding": {"utf8"}, "mobi": {"1"}, "strategy": {"2012"}, "ver": {"kwplayer_ar_8.5.5.0"}, "vipver": {"8.5.5.0"}}
	raw, _, err := ReadMusicURL(ctx, client, kuwoSearchEndpoint+"?"+q.Encode(), "", 2<<20)
	if err != nil {
		return Item{}, fmt.Errorf("酷我歌曲资料暂时无法读取")
	}
	return matchKuwoMetadata(raw, title, artist, album, knownTitle == "")
}

func matchKuwoMetadata(raw []byte, title, artist, album string, queryOnly bool) (Item, error) {
	var body struct {
		List []struct {
			Title  string `json:"SONGNAME"`
			Name   string `json:"NAME"`
			Artist string `json:"ARTIST"`
			Album  string `json:"ALBUM"`
			Cover  string `json:"web_albumpic_short"`
		} `json:"abslist"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return Item{}, fmt.Errorf("酷我歌曲资料格式无效")
	}
	for _, v := range body.List {
		if v.Title == "" {
			v.Title = v.Name
		}
		titleMatches := kuwoEqual(v.Title, title)
		if queryOnly {
			wanted, actual := musicNormal(kuwoText(title)), musicNormal(kuwoText(v.Title))
			if !titleMatches && strings.HasPrefix(wanted, "的") {
				wanted = strings.TrimPrefix(wanted, "的")
			}
			titleMatches = wanted == actual || musicTitleNear(wanted, actual)
		}
		if !titleMatches || !kuwoEqual(v.Artist, artist) || !kuwoEqual(v.Album, album) {
			continue
		}
		item := Item{Title: kuwoText(v.Title), Artist: kuwoText(v.Artist)}
		if item.Title == "" || len(item.Title) > 1000 || len(item.Artist) > 500 {
			continue
		}
		if v.Cover != "" && len(v.Cover) <= 2048 {
			cover := v.Cover
			if !strings.HasPrefix(cover, "https://") && !strings.HasPrefix(cover, "http://") {
				cover = "https://img4.kuwo.cn/star/albumcover/" + strings.TrimLeft(cover, "/")
			}
			if PublicURL(cover) == nil {
				item.Cover = cover
			}
		}
		return item, nil
	}
	return Item{}, fmt.Errorf("酷我没有返回歌名、歌手和专辑均匹配的歌曲资料")
}
