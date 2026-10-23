package toolbox

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func id3Fixture(version byte, title, artist []byte) []byte {
	syncsize := func(n int) []byte {
		return []byte{byte(n >> 21 & 127), byte(n >> 14 & 127), byte(n >> 7 & 127), byte(n & 127)}
	}
	var frames []byte
	for _, f := range []struct {
		id   string
		text []byte
	}{{"TIT2", title}, {"TPE1", artist}} {
		size := make([]byte, 4)
		binary.BigEndian.PutUint32(size, uint32(len(f.text)))
		if version == 4 {
			size = syncsize(len(f.text))
		}
		frames = append(frames, []byte(f.id)...)
		frames = append(frames, size...)
		frames = append(frames, 0, 0)
		frames = append(frames, f.text...)
	}
	return append(append([]byte{'I', 'D', '3', version, 0, 0}, syncsize(len(frames))...), frames...)
}

func TestMusicID3AndAudioFormats(t *testing.T) {
	for _, version := range []byte{3, 4} {
		head := id3Fixture(version, append([]byte{3}, []byte("测试歌曲")...), append([]byte{1, 0xff, 0xfe}, []byte{'A', 0, 'B', 0}...))
		title, artist := musicID3(head)
		if title != "测试歌曲" || artist != "AB" {
			t.Fatalf("ID3v2.%d 未正确读取文字", version)
		}
		if MusicAudioMIME(head) != "audio/mpeg" {
			t.Fatal("未识别 MP3")
		}
		if title, _ := musicID3(head[:12]); title != "" {
			t.Fatal("截断帧仍被读取")
		}
		head[6] = 0xff
		if title, _ := musicID3(head); title != "" {
			t.Fatal("无效大小仍被读取")
		}
	}
	for _, head := range [][]byte{[]byte("<html>failure</html>"), {}, []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00")} {
		if MusicAudioMIME(head) != "" {
			t.Fatal("将非音频当作音频")
		}
	}
	m4a := []byte("\x00\x00\x00\x1cftypM4A \x00\x00\x02\x00M4A isom")
	if MusicAudioMIME(m4a) != "audio/mp4" {
		t.Fatal("未识别实际 M4A 头")
	}
	if id3Text([]byte{3, 0xff}) != "" || id3Text([]byte{1, 0xff}) != "" {
		t.Fatal("接受损坏文本")
	}
}

func TestDouyinMatchesSongBeforeArtist(t *testing.T) {
	for _, code := range []string{`"0"`, `0`} {
		raw := []byte(`{"code":` + code + `,"data":[{"title":"别的歌---测试歌曲","url":"https://example.com/wrong"},{"title":"测试歌曲---真实歌手","url":"https://example.com/right","imgurl":"https://example.com/cover"},{"title":"测试歌曲---内网","url":"http://127.0.0.1/private"}]}`)
		items, e := parseDouyinMusic(raw, "测试歌曲", 5)
		if e != nil || len(items) != 2 || items[0].Title != "测试歌曲" || items[0].Artist != "真实歌手" || items[0].Cover == "" || items[0].Lyric != "" {
			t.Fatal("歌名匹配或字段丢失", e)
		}
	}
	for _, raw := range []string{`{}`, `{"code":"200","data":[]}`, `{"code":"0","data":[{"title":"歌名","url":"file:///private"}]}`} {
		if _, e := parseDouyinMusic([]byte(raw), "歌名", 5); e == nil {
			t.Fatal("接受失败或非法链接")
		}
	}
}

func TestMusicSourcesFailoverAndMetadata(t *testing.T) {
	for _, mode := range []string{"kuwo-ok", "kuwo-http-error", "kuwo-invalid-audio", "kuwo-no-tags", "kuwo-untagged-mp3", "all-fail", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls []string
			client := &http.Client{Transport: musicTransport(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.URL.Path)
				if r.Header.Get("Authorization") != "" || len(r.Cookies()) != 0 {
					t.Error("内置音源或媒体携带凭据")
				}
				status, body := 200, []byte{}
				switch r.URL.Path {
				case "/API/kwyy/api.php":
					if r.URL.Query().Get("msg") != "歌 & A+B" || r.URL.Query().Get("a") != "1" || r.URL.Query().Get("n") != "1" {
						t.Error("酷我参数错误")
					}
					if mode == "cancel" {
						cancel()
						return nil, ctx.Err()
					}
					if mode == "kuwo-http-error" || mode == "all-fail" {
						status = 503
					}
					body = []byte(`{"code":"200","name":"1. 专辑名---真实歌手","url":"https://example.com/kuwo.mp3"}`)
				case "/kuwo.mp3":
					if r.Header.Get("Range") != "bytes=0-65535" {
						t.Error("未限制音频探测范围")
					}
					body = id3Fixture(3, append([]byte{3}, []byte("歌 & A+B")...), append([]byte{3}, []byte("真实歌手")...))
					if mode == "kuwo-invalid-audio" {
						body = []byte("<html>expired</html>")
					}
					if mode == "kuwo-no-tags" {
						body = []byte("RIFF....WAVEfmt ")
					}
					if mode == "kuwo-untagged-mp3" {
						body = untaggedMP3Fixture()
					}
				case "/r.s":
					if r.URL.Query().Get("all") != "歌 & A+B" || r.URL.Query().Get("mobi") != "1" || r.URL.Query().Get("vipver") != "8.5.5.0" {
						t.Error("未使用完整资料接口参数")
					}
					body = []byte(`{"abslist":[{"SONGNAME":"歌 &amp; A+B","ARTIST":"真实歌手","ALBUM":"专辑名","web_albumpic_short":"120/fixture.jpg"}]}`)
				case "/API/dyyy/api.php":
					if mode == "all-fail" {
						status = 503
					}
					body = []byte(`{"code":"0","data":[{"title":"歌 & A+B---抖音歌手","url":"https://example.com/douyin.m4a","imgurl":"https://example.com/douyin.jpg"}]}`)
				case "/douyin.m4a":
					body = []byte("\x00\x00\x00\x1cftypM4A \x00\x00\x02\x00M4A isom")
				default:
					t.Error("意外请求", r.URL.Path)
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
			})}
			items, source, e := SearchMusicSources(ctx, client, "auto", "歌 & A+B", 5)
			if mode == "all-fail" || mode == "cancel" {
				if e == nil || len(items) != 0 {
					t.Fatal("失败或取消仍返回结果")
				}
				if mode == "cancel" && len(calls) != 1 {
					t.Fatal("取消后仍请求备用")
				}
				if mode == "all-fail" && (!strings.Contains(e.Error(), "酷我") || !strings.Contains(e.Error(), "抖音")) {
					t.Fatal("未保留两源失败原因")
				}
				return
			}
			if e != nil || len(items) != 1 {
				t.Fatal("没有可用结果", e)
			}
			if strings.Contains(mode, "error") || mode == "kuwo-invalid-audio" {
				if source != "抖音音乐" || items[0].Artist != "抖音歌手" || items[0].Cover != "https://example.com/douyin.jpg" {
					t.Fatal("备用或元数据混用")
				}
			} else {
				if source != "酷我音乐" || items[0].Cover != "https://img4.kuwo.cn/star/albumcover/120/fixture.jpg" || len(calls) != 3 {
					t.Fatal("主源正常仍切换或混用封面")
				}
				if mode == "kuwo-ok" && (items[0].Title != "歌 & A+B" || items[0].Artist != "真实歌手") {
					t.Fatal("把专辑当作歌名")
				}
				if mode == "kuwo-no-tags" && (items[0].Title != "歌 & A+B" || items[0].Artist != "真实歌手") {
					t.Fatal("无标签时编造歌名")
				}
			}
		})
	}
}

