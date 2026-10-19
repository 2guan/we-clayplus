// 文件名：group_sync.go
// 主要功能：按需补齐新会话的联系人资料，单工作协程合并请求并限制失败重试频率。
// 作者：2guan
package console

import (
	"log/slog"
	"time"

	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	"github.com/we-clay/we-clay/sdk/contact"
)

func (b *Bridge) startProfileSync(id string) {
	if !b.Ready.Load() || id == "" || !b.Dashboard.NeedsContactProfile(id) {
		return
	}
	b.contactSyncMu.Lock()
	defer b.contactSyncMu.Unlock()
	if b.closing || time.Since(b.profileSyncAttempt[id]) < time.Minute {
		return
	}
	if b.profileSyncPending == nil {
		b.profileSyncPending = map[string]bool{}
		b.profileSyncAttempt = map[string]time.Time{}
	}
	b.profileSyncAttempt[id] = time.Now()
	b.profileSyncPending[id] = true
	if b.profileSyncRunning {
		return
	}
	b.profileSyncRunning = true
	b.contactSyncWG.Add(1)
	go b.syncProfiles()
}

func (b *Bridge) syncProfiles() {
	defer b.contactSyncWG.Done()
	for {
		b.contactSyncMu.Lock()
		id := ""
		if !b.closing {
			for pending := range b.profileSyncPending {
				id = pending
				delete(b.profileSyncPending, id)
				break
			}
		}
		if id == "" {
			b.profileSyncRunning = false
			b.contactSyncMu.Unlock()
			return
		}
		b.contactSyncMu.Unlock()
		c := contact.Instance.Get(id)
		if c == nil || c.Nickname == "" || c.Nickname == id || c.Avatar == "" {
			if _, err := contactability.RefreshUsername(id); err != nil {
				slog.Warn("会话联系人资料暂未获取，后续消息将重试")
				continue
			}
		}
		if err := b.SyncContacts(); err != nil {
			slog.Warn("会话联系人资料更新到控制台失败")
		}
	}
}
