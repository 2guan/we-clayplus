// 文件名：errors.go
// 主要功能：定义消息回执缺失错误，不将其误判为发送失败。
// 作者：2guan
package message

import "errors"

// ErrRevokeReceiptUnavailable 表示当前 gateway 运行没有保存该会话/消息的完整发送回执。
// 不应把此错误当作消息发送失败而自动重发；旧消息或缺少撤回字段的媒体也会返回此错误。
var ErrRevokeReceiptUnavailable = errors.New("缺少完整撤回回执：未保存此消息的原始 client_id 和 create_time，无法可靠撤回")
