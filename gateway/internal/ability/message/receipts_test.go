package messageability

import (
	"sync"
	"testing"
)

func TestReceiptCacheBoundedAndDoesNotInventClientID(t *testing.T) {
	var c receiptCache
	for id := uint64(1); id <= maxRevokeReceipts+1; id++ {
		c.remember("test-peer", id, id+100, 42)
	}
	if _, ok := c.get("test-peer", 1); ok {
		t.Fatal("old receipt not evicted")
	}
	if r, ok := c.get("test-peer", maxRevokeReceipts+1); !ok || r.created != 42 {
		t.Fatal("new receipt missing")
	}
	if c.remember("test-peer", 99999, numericClientID("peer_1789890000"), 42) {
		t.Fatal("invented numeric client id")
	}
	if numericClientID("18446744073709551615") != 18446744073709551615 {
		t.Fatal("client ID lost precision")
	}
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 100 {
			c.remember("test-peer", 123, 456, 42)
			c.clear()
		}
	})
	workers.Go(func() {
		for range 100 {
			c.get("test-peer", 123)
		}
	})
	workers.Wait()
	c.clear()
	if _, ok := c.get("test-peer", 123); ok {
		t.Fatal("receipt not cleared")
	}
}
