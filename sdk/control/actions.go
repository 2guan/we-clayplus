// 文件名：actions.go
// 主要功能：声明 AI 可调用的有限操作与参数；不包含 gateway 实现或执行权限。
package control

// AIAction 是对话适配契约，Parameters 同时用于模型描述和服务端参数检查。
type AIAction struct {
	Operation  string         `json:"operation"`
	Parameters map[string]any `json:"parameters"`
	Write      bool           `json:"write"`
	Scope      string         `json:"scope"` // current、target、self、origin（建群）
	Legacy     bool           `json:"legacy,omitempty"`
}

func str(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "maxLength": 8000}
}
func refs(desc string) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": str("已查询得到的引用"), "maxItems": 50, "uniqueItems": true}
}
func schema(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}
func action(op, scope string, write bool, props map[string]any, required ...string) AIAction {
	if props == nil {
		props = map[string]any{}
	}
	if scope == "target" {
		props["target_ref"] = str("查询结果中的联系人或群引用；省略为当前会话，不接受猜测的ID")
	}
	return AIAction{Operation: op, Scope: scope, Write: write, Parameters: schema(props, required...)}
}

// AIActions 仅列真实 SDK 有适配路径的能力。资金、安全设备及占位接口不在此表。
var AIActions = func() map[string]AIAction {
	out := map[string]AIAction{}
	add := func(a AIAction) { out[a.Operation] = a }
	for _, op := range []string{"contact.SetRemark", "chatroom.SetName", "chatroom.SetAnnouncement"} {
		a := action(op, "target", true, map[string]any{"content": str("新的备注、群名或公告全文")}, "content")
		a.Legacy = true
		add(a)
	}
	add(action("message.Send", "target", true, map[string]any{
		"content": str("消息正文；文件名或链接标题"), "kind": map[string]any{"type": "string", "enum": []string{"text", "image", "audio", "video", "file", "link", "card", "location"}},
		"media_ref": str("已查询的媒体消息引用"), "member_refs": refs("需要@的当前群成员引用，仅文字"),
		"url": str("链接地址"), "person_ref": str("名片对应联系人引用"),
		"latitude": map[string]any{"type": "number", "minimum": -90, "maximum": 90}, "longitude": map[string]any{"type": "number", "minimum": -180, "maximum": 180},
	}, "content"))
	for _, op := range []string{"message.Forward", "message.Revoke", "message.Download"} {
		scope := "target"
		if op == "message.Download" {
			scope = "current"
		}
		add(action(op, scope, op != "message.Download", map[string]any{"message_ref": str("已查询的当前会话消息引用")}, "message_ref"))
	}
	add(action("message.Pat", "target", true, map[string]any{"person_ref": str("指定联系人或当前群成员引用，私聊可省略")}))
	add(action("contact.Get", "target", false, nil))
	add(action("contact.List", "self", false, map[string]any{"query": str("姓名、备注或微信号，分页返回"), "kind": map[string]any{"type": "string", "enum": []string{"friend", "group", "all"}}, "offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 100000}}))
	add(action("contact.GetSelf", "self", false, nil))
	add(action("contact.Search", "self", false, map[string]any{"query": str("用户明确提供的微信号或手机号")}, "query"))
	add(action("contact.AddFriend", "target", true, map[string]any{"content": str("好友验证消息")}, "target_ref", "content"))
	add(action("contact.VerifyFriend", "self", true, map[string]any{"request_ref": str("待处理申请引用")}, "request_ref"))
	add(action("chatroom.Create", "origin", true, map[string]any{"member_refs": refs("明确选定的好友引用，至少两位")}, "member_refs"))
	for _, op := range []string{"chatroom.GetInfo", "chatroom.GetQRCode"} {
		add(action(op, "target", false, nil))
	}
	add(action("chatroom.ListMembers", "target", false, map[string]any{"query": str("成员昵称或群内昵称"), "offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 100000}}))
	for _, op := range []string{"chatroom.GetMember", "chatroom.GetMembersDetail", "chatroom.InviteMember", "chatroom.SetAdmin"} {
		add(action(op, "target", op == "chatroom.InviteMember" || op == "chatroom.SetAdmin", map[string]any{"member_refs": refs("明确选定的联系人或群成员引用")}, "member_refs"))
	}
	add(action("chatroom.SetRemark", "target", true, map[string]any{"content": str("仅本人可见的群备注全文")}, "content"))
	add(action("chatroom.Save", "target", true, map[string]any{"save": map[string]any{"type": "boolean"}}, "save"))
	add(action("moments.Timeline", "self", false, map[string]any{"cursor": str("上页返回的游标")}))
	add(action("moments.UserPage", "target", false, map[string]any{"cursor": str("上页返回的游标")}))
	for _, op := range []string{"moments.Detail", "moments.Like", "moments.Unlike", "moments.Comment"} {
		props := map[string]any{"moment_ref": str("本任务已读取的朋友圈引用")}
		req := []string{"moment_ref"}
		if op == "moments.Comment" {
			props["content"] = str("评论正文")
			props["comment_ref"] = str("回复已读取评论时使用的引用")
			req = append(req, "content")
		}
		add(action(op, "target", op != "moments.Detail", props, req...))
	}
	add(action("moments.Post", "self", true, map[string]any{"content": str("发布正文"), "media_refs": refs("选定的图片消息引用，最多9张"), "blacklist_refs": refs("不可见的好友引用"), "with_refs": refs("提到的好友引用")}, "content"))
	for _, op := range []string{"label.List", "label.Add", "label.Delete", "label.Update", "label.ModifyContactLabels"} {
		props := map[string]any{}
		req := []string{}
		if op == "label.Add" || op == "label.Update" {
			props["content"] = str("标签名称")
			req = append(req, "content")
		}
		if op == "label.Delete" || op == "label.Update" {
			props["label_ref"] = str("已读取的标签引用")
			req = append(req, "label_ref")
		}
		if op == "label.ModifyContactLabels" {
			props["member_refs"] = refs("要修改的一位好友引用，多位好友逐次调用")
			props["mode"] = map[string]any{"type": "string", "enum": []string{"add", "remove", "replace"}, "description": "add添加、remove移除、replace完整替换，默认add。只有明确要求完整替换或清空时才用replace"}
			props["label_refs"] = refs("要添加、移除或完整替换的标签引用；空集合清空仅用于replace")
			req = append(req, "member_refs", "label_refs")
		}
		scope := "self"
		if op == "label.ModifyContactLabels" {
			scope = "target"
		}
		add(action(op, scope, op != "label.List", props, req...))
	}
	add(action("favor.GetInfo", "self", false, nil))
	add(action("favor.Sync", "self", false, map[string]any{"cursor": str("上次同步返回的游标")}))
	add(action("miniapp.GetRecord", "self", false, nil))
	add(action("user.GetProfile", "self", false, nil))
	add(action("user.GetQRCode", "self", false, nil))
	add(action("user.UploadAvatar", "self", true, map[string]any{"media_ref": str("明确指定的图片消息引用")}, "media_ref"))
	return out
}()

// AIBackendOperations 由后台配套完成，不作为模型自由调用工具。
var AIBackendOperations = map[string]bool{
	"contact.GetOwner": true, "moments.Upload": true, "moments.Sync": true,
	"cdn.UploadImage": true, "cdn.UploadVideo": true, "cdn.DownloadImage": true, "cdn.DownloadVideo": true, "cdn.UploadMomentsImage": true, "cdn.DownloadVideoCover": true,
	"report.StartTyping": true, "report.StopTyping": true,
}

// AIDeferredOperations 已接通 SDK，但尚无受控的 AI 任务适配；不向模型暴露。
// 语音转写和收藏单项操作仍待真实账号验证。
var AIDeferredOperations = map[string]bool{
	"message.TranscribeVoice":  true,
	"chatroom.SetDisplayName":  true,
	"favor.GetItem":            true,
	"favor.Delete":             true,
	"cdn.UploadMomentsVideo":   true,
	"cdn.DownloadMomentsVideo": true,
}
