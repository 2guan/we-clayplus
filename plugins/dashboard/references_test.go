package dashboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/plugins/ai"
)

func TestSelfieReferencePriorityAndPrompt(t *testing.T) {
	s := newServer(t, false)
	data, _ := base64.StdEncoding.DecodeString(protocolImage)
	path, err := s.saveMedia(data, "image/png")
	replyRegressionCheck(t, err)
	var avatarCalls atomic.Int32
	avatar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { avatarCalls.Add(1); _, _ = w.Write(data) }))
	defer avatar.Close()
	st := s.store.snapshot()
	st.Account.Name, st.Account.ID, st.Account.Photo = "fixture-self-name", "fixture-self-id", avatar.URL
	st.Settings["image.reference"] = path
	base := ai.Request{Kind: "image", Selfie: true, Prompt: "fixture-self-name、fixture-self-id，窗边举手机，笑着看向镜头"}
	for _, source := range []string{"single", "default", "avatar"} {
		q := base
		if source == "single" {
			q.Media, q.MIME = protocolImage, "image/png"
		}
		if source == "avatar" {
			st.Settings["image.reference"] = ""
		}
		replyRegressionCheck(t, s.prepareReference(context.Background(), st, &q))
		if q.Media != protocolImage || q.MIME != "image/png" || strings.Contains(q.Prompt, st.Account.Name) || strings.Contains(q.Prompt, st.Account.ID) || !strings.Contains(q.Prompt, "窗边举手机") || !strings.Contains(q.Prompt, "服装按需要描述") || !strings.Contains(q.Prompt, "保持面容") {
			t.Fatal(source, q.Prompt, q.MIME)
		}
		want := int32(0)
		if source == "avatar" {
			want = 1
		}
		if avatarCalls.Load() != want {
			t.Fatal("垫图优先级错误", source, avatarCalls.Load())
		}
	}
	st.Account.Photo = ""
	q := base
	if err := s.prepareReference(context.Background(), st, &q); err == nil {
		t.Fatal("没有垫图时仍生成另一张脸")
	}
	q = ai.Request{Kind: "image", Prompt: "画一座小屋"}
	replyRegressionCheck(t, s.prepareReference(context.Background(), st, &q))
	if q.Prompt != "画一座小屋" || q.Media != "" {
		t.Fatal("普通生图被自拍规则改变")
	}
}

func TestSelfieModelReceivesVisualPromptWithoutWechatIdentity(t *testing.T) {
	var imageCalls atomic.Int32
	const nickname = "fixture-self-nickname"
	var sentPrompt string
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/images/edits" {
			imageCalls.Add(1)
			replyRegressionCheck(t, r.ParseMultipartForm(1<<20))
			defer r.MultipartForm.RemoveAll()
			file, _, err := r.FormFile("image")
			replyRegressionCheck(t, err)
			defer file.Close()
			data, _ := io.ReadAll(file)
			want, _ := base64.StdEncoding.DecodeString(protocolImage)
			sentPrompt = r.FormValue("prompt")
			if !bytes.Equal(data, want) || strings.Contains(sentPrompt, nickname) || strings.Contains(sentPrompt, "fixture-retry-self") || !strings.Contains(sentPrompt, "窗边举手机") {
				t.Error("垫图或实际生图描述错误", sentPrompt)
			}
			fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, protocolImage)
			return
		}
		var body struct{ Messages []struct{ Content string } }
		replyRegressionCheck(t, json.NewDecoder(r.Body).Decode(&body))
		if !strings.Contains(body.Messages[0].Content, "服装可选") || !strings.Contains(body.Messages[0].Content, "不写微信昵称") {
			t.Error("对话模型没有自拍描述规则")
		}
		captionResult(w, `<image ref="selfie" prompt="fixture-self-nickname、fixture-retry-self，窗边举手机，神情自然"/>`)
	})
	data, _ := base64.StdEncoding.DecodeString(protocolImage)
	path, err := s.saveMedia(data, "image/png")
	replyRegressionCheck(t, err)
	m.Content = "来张自拍"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Account.Name = nickname
		st.Messages = []Message{m}
		st.Settings["reply.image_enabled"], st.Settings["image.reference"] = true, path
		st.Models[0].Capabilities = append(st.Models[0].Capabilities, "image")
		st.Pipelines = map[string][]string{"image": {st.Models[0].ID}}
		return nil
	}))
	task := retrySubmit(t, s, m)
	if task.Status != "completed" || imageCalls.Load() != 1 || sends.Load() != 1 {
		t.Fatal(task, imageCalls.Load(), sends.Load())
	}
	for _, task := range s.store.snapshot().Tasks {
		if task.Kind == "image" && task.Input != sentPrompt {
			t.Fatal("任务详情不是实际生图描述", task.Input)
		}
	}
	for _, msg := range s.store.snapshot().Messages {
		if msg.ImageGenerated && (strings.Contains(msg.GenerationPrompt, nickname) || strings.Contains(msg.GenerationPrompt, "fixture-retry-self") || !strings.Contains(msg.GenerationPrompt, "窗边举手机") || strings.Contains(msg.GenerationPrompt, "画面描述：")) {
			t.Fatal("生成意图未保留简明场景或混入身份与规则", msg.GenerationPrompt)
		}
	}
}
