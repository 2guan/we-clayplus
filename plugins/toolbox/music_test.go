package toolbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type musicTransport func(*http.Request) (*http.Response, error)

func (f musicTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMusicQueries(t *testing.T) {
	for input, want := range map[string]string{"点歌 晴天": "晴天", "点歌周杰伦": "周杰伦", "@测试昵称\u2005我想听 海阔天空这首歌": "海阔天空", "给我放一首歌 稻香": "稻香", "来首 陈奕迅": "陈奕迅", "music bohemian rhapsody": "bohemian rhapsody", "音乐 夜歌": "夜歌", "我想听你的声音": "", "我想听故事": "", "musicology": "", "昨天听了一首歌": "", "点歌": ""} {
		if got := MusicQuery(input); got != want {
			t.Errorf("%q = %q, want %q", input, got, want)
		}
		id, arg := Match(input)
		if want != "" && (id != "music" || arg != want) {
			t.Errorf("路由丢失点歌 %q", input)
		}
		if want == "" && id == "music" {
			t.Errorf("误触发 %q", input)
		}
	}
}

func TestMusicServiceContractAndErrors(t *testing.T) {
	const good = `{"code":200,"data":[{"song":"歌<&>","singer":"歌手","url":"https://example.com/a.mp3?a=1&b=2","cover":"https://example.com/a.jpg","lyric":"第一行\n第二行"}]}`
	for _, format := range []string{"golem", "items"} {
		client := &http.Client{Transport: musicTransport(func(r *http.Request) (*http.Response, error) {
			key := "msg"
			body := good
			if format == "items" {
				key = "q"
				body = `{"items":[{"title":"旧服务","url":"https://example.com/song","media_url":"https://example.com/a.mp3","cover":"https://example.com/a.jpg","lyric":"歌词"}]}`
			}
			if format == "golem" && r.Header.Get("User-Agent") != "Mozilla/5.0" {
				t.Error("未使用音乐源需要的浏览器请求头")
			}
			if r.URL.Query().Get(key) != "歌 & A+B" || r.URL.Query().Get("fixed") != "keep" || r.Header.Get("Authorization") != "Bearer fixture-key" {
				t.Fatal("查询参数或原配置丢失")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}
		got, err := SearchMusic(context.Background(), client, DefaultMusicEndpoint+"?fixed=keep", "fixture-key", "歌 & A+B", format, 5)
		if err != nil || len(got) != 1 || got[0].MediaURL == "" || got[0].Cover == "" || got[0].Lyric == "" {
			t.Fatal("服务字段丢失", got, err)
		}
	}
	for _, raw := range []string{`not json`, `{"code":500,"data":[]}`, `{"code":200,"data":[]}`, strings.Replace(good, "https://example.com/a.mp3?a=1&b=2", "http://127.0.0.1/private", 1)} {
		if _, err := ParseMusicResponse([]byte(raw), 5); err == nil {
			t.Fatal("错误响应被当作音乐")
		}
	}
	for _, status := range []int{404, 503} {
		client := &http.Client{Transport: musicTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("failure"))}, nil
		})}
		if _, err := SearchMusic(context.Background(), client, "", "", "song", "golem", 5); err == nil {
			t.Fatal("HTTP 失败被吞掉")
		}
	}
}

func TestMusicProviderMigration(t *testing.T) {
	var entries map[string]string
	if err := json.Unmarshal(musicProvidersJSON, &entries); err != nil || len(entries) != 218 {
		t.Fatal("Golem 来源表迁移不完整", len(entries), err)
	}
	ids := map[string]bool{}
	for name, id := range entries {
		if _, err := MusicAppID(id); err != nil {
			t.Fatalf("来源 %q: %v", name, err)
		}
		ids[id] = true
	}
	for i := 0; i < 20; i++ {
		id, err := MusicAppID("random")
		if err != nil || !ids[id] {
			t.Fatal("随机来源不属于原表", err)
		}
	}
}

func TestRelaxedMusicQueries(t *testing.T) {
	for input, want := range map[string]string{
		"点歌，完美的一天": "完美的一天", "点歌 孙燕姿 完美的第一天": "孙燕姿 完美的第一天",
		"点歌 随便点一首舒缓的": "随便点一首舒缓的", "点歌： 故事": "故事", "点歌 秘密": "秘密",
		"@昵称 有空格\u2005@另一个人\u2005点歌，完美的一天": "完美的一天", "music: Song": "Song",
		"我想听你的声音": "", "我想听 故事": "", "musicology": "",
	} {
		if got := MusicQuery(input); got != want {
			t.Errorf("%q: got %q want %q", input, got, want)
		}
	}
	if id, arg := Match("@昵称 有空格\u2005消息统计 7"); id != "statistics" || arg != "7" {
		t.Fatal("其它插件口令没有去除提醒前缀", id, arg)
	}
}