func TestMusicSourcesSingleSelection(t *testing.T) {
	var calls int
	client := &http.Client{Transport: musicTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/API/dyyy/api.php" {
			t.Fatal("选择抖音仍调用其它源")
		}
		return nil, fmt.Errorf("fixture failure")
	})}
	if _, _, e := SearchMusicSources(context.Background(), client, "douyin", "歌名", 5); e == nil || calls != 1 {
		t.Fatal("单选未生效")
	}
	calls = 0
	if _, _, e := SearchMusicSources(context.Background(), client, "invalid", "歌名", 5); e == nil || calls != 0 {
		t.Fatal("无效配置仍请求外网")
	}
}

func untaggedMP3Fixture() []byte {
	// MPEG-1 Layer III，320 kbps / 44.1 kHz，两帧均无 ID3 标签。
	mp3 := make([]byte, 1044*2)
	copy(mp3, []byte{0xff, 0xfb, 0xe0, 0})
	copy(mp3[1044:], []byte{0xff, 0xfb, 0xe0, 0})
	return mp3
}

func TestUntaggedMP3AndSingerMatching(t *testing.T) {
	mp3 := untaggedMP3Fixture()
	if MusicAudioMIME(mp3) != "audio/mpeg" {
		t.Fatal("将无标签的真实 MP3 误判为无效")
	}
	if MusicAudioMIME(mp3[:1044]) != "" {
		t.Fatal("仅凭单个帧头放行")
	}
	mp3[1044] = 0
	if MusicAudioMIME(mp3) != "" {
		t.Fatal("没有连续帧仍放行")
	}
	for _, query := range []string{"目标歌手 目标歌曲", "目标歌手目标歌曲", "目标歌曲 目标歌手", "目标歌曲", "目标歌手"} {
		if !musicMatchesQuery(Item{Title: "目标歌曲", Artist: "目标歌手"}, query) {
			t.Error("拒绝匹配", query)
		}
	}
	if musicMatchesQuery(Item{Title: "目标歌曲（Cover 目标歌手）", Artist: "另一歌手"}, "目标歌手 目标歌曲") {
		t.Fatal("把翻唱注释里的原唱当成演唱者")
	}
	items, e := parseDouyinMusic([]byte(`{"code":"0","data":[{"title":"目标歌曲（Cover 目标歌手）---另一歌手","url":"https://example.com/wrong"},{"title":"目标歌曲---目标歌手","url":"https://example.com/right"}]}`), "目标歌手 目标歌曲", 5)
	if e != nil || len(items) != 1 || items[0].Artist != "目标歌手" {
		t.Fatal("指定歌手仍选择其他歌手", e)
	}
}

func TestMusicQueryToleranceKeepsActualArtist(t *testing.T) {
	item := Item{Title: "完美的一天", Artist: "孙燕姿"}
	for _, query := range []string{"孙燕姿 完美的第一天", "孙燕姿，完美的一天", "孙燕姿的《完美的一天》", "《完美的一天》"} {
		if !musicMatchesQuery(item, query) {
			t.Error("拒绝格式或一字差异", query)
		}
	}
	for _, query := range []string{"其他歌手 完美的一天", "孙燕姿 完全不同的歌曲"} {
		if musicMatchesQuery(item, query) {
			t.Error("放宽查询后串歌手或无关歌曲", query)
		}
	}
	if musicMatchesQuery(Item{Title: "完美的一天（Cover 孙燕姿）", Artist: "其他歌手"}, "孙燕姿 完美的第一天") {
		t.Fatal("把翻唱错当指定歌手")
	}
}
