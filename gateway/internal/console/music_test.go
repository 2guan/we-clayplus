package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/message"
)

func TestMusicBridgeRetainsOriginalReceiptAndRejectsFailures(t *testing.T) {
	sends, revokes := 0, 0
	created := time.Now().Unix() - 1
	createdText := strconv.FormatInt(created, 10)
	originalClientID := "fixture-music-peer_" + createdText
	core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/message/app":
			sends++
			var q struct {
				Receiver, XML string
				Type          int
			}
			if e := json.NewDecoder(r.Body).Decode(&q); e != nil {
				t.Error(e)
			}
			m, e := message.ParseMusicXML(q.XML)
			if e != nil || q.Type != 76 || q.Receiver != "fixture-music-peer" || m.Title != "歌名<&>" || m.Lyric != "歌词" || m.Cover != "https://example.com/cover.jpg" || !strings.Contains(q.XML, "<thumburl>https://example.com/cover.jpg</thumburl>") {
				t.Error("控制台未按音乐协议发送", e)
			}
			if sends == 1 {
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"base_response": map[string]int{"code": 0}, "new_id": "9007199254740997", "client_id": originalClientID, "create_time": created}})
			} else {
				fmt.Fprint(w, `{"code":0,"data":{"base_response":{"code":-1}}}`)
			}
		case "/api/message/revoke":
			revokes++
			var q struct {
				Receiver       string      `json:"receiver"`
				NewID          json.Number `json:"new_id"`
				ClientID       json.Number `json:"client_id"`
				StringClientID string      `json:"string_client_id"`
				Created        json.Number `json:"create_time"`
			}
			if e := json.NewDecoder(r.Body).Decode(&q); e != nil {
				t.Error(e)
			}
			if q.Receiver != "fixture-music-peer" || q.NewID.String() != "9007199254740997" || q.ClientID != "" && q.ClientID != "0" || q.StringClientID != originalClientID || q.Created.String() != createdText {
				t.Error("重启后音乐原始回执丢失")
			}
			fmt.Fprint(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
		default:
			t.Error("意外请求", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	client, e := api.NewGRPCClient(context.Background(), core.Address, "")
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	old := api.GetCore
	api.GetCore = func() *api.Client { return client }
	defer func() { api.GetCore = old }()
	contactability.LoadCache(t.TempDir())
	contactability.SetSelf(&contact.SelfInfo{Username: "fixture-music-self"})
	defer contactability.SetSelf(nil)
	ready := messageability.OutboundReady()
	messageability.SetOutboundReady(true)
	defer func() { messageability.SetOutboundReady(ready); messageability.ResetReceipts() }()
	b := &Bridge{}
	b.Ready.Store(true)
	params, _ := json.Marshal(map[string]any{"kind": "music", "music": message.Music{Title: "歌名<&>", Artist: "歌手", Cover: "https://example.com/cover.jpg", AudioURL: "https://example.com/a.mp3", Lyric: "歌词"}})
	command := control.Command{Operation: "send", Target: "fixture-music-peer", Content: "[音乐] 歌名<&> - 歌手", Params: params}
	result, e := b.Execute(context.Background(), command)
	if e != nil {
		t.Fatal(e)
	}
	r := result.(map[string]string)
	if r["id"] != "9007199254740997" || r["account"] != "fixture-music-self" || r["original_client_id"] != originalClientID || r["create_time"] != createdText {
		t.Fatal("控制台回执不完整", r)
	}
	stored, _ := json.Marshal(control.RevokeReceipt{Account: r["account"], OriginalClientID: r["original_client_id"], Created: r["create_time"]})
	messageability.ResetReceipts()
	if _, e = b.Execute(context.Background(), control.Command{Operation: "revoke", Target: command.Target, ID: r["id"], Params: stored}); e != nil {
		t.Fatal(e)
	}
	if _, e = b.Execute(context.Background(), command); e == nil {
		t.Fatal("失败被当作发送成功")
	}
	if sends != 2 || revokes != 1 {
		t.Fatal("发生额外发送", sends, revokes)
	}
}
