package messageability

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	base "github.com/we-clay/we-clay/gateway/internal/api/base"
	api "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type attachmentSource struct {
	api.MessageService
	calls int
	size  uint32
	appID string
	badAt uint32
}

func (s *attachmentSource) DownloadFileChunk(_, _, appID string, size, offset, chunkSize uint32) (*api.DownloadFileResponse, error) {
	s.calls++
	s.size = size
	s.appID = appID
	if offset == s.badAt && s.badAt != 0 {
		return nil, errors.New("微信附件分块请求失败")
	}
	data := make([]byte, chunkSize)
	return &api.DownloadFileResponse{
		Size: proto.Uint32(size), Offset: proto.Uint32(offset), ChunkSize: proto.Uint32(chunkSize),
		Chunk: &base.Buffer{Size: proto.Uint32(chunkSize), Data: data},
	}, nil
}

func TestUploadingAttachmentCannotDownload(t *testing.T) {
	source := &attachmentSource{}
	a := &ability{api: source}
	msg, err := Build(&api.NewMessage{
		Sender: wrapperspb.String("test-peer"), Type: proto.Int32(49),
		Content: wrapperspb.String(`<msg><appmsg><type>74</type><title>uploading.zip</title><appattach><totallen>314572800</totallen></appattach></appmsg></msg>`),
	})
	if err != nil || msg.GetType().GetCode() != sdk.TypeAppFileNotify.GetCode() {
		t.Fatalf("upload notice must keep its app subtype: type=%d err=%v", msg.GetType().GetCode(), err)
	}
	if _, err := a.Download(msg); status.Code(err) != codes.FailedPrecondition || source.calls != 0 {
		t.Fatalf("uploading file must be rejected before contacting core: code=%v calls=%d", status.Code(err), source.calls)
	}
}

func TestLargeAttachmentDownloadsOneChunkAtATime(t *testing.T) {
	const size uint32 = 300 << 20
	source := &attachmentSource{badAt: attachmentChunkSize}
	a := &ability{api: source}
	msg := &sdk.Message{
		Type:   sdk.TypeAppFileAttach,
		Sender: &contact.Contact{Username: "test-peer"},
		Data:   &sdk.Message_App{App: &sdk.AppData{Xml: `<msg><appmsg appid="synthetic-app"><type>6</type><appattach><attachid>test-attach</attachid><totallen>314572800</totallen></appattach></appmsg></msg>`}},
	}
	r, err := a.Download(msg)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if source.calls != 1 || source.size != size || source.appID != "synthetic-app" {
		t.Fatalf("first chunk should preserve attachment metadata without buffering whole file: calls=%d size=%d appID=%q", source.calls, source.size, source.appID)
	}
	first := make([]byte, attachmentChunkSize)
	if _, err = io.ReadFull(r, first); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Read(first[:1]); err == nil || err == io.EOF || source.calls != 2 {
		t.Fatalf("later chunk failure must surface as error: err=%v calls=%d", err, source.calls)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Read(first[:1]); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("closed reader should stop downloading: %v", err)
	}
}

type cdnAttachmentSource struct {
	api.MessageService
	data     []byte
	cdnCalls int
	cgiCalls int
	offsets  []uint32
	sizes    []uint32
}

const bigCDNTestChunkSize uint32 = 256 << 10

func testBigCDNID() string    { return "7f" + strings.Repeat("00", 113) }
func testNormalCDNID() string { return "05" + strings.Repeat("00", 113) }

func (s *cdnAttachmentSource) DownloadCDNAttachmentChunk(fileID, aesKey string, offset, chunkSize uint32) ([]byte, error) {
	s.cdnCalls++
	s.offsets = append(s.offsets, offset)
	s.sizes = append(s.sizes, chunkSize)
	if (fileID != testBigCDNID() && fileID != testNormalCDNID()) || aesKey != "00112233445566778899aabbccddeeff" ||
		offset >= uint32(len(s.data)) || offset%16 != 0 || chunkSize == 0 || chunkSize%16 != 0 || chunkSize > api.CDNAttachmentChunkLimit(fileID) {
		return nil, errors.New("CDN 分片请求不正确")
	}
	end := min(uint32(len(s.data)), offset+chunkSize)
	return append([]byte(nil), s.data[offset:end]...), nil
}

func TestCDNAttachmentFinalCipherRange(t *testing.T) {
	for _, tc := range []struct {
		name       string
		plainSize  int
		cipherSize uint32
	}{
		{name: "partial final block", plainSize: 6659, cipherSize: 6672},
		{name: "full 256 KiB block", plainSize: int(bigCDNTestChunkSize), cipherSize: bigCDNTestChunkSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := bytes.Repeat([]byte("x"), tc.plainSize)
			sum := md5.Sum(data)
			source := &cdnAttachmentSource{data: data}
			a := &ability{api: source}
			r, err := a.Download(completedCDNMessage(len(data), hex.EncodeToString(sum[:]), testBigCDNID(), "00112233445566778899aabbccddeeff"))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			got, err := io.ReadAll(r)
			if err != nil || !bytes.Equal(got, data) || source.cdnCalls != 1 || source.cgiCalls != 0 ||
				len(source.sizes) != 1 || source.sizes[0] != tc.cipherSize {
				t.Fatalf("末片密文范围不正确: bytes=%d err=%v calls=%d sizes=%v", len(got), err, source.cdnCalls, source.sizes)
			}
		})
	}
}

