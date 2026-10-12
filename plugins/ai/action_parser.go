// 文件名: plugins/ai/action_parser.go
// 主要功能: 大模型 Action Directives 动作指令标签解析、参数提取与文本纯净化
// 作者: 2guan

package main

import (
	"regexp"
	"strings"
)

// ParsedActionResult 动作解析结果集合
type ParsedActionResult struct {
	CleanText          string
	IsVoice            bool
	PatTargets         []string
	AddFriendTasks     []AddFriendTask
	VerifyFriendTasks  []VerifyFriendTask
	RevokeTasks        []RevokeTask
	AtMentions         []string
	SetRemarkTasks     []SetRemarkTask
	Announcements      []AnnouncementTask
	KickTasks          []KickTask
}

// AddFriendTask 加好友任务结构体
type AddFriendTask struct {
	UserID string
	Greet  string
}

// VerifyFriendTask 审核好友任务
type VerifyFriendTask struct {
	V1    string
	V2    string
	Greet string
}

// RevokeTask 消息撤回任务
type RevokeTask struct {
	MsgID       string
	Explanation string
}

// SetRemarkTask 设置备注任务
type SetRemarkTask struct {
	UserID string
	Remark string
}

// AnnouncementTask 设置群公告任务
type AnnouncementTask struct {
	ChatroomID string
	Content    string
}

// KickTask 移出群成员任务
type KickTask struct {
	ChatroomID string
	UserID     string
	Reason     string
}

var (
	voiceTagRegex   = regexp.MustCompile(`\[VOICE\]`)
	patTagRegex     = regexp.MustCompile(`\[ACTION:PAT:([^\]]+)\]`)
	addFriendRegex  = regexp.MustCompile(`\[ACTION:ADD_FRIEND:([^:]+):([^\]]+)\]`)
	verifyRegex     = regexp.MustCompile(`\[ACTION:VERIFY_FRIEND:([^:]+):([^:]+):([^\]]+)\]`)
	revokeRegex     = regexp.MustCompile(`\[ACTION:REVOKE:([^:]+):([^\]]+)\]`)
	remarkRegex     = regexp.MustCompile(`\[ACTION:SET_REMARK:([^:]+):([^\]]+)\]`)
	announceRegex   = regexp.MustCompile(`\[ACTION:SET_ANNOUNCEMENT:([^:]+):([^\]]+)\]`)
	kickRegex       = regexp.MustCompile(`\[ACTION:KICK:([^:]+):([^:]+):([^\]]+)\]`)
	atTagRegex      = regexp.MustCompile(`\[@([^\]]+)\]`)
)

// ParseActionDirectives 解析模型输出并剥离标签
func ParseActionDirectives(raw string) *ParsedActionResult {
	res := &ParsedActionResult{}
	clean := raw

	// 1. 语音回复指令 [VOICE]
	if voiceTagRegex.MatchString(clean) {
		res.IsVoice = true
		clean = voiceTagRegex.ReplaceAllString(clean, "")
	}

	// 2. 拍一拍指令 [ACTION:PAT:target_id]
	for _, match := range patTagRegex.FindAllStringSubmatch(clean, -1) {
		if len(match) > 1 {
			target := strings.TrimSpace(match[1])
			if target != "" {
				res.PatTargets = append(res.PatTargets, target)
			}
		}
	}
	clean = patTagRegex.ReplaceAllString(clean, "")

	// 3. 群内加好友 [ACTION:ADD_FRIEND:user_id:greet]
	for _, match := range addFriendRegex.FindAllStringSubmatch(clean, -1) {
		if len(match) > 2 {
			res.AddFriendTasks = append(res.AddFriendTasks, AddFriendTask{
				UserID: strings.TrimSpace(match[1]),
				Greet:  strings.TrimSpace(match[2]),
			})
		}
	}
	clean = addFriendRegex.ReplaceAllString(clean, "")

	// 4. 通过好友验证 [ACTION:VERIFY_FRIEND:v1:v2:greet]
	for _, match := range verifyRegex.FindAllStringSubmatch(clean, -1) {
		if len(match) > 3 {
			res.VerifyFriendTasks = append(res.VerifyFriendTasks, VerifyFriendTask{
				V1:    strings.TrimSpace(match[1]),
				V2:    strings.TrimSpace(match[2]),
				Greet: strings.TrimSpace(match[3]),
			})
		}
	}
	clean = verifyRegex.ReplaceAllString(clean, "")

	// 5. 消息撤回 [ACTION:REVOKE:msg_id:explanation]
	for _, match := range revokeRegex.FindAllStringSubmatch(clean, -1) {
		if len(match) > 2 {
			res.RevokeTasks = append(res.RevokeTasks, RevokeTask{
				MsgID:       strings.TrimSpace(match[1]),
				Explanation: strings.TrimSpace(match[2]),
			})
		}
	}
	clean = revokeRegex.ReplaceAllString(clean, "")

	// 6. 修改好友备注 [ACTION:SET_REMARK:user_id:remark]
	for _, match := range remarkRegex.FindAllStringSubmatch(clean, -1) {
		if len(match) > 2 {
			res.SetRemarkTasks = append(res.SetRemarkTasks, SetRemarkTask{
				UserID: strings.TrimSpace(match[1]),
				Remark: strings.TrimSpace(match[2]),
			})
		}
	}
	clean = remarkRegex.ReplaceAllString(clean, "")

	// 7. 设置群公告 [ACTION:SET_ANNOUNCEMENT:room_id:content]
	for _, match := range announceRegex.FindAllStringSubmatch(clean, -1) {
		if len(match) > 2 {
			res.Announcements = append(res.Announcements, AnnouncementTask{
				ChatroomID: strings.TrimSpace(match[1]),
				Content:    strings.TrimSpace(match[2]),
			})
		}
	}
	clean = announceRegex.ReplaceAllString(clean, "")

	// 8. 踢人 [ACTION:KICK:room_id:user_id:reason]
	for _, match := range kickRegex.FindAllStringSubmatch(clean, -1) {
		if len(match) > 3 {
			res.KickTasks = append(res.KickTasks, KickTask{
				ChatroomID: strings.TrimSpace(match[1]),
				UserID:     strings.TrimSpace(match[2]),
				Reason:     strings.TrimSpace(match[3]),
			})
		}
	}
	clean = kickRegex.ReplaceAllString(clean, "")

	// 9. @群友 [@nickname] -> 转换为标准的 @nickname 格式
	for _, match := range atTagRegex.FindAllStringSubmatch(clean, -1) {
		if len(match) > 1 {
			nick := strings.TrimSpace(match[1])
			if nick != "" {
				res.AtMentions = append(res.AtMentions, nick)
			}
		}
	}
	// 将 [@nickname] 替换为纯文本 @nickname
	clean = atTagRegex.ReplaceAllString(clean, "@$1 ")

	res.CleanText = strings.TrimSpace(clean)
	return res
}
