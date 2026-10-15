// 文件名：group_sync.go
// 主要功能：按需补齐新群资料，单工作协程合并请求并限制失败重试频率。
// 作者：2guan
package console

import (
	"log/slog"
	"strings"
	"time"

	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	"github.com/we-clay/we-clay/sdk/contact"
)

func (b *Bridge) startGroupSync(id string) {
	if !b.Ready.Load() || !(strings.HasSuffix(id, "@chatroom") || strings.HasSuffix(id, "@im.chatroom")) || !b.Dashboard.NeedsContactProfile(id) {
		return
	}
	b.contactSyncMu.Lock()
	defer b.contactSyncMu.Unlock()
	if b.closing || time.Since(b.groupSyncAttempt[id]) < time.Minute {
		return
	}
	if b.groupSyncPending == nil {
		b.groupSyncPending = map[string]bool{}
		b.groupSyncAttempt = map[string]time.Time{}
	}
	b.groupSyncAttempt[id] = time.Now()
	b.groupSyncPending[id] = true
	if b.groupSyncRunning {
		return
	}
	b.groupSyncRunning = true
	b.contactSyncWG.Add(1)
	go b.syncGroups()
}

func (b *Bridge) syncGroups() {
	defer b.contactSyncWG.Done()
	for {
		b.contactSyncMu.Lock()
		id := ""
		if !b.closing {
			for pending := range b.groupSyncPending {
				id = pending
				delete(b.groupSyncPending, id)
				break
			}
		}
		if id == "" {
			b.groupSyncRunning = false
			b.contactSyncMu.Unlock()
			return
		}
		b.contactSyncMu.Unlock()
		c := contact.Instance.Get(id)
		if c == nil || c.Nickname == "" || c.Nickname == id || c.Avatar == "" {
			if _, err := contactability.RefreshUsername(id); err != nil {
				slog.Warn("新群资料暂未获取，后续消息将重试")
				continue
			}
		}
		if err := b.SyncContacts(); err != nil {
			slog.Warn("新群资料更新到控制台失败")
		}
	}
}
