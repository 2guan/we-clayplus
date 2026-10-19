package messageability

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"sync/atomic"

	api "github.com/we-clay/we-clay/gateway/internal/api/message"
)

const attachmentChunkSize uint32 = 64 << 10

// attachmentReader 每次只向 core 请求一个附件分块。读取中途出错时，
// Read 返回错误，调用方不能将已经写出的部分当作完整文件。
type attachmentReader struct {
	api      api.MessageService
	receiver string
	attachID string
	appID    string
	cdnID    string
	aesKey   string
	checksum []byte
	digest   hash.Hash
	total    uint32
	offset   uint32
	buffer   []byte
	failure  error
	closed   atomic.Bool
}

func newAttachmentReader(source api.MessageService, receiver, attachID, appID string, total uint32) (io.ReadCloser, error) {
	if source == nil || receiver == "" || attachID == "" || total == 0 || total > 1<<30 {
		return nil, errors.New("文件附件下载参数无效")
	}
	r := &attachmentReader{api: source, receiver: receiver, attachID: attachID, appID: appID, total: total}
	// 首块同步请求，让失效票据或微信拒绝直接从 Download 返回。
	if err := r.fetch(); err != nil {
		return nil, err
	}
	return r, nil
}

// newCDNAttachmentReader 对已完成的 CDN 附件逐块下载并核对明文 MD5。
// 只有最后一块通过校验后才会交给调用方，避免把损坏文件报告为成功。
func newCDNAttachmentReader(source api.MessageService, fileID, aesKey, checksum string, total uint32) (io.ReadCloser, error) {
	if source == nil || fileID == "" || len(fileID) > 4096 || total == 0 || total > 1<<30 || len(aesKey) != 32 || len(checksum) != 32 {
		return nil, errors.New("CDN 文件附件下载参数或 MD5 不完整")
	}
	if _, err := hex.DecodeString(aesKey); err != nil {
		return nil, errors.New("CDN 文件附件密钥格式无效")
	}
	want, err := hex.DecodeString(checksum)
	if err != nil {
		return nil, errors.New("CDN 文件附件 MD5 格式无效")
	}
	r := &attachmentReader{api: source, cdnID: fileID, aesKey: aesKey, checksum: want, digest: md5.New(), total: total}
	if err := r.fetch(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *attachmentReader) fetch() error {
	if r.closed.Load() {
		return io.ErrClosedPipe
	}
	length := min(attachmentChunkSize, r.total-r.offset)
	if r.cdnID != "" {
		// CDN 的 offset/chunk_size 以 AES 密文计数；末片须包含 PKCS#7
		// 填充字节。整块明文达到 total 时，已核对 MD5，直接结束。
		paddedTotal := r.total + 16 - r.total%16
		length = min(api.CDNAttachmentChunkLimit(r.cdnID), paddedTotal-r.offset)
	}
	var data []byte
	if r.cdnID != "" {
		chunk, err := r.api.DownloadCDNAttachmentChunk(r.cdnID, r.aesKey, r.offset, length)
		if err != nil {
			return err
		}
		data = chunk
	} else {
		resp, err := r.api.DownloadFileChunk(r.receiver, r.attachID, r.appID, r.total, r.offset, length)
		if err != nil {
			return err
		}
		if resp == nil || resp.GetChunk() == nil || resp.GetOffset() != r.offset || resp.GetSize() != r.total ||
			len(resp.GetChunk().GetData()) == 0 || uint32(len(resp.GetChunk().GetData())) > length ||
			resp.GetChunkSize() != uint32(len(resp.GetChunk().GetData())) {
			return errors.New("文件附件分块回执不完整或位置不匹配")
		}
		data = resp.GetChunk().GetData()
	}
	if r.closed.Load() {
		return io.ErrClosedPipe
	}
	if len(data) == 0 || uint32(len(data)) > length {
		return errors.New("文件附件分块回执不完整或位置不匹配")
	}
	if r.digest != nil {
		if uint32(len(data)) != min(length, r.total-r.offset) {
			return errors.New("CDN 文件附件明文长度与请求范围不匹配")
		}
		_, _ = r.digest.Write(data)
		if r.offset+uint32(len(data)) == r.total && !bytes.Equal(r.digest.Sum(nil), r.checksum) {
			return errors.New("CDN 文件附件 MD5 校验失败")
		}
	}
	r.buffer = data
	r.offset += uint32(len(r.buffer))
	return nil
}

func (r *attachmentReader) Read(p []byte) (int, error) {
	if r.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.failure != nil {
		return 0, r.failure
	}
	if len(r.buffer) == 0 {
		if r.offset == r.total {
			return 0, io.EOF
		}
		if err := r.fetch(); err != nil {
			r.failure = err
			return 0, err
		}
	}
	if r.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	n := copy(p, r.buffer)
	r.buffer = r.buffer[n:]
	return n, nil
}

func (r *attachmentReader) Close() error {
	r.closed.Store(true)
	return nil
}
