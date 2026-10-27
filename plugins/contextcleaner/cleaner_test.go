// 文件名：cleaner_test.go
// 主要功能：验证会话隔离、去重、预算裁剪和输出整理不会损坏代码或原始记录。
// 作者：2guan
package contextcleaner

import (
	"strings"
	"testing"
)

func TestPreparePreservesSourceAndLatest(t *testing.T) {
	c := Config{Enabled: true, Controls: true, Dedupe: true, Media: true, MaxMessages: 10, Budget: 10000}
	input := []Message{{ID: "a", Session: "other", Role: "system", Text: "其他会话"}, {ID: "b", Session: "one", Role: "user", Text: "重复"}, {ID: "b", Session: "one", Role: "user", Text: "重复"}, {ID: "c", Session: "one", Role: "user", Text: "重复"}, {ID: "d", Session: "one", Role: "user", Kind: "image", Text: "base64-fixture"}, {ID: "e", Session: "one", Role: "system", Text: "最新\x00消息"}}
	out, err := Prepare(c, input, "one")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 4 || out.Messages[3].Role != "user" || out.Messages[3].Text != "最新消息" || strings.Contains(Encode(out.Messages), "base64-fixture") {
		t.Fatalf("上下文处理有误 %+v", out)
	}
	if input[5].Text != "最新\x00消息" || input[5].Role != "system" {
		t.Fatal("修改了归档输入")
	}
	c.MaxMessages = 1
	out, err = Prepare(c, input, "one")
	if err != nil || len(out.Messages) != 1 || out.Messages[0].ID != "e" {
		t.Fatal("未保留最新消息")
	}
	c.Budget = 1
	if _, err = Prepare(c, input, "one"); err == nil {
		t.Fatal("最新消息超限时必须明确报错")
	}
}
func TestOutputPreservesRefusalAndCode(t *testing.T) {
	c := Config{Enabled: true, Thinking: true, Silence: true, Stage: true}
	text, silent := Output(c, "<think>不展示的内容</think>正常回答", true)
	if text != "正常回答" || silent {
		t.Fatal(text)
	}
	for _, value := range []string{"抱歉，我无法提供这些私密信息。", "```xml\n<think>标签示例</think>\n```", `{"template":"<think>示例</think>"}`} {
		text, _ = Output(c, value, true)
		if text != value {
			t.Fatalf("误改写正文 %q", text)
		}
	}
	if _, silent = Output(c, "NO_REPLY", true); !silent {
		t.Fatal("未识别沉默标记")
	}
	if text, silent = Output(c, "NO_REPLY", false); silent || text != "NO_REPLY" {
		t.Fatal("误处理报告正文")
	}
	text, _ = Output(c, "<think>截断的推理", true)
	if text != "" {
		t.Fatal("泄露了未闭合推理")
	}
}

func TestOutputSilenceReasonAndBoundaries(t *testing.T) {
	c := Config{Enabled: true, Thinking: true, Silence: true}
	for _, text := range []string{"NO_REPLY: 非本人对话", "No_reply：非本人对话", "[NO_REPLY] 原因：非本人对话", "NO_REPLY\n沉默原因：非本人对话", "<think>hidden reasoning</think>NO_REPLY: 非本人对话"} {
		body, reason, silent := OutputWithReason(c, text, true)
		if body != "" || reason != "模型判断：非本人对话" || !silent {
			t.Fatal(body, reason, silent)
		}
	}
	if body, reason, silent := OutputWithReason(c, "[NO_REPLY] 沉默原因：重复告别、话题已结束", true); body != "" || reason != "模型判断：话题已结束" || !silent {
		t.Fatal("带说明文字的沉默协议外泄", body, reason, silent)
	}
	for _, text := range []string{"NO_REPLY", "[silent]", "NO_REPLY: arbitrary private details\nnot for logs"} {
		body, reason, silent := OutputWithReason(c, text, true)
		if body != "" || reason != "模型未提供具体原因" || !silent {
			t.Fatal(body, reason, silent)
		}
	}
	for _, text := range []string{"NO_REPLY 是一个标记。", "NO_REPLY是一个标记。", "示例：NO_REPLY: 非本人对话", "```text\nNO_REPLY: 非本人对话\n```"} {
		body, reason, silent := OutputWithReason(c, text, true)
		if body != text || reason != "" || silent {
			t.Fatal(body, reason, silent)
		}
	}
	text := "NO_REPLY: 非本人对话"
	for _, chat := range []bool{false, true} {
		c.Enabled = !chat
		body, reason, silent := OutputWithReason(c, text, chat)
		if body != text || reason != "" || silent {
			t.Fatal("非聊天或关闭清洗时误处理", body, reason, silent)
		}
	}
}

func TestToolCallTextPreservesExamples(t *testing.T) {
	for _, tc := range []struct {
		text    string
		blocked bool
	}{
		{"行吧<tool_call><function=image_gen><parameter=prompt>风景</parameter></function></tool_call>", true},
		{"<|tool_calls_section_begin|>payload", true},
		{"```xml\n<tool_call><function=image_gen/></tool_call>\n```\n这是示例。", false},
		{"~~~xml\n<tool_call>example</tool_call>\n~~~", false},
		{"> <tool_call>这是引用</tool_call>", false},
		{"这里的 `<tool_call>` 是格式名。", false},
		{"<image prompt=\"风景\"/>", false},
		{"正常代码：function draw() {}", false},
	} {
		if got := ToolCallText(tc.text); got != tc.blocked {
			t.Errorf("%q: %v", tc.text, got)
		}
	}
}

func TestMediaCleanerKeepsAssociatedDescriptionAndRemovesRawMedia(t *testing.T) {
	input := []Message{
		{ID: "described", Session: "fixture", Role: "user", Kind: "image", Text: "[收到图片] 红色屋顶", MediaText: true},
		{ID: "raw", Session: "fixture", Role: "user", Kind: "image", Text: "data:image/png;base64,fixture-private-binary"},
	}
	result, err := Prepare(Config{Enabled: true, Media: true, Budget: 10000}, input, "fixture")
	if err != nil || result.Messages[0].Text != input[0].Text || strings.Contains(result.Messages[1].Text, "fixture-private-binary") {
		t.Fatal(result, err)
	}
	if input[1].Text != "data:image/png;base64,fixture-private-binary" {
		t.Fatal("改写原始媒体")
	}
}
