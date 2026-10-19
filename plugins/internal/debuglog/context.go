// 主要功能：在单次任务内传递调试日志接收器，不改变模型或 SDK 的公共接口。
package debuglog

import "context"

type key struct{}
type Sink func(string, map[string]any)

func WithSink(ctx context.Context, sink Sink) context.Context {
	return context.WithValue(ctx, key{}, sink)
}

func From(ctx context.Context) Sink {
	sink, _ := ctx.Value(key{}).(Sink)
	return sink
}
