package console

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"

	momentsapi "github.com/we-clay/we-clay/gateway/internal/api/moments"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/moments"
	"google.golang.org/protobuf/proto"
)

func (b *Bridge) syncMomentNotifications(params json.RawMessage) (dashboard.MomentNotificationSync, error) {
	out := dashboard.MomentNotificationSync{Items: []dashboard.MomentNotification{}}
	var q struct {
		Key []byte `json:"key"`
	}
	if len(params) > 0 && json.Unmarshal(params, &q) != nil {
		return out, fmt.Errorf("通知同步参数无效")
	}
	if moments.Instance == nil || contact.Instance == nil || contact.Instance.GetSelf() == nil {
		return out, fmt.Errorf("朋友圈尚未就绪")
	}
	self := contact.Instance.GetSelf().Username
	r, err := moments.Instance.Sync(q.Key)
	if err != nil {
		return out, err
	}
	if r == nil || r.GetBaseResult() == nil || r.GetBaseResult().GetCode() != 0 {
		return out, fmt.Errorf("朋友圈互动通知同步失败")
	}
	out.Key = r.GetKey().GetData()
	if r.GetContinueFlag() != 0 {
		out.Notice = "还有互动通知等待继续同步"
	}
	for _, command := range r.GetCommands() {
		if command.GetId() != 46 {
			continue
		}
		var event momentsapi.ActionNotification
		if err := proto.Unmarshal(command.GetBuffer().GetData(), &event); err != nil {
			return out, fmt.Errorf("朋友圈互动通知格式无效")
		}
		a, ref := event.GetCurrentAction(), event.GetReferAction()
		if event.GetId() == 0 || a.GetSender() == "" || a.GetSender() == self || a.GetCreateTime() == 0 || (a.GetReceiver() != self && ref.GetSender() != self) {
			continue
		}
		kind := "comment"
		if a.GetType() == 1 {
			kind = "like"
		} else if a.GetType() != 2 {
			continue
		}
		fingerprint := fmt.Sprintf("%s\x00%d\x00%s\x00%d\x00%d\x00%d\x00%d", self, event.GetId(), a.GetSender(), a.GetCreateTime(), a.GetCommentId(), a.GetCommentId2(), a.GetType())
		out.Items = append(out.Items, dashboard.MomentNotification{CommentID: a.GetCommentId(), ReplyToSelf: ref.GetSender() == self && a.GetReceiver() == self && ref.GetType() == 2 && ref.GetContent() != "", ID: fmt.Sprintf("%x", sha256.Sum256([]byte(fingerprint))), MomentID: strconv.FormatUint(event.GetId(), 10), ActorID: a.GetSender(), Actor: a.GetSenderNickname(), Kind: kind, Created: int64(a.GetCreateTime()), Content: a.GetContent(), Original: ref.GetContent(), ReplyTo: a.GetReceiverNickname()})
	}
	return out, nil
}
