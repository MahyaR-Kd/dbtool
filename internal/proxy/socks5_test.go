package proxy

import (
	"net"
	"testing"
	"time"
)

func TestRelayReturnsWhenBothCopiesFail(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, b := net.Pipe()
	a.Close()
	b.Close()
	done := make(chan struct{})
	go func() { relay(a, b); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay hung after copy errors")
	}
}
