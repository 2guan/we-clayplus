// 文件名：incoming_media_test.go
// 主要功能：使用合成音频验证 SILK 解码链路和无效输入拒绝，不连接真实微信。
// 作者：2guan
package console

import (
	"context"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/protobuf/encoding/protojson"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestIncomingVoiceRoundTrip(t *testing.T) {
	requireVoiceTools(t)
	if _, err := exec.LookPath("silk_v3_decoder"); err != nil {
		t.Skip("缺少 SILK 解码器")
	}
	encoded, _, err := encodeVoice(context.Background(), voiceWAV(1))
	if err != nil {
		t.Fatal(err)
	}
	wav, err := decodeIncomingVoice(context.Background(), encoded)
	if err != nil || len(wav) < 48000 || string(wav[:4]) != "RIFF" {
		t.Fatalf("解码失败 %d %v", len(wav), err)
	}
	if _, err := decodeIncomingVoice(context.Background(), []byte("bad")); err == nil {
		t.Fatal("接受无效音频")
	}
}

type incomingFileFixture struct {
	message.Ability
	calls int
}

func (f *incomingFileFixture) Download(m *message.Message) (io.ReadCloser, error) {
	f.calls++
	return io.NopCloser(strings.NewReader("fixture-file")), nil
}
func TestIncomingFileBinding(t *testing.T) {
	oldContact, oldMessage := contact.Instance, message.Instance
	contact.Instance = messageContactFixture{}
	f := &incomingFileFixture{}
	message.Instance = f
	t.Cleanup(func() { contact.Instance = oldContact; message.Instance = oldMessage })
	m := &message.Message{Id: 9007199254740993, Type: message.TypeAppFileAttach, Sender: &contact.Contact{Username: "fixture-peer"}, Data: &message.Message_App{App: &message.AppData{Xml: "<msg><appmsg><type>6</type></appmsg></msg>"}}}
	raw, _ := protojson.Marshal(m)
	c := control.Command{ID: "9007199254740993", Target: "fixture-peer", Params: raw}
	b, e := incomingMedia(context.Background(), c)
	if e != nil || string(b) != "fixture-file" {
		t.Fatal("file rejected", e)
	}
	c.Target = "other"
	if _, e = incomingMedia(context.Background(), c); e == nil {
		t.Fatal("cross-conversation file accepted")
	}
	c.Target = "fixture-peer"
	c.ID = "9007199254740992"
	if _, e = incomingMedia(context.Background(), c); e == nil {
		t.Fatal("wrong message accepted")
	}
	if f.calls != 1 {
		t.Fatal("invalid request reached downloader")
	}
}

func TestIncomingFileStreamBinding(t *testing.T) {
	oldContact, oldMessage := contact.Instance, message.Instance
	contact.Instance = messageContactFixture{}
	f := &incomingFileFixture{}
	message.Instance = f
	t.Cleanup(func() { contact.Instance = oldContact; message.Instance = oldMessage })
	m := &message.Message{Id: 42, Type: message.TypeAppFileAttach, Sender: &contact.Contact{Username: "fixture-peer"}}
	for _, typ := range []*message.Type{message.TypeAppFileAttach, message.TypeAppFileNotify, message.TypeImage} {
		m.Type = typ
		raw, _ := protojson.Marshal(m)
		reader, err := incomingMediaReader(context.Background(), control.Command{Operation: "message-file", ID: "42", Target: "fixture-peer", Params: raw})
		if typ.Code == message.TypeAppFileAttach.Code {
			if err != nil {
				t.Fatal(err)
			}
			reader.Close()
		} else if err == nil {
			reader.Close()
			t.Fatal("non-completed attachment accepted")
		}
	}
	if f.calls != 1 {
		t.Fatal("invalid stream request reached downloader")
	}
}
