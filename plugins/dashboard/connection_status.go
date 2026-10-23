package dashboard

import "fmt"

// SetConnectionStatus changes only the live connection state. It preserves the
// account identity, archived conversations and per-account settings while the
// independently managed core is stopped or reconnecting.
func (s *Server) SetConnectionStatus(status string) error {
	if status != "online" && status != "offline" {
		return fmt.Errorf("未知连接状态")
	}
	err := s.store.change(func(v *State) error {
		v.Account.Status = status
		return nil
	})
	if err == nil {
		s.notify()
	}
	return err
}
