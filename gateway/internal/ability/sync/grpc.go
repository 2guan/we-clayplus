package sync

import (
	"context"
	"errors"
	"fmt"
	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	"github.com/we-clay/we-clay/gateway/internal/api"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/corewire"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"log/slog"
	"time"
)

func RunEvents(ctx context.Context, client *api.Client, hooks Hooks) error {
	p := &processor{hooks: hooks}
	return runEvents(ctx, client, func(result *messageapi.SyncResult) error {
		if !messageability.OutboundReady() {
			return fmt.Errorf("gateway 尚未完成初始化")
		}
		return p.handle(result)
	}, func(err error) { slog.Warn("core 事件流断开，等待重连", "code", status.Code(err)) }, hooks.Ready)
}

func runEvents(ctx context.Context, client *api.Client, consume func(*messageapi.SyncResult) error, report func(error), ready ...func() error) error {
	readyDone := false
	onReady := func() error {
		if readyDone {
			return nil
		}
		if len(ready) > 0 && ready[0] != nil {
			if err := ready[0](); err != nil {
				return &callbackError{err}
			}
		}
		readyDone = true
		return nil
	}
	seen := make(map[string]bool)
	var order []string
	delay := time.Second
	for ctx.Err() == nil {
		count, err := eventSession(ctx, client, func(event *corewire.CoreEvent) error {
			id := event.GetEventId()
			if id == "" {
				return fmt.Errorf("core event ID missing")
			}
			if seen[id] {
				return nil
			}
			var result messageapi.SyncResult
			if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(event.GetSyncJson(), &result); err != nil {
				return fmt.Errorf("invalid core sync event")
			}
			if err := consume(&result); err != nil {
				return err
			}
			seen[id] = true
			order = append(order, id)
			if len(order) > 4096 {
				delete(seen, order[0])
				order = order[1:]
			}
			return nil
		}, onReady)
		if ctx.Err() != nil {
			return nil
		}
		var failed *callbackError
		if errors.As(err, &failed) {
			return failed.err
		}
		switch status.Code(err) {
		case codes.Unauthenticated, codes.PermissionDenied, codes.Unimplemented, codes.FailedPrecondition:
			return fmt.Errorf("core 事件订阅失败: %w", err)
		}
		if report != nil {
			report(err)
		}
		if count > 0 {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
	return nil
}
func eventSession(parent context.Context, client *api.Client, consume func(*corewire.CoreEvent) error, onReady ...func() error) (int, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	stream, err := client.Events(ctx)
	if err != nil {
		return 0, err
	}
	if err = stream.Send(&corewire.EventControl{Control: &corewire.EventControl_Subscribe{Subscribe: &corewire.Subscribe{Version: corewire.Version}}}); err != nil {
		return 0, err
	}
	ready, err := stream.Recv()
	if err != nil {
		return 0, err
	}
	if !ready.GetReady() {
		return 0, fmt.Errorf("core event subscription missing ready frame")
	}
	if len(onReady) > 0 && onReady[0] != nil {
		if err := onReady[0](); err != nil {
			return 0, err
		}
	}
	count := 0
	for {
		event, err := stream.Recv()
		if err != nil {
			return count, err
		}
		if event.GetReady() {
			return count, fmt.Errorf("unexpected ready frame")
		}
		if err = consume(event); err != nil {
			return count, err
		}
		if err := ctx.Err(); err != nil {
			return count, err
		}
		count++
		if err = stream.Send(&corewire.EventControl{Control: &corewire.EventControl_Ack{Ack: &corewire.Ack{EventId: event.GetEventId()}}}); err != nil {
			return count, err
		}
	}
}