func TestCDNAttachmentFullBlockChecksMD5WithoutPaddingRequest(t *testing.T) {
	source := &cdnAttachmentSource{data: bytes.Repeat([]byte("x"), int(bigCDNTestChunkSize))}
	a := &ability{api: source}
	_, err := a.Download(completedCDNMessage(len(source.data), strings.Repeat("0", 32), testBigCDNID(), "00112233445566778899aabbccddeeff"))
	if err == nil || !strings.Contains(err.Error(), "MD5") || source.cdnCalls != 1 {
		t.Fatalf("完整块未进行 MD5 校验: err=%v calls=%d", err, source.cdnCalls)
	}
}

func (s *cdnAttachmentSource) DownloadFileChunk(_, _, _ string, _, _, _ uint32) (*api.DownloadFileResponse, error) {
	s.cgiCalls++
	return nil, errors.New("CDN 附件不应访问旧 CGI")
}

func completedCDNMessage(size int, checksum, cdnID, aesKey string) *sdk.Message {
	return &sdk.Message{
		Type:   sdk.TypeAppFileAttach,
		Sender: &contact.Contact{Username: "synthetic-peer"},
		Data: &sdk.Message_App{App: &sdk.AppData{Xml: fmt.Sprintf(
			`<msg><appmsg><type>6</type><md5>%s</md5><appattach><totallen>%d</totallen><attachid>@cdn_synthetic</attachid><cdnattachurl>%s</cdnattachurl><aeskey>%s</aeskey></appattach></appmsg></msg>`,
			checksum, size, cdnID, aesKey)}},
	}
}

func TestCompletedCDNAttachmentStreamsAndChecksMD5(t *testing.T) {
	data := bytes.Repeat([]byte("synthetic attachment bytes"), int(2*bigCDNTestChunkSize+6659)/len("synthetic attachment bytes")+1)
	data = data[:2*bigCDNTestChunkSize+6659]
	sum := md5.Sum(data)
	source := &cdnAttachmentSource{data: data}
	a := &ability{api: source}
	r, err := a.Download(completedCDNMessage(len(data), hex.EncodeToString(sum[:]), testBigCDNID(), "00112233445566778899aabbccddeeff"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if source.cdnCalls != 1 {
		t.Fatalf("Download 应只预取首块: calls=%d", source.cdnCalls)
	}
	first := make([]byte, bigCDNTestChunkSize)
	if _, err := io.ReadFull(r, first); err != nil || source.cdnCalls != 1 {
		t.Fatalf("读取首块不应预取后续分块: err=%v calls=%d", err, source.cdnCalls)
	}
	rest, err := io.ReadAll(r)
	got := append(first, rest...)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("CDN 文件流未完整校验: bytes=%d err=%v", len(got), err)
	}
	if source.cdnCalls != 3 || source.cgiCalls != 0 ||
		!slices.Equal(source.offsets, []uint32{0, bigCDNTestChunkSize, 2 * bigCDNTestChunkSize}) ||
		!slices.Equal(source.sizes, []uint32{bigCDNTestChunkSize, bigCDNTestChunkSize, 6672}) {
		t.Fatalf("下载分块或路由错误: CDN=%d CGI=%d offsets=%v sizes=%v", source.cdnCalls, source.cgiCalls, source.offsets, source.sizes)
	}
}

func TestOrdinaryCDNAttachmentKeeps64KiBChunks(t *testing.T) {
	data := bytes.Repeat([]byte("x"), int(2*attachmentChunkSize+17))
	sum := md5.Sum(data)
	source := &cdnAttachmentSource{data: data}
	r, err := (&ability{api: source}).Download(completedCDNMessage(len(data), hex.EncodeToString(sum[:]), testNormalCDNID(), "00112233445566778899aabbccddeeff"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, data) || source.cgiCalls != 0 ||
		!slices.Equal(source.offsets, []uint32{0, attachmentChunkSize, 2 * attachmentChunkSize}) ||
		!slices.Equal(source.sizes, []uint32{attachmentChunkSize, attachmentChunkSize, 32}) {
		t.Fatalf("普通 CDN 附件未保持 64 KiB 分块: bytes=%d err=%v offsets=%v sizes=%v", len(got), err, source.offsets, source.sizes)
	}
}

func TestCDNAttachmentMD5FailureWithholdsFinalChunk(t *testing.T) {
	data := bytes.Repeat([]byte("x"), int(bigCDNTestChunkSize)+17)
	source := &cdnAttachmentSource{data: data}
	a := &ability{api: source}
	r, err := a.Download(completedCDNMessage(len(data), strings.Repeat("0", 32), testBigCDNID(), "00112233445566778899aabbccddeeff"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if len(got) != int(bigCDNTestChunkSize) || err == nil || !strings.Contains(err.Error(), "MD5") {
		t.Fatalf("错误的末块被输出或未报告校验失败: bytes=%d err=%v", len(got), err)
	}
	if _, err := r.Read(make([]byte, 1)); err == nil || source.cdnCalls != 2 {
		t.Fatalf("校验失败后不应重复请求末块: calls=%d err=%v", source.cdnCalls, err)
	}
}

func TestIncompleteCDNAttachmentNeverFallsBackToCGI(t *testing.T) {
	source := &cdnAttachmentSource{}
	a := &ability{api: source}
	msg := completedCDNMessage(100, strings.Repeat("0", 32), testBigCDNID(), "")
	_, err := a.Download(msg)
	if status.Code(err) != codes.FailedPrecondition || source.cdnCalls != 0 || source.cgiCalls != 0 {
		t.Fatalf("不完整 CDN 票据触发了下载: code=%v CDN=%d CGI=%d", status.Code(err), source.cdnCalls, source.cgiCalls)
	}
}
