// 文件名：refusal.go
// 主要功能：将服务方明确拒绝识别为调用失败，避免把拒绝说明当成有效报告。
// 作者：2guan
package ai

import (
	"errors"
	"strings"
)

type RefusalError struct{}

func (*RefusalError) Error() string {
	return "模型服务拒绝了本次生成（content_filter/refusal），未完成请求"
}
func Refused(err error) bool { var refusal *RefusalError; return errors.As(err, &refusal) }

// 只匹配已观测到的完整拒绝说明；报告正文引用该句、讨论风险均不算拒绝。
func RefusalText(text string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(text), ".。"), "The request was rejected because it was considered high risk")
}
func checkRefusal(result Result, reason, refusal string) (Result, error) {
	if reason != "content_filter" && refusal == "" && !RefusalText(result.Text) {
		return result, nil
	}
	if RefusalText(result.Text) || strings.TrimSpace(result.Text) == strings.TrimSpace(refusal) {
		result.Text = ""
	}
	return result, &RefusalError{}
}
func checkResult(result Result, err error) (Result, error) {
	if err != nil {
		return result, err
	}
	return checkRefusal(result, "", "")
}
