//go:build windows

package checks

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
)

// Ping real por IcmpSendEcho a loopback (el pinger no aplica politica: esa es del Runner).
func TestIcmpSendEchoLoopback(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	rtt, err := newNativePinger().Ping(ctx, netip.MustParseAddr("127.0.0.1"))
	if err != nil {
		t.Fatalf("Ping loopback: %v", err)
	}
	if rtt < 0 || rtt > time.Second {
		t.Errorf("rtt = %s, fuera de rango", rtt)
	}
}

func TestIcmpSendEchoIPv6IsUnavailable(t *testing.T) {
	t.Parallel()
	if _, err := newNativePinger().Ping(t.Context(), netip.MustParseAddr("::1")); !errors.Is(err, ErrPingUnavailable) {
		t.Errorf("err = %v, want ErrPingUnavailable (IPv6 va por el ping del SO)", err)
	}
}
