// 文件名：contact.go
// 主要功能：维护联系人缓存，按批读取并容忍单个失效联系人，刷新失败保留已知资料。
// 作者：2guan
// Package contactability 提供联系人能力的实现（缓存型）。
package contactability

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/duke-git/lancet/v2/slice"
	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
	hc "github.com/we-clay/we-clay/gateway/internal/config"
	sdkroot "github.com/we-clay/we-clay/sdk"
	sdk "github.com/we-clay/we-clay/sdk/contact"
	"google.golang.org/protobuf/encoding/protojson"
)

// ability 联系人能力实现（缓存型）
type ability struct {
	api     contactapi.ContactService
	cache   map[string]*sdk.Contact
	self    *sdk.SelfInfo
	selfMu  sync.RWMutex
	cacheMu sync.RWMutex
}

var instance ability
var cacheDir string

func init() {
	instance = ability{api: contactapi.Get(), cache: map[string]*sdk.Contact{}}
	sdk.Instance = &instance
}

// LoadCache 只加载本地资料，控制台接入时允许先启动页面再后台同步。
func LoadCache(dataDir string) {
	cacheDir = dataDir
	instance.cacheMu.Lock()
	instance.cache = map[string]*sdk.Contact{}
	if file, err := os.ReadFile(filepath.Join(cacheDir, "contact.json")); err == nil {
		var records map[string]json.RawMessage
		if json.Unmarshal(file, &records) == nil {
			for id, raw := range records {
				c := &sdk.Contact{}
				// protobuf oneof 不能用 encoding/json 直接恢复；兼容旧缓存的 data 包装。
				if (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, c) != nil {
					continue
				}
				var legacy struct {
					Data struct {
						Friend   *sdk.Friend
						Chatroom *sdk.Chatroom
					} `json:"data"`
				}
				if json.Unmarshal(raw, &legacy) == nil {
					if legacy.Data.Friend != nil {
						c.Data = &sdk.Contact_Friend{Friend: legacy.Data.Friend}
					}
					if legacy.Data.Chatroom != nil {
						c.Data = &sdk.Contact_Chatroom{Chatroom: legacy.Data.Chatroom}
					}
				}
				if c.Username != "" {
					instance.cache[id] = c
				}
			}
		}
	}
	instance.cacheMu.Unlock()
}

func Initial(dataDir string) {
	LoadCache(dataDir)
	if len(instance.List()) > 0 {
		return
	}
	if err := Refresh(); err != nil {
		slog.Warn("联系人初始化未完成", "err", err)
	}
}

// SetSelf 保存当前登录账号信息。
func SetSelf(self *sdk.SelfInfo) {
	instance.setSelf(self)
}

// GetSelf 获取当前登录账号信息。
func GetSelf() *sdk.SelfInfo {
	return instance.GetSelf()
}

func Destroy() {
	instance.cacheMu.RLock()
	defer instance.cacheMu.RUnlock()
	if cacheDir == "" {
		return
	}
	destroyLocked()
}

func destroyLocked() {
	// 用 protobuf JSON 保存 oneof，避免下次启动无法恢复联系人详情。
	records := make(map[string]json.RawMessage, len(instance.cache))
	for id, c := range instance.cache {
		raw, err := protojson.Marshal(c)
		if err != nil {
			slog.Warn("联系人缓存编码失败")
			return
		}
		records[id] = raw
	}
	marshal, err := json.Marshal(records)
	if err != nil {
		slog.Warn("[contact ability] 序列化联系人信息失败", "err", err)
		return
	}
	if err := writeContactCache(filepath.Join(cacheDir, "contact.json"), marshal, os.Rename); err != nil {
		slog.Warn("[contact ability] 保存联系人信息失败", "err", err)
		return
	}
}

// 先写同目录私有临时文件，再替换旧缓存；写入失败时仍能读取原文件。
func writeContactCache(path string, data []byte, replace func(string, string) error) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".contact-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return replace(temporary, path)
}

// Refresh 刷新缓存
// refreshMu 防止多次点击刷新并发向微信请求，网络访问期间不占用缓存锁。
var refreshMu sync.Mutex

func Refresh() error { return RefreshWithProgress(nil) }

// RefreshWithProgress 以有界并发读取，每批通知控制台显示已取回的资料。
func RefreshWithProgress(progress func() error, additionalIDs ...string) error {
	return refreshContacts(progress, false, additionalIDs...)
}

