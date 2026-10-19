// 文件名：factory.go
// 主要功能：安全转换联系人资料并区分好友、群、公众号和系统账号。
// 作者：2guan
package contactability

import (
	"fmt"
	"slices"
	"strings"

	base "github.com/we-clay/we-clay/gateway/internal/api/base"
	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
	"github.com/we-clay/we-clay/sdk/contact"
)

// specials 特殊账号列表
var specials = []string{
	"weixin", "qqmail", "qmessage", "tmessage", "floatbottle",
	"facebookapp", "qqfriend", "newsapp", "feedsapp", "masssendapp",
	"blogapp", "voipapp", "voicevoipapp", "voiceinputapp", "googlecontact",
	"fmessage", "medianote", "qqsync", "lbsapp", "shakeapp", "linkedinplugin",
	"gh_43f2581f6fd6", "gh_3dfda90e39d6", "gh_f0a92aa7146c",
	"gh_579db1f2cf89", "gh_b4af18eac3d5", "gh_e087bb5b95e6", "gh_051d9102de63",
}

func Build(data *contactapi.ModifyContact) (*contact.Contact, error) {
	if data == nil || data.GetUsername().GetValue() == "" {
		return nil, fmt.Errorf("联系人缺少标识")
	}
	var result contact.Contact

	result.Username = data.GetUsername().GetValue()
	result.Nickname = data.GetNickname().GetValue()
	result.Remark = data.GetRemark().GetValue()
	result.Alias = data.GetAlias()
	result.Avatar = data.GetBigAvatarUrl()
	if result.Avatar == "" {
		result.Avatar = data.GetSmallAvatarUrl()
	}
	result.Type = getType(data)
	mergeConversation(&result, data.BitMask, data.BitValue, data.ChatroomNotify)

	switch result.Type {
	case contact.ContactType_CONTACT_TYPE_UNSPECIFIED:
		return nil, fmt.Errorf("未知联系人 [%s]", data.GetUsername().GetValue())
	case contact.ContactType_CONTACT_TYPE_FRIEND, contact.ContactType_CONTACT_TYPE_SPECIAL:
		friend := contact.Friend{
			Country:   data.GetCountry(),
			Province:  data.GetProvince(),
			City:      data.GetCity(),
			Gender:    GetGender(data.GetGender()),
			Signature: data.GetSignature(),
		}
		result.Data = &contact.Contact_Friend{Friend: &friend}
	case contact.ContactType_CONTACT_TYPE_CHATROOM:
		// 微信未设置群名时，沿用成员列表顺序，优先群内昵称。
		if strings.TrimSpace(result.Nickname) == "" {
			names := []string{}
			for _, m := range data.GetMembers().GetList() {
				name := m.GetDisplayName()
				if strings.TrimSpace(name) == "" {
					name = m.GetNickname()
				}
				names = append(names, name)
			}
			result.Nickname = memberRoomName(names)
		}
		chatroom := contact.Chatroom{
			Owner: data.GetChatroomOwner(),
			// 使用实际成员数量，不能将群容量或旧版信息条数当成人数。
			MemberCount: data.GetMembers().GetCount(),
		}
		result.Data = &contact.Contact_Chatroom{Chatroom: &chatroom}
	case contact.ContactType_CONTACT_TYPE_OFFICIAL:

	}

	return &result, nil
}

func getType(data *contactapi.ModifyContact) contact.ContactType {
	// 自己
	//if data.GetUsername().GetValue() == ca.user.Username {
	//	return contact.TypeSelf
	//}
	// 特殊账号
	if slices.Contains(specials, data.GetUsername().GetValue()) {
		return contact.ContactType_CONTACT_TYPE_SPECIAL
	}
	// 群组
	if data.GetChatroomOwner() != "" || (strings.HasSuffix(data.GetUsername().GetValue(), "@chatroom") || strings.HasSuffix(data.GetUsername().GetValue(), "@im.chatroom")) {
		return contact.ContactType_CONTACT_TYPE_CHATROOM
	}
	// 公众号
	if strings.HasPrefix(data.GetUsername().GetValue(), "gh_") {
		return contact.ContactType_CONTACT_TYPE_OFFICIAL
	}

	return contact.ContactType_CONTACT_TYPE_FRIEND
}

func GetGender(data base.Gender) string {
	switch data {
	case 1:
		return "男"
	case 2:
		return "女"
	default:
		return "未知"
	}
}
