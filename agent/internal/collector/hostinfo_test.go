package collector

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"argos/agent/internal/sender"
)

func addrs(ss ...string) []netip.Addr {
	out := make([]netip.Addr, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParseAddr(s)
	}
	return out
}

func TestUsableIPs(t *testing.T) {
	t.Parallel()

	up := net.FlagUp | net.FlagBroadcast
	ifs := []ifaceAddrs{
		{Name: "lo", Flags: net.FlagUp | net.FlagLoopback, Addrs: addrs("127.0.0.1", "::1")},
		{Name: "eth0", Flags: up, Addrs: addrs("192.168.1.25", "fe80::1", "169.254.10.10", "2800:200::5", "ff02::1", "0.0.0.0")},
		{Name: "wlan0", Flags: up, Addrs: addrs("10.0.0.7", "::ffff:192.168.1.25")}, // v4-mapped = la misma IP de eth0
		{Name: "eth1", Flags: net.FlagBroadcast, Addrs: addrs("172.16.0.1")},        // caida
		{Name: "docker0", Flags: up, Addrs: addrs("172.17.0.1")},
		{Name: "vEthernet (WSL)", Flags: up, Addrs: addrs("172.28.0.1")},
		{Name: "br-1a2b3c", Flags: up, Addrs: addrs("172.18.0.1")},
		{Name: "VMware Network Adapter VMnet8", Flags: up, Addrs: addrs("192.168.221.1")},
		{Name: "VirtualBox Host-Only Network", Flags: up, Addrs: addrs("192.168.56.1")},
	}
	got := usableIPs(ifs)
	want := []string{"10.0.0.7", "192.168.1.25", "2800:200::5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestUsableIPsCapsAtSixteenAndIsStable(t *testing.T) {
	t.Parallel()

	var all []netip.Addr
	for i := 40; i >= 1; i-- {
		all = append(all, netip.AddrFrom4([4]byte{10, 0, 0, byte(i)}))
	}
	got := usableIPs([]ifaceAddrs{{Name: "eth0", Flags: net.FlagUp, Addrs: all}})
	if len(got) != maxIPs || got[0] != "10.0.0.1" || got[15] != "10.0.0.16" {
		t.Errorf("got %v, want las 16 primeras ordenadas", got)
	}
}

func hostSystem(now *time.Time) (*System, *int) {
	s := NewSystem(nil)
	calls := 0
	s.now = func() time.Time { return *now }
	s.staticInfo = func(context.Context) (sender.HostInfo, []error) {
		calls++
		return sender.HostInfo{BootTime: 1000, CPUModel: "cpu", CPUCores: 4, CPUThreads: 8, OSName: "os", OSVersion: "1", KernelVersion: "k"}, nil
	}
	return s, &calls
}

func TestHostInfoCachesStaticPartButReadsIPsEveryTime(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_000_000, 0)
	s, staticCalls := hostSystem(&now)
	ip := "192.168.1.25"
	s.interfaces = func() ([]ifaceAddrs, error) {
		return []ifaceAddrs{{Name: "eth0", Flags: net.FlagUp, Addrs: addrs(ip)}}, nil
	}
	ctx := context.Background()

	first, err := s.HostInfo(ctx)
	if err != nil || !reflect.DeepEqual(first.IPs, []string{"192.168.1.25"}) || first.CPUModel != "cpu" {
		t.Fatalf("first got %+v err %v", first, err)
	}
	ip = "10.1.1.1" // cambio de red
	now = now.Add(time.Minute)
	second, err := s.HostInfo(ctx)
	if err != nil || !reflect.DeepEqual(second.IPs, []string{"10.1.1.1"}) {
		t.Fatalf("second got %+v err %v, want la IP nueva de inmediato", second, err)
	}
	if *staticCalls != 1 {
		t.Errorf("lecturas estaticas got %d, want 1 (cache)", *staticCalls)
	}
	now = now.Add(staticInfoTTL)
	if _, err := s.HostInfo(ctx); err != nil {
		t.Fatal(err)
	}
	if *staticCalls != 2 {
		t.Errorf("lecturas estaticas got %d, want 2 tras vencer el TTL", *staticCalls)
	}
}

func TestHostInfoPartialFailureKeepsWhatItCanRead(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_000_000, 0)
	s, _ := hostSystem(&now)
	s.staticInfo = func(context.Context) (sender.HostInfo, []error) {
		return sender.HostInfo{OSName: "os"}, []error{errors.New("wmi fallo")}
	}
	s.interfaces = func() ([]ifaceAddrs, error) { return nil, errors.New("sin netlink") }
	got, err := s.HostInfo(context.Background())
	if err != nil {
		t.Fatalf("con datos parciales no debe fallar: %v", err)
	}
	if got.OSName != "os" || got.CPUModel != "" || got.IPs != nil {
		t.Errorf("got %+v, want solo os_name", got)
	}

	s.staticInfo = func(context.Context) (sender.HostInfo, []error) {
		return sender.HostInfo{}, []error{errors.New("todo roto")}
	}
	now = now.Add(2 * staticInfoTTL)
	if _, err := s.HostInfo(context.Background()); err == nil {
		t.Error("sin ningun dato se esperaba error")
	}
}

func TestOSName(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ platform, goos, want string }{
		{"Microsoft Windows 11 Pro", "windows", "Microsoft Windows 11 Pro"},
		{"ubuntu", "linux", "ubuntu"},
		{"darwin", "darwin", "macOS"},
		{"", "linux", "linux"},
		{"  ", "", ""},
	} {
		if got := osName(tt.platform, tt.goos); got != tt.want {
			t.Errorf("osName(%q,%q) got %q, want %q", tt.platform, tt.goos, got, tt.want)
		}
	}
}
