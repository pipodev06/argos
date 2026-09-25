package checks

import (
	"context"
	"errors"
	"net/netip"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Estos tests fijan el argv exacto: si alguien agrega un argumento (o un shell), fallan.
func TestPingArgvExact(t *testing.T) {
	t.Parallel()
	v4 := netip.MustParseAddr("192.168.1.10")
	v6 := netip.MustParseAddr("2001:db8::5")
	mapped := netip.MustParseAddr("::ffff:192.168.1.10")
	tests := []struct {
		name    string
		goos    string
		ip      netip.Addr
		timeout time.Duration
		want    []string
	}{
		{"linux v4", "linux", v4, 2 * time.Second, []string{"-c", "1", "-W", "2", "--", "192.168.1.10"}},
		{"linux v4 redondea hacia arriba", "linux", v4, 2500 * time.Millisecond, []string{"-c", "1", "-W", "3", "--", "192.168.1.10"}},
		{"linux minimo 1 s", "linux", v4, 50 * time.Millisecond, []string{"-c", "1", "-W", "1", "--", "192.168.1.10"}},
		{"linux v6", "linux", v6, 2 * time.Second, []string{"-6", "-c", "1", "-W", "2", "--", "2001:db8::5"}},
		{"linux mapped se formatea como v4", "linux", mapped, 2 * time.Second, []string{"-c", "1", "-W", "2", "--", "192.168.1.10"}},
		{"darwin v4", "darwin", v4, 2 * time.Second, []string{"-c", "1", "-W", "2000", "--", "192.168.1.10"}},
		{"windows v4", "windows", v4, 2 * time.Second, []string{"-n", "1", "-w", "2000", "192.168.1.10"}},
		{"windows v6", "windows", v6, 2 * time.Second, []string{"-6", "-n", "1", "-w", "2000", "2001:db8::5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := pingArgv(tt.goos, tt.ip, tt.timeout); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("argv = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExecPingerUsesFixedBinaryAndArgv(t *testing.T) {
	t.Parallel()
	var gotPath string
	var gotArgs []string
	p := &execPinger{goos: "linux", binary: "/bin/ping", run: func(_ context.Context, path string, args []string) ([]byte, error) {
		gotPath, gotArgs = path, args
		return []byte("64 bytes from 192.168.1.10: icmp_seq=1 ttl=64 time=0.045 ms\n"), nil
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	rtt, err := p.Ping(ctx, netip.MustParseAddr("192.168.1.10"))
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if rtt != 45*time.Microsecond {
		t.Errorf("rtt = %s, want 45us", rtt)
	}
	if gotPath != "/bin/ping" {
		t.Errorf("binario = %q, want /bin/ping (ruta fija, sin shell)", gotPath)
	}
	if want := []string{"-c", "1", "-W", "2", "--", "192.168.1.10"}; !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("argv = %q, want %q", gotArgs, want)
	}
}

func TestExecPingerParsesOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		goos    string
		out     string
		runErr  error
		want    time.Duration
		wantErr error
	}{
		{"linux", "linux", "time=12.5 ms", nil, 12500 * time.Microsecond, nil},
		{"windows en", "windows", "Reply from 10.0.0.1: bytes=32 time=3ms TTL=64", nil, 3 * time.Millisecond, nil},
		{"windows menor a 1 ms", "windows", "Reply from 10.0.0.1: bytes=32 time<1ms TTL=64", nil, time.Millisecond, nil},
		{"windows es", "windows", "Respuesta desde 10.0.0.1: bytes=32 tiempo=2ms TTL=64", nil, 2 * time.Millisecond, nil},
		{"windows inalcanzable con exit 0", "windows", "Reply from 10.0.0.9: Destination host unreachable.", nil, 0, ErrPingNoReply},
		{"salida basura no rompe", "windows", "=== ms ===", nil, 0, ErrPingNoReply},
		{"exit distinto de 0", "linux", "", &exec.ExitError{}, 0, ErrPingNoReply},
		{"no se pudo lanzar", "linux", "", errors.New("permiso denegado"), 0, ErrPingUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &execPinger{goos: tt.goos, binary: "ping", run: func(context.Context, string, []string) ([]byte, error) {
				return []byte(tt.out), tt.runErr
			}}
			got, err := p.Ping(t.Context(), netip.MustParseAddr("10.0.0.1"))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("rtt = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestExecPingerNoBinaryIsUnavailable(t *testing.T) {
	t.Parallel()
	p := &execPinger{goos: "linux", run: func(context.Context, string, []string) ([]byte, error) {
		t.Error("no debe ejecutar nada sin binario")
		return nil, nil
	}}
	if _, err := p.Ping(t.Context(), netip.MustParseAddr("10.0.0.1")); !errors.Is(err, ErrPingUnavailable) {
		t.Errorf("err = %v, want ErrPingUnavailable", err)
	}
}

func TestExecPingerContextCancelIsNoReply(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	p := &execPinger{goos: "linux", binary: "/bin/ping", run: func(ctx context.Context, _ string, _ []string) ([]byte, error) {
		cancel()
		return nil, ctx.Err()
	}}
	if _, err := p.Ping(ctx, netip.MustParseAddr("10.0.0.1")); !errors.Is(err, ErrPingNoReply) {
		t.Errorf("err = %v, want ErrPingNoReply", err)
	}
}

func TestLimitedBufferCapsOutput(t *testing.T) {
	t.Parallel()
	var b limitedBuffer
	n, err := b.Write([]byte(strings.Repeat("x", maxPingOutput*3)))
	if err != nil || n != maxPingOutput*3 {
		t.Errorf("Write = %d, %v; debe aceptar todo aunque descarte", n, err)
	}
	if b.b.Len() != maxPingOutput {
		t.Errorf("guardado = %d, want %d", b.b.Len(), maxPingOutput)
	}
}