// RefreshMissingWithProgress 在后台重连时只补缺失资料，避免每次改 Web 都重读整个通讯录。
func RefreshMissingWithProgress(progress func() error, additionalIDs ...string) error {
	return refreshContacts(progress, true, additionalIDs...)
}
func refreshContacts(progress func() error, missingOnly bool, additionalIDs ...string) error {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	list, err := instance.api.List()
	if err != nil {
		return fmt.Errorf("获取联系人列表失败: %w", err)
	}
	// 会话里的群不一定出现在 core 通讯录列表中，合并已归档会话标识单独查询。
	seen := map[string]bool{}
	for _, id := range list {
		seen[id] = true
	}
	for _, id := range additionalIDs {
		if id != "" && !seen[id] {
			list = append(list, id)
			seen[id] = true
		}
	}
	// 先补齐缺失资料，群聊和普通好友优先于公众号，避免页面长期只出现公众号。
	known := map[string]bool{}
	for _, c := range instance.List() {
		known[c.Username] = c.Nickname != "" && c.Nickname != c.Username && c.Avatar != ""
	}
	if missingOnly {
		list = slices.DeleteFunc(list, func(id string) bool { return known[id] })
	}
	active := map[string]bool{}
	for _, id := range additionalIDs {
		active[id] = true
	}
	rank := func(id string) int {
		if active[id] && !known[id] {
			return -1
		}
		n := 1
		if strings.HasSuffix(id, "@chatroom") {
			n = 0
		} else if strings.HasPrefix(id, "gh_") {
			n = 2
		}
		if known[id] {
			n += 3
		}
		return n
	}
	sort.SliceStable(list, func(i, j int) bool { return rank(list[i]) < rank(list[j]) })
	failed := 0
	// 成功子批次立即发布，不等待同批中的失效联系人全部排查完。
	var progressMu sync.Mutex
	publish := func(items []*sdk.Contact) error {
		progressMu.Lock()
		defer progressMu.Unlock()
		instance.cacheMu.Lock()
		for _, c := range items {
			instance.cache[c.Username] = c
		}
		instance.cacheMu.Unlock()
		Destroy()
		if progress != nil {
			return progress()
		}
		return nil
	}
	batches := slice.Chunk(list, 20)
	for offset := 0; offset < len(batches); offset += 4 {
		type result struct {
			contacts []*sdk.Contact
			missing  int
			err      error
		}
		count := min(4, len(batches)-offset)
		results := make([]result, count)
		var workers sync.WaitGroup
		for i := 0; i < count; i++ {
			workers.Add(1)
			go func(i int) {
				defer workers.Done()
				results[i].contacts, results[i].missing, results[i].err = readDetailsWithProgress(batches[offset+i], publish)
			}(i)
		}
		workers.Wait()
		var batchErr error
		for _, r := range results {

			failed += r.missing
			if r.err != nil {
				batchErr = r.err
			}
		}
		// 每批落盘；网络访问期间不占用缓存锁，也不让未完成同步清空已有资料。
		Destroy()
		slog.Info("联系人同步进度", "loaded", len(instance.List()), "processed", min(offset+count, len(batches))*20, "total", len(list))
		if progress != nil {
			if err := progress(); err != nil {
				return err
			}
		}
		if batchErr != nil {
			return batchErr
		}
	}

	Destroy()
	slog.Info("联系人同步完成", "count", len(instance.List()), "unavailable", failed)
	return nil
}

// 一条已失效的账号会让 core 拒绝整批详情；仅对明确的业务失败拆分，不重试网络超时。
func readDetails(ids []string) ([]*sdk.Contact, int, error) { return readDetailsWithProgress(ids, nil) }

func readDetailsWithProgress(ids []string, publish func([]*sdk.Contact) error) ([]*sdk.Contact, int, error) {
	detail, err := instance.api.Detail(ids)
	if err != nil {
		if !strings.Contains(err.Error(), "contact detail item returned code") {
			return nil, len(ids), err
		}
		if len(ids) == 1 {
			return nil, 1, nil
		}
		mid := len(ids) / 2
		left, missing, err := readDetailsWithProgress(ids[:mid], publish)
		if err != nil {
			return left, missing, err
		}
		right, more, err := readDetailsWithProgress(ids[mid:], publish)
		return append(left, right...), missing + more, err
	}
	items := []*sdk.Contact{}
	for _, c := range detail.GetContactList() {
		if built, err := Build(c); err == nil {
			items = append(items, built)
		}
	}
	if publish != nil && len(items) > 0 {
		if err := publish(items); err != nil {
			return items, len(ids) - len(items), err
		}
	}
	return items, len(ids) - len(items), nil
}

// RefreshOne 刷新指定联系人缓存。
func RefreshOne(key string) (*sdk.Contact, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("联系人 key 不能为空")
	}

	contact := instance.Get(key)
	if contact == nil || contact.Username == "" {
		return nil, fmt.Errorf("联系人不存在：%s", key)
	}

	return RefreshUsername(contact.Username)
}

