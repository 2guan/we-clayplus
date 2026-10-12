// Package chatroomability 提供群聊能力的实现。
package chatroomability

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/duke-git/lancet/v2/maputil"
	"github.com/duke-git/lancet/v2/stream"
	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
	chatroomapi "github.com/we-clay/we-clay/gateway/internal/api/chatroom"
	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
	sdkroot "github.com/we-clay/we-clay/sdk"
	sdk "github.com/we-clay/we-clay/sdk/chatroom"
	"github.com/we-clay/we-clay/sdk/contact"
)

// ability 群聊能力实现（缓存型）
type ability struct {
	cacheMu sync.RWMutex
	none    sdk.Member
	api     chatroomapi.ChatroomService
	cache   map[string]map[string]*sdk.Member
}

var instance ability
var cacheDir string

func init() {
	instance = ability{api: chatroomapi.Get(), cache: map[string]map[string]*sdk.Member{}}
	sdk.Instance = &instance
}

func Initial(dataDir string) {
	instance.cacheMu.Lock()
	defer instance.cacheMu.Unlock()
	cacheDir = dataDir
	instance.cache = map[string]map[string]*sdk.Member{}
	// 从文件加载数据
	if file, err := os.ReadFile(filepath.Join(cacheDir, "chatroom.json")); err == nil {
		if err := json.Unmarshal(file, &instance.cache); err != nil {
			slog.Warn("[chatroom ability] 反序列化群组信息失败")
			return
		}
		slog.Info("[chatroom ability] 从文件加载群组信息成功", "count", len(instance.cache))
		return
	}

	// 从api获取数据
	stream.Of(contact.Instance.List()...).
		Filter(func(item *contact.Contact) bool { return item.Type == contact.ContactType_CONTACT_TYPE_CHATROOM }).
		ForEach(func(item *contact.Contact) {
			if chatroom, err := contactapi.Get().Detail([]string{item.Username}); err != nil {
				slog.Warn("[chatroom ability] 获取群组信息失败", "chatroom", item.Nickname, "err", err)
				return
			} else {
				for _, info := range chatroom.ContactList[0].Members.List {
					cache := maputil.GetOrSet(instance.cache, item.Username, map[string]*sdk.Member{})
					cache[info.GetUsername()] = &sdk.Member{
						Username:        info.GetUsername(),
						Nickname:        info.GetNickname(),
						DisplayName:     info.GetDisplayName(),
						Avatar:          info.GetSmallAvatarUrl(),
						Flag:            info.GetChatroomMemberFlag(),
						InviterUsername: info.GetInviterUsername(),
					}
				}
			}
			slog.Info("[chatroom ability] 群成员获取成功", "chatroom", item.Nickname, "count", len(instance.cache[item.Username]))
		})
	destroyLocked()
}

func Destroy() {
	instance.cacheMu.RLock()
	defer instance.cacheMu.RUnlock()
	destroyLocked()
}

func destroyLocked() {
	marshal, err := json.Marshal(instance.cache)
	if err != nil {
		slog.Warn("[chatroom ability] 序列化群组信息失败", "err", err)
		return
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "chatroom.json"), marshal, 0600); err != nil {
		slog.Warn("[chatroom ability] 保存群组信息失败", "err", err)
		return
	}
}

func Refresh(name string) error {
	instance.cacheMu.Lock()
	defer instance.cacheMu.Unlock()
	// 清除缓存
	instance.cache[name] = map[string]*sdk.Member{}
	// 从api获取数据
	if chatroom, err := chatroomapi.Get().ListMembers(name); err != nil {
		return fmt.Errorf("[chatroom ability] 获取群组 [%s] 信息失败: %w", name, err)
	} else {
		for _, info := range chatroom.Result.List {
			instance.cache[name][info.GetUsername()] = &sdk.Member{
				Username:        info.GetUsername(),
				Nickname:        info.GetNickname(),
				DisplayName:     info.GetDisplayName(),
				Avatar:          info.GetSmallAvatarUrl(),
				Flag:            info.GetFlag(),
				InviterUsername: info.GetInviterUsername(),
			}
		}
	}
	slog.Info("[chatroom ability] 群成员获取成功", "chatroom", name, "count", len(instance.cache[name]))
	destroyLocked()
	return nil
}

