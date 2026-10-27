package messageapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
)

// 自制2x2透明色块，无真实账号、照片或网络资源。
const uploadWebPFixture = "UklGRh4AAABXRUJQVlA4TBEAAAAvAUAAEAdQ0KJUq9yBiOh/AAA="

func TestSendWebPImageConvertsPNGAndPreservesReceipt(t *testing.T) {
	data, _ := base64.StdEncoding.DecodeString(uploadWebPFixture)
	var calls atomic.Int32
	server := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/message/image" || r.Method != "POST" {
			t.Error("图片上传路由改变")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, _, err := r.FormFile("image")
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		im, err := png.Decode(bytes.NewReader(body))
		if err != nil || r.FormValue("receiver") != "fixture-image-recipient" {
			t.Errorf("实际上传不是PNG或会话变化：%v", err)
		} else if im.Bounds() != image.Rect(0, 0, 2, 2) || color.NRGBAModel.Convert(im.At(0, 0)) != (color.NRGBA{R: 40, G: 160, B: 90, A: 220}) {
			t.Error("图片像素或透明度被改变")
		}
		_, _ = io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"new_id":"9007199254740993","create_time":123,"client_id":"fixture-image-client"}}`)
	}))
	defer server.Close()
	client, err := api.NewGRPCClient(context.Background(), server.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&remote{client: client}).SendImage("fixture-image-recipient", bytes.NewReader(data))
	if err != nil || result.GetNewId() != 9007199254740993 || result.GetCreateTime() != 123 || result.GetClientId().GetValue() != "fixture-image-client" || calls.Load() != 1 {
		t.Fatal("回执字段丢失或重复上传", result, err, calls.Load())
	}
}

func TestPrepareImageUploadPreservesPNGAndJPEGAndRejectsBrokenWebP(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var pngData, jpegData bytes.Buffer
	_ = png.Encode(&pngData, im)
	_ = jpeg.Encode(&jpegData, im, nil)
	for _, data := range [][]byte{pngData.Bytes(), jpegData.Bytes()} {
		got, err := prepareImageUpload(bytes.NewReader(data))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("原有PNG/JPEG上传被修改", err)
		}
	}
	data, _ := base64.StdEncoding.DecodeString(uploadWebPFixture)
	if _, err := prepareImageUpload(bytes.NewReader(data[:20])); err == nil {
		t.Fatal("损坏WebP仍被上传")
	}
	if _, err := prepareImageUpload(nil); err == nil {
		t.Fatal("空图片仍被上传")
	}
}
