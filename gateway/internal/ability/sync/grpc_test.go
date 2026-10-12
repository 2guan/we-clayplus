package sync

import (
	"context"
	"errors"
	"github.com/we-clay/we-clay/gateway/internal/api"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/corewire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync/atomic"
	"testing"
	"time"
)

type replayService struct {
	corewire.UnimplementedCoreServiceServer
	calls atomic.Int32
	acked chan string
}

func (s *replayService) Events(stream grpc.BidiStreamingServer[corewire.EventControl, corewire.CoreEvent]) error {
	s.calls.Add(1)
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	if first.GetSubscribe().GetVersion() != 1 {
		return status.Error(codes.FailedPrecondition, "version")
	}
	if err = stream.Send(&corewire.CoreEvent{Ready: true}); err != nil {
		return err
	}
	if err = stream.Send(&corewire.CoreEvent{EventId: "same-event", SyncJson: []byte(`{"new_message":[{"new_id":"9223372036854770000"}]}`)}); err != nil {
		return err
	}
	ack, err := stream.Recv()
	if err != nil {
		return err
	}
	s.acked <- ack.GetAck().GetEventId()
	return status.Error(codes.Unavailable, "simulated lost acknowledgement")
}
func TestEventReconnectDeduplicatesAndAcknowledgesAgain(t *testing.T) {
	fixture := &replayService{acked: make(chan string, 4)}
	server := coretest.NewService(t, fixture)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := api.NewGRPCClient(ctx, server.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- runEvents(ctx, client, func(r *messageapi.SyncResult) error {
			if len(r.GetNewMessage()) != 1 || r.GetNewMessage()[0].GetNewId() != 9223372036854770000 {
				return errors.New("ID precision lost")
			}
			calls.Add(1)
			return nil
		}, nil)
	}()
	for i := 0; i < 2; i++ {
		select {
		case id := <-fixture.acked:
			if id != "same-event" {
				t.Fatal("wrong ack")
			}
		case <-ctx.Done():
			t.Fatal("missing replay", ctx.Err())
		}
	}
	cancel()
	if err = <-done; err != nil || calls.Load() != 1 || fixture.calls.Load() != 2 {
		t.Fatal("replay was consumed twice or loop failed", calls.Load(), err)
	}
}
func TestFailedConsumerNeverAcknowledges(t *testing.T) {
	fixture := &replayService{acked: make(chan string, 4)}
	server := coretest.NewService(t, fixture)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := api.NewGRPCClient(ctx, server.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sentinel := errors.New("consumer failed")
	_, err = eventSession(ctx, client, func(*corewire.CoreEvent) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	select {
	case <-fixture.acked:
		t.Fatal("failed event was acknowledged")
	default:
	}
}
