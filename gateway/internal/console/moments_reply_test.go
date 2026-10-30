package console

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/moments"
)

type momentReplyFixture struct {
	moments.Ability
	mode          string
	writes, reads int
	reply, typ    int32
}

func (f *momentReplyFixture) object() *moments.Moment {
	obj := &moments.Moment{Id: 123, Username: "fixture-author", Nickname: "动态作者", ObjectDescription: "<TimelineObject><contentDesc>原动态</contentDesc></TimelineObject>", CommentUserList: []*moments.CommentInfo{{Username: "fixture-peer", Nickname: "原评论作者", CommentId: 7, Content: "原评论"}}}
	if f.mode == "deleted" {
		obj.CommentUserList[0].DeleteFlag = 1
	}
	if f.mode == "old" {
		obj.CommentUserList = append(obj.CommentUserList, f.fresh())
	}
	return obj
}
func (f *momentReplyFixture) fresh() *moments.CommentInfo {
	return &moments.CommentInfo{Username: "fixture-self", Nickname: "本人昵称", CommentId: 8, Content: "本次回复", ReplyCommentId: 7, ReplyUsername: "fixture-peer"}
}
func (f *momentReplyFixture) Detail(uint64) (*moments.Detail_Response, error) {
	f.reads++
	obj := f.object()
	if f.mode == "readback" && f.writes > 0 {
		obj.CommentUserList = append(obj.CommentUserList, f.fresh())
	}
	return &moments.Detail_Response{BaseResult: &moments.BaseResult{}, Object: obj}, nil
}
func (f *momentReplyFixture) Comment(id uint64, text string, typ, reply int32) (*moments.Comment_Response, error) {
	f.writes++
	f.reply, f.typ = reply, typ
	if id != 123 || text != "本次回复" {
		return nil, fmt.Errorf("错误的目标或正文")
	}
	obj := f.object()
	if f.mode == "correct" {
		obj.CommentUserList = append(obj.CommentUserList, f.fresh())
	} else if f.mode == "partial" {
		fresh := f.fresh()
		fresh.ReplyUsername = ""
		obj.CommentUserList = []*moments.CommentInfo{fresh}
		obj.ObjectDescription = ""
	} else if f.mode == "wrong-relation" {
		fresh := f.fresh()
		fresh.ReplyCommentId = 0
		obj.CommentUserList = append(obj.CommentUserList, fresh)
	}
	return &moments.Comment_Response{BaseResult: &moments.BaseResult{}, Object: obj}, nil
}

func TestMomentReplyUsesVerifiedOriginalAndReceipt(t *testing.T) {
	oldM, oldC := moments.Instance, contact.Instance
	defer func() { moments.Instance, contact.Instance = oldM, oldC }()
	contact.Instance = postContacts{}
	for _, mode := range []string{"correct", "partial", "readback", "unknown", "old", "wrong-relation", "deleted", "wrong-author", "missing", "negative"} {
		t.Run(mode, func(t *testing.T) {
			f := &momentReplyFixture{mode: mode}
			moments.Instance = f
			reply := dashboard.MomentReply{CommentID: 7, AuthorID: "fixture-peer"}
			if mode == "wrong-author" {
				reply.AuthorID = "fixture-other"
			} else if mode == "missing" {
				reply.CommentID = 9
			} else if mode == "negative" {
				reply.CommentID = -1
			}
			params, _ := json.Marshal(reply)
			result, err := (&Bridge{}).interactMoment(control.Command{Operation: "moments-comment", ID: "123", Content: "本次回复", Params: params})
			if mode == "correct" || mode == "partial" || mode == "readback" {
				if err != nil || f.writes != 1 || f.reply != 7 || f.typ != 2 || result.Content != "原动态" {
					t.Fatal("未向实际接口传递原评论编号", result, err, f.writes, f.reply)
				}
				last := result.Comments[len(result.Comments)-1]
				if last.Author != "本人昵称" || last.ReplyToID != "fixture-peer" || last.ReplyTo != "原评论作者" || last.Content != "本次回复" {
					t.Fatal("回复身份或关系丢失", last)
				}
			} else if mode == "deleted" || mode == "wrong-author" || mode == "missing" || mode == "negative" {
				if err == nil || f.writes != 0 {
					t.Fatal("原评论无效仍进行了写操作", err, f.writes)
				}
			} else if err == nil || !strings.Contains(err.Error(), "不会自动重试") || f.writes != 1 || f.reads != 2 {
				t.Fatal("把不确定回执当成成功或重复写入", err, f.writes, f.reads)
			}
		})
	}
}

func TestRandomMomentGuardSkipsExistingAndPreservesReplyID(t *testing.T) {
	oldM, oldC := moments.Instance, contact.Instance
	defer func() { moments.Instance, contact.Instance = oldM, oldC }()
	contact.Instance = postContacts{}
	like := &interactionFixture{liked: true}
	moments.Instance = like
	params := json.RawMessage(`{"only_if_missing":true,"account":"fixture-self"}`)
	if _, err := (&Bridge{}).interactMoment(control.Command{Operation: "moments-like", ID: "123", Params: params}); err == nil || like.writes != 0 {
		t.Fatal("随机点赞取消了已有点赞", err, like.writes)
	}
	previous := &momentReplyFixture{mode: "old"}
	moments.Instance = previous
	if _, err := (&Bridge{}).interactMoment(control.Command{Operation: "moments-comment", ID: "123", Content: "本次回复", Params: params}); err == nil || previous.writes != 0 {
		t.Fatal("已有本人评论仍追加", err, previous.writes)
	}
	params = json.RawMessage(`{"only_if_missing":true,"account":"fixture-self","reply_comment_id":7,"reply_author_id":"fixture-peer"}`)
	if _, err := (&Bridge{}).interactMoment(control.Command{Operation: "moments-comment", ID: "123", Content: "本次回复", Params: params}); err == nil || previous.writes != 0 {
		t.Fatal("重复回复同一评论", err, previous.writes)
	}
	fresh := &momentReplyFixture{mode: "correct"}
	moments.Instance = fresh
	if _, err := (&Bridge{}).interactMoment(control.Command{Operation: "moments-comment", ID: "123", Content: "本次回复", Params: params}); err != nil || fresh.writes != 1 || fresh.reply != 7 {
		t.Fatal("未传递真实回复编号", err, fresh.writes, fresh.reply)
	}
	params = json.RawMessage(`{"only_if_missing":true,"account":"fixture-other"}`)
	if _, err := (&Bridge{}).interactMoment(control.Command{Operation: "moments-comment", ID: "123", Content: "本次回复", Params: params}); err == nil || fresh.writes != 1 {
		t.Fatal("跨账号发送", err, fresh.writes)
	}
}
