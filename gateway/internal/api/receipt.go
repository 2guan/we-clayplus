package api

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// 校验 core 回执中的状态字段，不能把 protobuf 缺省值 0 当成明确成功。
// 不输出响应体、票据或账号信息，也不自动重试结果不确定的写操作。
func validateReceipt(result proto.Message, path string, allowed []int32) error {
	m := result.ProtoReflect()
	if !m.IsValid() {
		return errors.New("core response missing")
	}
	fields := m.Descriptor().Fields()
	if base := fields.ByName("base_response"); base != nil {
		if !m.Has(base) {
			return errors.New("core response missing business status; result is unconfirmed")
		}
		b := m.Get(base).Message()
		code := b.Descriptor().Fields().ByName("code")
		if code == nil || !b.Has(code) {
			return errors.New("core response missing business code; result is unconfirmed")
		}
		n := int32(b.Get(code).Int())
		if n == 0 {
			return nil
		}
		for _, v := range allowed {
			if n == v {
				return nil
			}
		}
		return fmt.Errorf("core business request failed (code %d)", n)
	}
	// oplog 在联系人与群聊有不同 Go 类型，但两者使用同样的 code/result 契约。
	code, nested := fields.ByName("code"), fields.ByName("result")
	if code != nil && nested != nil && nested.Kind() == protoreflect.MessageKind && code.Kind() == protoreflect.Int32Kind {
		if !m.Has(code) {
			return errors.New("core operation status missing; result is unconfirmed")
		}
		if n := m.Get(code).Int(); n != 0 {
			return fmt.Errorf("core operation failed (code %d)", n)
		}
		if !m.Has(nested) {
			return errors.New("core command receipt missing; result is unconfirmed")
		}
		r := m.Get(nested).Message()
		inner := r.Descriptor().Fields().ByName("code")
		if inner != nil && r.Has(inner) {
			if n := r.Get(inner).Int(); n != 0 {
				return fmt.Errorf("core command failed (code %d)", n)
			}
			return nil
		}
		// core 的备注/保存接口会验证 packed 命令状态并读回；该状态属于
		// protobuf unknown 字段，经过 core 的 JSON 转换后不再出现。只在这些
		// 已核实的路由接受 count=1，不能要求一个已被序列化丢弃的 scalar code。
		// 群名在 chatroom 远程适配中补充只读核对后才返回成功。
		verified := strings.HasPrefix(path, "/api/contacts/remark/") || strings.HasPrefix(path, "/api/chatroom/remark/") || strings.HasPrefix(path, "/api/chatroom/contact-list/") || strings.HasPrefix(path, "/api/chatroom/name/")
		count := r.Descriptor().Fields().ByName("count")
		if verified && count != nil && r.Has(count) && r.Get(count).Uint() == 1 {
			return nil
		}
		return errors.New("core command status missing; result is unconfirmed")
	}
	return nil
}
