// 文件名：voice_test.go
// 主要功能：验证真实编解码、语音参数及发送后撤回的原始回执，全部请求限于本机协议服务。
// 作者：2guan
package console

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"

	_ "github.com/we-clay/we-clay/gateway/internal/ability/message"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/control"
)

// 生成确定性的正弦音，不读取任何真实录音或账号数据。
func voiceWAV(seconds int) []byte {
	data := make([]byte, 44+seconds*24000*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 24000)
	binary.LittleEndian.PutUint32(data[28:], 48000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(len(data)-44))
	for i := 0; i < seconds*24000; i++ {
		value := int16(4000 * math.Sin(float64(i)*2*math.Pi*440/24000))
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(value))
	}
	return data
}

func requireVoiceTools(t *testing.T) {
	t.Helper()
	for env, fallback := range map[string]string{"WE_CLAY_FFMPEG": "ffmpeg", "WE_CLAY_SILK_ENCODER": "silk_v3_encoder"} {
		name := os.Getenv(env)
		if name == "" {
			name = fallback
		}
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("真实编码验收需要 %s 或 %s", fallback, env)
		}
	}
}

func TestVoiceRejectsInvalidInputAndMissingTool(t *testing.T) {
	if _, _, err := encodeVoice(context.Background(), []byte("not wave")); err == nil {
		t.Fatal("接收了无效 WAV")
	}
	t.Setenv("WE_CLAY_FFMPEG", "/nonexistent/weclay-ffmpeg")
	if _, _, err := encodeVoice(context.Background(), voiceWAV(1)); err == nil {
		t.Fatal("缺少工具仍返回成功")
	}
}

func TestVoiceEncodingAndReceipt(t *testing.T) {
	requireVoiceTools(t)
	ctx := context.Background()
	if _, _, err := encodeVoice(ctx, voiceWAV(61)); err == nil {
		t.Fatal("超长语音被静默截断")
	}
	var sends, revokes atomic.Int32
	core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/message/voice":
			file, _, err := r.FormFile("voice")
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer file.Close()
			data, _ := io.ReadAll(file)
			if r.FormValue("receiver") != "synthetic-voice-peer" || r.FormValue("duration") != "1000" || r.FormValue("format") != "4" || !bytes.HasPrefix(data, []byte("\x02#!SILK_V3")) || len(data) < 100 {
				t.Error("语音接收人、毫秒时长、格式或编码不符合契约")
				w.WriteHeader(400)
				return
			}
			n := sends.Add(1)
			client := "9007199254740993"
			if n == 2 {
				client = "synthetic_voice_1789890000"
			}
			fmt.Fprintf(w, `{"code":0,"data":{"base_response":{"code":0},"new_id":"%d","client_id":"%s","create_time":1789890000}}`, uint64(9223372036854770000)+uint64(n), client)
		case "/api/message/receipt":
			// 旧 core 不提供完整媒体回执时，发送成功不受影响。
			w.WriteHeader(404)
		case "/api/message/revoke":
			var q struct {
				Receiver   string
				NewID      uint64 `json:"new_id"`
				ClientID   uint64 `json:"client_id"`
				CreateTime uint64 `json:"create_time"`
			}
			if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
				t.Error(err)
			}
			if q.Receiver != "synthetic-voice-peer" || q.NewID != 9223372036854770001 || q.ClientID != 9007199254740993 || q.CreateTime != 1789890000 {
				t.Error("撤回丢失原始回执")
				w.WriteHeader(400)
				return
			}
			revokes.Add(1)
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
		default:
			t.Errorf("意外请求 %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	client, err := api.NewGRPCClient(ctx, core.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	old := api.GetCore
	api.GetCore = func() *api.Client { return client }
	defer func() { api.GetCore = old }()
	b := &Bridge{}
	b.Ready.Store(true)
	params, _ := json.Marshal(map[string]string{"kind": "audio", "media": base64.StdEncoding.EncodeToString(voiceWAV(1)), "mime": "audio/wav"})
	for i := 1; i <= 2; i++ {
		result, err := b.Execute(ctx, control.Command{Operation: "send", Target: "synthetic-voice-peer", Params: params})
		if err != nil {
			t.Fatal(err)
		}
		receipt := result.(map[string]string)
		if receipt["id"] != fmt.Sprint(uint64(9223372036854770000)+uint64(i)) || receipt["duration"] != "1" {
			t.Fatal("控制台回执精度或时长丢失")
		}
		_, err = b.Execute(ctx, control.Command{Operation: "revoke", Target: "synthetic-voice-peer", ID: receipt["id"]})
		// 非数字 client_id 不能凭空构造，必须在发起撤回 RPC 前拒绝。
		if (i == 1 && err != nil) || (i == 2 && err == nil) {
			t.Fatalf("撤回结果错误: %v", err)
		}
	}
	if sends.Load() != 2 || revokes.Load() != 1 {
		t.Fatal("发送或撤回调用次数错误")
	}
}
