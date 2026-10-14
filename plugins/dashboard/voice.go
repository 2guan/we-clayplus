// 文件名：voice.go
// 主要功能：将已完成的语音合成任务发送到原会话，隔离媒体路径并防止重复发送。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func (s *Server) sendTaskVoice(ctx context.Context, q actionRequest) error {
	state := s.store.snapshot()
	index := slices.IndexFunc(state.Tasks, func(t Task) bool { return t.ID == q.ID })
	if index < 0 {
		return fmt.Errorf("语音任务不存在")
	}
	task := state.Tasks[index]
	if task.Kind != "tts" || task.Status != "completed" || task.Target == "" || task.Target != q.Target {
		return fmt.Errorf("只能把已完成的语音发送到合成时选定的会话")
	}
	if state.Account.Status != "online" || !slices.ContainsFunc(state.Contacts, func(c Contact) bool { return c.ID == q.Target }) {
		return fmt.Errorf("微信离线或目标会话不存在")
	}
	// 只读取服务端生成的媒体；不接收客户端文件路径或远程媒体地址。
	var data []byte
	var err error
	if s.cfg.Mock && task.Result == "/sample-audio.wav" {
		data, err = assets.ReadFile("assets/sample-audio.wav")
	} else {
		name := strings.TrimPrefix(task.Result, "/media/")
		if name == task.Result || name == "" || name != filepath.Base(name) || strings.Contains(name, "..") {
			return fmt.Errorf("语音任务没有有效的本机媒体")
		}
		file, e := os.Open(filepath.Join(s.cfg.DataDir, "media", name))
		if e != nil {
			return fmt.Errorf("合成音频已不可用，请重新生成")
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, (16<<20)+1))
	}
	if err != nil || len(data) == 0 || len(data) > 16<<20 {
		return fmt.Errorf("合成音频无效或超过 16 MB")
	}
	// 持久化抢占发送权；并发点击、刷新或结果未知时均不能再次发送同一任务。
	if err := s.store.change(func(v *State) error {
		for i := range v.Tasks {
			if v.Tasks[i].ID == task.ID {
				if v.Tasks[i].Delivery != "" {
					return fmt.Errorf("此语音已提交发送，请在聊天记录中核对结果")
				}
				v.Tasks[i].Delivery = "sending"
				return nil
			}
		}
		return fmt.Errorf("语音任务已失效")
	}); err != nil {
		return err
	}
	s.notify()
	err = s.send(ctx, actionRequest{Target: task.Target, Content: task.Input, Group: "audio", Media: base64.StdEncoding.EncodeToString(data), MIME: "audio/wav"}, "manual")
	s.updateTask(task.ID, func(t *Task) {
		t.Delivery = "sent"
		if err != nil {
			t.Delivery = "unknown"
			t.Error = err.Error()
		}
	})
	return err
}
