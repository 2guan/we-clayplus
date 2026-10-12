// Package change 提供通用变更检测与事件发布能力。
package change

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	pluginsdk "github.com/we-clay/we-clay/sdk/plugin"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	ActionCreate = "create"
	ActionModify = "modify"
	ActionDelete = "delete"
)

// Subject 描述一个可发布的变更主体。
type Subject struct {
	Domain  string
	Action  string
	Subject string
	Parent  string
	Old     proto.Message
	New     proto.Message
}

// Detect 对比两个 proto 消息并生成通用变更事件。
func Detect(subject Subject) (*pluginsdk.ChangeEvent, bool, error) {
	changes, err := Diff(subject.Old, subject.New)
	if err != nil {
		return nil, false, err
	}
	if len(changes) == 0 {
		return nil, false, nil
	}

	event := &pluginsdk.ChangeEvent{
		Domain:   subject.Domain,
		Action:   subject.Action,
		Subject:  subject.Subject,
		Parent:   subject.Parent,
		Changes:  changes,
		OldValue: marshalMessage(subject.Old),
		NewValue: marshalMessage(subject.New),
	}
	return event, true, nil
}

// Diff 返回两个 proto 消息之间的字段级差异。
func Diff(oldMessage, newMessage proto.Message) ([]*pluginsdk.FieldChange, error) {
	if oldMessage == nil && newMessage == nil {
		return nil, nil
	}
	if oldMessage != nil && newMessage != nil {
		oldName := oldMessage.ProtoReflect().Descriptor().FullName()
		newName := newMessage.ProtoReflect().Descriptor().FullName()
		if oldName != newName {
			return nil, fmt.Errorf("proto message 类型不一致: %s != %s", oldName, newName)
		}
	}
	return diffMessage("", messageReflect(oldMessage), messageReflect(newMessage)), nil
}

func messageReflect(message proto.Message) protoreflect.Message {
	if message == nil {
		return nil
	}
	return message.ProtoReflect()
}

func diffMessage(prefix string, oldMessage, newMessage protoreflect.Message) []*pluginsdk.FieldChange {
	descriptor := messageDescriptor(oldMessage, newMessage)
	if descriptor == nil {
		return nil
	}

	var changes []*pluginsdk.FieldChange
	fields := descriptor.Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		path := joinPath(prefix, string(field.JSONName()))
		oldHas, newHas := hasField(oldMessage, field), hasField(newMessage, field)

		switch {
		case !oldHas && !newHas:
			continue
		case !oldHas:
			changes = append(changes, buildChange(path, ActionCreate, protoreflect.Value{}, valueOf(newMessage, field), field))
		case !newHas:
			changes = append(changes, buildChange(path, ActionDelete, valueOf(oldMessage, field), protoreflect.Value{}, field))
		default:
			oldValue, newValue := valueOf(oldMessage, field), valueOf(newMessage, field)
			changes = append(changes, diffField(path, field, oldValue, newValue)...)
		}
	}
	return changes
}

func diffField(path string, field protoreflect.FieldDescriptor, oldValue, newValue protoreflect.Value) []*pluginsdk.FieldChange {
	if field.IsList() {
		if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
			return diffList(path, field, oldValue.List(), newValue.List())
		}
		if !valueEqual(field, oldValue, newValue) {
			return []*pluginsdk.FieldChange{buildChange(path, ActionModify, oldValue, newValue, field)}
		}
		return nil
	}
	if field.IsMap() {
		if !valueEqual(field, oldValue, newValue) {
			return []*pluginsdk.FieldChange{buildChange(path, ActionModify, oldValue, newValue, field)}
		}
		return nil
	}
	if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
		return diffMessage(path, oldValue.Message(), newValue.Message())
	}
	if !valueEqual(field, oldValue, newValue) {
		return []*pluginsdk.FieldChange{buildChange(path, ActionModify, oldValue, newValue, field)}
	}
	return nil
}

