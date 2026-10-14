// 文件名：receipts.go
// 主要功能：管理原始撤回回执缓存，并向宿主提供可持久化的原始字段。
// 作者：2guan
package messageability

import (
	"container/list"
	"strconv"
	"sync"
)

// 撤回只保留当前 Run 最近的完整发送回执，不保存正文或持久化账号数据。
const maxRevokeReceipts = 4096

type receiptKey struct {
	receiver string
	newID    uint64
}
type revokeReceipt struct {
	key               receiptKey
	clientID, created uint64
}
type receiptCache struct {
	mu      sync.Mutex
	entries map[receiptKey]*list.Element
	order   list.List
}

func (c *receiptCache) remember(receiver string, newID, clientID, created uint64) bool {
	if receiver == "" || newID == 0 || clientID == 0 || created == 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[receiptKey]*list.Element)
	}
	r := revokeReceipt{receiptKey{receiver, newID}, clientID, created}
	if e := c.entries[r.key]; e != nil {
		e.Value = r
		c.order.MoveToBack(e)
		return true
	}
	c.entries[r.key] = c.order.PushBack(r)
	if len(c.entries) > maxRevokeReceipts {
		first := c.order.Front()
		delete(c.entries, first.Value.(revokeReceipt).key)
		c.order.Remove(first)
	}
	return true
}

func (c *receiptCache) get(receiver string, newID uint64) (revokeReceipt, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[receiptKey{receiver, newID}]
	if e == nil {
		return revokeReceipt{}, false
	}
	return e.Value.(revokeReceipt), true
}

func (c *receiptCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
	c.order.Init()
}

// ResetReceipts 在 Run 的启动和清理阶段调用，避免回执跨账号/运行复用。
func ResetReceipts() { instance.receipts.clear() }

// RememberReceipt 供绕过通用 Send 的名片入口记录原始发送回执。
func RememberReceipt(receiver string, newID, clientID, created uint64) {
	instance.receipts.remember(receiver, newID, clientID, created)
}

func numericClientID(value string) uint64 {
	// 媒体回执可能是不可用于撤回的字符串 ID；不得哈希、截断或用时间代替。
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// ReceiptFields 仅导出真实发送回执，不能用本地时间或随机 ID 补齐。
func ReceiptFields(receiver string, newID uint64) (uint64, uint64, bool) {
	r, ok := instance.receipts.get(receiver, newID)
	return r.clientID, r.created, ok
}