func RefreshMember(chatroom string, member string) (*sdk.Member, error) {
	instance.cacheMu.Lock()
	defer instance.cacheMu.Unlock()
	if chatroom == "" {
		return nil, fmt.Errorf("群聊不能为空")
	}
	if member == "" {
		return nil, fmt.Errorf("群成员不能为空")
	}

	detail, err := instance.api.GetMemberDetail(chatroom, []string{member})
	if err != nil {
		return nil, fmt.Errorf("[chatroom ability] 获取群成员 [%s] 信息失败: %w", member, err)
	}
	list := detail.GetContactList()
	if len(list) == 0 {
		return nil, fmt.Errorf("群成员不存在：%s", member)
	}

	res := buildMemberFromContact(list[0])
	cache := maputil.GetOrSet(instance.cache, chatroom, map[string]*sdk.Member{})
	cache[res.Username] = res
	destroyLocked()
	return cloneMember(res), nil
}

func buildMemberFromContact(c *contactapi.ModifyContact) *sdk.Member {
	return &sdk.Member{
		Username:        c.GetUsername().GetValue(),
		Nickname:        c.GetNickname().GetValue(),
		Remark:          c.GetRemark().GetValue(),
		Alias:           c.GetAlias(),
		DisplayName:     "",
		Avatar:          c.GetSmallAvatarUrl(),
		Flag:            0,
		InviterUsername: "",
		Gender:          contactability.GetGender(c.GetGender()),
		Country:         c.GetCountry(),
		Province:        c.GetProvince(),
		City:            c.GetCity(),
		Signature:       c.GetSignature(),
	}
}

func (a *ability) loadCache() error {
	return nil
}

func (a *ability) saveCache() error {
	return nil
}

func (a *ability) Create(members []string) (*sdk.Create_Response, error) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	res, err := a.api.Create(members)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("建群回执缺失")
	}
	if err := checkBase(res.GetBaseResponse()); err != nil {
		return nil, err
	}
	if res.GetChatroom() == "" {
		return nil, fmt.Errorf("建群回执缺少群 ID")
	}
	response := sdk.Create_Response{
		Chatroom:       res.GetChatroom(),
		Name:           res.GetTopic(),
		Members:        make([]*sdk.Member, 0),
		BigAvatarUrl:   res.GetBigAvatarUrl(),
		SmallAvatarUrl: res.GetSmallAvatarUrl(),
	}
	for _, member := range res.MemberList {
		m := sdk.Member{
			Username:        member.GetUsername().GetValue(),
			Nickname:        member.GetNickname().GetValue(),
			Remark:          member.GetRemark().GetValue(),
			Alias:           "",
			DisplayName:     "",
			Avatar:          "",
			Flag:            0,
			InviterUsername: "",
			Gender:          contactability.GetGender(member.GetGender()),
			Country:         member.GetCountry(),
			Province:        member.GetProvince(),
			City:            member.GetCity(),
			Signature:       member.GetSignature(),
		}
		response.Members = append(response.Members, &m)
		cache := maputil.GetOrSet(a.cache, response.Chatroom, map[string]*sdk.Member{})
		cache[m.Username] = cloneMember(&m)
	}
	_ = a.saveCache()
	return &response, nil
}

func (a *ability) FacingCreate(password string, latitude, longitude float32, operate uint32) (*sdk.FacingCreate_Response, error) {
	return nil, sdkroot.NotImplemented("chatroom.FacingCreate")
}

func (a *ability) GetInfo(chatroom string) (*sdk.GetInfo_Response, error) {
	detail, err := a.api.GetInfo(chatroom)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, fmt.Errorf("群详情回执缺失")
	}
	if err := checkBase(detail.GetBaseResponse()); err != nil {
		return nil, err
	}
	get := contact.Instance.Get(chatroom)
	if get == nil {
		return nil, fmt.Errorf("[%s] 不存在", chatroom)
	}
	res := sdk.GetInfo_Response{
		Username:     get.Username,
		Nickname:     get.Nickname,
		Remark:       get.Remark,
		Alias:        get.Alias,
		Avatar:       get.Avatar,
		Owner:        get.GetChatroom().GetOwner(),
		MemberCount:  get.GetChatroom().GetMemberCount(),
		Announcement: detail.GetAnnouncement(),
	}
	return &res, nil
}

func (a *ability) GetQRCode(chatroom string) (*sdk.GetQRCode_Response, error) {
	code, err := a.api.GetQRCode(chatroom)
	if err != nil {
		return nil, err
	}
	return &sdk.GetQRCode_Response{
		Qrcode:        code.GetQrcodeBuffer().GetData(),
		QrcodeUrl:     code.GetQrcodeUrl(),
		FooterWording: code.GetFooterWording(),
	}, nil
}