func diffList(path string, field protoreflect.FieldDescriptor, oldList, newList protoreflect.List) []*pluginsdk.FieldChange {
	oldLen, newLen := oldList.Len(), newList.Len()
	limit := oldLen
	if newLen < limit {
		limit = newLen
	}

	var changes []*pluginsdk.FieldChange
	for index := 0; index < limit; index++ {
		itemPath := path + "." + strconv.Itoa(index)
		changes = append(changes, diffMessage(itemPath, oldList.Get(index).Message(), newList.Get(index).Message())...)
	}
	for index := limit; index < newLen; index++ {
		itemPath := path + "." + strconv.Itoa(index)
		changes = append(changes, buildChange(itemPath, ActionCreate, protoreflect.Value{}, newList.Get(index), field))
	}
	for index := limit; index < oldLen; index++ {
		itemPath := path + "." + strconv.Itoa(index)
		changes = append(changes, buildChange(itemPath, ActionDelete, oldList.Get(index), protoreflect.Value{}, field))
	}
	return changes
}

func messageDescriptor(oldMessage, newMessage protoreflect.Message) protoreflect.MessageDescriptor {
	if newMessage != nil {
		return newMessage.Descriptor()
	}
	if oldMessage != nil {
		return oldMessage.Descriptor()
	}
	return nil
}

func hasField(message protoreflect.Message, field protoreflect.FieldDescriptor) bool {
	if message == nil {
		return false
	}
	if field.IsMap() {
		return message.Get(field).Map().Len() > 0
	}
	if field.IsList() {
		return message.Get(field).List().Len() > 0
	}
	return message.Has(field)
}

func valueOf(message protoreflect.Message, field protoreflect.FieldDescriptor) protoreflect.Value {
	if message == nil {
		return protoreflect.Value{}
	}
	return message.Get(field)
}

func valueEqual(field protoreflect.FieldDescriptor, oldValue, newValue protoreflect.Value) bool {
	oldInterface := valueInterface(field, oldValue)
	newInterface := valueInterface(field, newValue)
	return fmt.Sprint(oldInterface) == fmt.Sprint(newInterface)
}

func buildChange(path, action string, oldValue, newValue protoreflect.Value, field protoreflect.FieldDescriptor) *pluginsdk.FieldChange {
	return &pluginsdk.FieldChange{
		Path:     path,
		Action:   action,
		OldValue: structValue(valueInterface(field, oldValue)),
		NewValue: structValue(valueInterface(field, newValue)),
	}
}

func valueInterface(field protoreflect.FieldDescriptor, value protoreflect.Value) any {
	if !value.IsValid() {
		return nil
	}
	if field.IsList() || field.IsMap() {
		return fmt.Sprint(value.Interface())
	}
	if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
		bytes, err := protojson.Marshal(value.Message().Interface())
		if err == nil {
			var data any
			if json.Unmarshal(bytes, &data) == nil {
				return data
			}
		}
		return value.Interface()
	}
	switch field.Kind() {
	case protoreflect.BoolKind:
		return value.Bool()
	case protoreflect.EnumKind:
		return string(field.Enum().Values().ByNumber(value.Enum()).Name())
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return value.Int()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return value.Uint()
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return value.Float()
	case protoreflect.StringKind:
		return value.String()
	case protoreflect.BytesKind:
		return base64.StdEncoding.EncodeToString(value.Bytes())
	default:
		return value.Interface()
	}
}

func structValue(value any) *structpb.Value {
	if value == nil {
		return structpb.NewNullValue()
	}
	result, err := structpb.NewValue(value)
	if err != nil {
		return structpb.NewStringValue(fmt.Sprint(value))
	}
	return result
}

func marshalMessage(message proto.Message) []byte {
	if message == nil {
		return nil
	}
	bytes, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(message)
	if err != nil {
		return nil
	}
	return bytes
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	if name == "" {
		return prefix
	}
	return strings.Join([]string{prefix, name}, ".")
}
