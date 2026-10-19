// 文件名：wechat_rich_test.go
// 主要功能：用虚构 XML 验证支付通知、引用、位置、拍一拍及表情包解析边界。
// 作者：2guan
package dashboard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRichMessageKinds(t *testing.T) {
	for _, tc := range []struct{ kind, raw, want, text string }{
		{"system", `<sysmsg type="pat"><pat><template>"${peer}" 拍了拍我</template></pat></sysmsg>`, "system", "拍了拍"},
		{"location", `<msg><location x="30.2" y="120.1" poiname="测试公园" label="测试街道"/></msg>`, "location", "测试公园"},
		{"link", `<msg><appmsg><type>2000</type><url>https://example.com/upgrade</url><wcpayinfo><paysubtype>1</paysubtype><feedesc>￥1.00</feedesc><transferid>hidden-ticket</transferid></wcpayinfo></appmsg></msg>`, "transfer", "1.00"},
		{"link", `<msg><appmsg><type>2001</type><wcpayinfo><receivertitle>测试祝福</receivertitle><nativeurl>wxpay://secret</nativeurl></wcpayinfo></appmsg></msg>`, "redpacket", "红包"},
		{"link", `<msg><appmsg><type>57</type><title>回复正文</title><refermsg><type>1</type><displayname>测试联系人</displayname><content>原文内容</content></refermsg></appmsg></msg>`, "quote", "回复正文"},
		{"emoji", `<msg><emoji productid="pack" cdnurl="https://wxapp.tc.qq.com/sample.gif"/></msg>`, "emoji", "表情包"},
		{"audio", `<msg><voicemsg voicelength="1929"/></msg>`, "audio", "语音"},
	} {
		m, keep := ParseWechatMessage(Message{Kind: tc.kind}, tc.raw)
		if !keep || m.Kind != tc.want || !strings.Contains(m.Content, tc.text) {
			t.Fatalf("%s: %+v", tc.kind, m)
		}
		b, _ := json.Marshal(m)
		if strings.Contains(string(b), "hidden-ticket") || strings.Contains(string(b), "wxpay://") {
			t.Fatal("支付票据泄露")
		}
		if m.Kind == "audio" && m.Duration != 2 {
			t.Fatal("毫秒时长转换错误")
		}
	}
	m, _ := ParseWechatMessage(Message{Kind: "emoji"}, `<msg><emoji cdnurl="http://localhost/private"/></msg>`)
	if m.Rich.Image != "" {
		t.Fatal("表情地址绕过白名单")
	}
	m, _ = ParseWechatMessage(Message{Kind: "location"}, `<msg><location x="NaN" y="200"/></msg>`)
	if m.Rich.Latitude != nil {
		t.Fatal("无效坐标被接受")
	}
	m = resolveNotice(State{Contacts: []Contact{{ID: "peer", Remark: "好友"}}}, Message{Kind: "system", Content: "${peer} 拍了拍我"})
	if m.Content != "好友 拍了拍我" {
		t.Fatal(m.Content)
	}
	raw := `<msg><appmsg><type>2001</type></appmsg></msg>`
	m, _ = ParseWechatMessage(Message{Kind: "text", Content: raw}, raw)
	if m.Kind != "text" || m.Content != raw {
		t.Fatal("普通 XML 被解释成支付")
	}
}
