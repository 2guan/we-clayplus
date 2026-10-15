// 文件名：payment_test.go
// 主要功能：验证支付内层业务码；未知、缺失和冲突结果不标记成功。
// 作者：2guan
package console

import "testing"

func TestPaymentTextConfirmation(t *testing.T) {
	for _, v := range []struct {
		text string
		ok   bool
	}{{`{"retcode":0}`, true}, {`{"retcode":"0"}`, true}, {`{"retcode":0,"err_code":5}`, false}, {`{}`, false}, {`null`, false}, {`{"retcode":null}`, false}, {`{"retcode":7}`, false}, {`invalid`, false}} {
		if e := paymentTextConfirmed(v.text); (e == nil) != v.ok {
			t.Fatalf("%q: %v", v.text, e)
		}
	}
}
