// 文件名：contact_projection_test.go
// 主要功能：验证首次联系人与重放均投影到控制台，投影失败阻止事件确认。
// 作者：2guan
package sync

import (
	"errors"
	"testing"

	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/contact"
	pluginsdk "github.com/we-clay/we-clay/sdk/plugin"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestNewContactProjectionAndFailure(t *testing.T) {
	contactability.LoadCache(t.TempDir())
	calls, events := 0, 0
	failure := errors.New("projection failed")
	p := processor{hooks: Hooks{WithoutPlugins: true, Contacts: func() error {
		calls++
		items := contact.Instance.List()
		if len(items) != 1 || items[0].Username != "fixture-new-peer" || items[0].Avatar == "" || items[0].Type != contact.ContactType_CONTACT_TYPE_FRIEND {
			t.Fatal("联系人未完整更新就通知控制台")
		}
		return failure
	}, Event: func(*pluginsdk.Event) error { events++; return nil }}}
	avatar := "https://wx.qlogo.cn/fixture"
	data := []*messageapi.ModifyContact{{Username: wrapperspb.String("fixture-new-peer"), Nickname: wrapperspb.String("测试好友"), SmallAvatarUrl: &avatar}}
	if err := p.handleContact(data); !errors.Is(err, failure) || calls != 1 {
		t.Fatal("首次联系人未投影或失败未阻止确认", err)
	}
	// 第一次出现没有修改事件；重放即使没有字段变化，也必须重试投影。
	failure = nil
	if err := p.handleContact(data); err != nil || calls != 2 || events != 0 {
		t.Fatal("重放未重新投影或改变了原有事件语义", err)
	}
	if err := p.handleContact(nil); err != nil || calls != 2 {
		t.Fatal("空增量重复投影", err)
	}
}