func (a *ability) AddMember(chatroom string, members []string) (*sdk.AddMember_Response, error) {
	return nil, sdkroot.NotImplemented("chatroom.AddMember")
}

func (a *ability) InviteMember(chatroom string, members []string) (*sdk.InviteMember_Response, error) {
	resp, err := a.api.InviteMember(chatroom, members)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("邀请回执缺失，请核对群成员状态")
	}
	if err := checkBase(resp.GetBaseResponse()); err != nil {
		return nil, err
	}
	out := &sdk.InviteMember_Response{MemberCount: resp.GetMemberCount()}
	for _, m := range resp.GetMemberList() {
		out.Members = append(out.Members, mapMember(m))
	}
	return out, nil
}

func (a *ability) RemoveMember(chatroom string, members []string) (*sdk.RemoveMember_Response, error) {
	return nil, sdkroot.NotImplemented("chatroom.RemoveMember")
}

func (a *ability) SetName(chatroom, name string) (*sdk.SetName_Response, error) {
	resp, err := a.api.SetName(chatroom, name)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Code == nil {
		return nil, fmt.Errorf("SetName回执不完整，请核对群状态")
	}
	if resp.GetCode() != 0 || resp.GetResult().GetCode() != 0 {
		return nil, fmt.Errorf("SetName失败：%d/%d", resp.GetCode(), resp.GetResult().GetCode())
	}
	return &sdk.SetName_Response{Code: resp.GetCode(), Message: string(resp.GetResult().GetMessage())}, nil
}

func (a *ability) SetAnnouncement(chatroom, content string) (*sdk.SetAnnouncement_Response, error) {
	resp, err := a.api.SetAnnouncement(chatroom, content)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("SetAnnouncement回执缺失")
	}
	if err := checkBase(resp.GetBaseResponse()); err != nil {
		return nil, err
	}
	return &sdk.SetAnnouncement_Response{Code: resp.GetBaseResponse().GetCode(), Message: resp.GetBaseResponse().GetMessage().GetValue()}, nil
}

func (a *ability) SetRemark(chatroom, remark string) (*sdk.SetRemark_Response, error) {
	resp, err := a.api.SetRemark(chatroom, remark)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Code == nil {
		return nil, fmt.Errorf("SetRemark回执不完整，请核对群状态")
	}
	if resp.GetCode() != 0 || resp.GetResult().GetCode() != 0 {
		return nil, fmt.Errorf("SetRemark失败：%d/%d", resp.GetCode(), resp.GetResult().GetCode())
	}
	return &sdk.SetRemark_Response{Code: resp.GetCode(), Message: string(resp.GetResult().GetMessage())}, nil
}

func (a *ability) Save(chatroom string, save bool) (*sdk.Save_Response, error) {
	resp, err := a.api.SetContactList(chatroom, save)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Code == nil {
		return nil, fmt.Errorf("Save回执不完整，请核对群状态")
	}
	if resp.GetCode() != 0 || resp.GetResult().GetCode() != 0 {
		return nil, fmt.Errorf("Save失败：%d/%d", resp.GetCode(), resp.GetResult().GetCode())
	}
	return &sdk.Save_Response{Code: resp.GetCode(), Message: string(resp.GetResult().GetMessage())}, nil
}

func (a *ability) SetAdmin(chatroom string, members []string) (*sdk.SetAdmin_Response, error) {
	resp, err := a.api.SetAdmin(chatroom, members)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("SetAdmin回执缺失")
	}
	if err := checkBase(resp.GetBaseResponse()); err != nil {
		return nil, err
	}
	return &sdk.SetAdmin_Response{Code: resp.GetBaseResponse().GetCode(), Message: resp.GetBaseResponse().GetMessage().GetValue()}, nil
}

func (a *ability) RemoveAdmin(chatroom string, members []string) (*sdk.RemoveAdmin_Response, error) {
	return nil, sdkroot.NotImplemented("chatroom.RemoveAdmin")
}

func (a *ability) TransferOwner(chatroom, newOwner string) (*sdk.TransferOwner_Response, error) {
	return nil, sdkroot.NotImplemented("chatroom.TransferOwner")
}

func (a *ability) Quit(chatroom string) (*sdk.Quit_Response, error) {
	return nil, sdkroot.NotImplemented("chatroom.Quit")
}

func (a *ability) ScanJoin(qrcodeURL string) (*sdk.ScanJoin_Response, error) {
	return nil, sdkroot.NotImplemented("chatroom.ScanJoin")
}

