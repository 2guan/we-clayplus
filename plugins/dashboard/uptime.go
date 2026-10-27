package dashboard

import (
	"context"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

func (s *Server) coreUptime(ctx context.Context) *int64 {
	if s.cfg.Mock {
		return nil
	}
	host, ok := s.host.(control.CoreRuntimeHost)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	elapsed, err := host.CoreUptime(ctx)
	if err != nil || elapsed < 0 {
		return nil
	}
	seconds := int64(elapsed / time.Second)
	return &seconds
}
