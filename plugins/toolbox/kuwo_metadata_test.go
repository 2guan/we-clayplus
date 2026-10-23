package toolbox

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestKuwoMetadataMatchesTitleArtistAndAlbum(t *testing.T) {
	const raw = `{"abslist":[{"SONGNAME":"目标歌曲","ARTIST":"另一歌手","ALBUM":"目标专辑","web_albumpic_short":"120/wrong-singer.jpg"},{"SONGNAME":"目标歌曲","ARTIST":"目标歌手","ALBUM":"演唱会专辑","web_albumpic_short":"120/wrong-album.jpg"},{"NAME":"目标歌曲","ARTIST":"目标歌手","ALBUM":"目标专辑","web_albumpic_short":"120/right.jpg"}]}`
	item, e := matchKuwoMetadata([]byte(raw), "目标歌曲", "目标歌手", "目标专辑", false)
	if e != nil || item.Title != "目标歌曲" || item.Artist != "目标歌手" || item.Cover != "https://img4.kuwo.cn/star/albumcover/120/right.jpg" {
		t.Fatal("没有按三项资料匹配", e)
	}
	for _, bad := range []string{`not json`, `{"abslist":[]}`, strings.ReplaceAll(raw, "目标歌曲", "无关歌曲")} {
		if _, e := matchKuwoMetadata([]byte(bad), "目标歌曲", "目标歌手", "目标专辑", false); e == nil {
			t.Fatal("资料不匹配仍返回首项")
		}
	}
	private := []byte(`{"abslist":[{"SONGNAME":"目标歌曲","ARTIST":"目标歌手","ALBUM":"目标专辑","web_albumpic_short":"http://127.0.0.1/private"}]}`)
	if item, e := matchKuwoMetadata(private, "目标歌曲", "目标歌手", "目标专辑", false); e != nil || item.Cover != "" {
		t.Fatal("封面绕过公网校验", e)
	}
	if album, artist := kuwoCatalog("1. 目标专辑---目标歌手\n2. 其它专辑---其它歌手"); album != "目标专辑" || artist != "目标歌手" {
		t.Fatal("未核对所选条目")
	}
}

func TestKuwoMetadataMissingDoesNotInventTitle(t *testing.T) {
	for _, tagged := range []bool{false, true} {
		client := &http.Client{Transport: musicTransport(func(r *http.Request) (*http.Response, error) {
			status, body := 200, []byte{}
			switch r.URL.Path {
			case "/API/kwyy/api.php":
				body = []byte(`{"code":"200","name":"1. 目标专辑---目标歌手","url":"https://example.com/audio"}`)
			case "/audio":
				body = untaggedMP3Fixture()
				if tagged {
					body = id3Fixture(3, append([]byte{3}, []byte("目标歌曲")...), append([]byte{3}, []byte("目标歌手")...))
				}
			case "/r.s":
				status = 503
			default:
				t.Error("意外请求", r.URL.Path)
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
		})}
		items, _, e := SearchMusicSources(context.Background(), client, "kuwo", "目标歌手 目标歌曲", 5)
		if tagged {
			if e != nil || len(items) != 1 || items[0].Title != "目标歌曲" {
				t.Fatal("资料接口失败覆盖了真实音频标签", e)
			}
		} else if e == nil || len(items) > 0 {
			t.Fatal("没有歌名仍使用查询词生成卡片")
		}
	}
}

func TestKuwoQueryToleranceDoesNotLoosenKnownAudioIdentity(t *testing.T) {
	raw := []byte(`{"abslist":[{"SONGNAME":"完美的一天","ARTIST":"孙燕姿","ALBUM":"示例专辑","web_albumpic_short":"120/example.jpg"}]}`)
	if item, err := matchKuwoMetadata(raw, "完美的第一天", "孙燕姿", "示例专辑", true); err != nil || item.Title != "完美的一天" {
		t.Fatal("无音频标签时未接受已核对歌手和专辑的近似查询", err)
	}
	if _, err := matchKuwoMetadata(raw, "完美的第一天", "孙燕姿", "示例专辑", false); err == nil {
		t.Fatal("已有音频歌名时却放宽资料身份，可能错配封面")
	}
}