// RefreshUsername 按协议标识补齐新会话，无需预先存在于通讯录缓存。
func RefreshUsername(username string) (*sdk.Contact, error) {
	if strings.TrimSpace(username) == "" {
		return nil, fmt.Errorf("联系人标识不能为空")
	}
	detail, err := instance.api.Detail([]string{username})
	if err != nil {
		return nil, fmt.Errorf("获取会话资料失败: %w", err)
	}
	list := detail.GetContactList()
	if len(list) != 1 || list[0].GetUsername().GetValue() != username {
		return nil, fmt.Errorf("会话资料回执标识不匹配")
	}

	build, err := Build(list[0])
	if err != nil {
		return nil, fmt.Errorf("解析会话资料失败: %w", err)
	}
	instance.cacheMu.Lock()
	instance.cache[build.Username] = build
	instance.cacheMu.Unlock()
	Destroy()
	return build, nil
}

func (a *ability) setSelf(self *sdk.SelfInfo) {
	a.selfMu.Lock()
	defer a.selfMu.Unlock()

	a.self = cloneSelfInfo(self)
}

// GetSelf 获取当前登录账号信息
func (a *ability) GetSelf() *sdk.SelfInfo {
	a.selfMu.RLock()
	defer a.selfMu.RUnlock()

	return cloneSelfInfo(a.self)
}

// Get 按键查询缓存联系人，支持前缀：username::（默认）、nickname::、remark::
func (a *ability) Get(key string) *sdk.Contact {
	// username 查询未命中时会写入缓存，不能持有读锁执行。
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	if c := getStrategy(key).find(a.cache); c != nil {
		return cloneContact(c)
	}
	return nil
}

// // Delete 从缓存中删除指定联系人。
//
//	func (a *ability) Delete(username string) {
//		delete(a.cache, username)
//	}
//
// // Has 判断缓存中是否存在指定联系人。
//
//	func (a *ability) Has(username string) bool {
//		_, ok := a.cache[username]
//		return ok
//	}
//
// List 获取联系人列表
func (a *ability) List() []*sdk.Contact {
	a.cacheMu.RLock()
	defer a.cacheMu.RUnlock()
	var result []*sdk.Contact
	for _, c := range a.cache {
		result = append(result, cloneContact(c))
	}
	// map 遍历无固定顺序；缓存投影不能让页面默认对象随增量更新变化。
	sort.Slice(result, func(i, j int) bool { return result[i].Username < result[j].Username })
	return result
}

// SetRemark 设置联系人备注
func (a *ability) SetRemark(username, remark string) error {
	// 传输层已校验外层和命令状态；core 负责保留好友关系并读回备注。
	_, err := a.api.SetRemark(username, remark)
	return err
}

// AddFriend 发送好友申请
func (a *ability) AddFriend(v1, v2, content string, operate, scene int) error {
	_, err := a.api.Request(v1, v2, content, operate, scene)
	if err != nil {
		return err
	}
	return nil
}

// VerifyFriend 通过好友验证
func (a *ability) VerifyFriend(v1, v2 string, scene int) error {
	if _, err := a.api.Verify(v1, v2, scene); err != nil {
		return err
	}
	return nil
}

// Delete 删除联系人
func (a *ability) Delete(username string) error { return sdkroot.NotImplemented("contact.Delete") }

// BlacklistAdd 添加到黑名单
func (a *ability) BlacklistAdd(username string) error {
	return sdkroot.NotImplemented("contact.BlacklistAdd")
}

// BlacklistRemove 从黑名单移除
func (a *ability) BlacklistRemove(username string) error {
	return sdkroot.NotImplemented("contact.BlacklistRemove")
}

// Search 搜索联系人
func (a *ability) Search(keyword string, fromScene, searchScene uint32) *sdk.Contact {
	search, err := a.api.Search(keyword, fromScene, searchScene)
	if err != nil || search.GetUsername().GetValue() == "" {
		return nil
	}
	return &sdk.Contact{
		Username: search.GetUsername().GetValue(),
		Nickname: search.GetNickname().GetValue(),
		Remark:   "",
		Alias:    search.GetAlias(),
		Avatar:   search.GetSmallAvatarUrl(),
		Type:     sdk.ContactType_CONTACT_TYPE_FRIEND,
		Data:     nil,
		V1:       search.GetUsername().GetValue(),
		V2:       search.GetAntispamTicket(),
	}
}

// GetOwner 获取机器人所有者信息
func (a *ability) GetOwner() *sdk.Contact {
	ownerID := hc.Get().Owner
	if ownerID == "" {
		return nil
	}
	return a.Get(ownerID)
}

func cloneSelfInfo(self *sdk.SelfInfo) *sdk.SelfInfo {
	if self == nil {
		return nil
	}
	return &sdk.SelfInfo{
		Username: self.Username,
		Nickname: self.Nickname,
		Alias:    self.Alias,
		Avatar:   self.Avatar,
		Uin:      self.Uin,
		Email:    self.Email,
		Mobile:   self.Mobile,
	}
}
