// 文件名：stickers_test.go
// 主要功能：脱机验证表情归档、账号隔离、原文件保留、授权与 AI 发送协议。
// 作者：2guan
package dashboard

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const stickerFixtureID = "0123456789abcdef0123456789abcdef"

func TestStickerCollectionAndIsolation(t *testing.T) {
	s := newServer(t, true)
	raw := `<msg><emoji md5="` + stickerFixtureID + `" productid="synthetic-pack" cdnurl="https://wxapp.tc.qq.com/synthetic" attachedtext="你好"/></msg>`
	m := Message{ID: "synthetic", Target: "synthetic", Kind: "emoji"}
	if e := s.RememberQuoteSource(m, raw); e != nil {
		t.Fatal(e)
	}
	if e := s.collectSticker(raw); e != nil {
		t.Fatal(e)
	}
	items, e := s.stickerList()
	if e != nil || len(items) != 1 || items[0].Kind != "pack" || items[0].AIAllowed || items[0].Name != "你好" {
		t.Fatal(items, e)
	}
	if b, _ := json.Marshal(items); strings.Contains(string(b), "wxapp") {
		t.Fatal("资源凭据泄露")
	}
	if _, e = s.sticker(stickerFixtureID, true); e == nil {
		t.Fatal("收集即获得了 AI 权限")
	}
	if e = s.initStickers(); e != nil {
		t.Fatal(e)
	}
	original := s.store.snapshot().Account.ID
	s.store.change(func(st *State) error { st.Account.ID = "other-account"; return nil })
	items, e = s.stickerList()
	if e != nil || len(items) != 0 {
		t.Fatal("跨账号读取表情")
	}
	if _, e = s.sticker(stickerFixtureID, false); e == nil {
		t.Fatal("跨账号发送表情")
	}
	s.store.change(func(st *State) error { st.Account.ID = original; return nil })
	if _, e = s.sticker(stickerFixtureID, false); e != nil {
		t.Fatal(e)
	}
}
func TestStickerUploadKeepsBytesAndPermissions(t *testing.T) {
	s := newServer(t, true)
	raw := "R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"
	data, _ := base64.StdEncoding.DecodeString(raw)
	w := call(t, s, "/api/stickers", map[string]any{"action": "upload", "media": raw}, nil, "http://127.0.0.1")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	id := fmt.Sprintf("%x", md5.Sum(data))
	v, e := s.sticker(id, false)
	if e != nil {
		t.Fatal(e)
	}
	b, mime, e := s.replyMediaBytes(v.Local)
	if e != nil || !bytes.Equal(data, b) || mime != "image/gif" {
		t.Fatal("原始 GIF 被改写", e)
	}
	v.AIAllowed = true
	w = call(t, s, "/api/stickers", map[string]any{"action": "update", "item": v}, nil, "http://127.0.0.1")
	if w.Code != 400 {
		t.Fatal("无含义允许 AI 使用")
	}
	v.Meaning = "开心地回应"
	v.ProductID = "forged"
	w = call(t, s, "/api/stickers", map[string]any{"action": "update", "item": v}, nil, "http://127.0.0.1")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	actual, _ := s.sticker(id, false)
	if actual.ProductID != "" {
		t.Fatal("客户端篡改资源归属")
	}
	// 更新元数据后本地原文件路径不能丢失。
	if actual.Local != v.Local {
		t.Fatal("本地原文件路径丢失")
	}
	if _, e = s.sticker(id, true); e == nil {
		t.Fatal("绕过全局开关")
	}
}
func TestStickerReplyProtocolAndDelivery(t *testing.T) {
	tag := `<emoji id="` + stickerFixtureID + `"/>`
	for _, bad := range []string{tag + tag, `<emoji id="url"/>`, `<emoji id="` + stickerFixtureID + `" url="x"/>`, `<emoji id="` + stickerFixtureID + `">x</emoji>`, "```" + tag + "```"} {
		if _, e := parseReply(bad, 5); e == nil {
			t.Fatal("接受无效协议", bad)
		}
	}
	s, target := replyServer(t)
	if e := s.collectSticker(`<msg><emoji md5="` + stickerFixtureID + `"/></msg>`); e != nil {
		t.Fatal(e)
	}
	before := len(s.store.snapshot().Messages)
	if e := s.deliverReply(context.Background(), target, "你好"+tag); e == nil {
		t.Fatal("未授权表情被发送")
	}
	if len(s.store.snapshot().Messages) != before {
		t.Fatal("验证计划前发送了部分文字")
	}
	v, _ := s.sticker(stickerFixtureID, false)
	v.Meaning = "快乐"
	v.AIAllowed = true
	w := call(t, s, "/api/stickers", map[string]any{"action": "update", "item": v}, nil, "http://127.0.0.1")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	s.store.change(func(st *State) error { st.Settings["reply.emoji_enabled"] = true; return nil })
	if e := s.deliverReply(context.Background(), target, tag); e != nil {
		t.Fatal(e)
	}
	messages := s.store.snapshot().Messages
	m := messages[len(messages)-1]
	if m.Kind != "emoji" || m.Rich == nil || m.Rich.StickerID != stickerFixtureID || m.Status != "sent" {
		t.Fatal("未按原生表情归档")
	}
	if e := s.deliverReply(context.Background(), target, tag); e == nil {
		t.Fatal("连续重复表情")
	}
	if strings.Contains(s.stickerInstructions(target, "快乐"), stickerFixtureID) {
		t.Fatal("重复表情仍进入候选")
	}
}
