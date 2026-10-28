package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/sdk/control"
)

func draftImage(t *testing.T, s *Server) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	path, err := s.saveMedia(b.Bytes(), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	return path
}
func TestMomentDraftReadsOwnValidMemoriesAndThumbnail(t *testing.T) {
	s, account, source := memoryFixture(t)
	confirmed := confirmMemory(t, s, account, mustMemory(t, s, account, memoryPreference(source)))
	pending := memoryPreference(source)
	pending.Key = "pending-name"
	pending.Content = "待确认的称呼秘密"
	mustMemory(t, s, account, pending)
	expired := memoryPreference(source)
	expired.Key = "expired-name"
	expired.Content = "过期的称呼秘密"
	savedExpired := confirmMemory(t, s, account, mustMemory(t, s, account, expired))
	if _, err := s.memoryAction(account, savedExpired.ID, "expire", savedExpired.Version, "fixture-admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.change(func(st *State) error {
		st.Settings["memory.enabled"] = true
		st.Overrides[source.Target] = map[string]any{"memory_enabled": true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m := Moment{ID: "fixture-moment", AuthorID: source.SenderID, Author: "朋友圈昵称", Content: "今天看到了落日", Images: []string{draftImage(t, s)}}
	for i := 0; i < 24; i++ {
		m.Comments = append(m.Comments, Comment{Author: fmt.Sprintf("评论者%d", i), Content: fmt.Sprintf("已有讨论%d", i)})
	}
	q, warnings, err := s.momentDraftRequest(context.Background(), s.store.snapshot(), m)
	if !strings.Contains(q.Prompt, "评论者23") || !strings.Contains(q.Prompt, "已有讨论23") {
		t.Fatal("遗漏末尾已有评论")
	}
	if err != nil || len(warnings) > 0 || len(q.Images) != 1 || q.Images[0].MIME != "image/jpeg" || !q.NoTools || q.AccountID != account || !strings.Contains(q.Prompt, confirmed.Content) || !strings.Contains(q.Prompt, m.Author) || !strings.Contains(q.Prompt, m.Content) || strings.Contains(q.Prompt, pending.Content) || strings.Contains(q.Prompt, expired.Content) {
		t.Fatal("草稿资料或记忆范围不正确", err)
	}
	m.AuthorID = "fixture-other"
	q, _, err = s.momentDraftRequest(context.Background(), s.store.snapshot(), m)
	if err != nil || strings.Contains(q.Prompt, confirmed.Content) {
		t.Fatal("他人动态带入原作者记忆", err)
	}
	m.Content = ""
	m.Images = []string{"/media/missing.png"}
	if _, _, err = s.momentDraftRequest(context.Background(), s.store.snapshot(), m); err == nil {
		t.Fatal("图片全部失败时仍猜测图片内容")
	}
}
func TestMomentDraftUsesVisionPipelineAndNeverSends(t *testing.T) {
	s := newServer(t, false)
	var reads, models atomic.Int32
	var withImage atomic.Bool
	withImage.Store(true)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string
			Messages []struct {
				Role    string
				Content json.RawMessage
			}
			Tools []any
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("模型请求不是JSON")
		}
		models.Add(1)
		if len(body.Tools) > 0 {
			t.Error("草稿请求提供了操作工具")
		}
		if body.Model == "first" {
			w.WriteHeader(500)
			fmt.Fprint(w, `{"error":{"message":"failed"}}`)
			return
		}
		if body.Model != "vision" {
			t.Error("未使用识图流水线", body.Model)
		}
		raw, _ := json.Marshal(body.Messages)
		if (bytes.Contains(raw, []byte("image_url")) != withImage.Load()) || !bytes.Contains(raw, []byte("朋友圈昵称")) || !bytes.Contains(raw, []byte("海边的落日")) || !bytes.Contains(raw, []byte("已有评论者")) {
			t.Error("模型未同时收到昵称、正文及缩略图")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"这片落日也太温柔了！"}}]}`)
	}))
	defer remote.Close()
	path := draftImage(t, s)
	s.host = replyRegressionHost(func(ctx context.Context, c control.Command) (any, error) {
		if c.Operation != "ai-action" || c.ID != "moments.Detail" || c.Content != "123" {
			t.Error("草稿执行了朋友圈读取之外的操作", c.Operation, c.ID)
			return nil, fmt.Errorf("禁止写操作")
		}
		reads.Add(1)
		var images []string
		if withImage.Load() {
			images = []string{path}
		}
		return map[string]any{"moment": Moment{ID: "123", AuthorID: "fixture-author", Author: "朋友圈昵称", Content: "海边的落日", Images: images, Comments: []Comment{{Author: "已有评论者", Content: "好漂亮"}}}}, nil
	})
	if err := s.store.putSecret("draft-vendor", "fixture-key"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.change(func(st *State) error {
		st.Account.ID = "fixture-account"
		st.Settings["routing.fallback"] = true
		st.Models = []Model{{ID: "first", Name: "first", Model: "first", Provider: "openai", Endpoint: remote.URL, VendorID: "draft-vendor", Enabled: true, Capabilities: []string{"chat", "vision"}}, {ID: "vision", Name: "vision", Model: "vision", Provider: "openai", Endpoint: remote.URL, VendorID: "draft-vendor", Enabled: true, Capabilities: []string{"chat", "vision"}}}
		st.Pipeline = []string{"first", "vision"}
		st.Settings["routing.vision"] = "vision"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.store.mu.Lock()
	s.store.accountEpoch = 7
	s.store.mu.Unlock()
	request := httptest.NewRequest("POST", "/api/moments/draft", strings.NewReader(`{"id":"123","author_id":"forged","images":["https://invalid.test/forged"]}`))
	w := httptest.NewRecorder()
	request.Header.Set("Content-Type", "application/json")
	s.momentDraftAPI(w, request, User{Role: "admin"})
	if w.Code != 400 || reads.Load() != 0 || models.Load() != 0 {
		t.Fatal("允许浏览器伪造作者或图片")
	}
	request = httptest.NewRequest("POST", "/api/moments/draft", strings.NewReader(`{"id":"123"}`))
	request.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.momentDraftAPI(w, request, User{Role: "admin"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "这片落日也太温柔了") || models.Load() != 1 || reads.Load() != 1 {
		t.Fatal("未仅起草或流水线未切换", w.Code, w.Body.String(), models.Load(), reads.Load())
	}
	withImage.Store(false)
	request = httptest.NewRequest("POST", "/api/moments/draft", strings.NewReader(`{"id":"123"}`))
	request.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.momentDraftAPI(w, request, User{Role: "admin"})
	if w.Code != 200 || models.Load() != 3 || reads.Load() != 2 {
		t.Fatal("文字草稿没有沿用对话流水线", w.Code, w.Body.String(), models.Load())
	}
	request = httptest.NewRequest("POST", "/api/moments/draft", strings.NewReader(`{"id":"123"}`))
	w = httptest.NewRecorder()
	s.momentDraftAPI(w, request, User{Role: "viewer"})
	if w.Code != 403 || reads.Load() != 2 {
		t.Fatal("只读用户获得记忆草稿权限")
	}
	for _, task := range s.store.snapshot().Tasks {
		if task.Source == "moment-comment-draft" && strings.Contains(task.Input, "朋友圈昵称") {
			t.Fatal("任务列表保存了起草资料原文")
		}
	}
	admin := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-draft-admin", "password": "fixture-password-123"}, nil, "")
	if admin.Code != 200 {
		t.Fatal(admin.Body.String())
	}
	cookie := admin.Result().Cookies()[0]
	if r := call(t, s, "/api/action", actionRequest{Kind: "user", Name: "fixture-draft-operator", Password: "fixture-password-123", Role: "operator", Enabled: true}, cookie, ""); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	login := call(t, s, "/api/auth/login", map[string]string{"name": "fixture-draft-operator", "password": "fixture-password-123"}, nil, "")
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	visible := call(t, s, "/api/state", nil, login.Result().Cookies()[0], "")
	if strings.Contains(visible.Body.String(), "moment-comment-draft") || strings.Contains(visible.Body.String(), "这片落日也太温柔了") {
		t.Fatal("操作员通过任务列表读取了管理员草稿")
	}
}
