// 文件名：group_sync.go
// 主要功能：按需补齐新会话的联系人资料，单工作协程合并请求并限制失败重试频率。
// 作者：2guan
package console

import (
	"log/slog"
	"strings"
	"time"

	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	"github.com/we-clay/we-clay/sdk/contact"
)

func (b *Bridge) startProfileSync(id string) {
	b.startProfileRefresh(id, false, false)
}

func isChatroomID(id string) bool {
	return strings.HasSuffix(id, "@chatroom") || strings.HasSuffix(id, "@im.chatroom")
}

// 群系统消息未必附带联系人增量；明显的改名通知立即核对，其他通知最多每分钟兜底核对一次。
func (b *Bridge) startGroupNoticeProfileSync(id, content string) {
	b.startProfileRefresh(id, true, isGroupNameNotice(content))
}

func isGroupNameNotice(content string) bool {
	name := strings.Contains(content, "群名") || strings.Contains(content, "群聊名称") || strings.Contains(content, "群名称")
	changed := strings.Contains(content, "修改") || strings.Contains(content, "更改") || strings.Contains(content, "改为") || strings.Contains(content, "改成")
	return name && changed
}

func (b *Bridge) startProfileRefresh(id string, force, immediate bool) {
	if !b.Ready.Load() || id == "" || !force && !b.Dashboard.NeedsContactProfile(id) {
		return
	}
	b.contactSyncMu.Lock()
	defer b.contactSyncMu.Unlock()
	if b.closing || !immediate && time.Since(b.profileSyncAttempt[id]) < time.Minute {
		return
	}
	if b.profileSyncPending == nil {
		b.profileSyncPending = map[string]bool{}
		b.profileSyncAttempt = map[string]time.Time{}
	}
	b.profileSyncAttempt[id] = time.Now()
	b.profileSyncPending[id] = b.profileSyncPending[id] || force
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
		force := false
		if !b.closing {
			for pending := range b.profileSyncPending {
				id = pending
				force = b.profileSyncPending[id]
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
		if force || c == nil || c.Nickname == "" || c.Nickname == id || c.Avatar == "" {
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
