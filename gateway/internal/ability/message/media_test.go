package messageability

import (
	"encoding/json"
	base "github.com/we-clay/we-clay/gateway/internal/api/base"
	api "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/chatroom"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"io"
	"strings"
	"testing"
)

type mediaAPI struct {
	api.MessageService
	receiver, id, key, xml string
	size                   uint32
	calls                  int
}

func (f *mediaAPI) ForwardImage(receiver string, r io.Reader) (*api.UploadImageResponse, error) {
	f.calls++
	f.receiver = receiver
	b, _ := io.ReadAll(r)
	f.xml = string(b)
	return &api.UploadImageResponse{NewId: proto.Uint64(123)}, nil
}
func (f *mediaAPI) ForwardVideo(receiver string, r io.Reader) (*api.UploadVideoResponse, error) {
	f.ForwardImage(receiver, r)
	return &api.UploadVideoResponse{NewId: proto.Uint64(123)}, nil
}
func (f *mediaAPI) ForwardFile(receiver, xml string) (*api.SendAppMessageResponse, error) {
	f.ForwardImage(receiver, strings.NewReader(xml))
	return &api.SendAppMessageResponse{NewId: proto.Uint64(123)}, nil
}
func (f *mediaAPI) DownloadImg(receiver, id, key string, size uint32) (*api.DownloadImageResponse, error) {
	f.calls++
	f.receiver, f.id, f.key, f.size = receiver, id, key, size
	return &api.DownloadImageResponse{Chunk: &base.Buffer{Data: []byte("media")}}, nil
}
func (f *mediaAPI) DownloadVideo(receiver, id, key string, size uint32) (*api.DownloadVideoResponse, error) {
	r, _ := f.DownloadImg(receiver, id, key, size)
	return &api.DownloadVideoResponse{Chunk: r.Chunk}, nil
}
func (f *mediaAPI) DownloadVoice(receiver, id, key string, size, length uint32) (*api.DownloadVoiceResponse, error) {
	r, _ := f.DownloadImg(receiver, id, key, size)
	return &api.DownloadVoiceResponse{Data: r.Chunk}, nil
}
func (f *mediaAPI) DownloadFile(receiver, id, key string, size uint32) (*api.DownloadFileResponse, error) {
	r, _ := f.DownloadImg(receiver, id, key, size)
	return &api.DownloadFileResponse{Chunk: r.Chunk}, nil
}

type messageContacts struct{ contact.Ability }

func (messageContacts) Get(name string) *contact.Contact { return &contact.Contact{Username: name} }

type messageMembers struct{ chatroom.Ability }

func (messageMembers) GetMember(room, name string) *chatroom.Member {
	return &chatroom.Member{Username: name}
}

func TestReceivedMediaCanBeForwardedAndDownloaded(t *testing.T) {
	oldContact, oldRoom := contact.Instance, chatroom.Instance
	contact.Instance = messageContacts{}
	chatroom.Instance = messageMembers{}
	defer func() { contact.Instance = oldContact; chatroom.Instance = oldRoom }()
	for _, tc := range []struct {
		name   string
		typ    int32
		xml    string
		code   int32
		wantID string
	}{
		{"image", 3, `<msg><img md5="not-an-id" aeskey="test-key" cdnmidimgurl="test-cdn-id" length="5"/></msg>`, 3, "test-cdn-id"},
		{"video", 43, `<msg><videomsg aeskey="test-key" cdnvideourl="test-cdn-id" length="5"/></msg>`, 43, "test-cdn-id"},
		{"small video", 62, `<msg><videomsg aeskey="test-key" cdnvideourl="test-cdn-id" length="5"/></msg>`, 62, "test-cdn-id"},
		{"file", 49, `<msg><appmsg><type>6</type><title>test.txt</title><appattach><attachid>test-attachment</attachid><totallen>5</totallen></appattach></appmsg></msg>`, 4906, "test-attachment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := &api.NewMessage{Sender: wrapperspb.String("test-room@chatroom"), Receiver: wrapperspb.String("test-self"), Content: wrapperspb.String("test-member:\n" + tc.xml), Type: proto.Int32(tc.typ), NewId: proto.Int64(9007199254740993)}
			msg, err := Build(raw)
			if err != nil {
				t.Fatal(err)
			}
			msg = proto.Clone(msg).(*sdk.Message)
			if msg.GetType().GetCode() != tc.code || msg.GetMember().GetUsername() != "test-member" {
				t.Fatal("message parse lost subtype or group member")
			}
			f := &mediaAPI{}
			a := &ability{api: f}
			sent, err := a.Forward(msg, "test-target")
			if err != nil || sent.GetNewId() != 123 || f.xml != tc.xml || f.receiver != "test-target" {
				t.Fatal("forward lost original XML", err)
			}
			reader, err := a.Download(msg)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(reader)
			reader.Close()
			if string(data) != "media" || f.id != tc.wantID || f.receiver != "test-room@chatroom" || f.size != 5 {
				t.Fatal("download uses wrong identity")
			}
			if f.calls != 2 {
				t.Fatal("request repeated")
			}
		})
	}
	raw := &api.NewMessage{Content: wrapperspb.String(`<msg><voicemsg length="5" voicelength="1000"/></msg>`)}
	b, _ := json.Marshal(raw)
	msg := &sdk.Message{Type: sdk.TypeVoice, Id: 9007199254740993, Sender: &contact.Contact{Username: "test-peer"}, Raw: string(b), Data: &sdk.Message_Voice{Voice: &sdk.VoiceData{Media: &sdk.Media{Size: 5, Md5: "not-an-id"}, Duration: 1000}}}
	f := &mediaAPI{}
	a := &ability{api: f}
	reader, err := a.Download(msg)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	if f.id != "9007199254740993" || f.key != "" {
		t.Fatal("voice message ID lost")
	}
	if _, err = a.Forward(&sdk.Message{Type: sdk.TypeImage}, "test-target"); err == nil || f.calls != 1 {
		t.Fatal("missing XML accepted")
	}
}
