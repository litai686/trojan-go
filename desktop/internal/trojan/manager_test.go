package trojan

import (
	"net"
	"testing"

	"github.com/litai686/trojan-go/desktop/internal/store"
)

func TestProxyURL(t *testing.T) {
	m := NewManager(t.TempDir(), func() store.Settings { return store.Settings{} }, nil, nil)
	if got := m.ProxyURL(); got != "" {
		t.Fatalf("stopped manager returned %q", got)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	m.mu.Lock()
	m.state = store.RuntimeState{Status: "running", LocalAddr: addr}
	m.mu.Unlock()

	if got, want := m.ProxyURL(), "socks5://"+addr; got != want {
		t.Fatalf("ProxyURL() = %q, want %q", got, want)
	}

	_ = ln.Close()
	if got := m.ProxyURL(); got != "" {
		t.Fatalf("closed listener returned %q", got)
	}
}