func (a *ability) ScanJoinEnterprise(qrcodeURL string) (*sdk.ScanJoin_Response, error) {
	return nil, sdkroot.NotImplemented("chatroom.ScanJoinEnterprise")
}

func (a *ability) ConsentJoin(inviteURL string) (*sdk.ConsentJoin_Response, error) {
	return nil, sdkroot.NotImplemented("chatroom.ConsentJoin")
}

func (a *ability) ListMembers(chatroom string) []*sdk.Member {
	a.cacheMu.RLock()
	defer a.cacheMu.RUnlock()
	result := make([]*sdk.Member, 0, len(a.cache[chatroom]))
	for _, member := range a.cache[chatroom] {
		result = append(result, cloneMember(member))
	}
	return result
}

func (a *ability) GetMember(chatroom string, name string) *sdk.Member {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	cache := maputil.GetOrSet(a.cache, chatroom, map[string]*sdk.Member{}) // 获取群组缓存
	member, ex := cache[name]                                              // 获取群成员
	if ex && member.Username != "" {                                       // 如果缓存存在且username不为空，则返回
		return cloneMember(member)
	} else if ex && member.Username == "" { // 如果缓存存在但是username为空，说明成员不存在，返回nil
		return nil
	}

	// 缓存不存在，通过api获取成员信息
	detail, err := a.api.GetMemberDetail(chatroom, []string{name})
	if err != nil {
		slog.Warn("[chatroom ability] 获取群成员列表失败", "err", err)
		return nil
	}
	list := detail.GetContactList()
	if len(list) == 0 {
		cache[name] = &a.none
		return nil
	}
	res := buildMemberFromContact(list[0])
	// 添加缓存
	cache[res.Username] = res
	_ = a.saveCache()

	return cloneMember(res)
}

func (a *ability) GetMembersDetail(chatroom string, members []string) []*sdk.Member {
	resp, err := a.api.GetMemberDetail(chatroom, members)
	if err != nil {
		slog.Warn("获取群成员详情失败", "err", err)
		return nil
	}
	if resp == nil {
		return nil
	}
	if err := checkBase(resp.GetBaseResponse()); err != nil {
		slog.Warn("获取群成员详情失败", "err", err)
		return nil
	}
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	cache := maputil.GetOrSet(a.cache, chatroom, map[string]*sdk.Member{})
	var result []*sdk.Member
	for _, item := range resp.GetContactList() {
		m := buildMemberFromContact(item)
		cache[m.Username] = cloneMember(m)
		result = append(result, m)
	}
	return result
}

// Members 返回指定群的成员缓存（供同步层清理使用）。
func (a *ability) Members(chatroom string) map[string]*sdk.Member {
	a.cacheMu.RLock()
	defer a.cacheMu.RUnlock()
	result := make(map[string]*sdk.Member, len(a.cache[chatroom]))
	for key, member := range a.cache[chatroom] {
		result[key] = cloneMember(member)
	}
	return result
}

// DeleteMember 从群缓存中删除指定成员。
func (a *ability) DeleteMember(chatroom, username string) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	if members, ok := a.cache[chatroom]; ok {
		delete(members, username)
	}
}

// HasChatroom 判断缓存中是否存在该群。
func (a *ability) HasChatroom(chatroom string) bool {
	a.cacheMu.RLock()
	defer a.cacheMu.RUnlock()
	_, ok := a.cache[chatroom]
	return ok
}

// ChatroomKeys 返回所有群的 key 列表。
func (a *ability) ChatroomKeys() []string {
	a.cacheMu.RLock()
	defer a.cacheMu.RUnlock()
	keys := make([]string, 0, len(a.cache))
	for k := range a.cache {
		keys = append(keys, k)
	}
	return keys
}

func checkBase(base *baseapi.BaseResponse) error {
	if base == nil || base.Code == nil {
		return fmt.Errorf("群操作回执缺少业务状态，结果未确认")
	}
	if base.GetCode() != 0 {
		return fmt.Errorf("群操作失败，返回码 %d", base.GetCode())
	}
	return nil
}
func mapMember(m *chatroomapi.Member) *sdk.Member {
	return &sdk.Member{Username: m.GetUsername().GetValue(), Nickname: m.GetNickname().GetValue(), Remark: m.GetRemark().GetValue(), Gender: contactability.GetGender(m.GetGender()), Country: m.GetCountry(), Province: m.GetProvince(), City: m.GetCity(), Signature: m.GetSignature()}
}
