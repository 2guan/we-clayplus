package messageability

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	coreapi "github.com/we-clay/we-clay/gateway/internal/api"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
)

func TestMediaRevokeUsesOriginalReceiptAcrossGateway(t *testing.T) {
	const newID = "9007199254740993"
	for _, tc := range []struct {
		name, path, clientID string
		created, numeric     bool
		msg                  *sdk.Message
	}{
		{"image", "image", "synthetic_sender_1789890000", true, false, &sdk.Message{Type: sdk.TypeImage, Data: &sdk.Message_Image{Image: &sdk.ImageData{Media: &sdk.Media{Data: []byte("image")}}}}},
		{"voice", "voice", "synthetic_sender_1789890000", true, false, &sdk.Message{Type: sdk.TypeVoice, Data: &sdk.Message_Voice{Voice: &sdk.VoiceData{Media: &sdk.Media{Data: []byte("voice")}, Duration: 1000}}}},
		{"video", "video", "1789890000", false, true, &sdk.Message{Type: sdk.TypeVideo, Data: &sdk.Message_Video{Video: &sdk.VideoData{Media: &sdk.Media{Data: []byte("video")}, Thumb: []byte("thumb"), Duration: 1}}}},
		{"file", "file", "synthetic_peer_1789890000", true, false, &sdk.Message{Type: sdk.TypeFile, Data: &sdk.Message_File{File: &sdk.FileData{Filename: "test.txt", Media: &sdk.Media{Data: []byte("file")}}}}},
		{"link", "link", "synthetic_peer_1789890000", true, false, &sdk.Message{Type: sdk.TypeAppLink, Data: &sdk.Message_App{App: &sdk.AppData{Title: "test", Url: "https://example.invalid"}}}},
		{"emoji", "emoji", "1789890000", false, true, &sdk.Message{Type: sdk.TypeEmoji, Data: &sdk.Message_Emoji{Emoji: &sdk.EmojiData{Media: &sdk.Media{Data: []byte("emoji"), Md5: "synthetic"}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/message/revoke" {
					calls++
					var q struct {
						Receiver       string `json:"receiver"`
						NewID          uint64 `json:"new_id"`
						ClientID       uint64 `json:"client_id"`
						StringClientID string `json:"string_client_id"`
						CreateTime     uint64 `json:"create_time"`
					}
					if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
						t.Error(err)
					}
					if q.Receiver != "synthetic_peer" || q.NewID != 9007199254740993 || q.CreateTime != 1789890000 {
						t.Error("revoke lost receiver, ID or original time")
					}
					if tc.numeric && (q.ClientID != 1789890000 || q.StringClientID != "") || !tc.numeric && (q.ClientID != 0 || q.StringClientID != tc.clientID) {
						t.Error("revoke changed the original client ID")
					}
					io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
					return
				}
				if r.URL.Path != "/api/message/"+tc.path {
					t.Errorf("unexpected route %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				data := map[string]any{"base_response": map[string]any{"code": 0}, "new_id": newID, "client_id": tc.clientID}
				if tc.created {
					data["create_time"] = 1789890000
				}
				if tc.name == "image" {
					data["client_id"] = tc.clientID
				}
				if tc.name == "emoji" {
					data = map[string]any{"base_response": map[string]any{"code": 0}, "count": 1, "result": []any{map[string]any{"code": 0, "new_id": newID}}, "revoke_client_id": tc.clientID, "revoke_create_time": "1789890000"}
				}
				b, _ := json.Marshal(map[string]any{"code": 0, "data": data})
				w.Write(b)
			}))
			conn, err := coreapi.NewGRPCClient(context.Background(), core.Address, "")
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			a := &ability{api: messageapi.New(conn)}
			copy := *tc.msg
			copy.Receiver = &contact.Contact{Username: "synthetic_peer"}
			sent, err := a.Send(&copy)
			if err != nil {
				t.Fatal(err)
			}
			if sent.GetNewId() != 9007199254740993 || sent.GetCreateTime() != 1789890000 {
				t.Fatal("send lost receipt fields")
			}
			if _, err = a.Revoke("synthetic_peer", sent.GetNewId()); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("revoke dispatched %d times", calls)
			}
		})
	}
}
