package api

import "google.golang.org/protobuf/proto"

func TransformProto(source, target proto.Message) error {
	marshal, err := proto.Marshal(source)
	if err != nil {
		return err
	}
	return proto.Unmarshal(marshal, target)
}
