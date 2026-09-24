package checks

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestParseSystemctlShow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		out     string
		wantUp  bool
		wantErr error
		parsed  bool
	}{
		{"activo", "LoadState=loaded\nActiveState=active\nSubState=running\n", true, nil, true},
		{"orden distinto", "SubState=running\nActiveState=active\nLoadState=loaded\n", true, nil, true},
		{"recargando", "LoadState=loaded\nActiveState=reloading\nSubState=reload\n", true, nil, true},
		{"inactivo", "LoadState=loaded\nActiveState=inactive\nSubState=dead\n", false, nil, true},
		{"fallido", "LoadState=loaded\nActiveState=failed\nSubState=failed\n", false, nil, true},
		{"activando", "LoadState=loaded\nActiveState=activating\nSubState=start\n", false, nil, true},
		{"no existe", "LoadState=not-found\nActiveState=inactive\nSubState=dead\n", false, ErrServiceNotFound, true},
		{"salida vacia", "", false, nil, false},
		{"basura", "hola\nmundo=\n", false, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st, ok := parseSystemctlShow(tt.out)
			if ok != tt.parsed {
				t.Fatalf("parsed = %v, want %v", ok, tt.parsed)
			}
			if !ok {
				return
			}
			up, err := systemdUp(st)
			if up != tt.wantUp || !errors.Is(err, tt.wantErr) {
				t.Errorf("up=%v err=%v, want up=%v err=%v", up, err, tt.wantUp, tt.wantErr)
			}
		})
	}
}

func TestSystemdProberArgv(t *testing.T) {
	t.Parallel()
	var gotBin string
	var gotArgs []string
	p := &systemdProber{bin: "/usr/bin/systemctl", run: func(_ context.Context, bin string, args ...string) ([]byte, error) {
		gotBin, gotArgs = bin, args
		return []byte("LoadState=loaded\nActiveState=active\nSubState=running\n"), nil
	}}
	up, err := p.Probe(t.Context(), "nginx.service")
	if err != nil || !up {
		t.Fatalf("up=%v err=%v, want up", up, err)
	}
	want := []string{"show", "--no-pager", "-p", "LoadState", "-p", "ActiveState", "-p", "SubState", "--", "nginx.service"}
	if gotBin != "/usr/bin/systemctl" || !slices.Equal(gotArgs, want) {
		t.Errorf("argv = %s %v, want %v", gotBin, gotArgs, want)
	}
}

func TestSystemdProberErrors(t *testing.T) {
	t.Parallel()
	ok := func(context.Context, string, ...string) ([]byte, error) { return []byte("ActiveState=active\n"), nil }
	tests := []struct {
		name string
		p    *systemdProber
		unit string
		want error
	}{
		{"sin systemctl", &systemdProber{run: ok}, "nginx", ErrServiceUnsupported},
		{"nombre con opcion", &systemdProber{bin: "/x", run: ok}, "--all", ErrServiceNotFound},
		{"salida ilegible", &systemdProber{bin: "/x", run: func(context.Context, string, ...string) ([]byte, error) { return []byte("x"), nil }}, "nginx", ErrServiceUnsupported},
		{"no se puede ejecutar", &systemdProber{bin: "/x", run: func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("boom") }}, "nginx", ErrServiceUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := tt.p.Probe(t.Context(), tt.unit); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestWindowsMapping(t *testing.T) {
	t.Parallel()
	states := map[uint32]bool{1: false, 2: false, 3: false, 4: true, 5: false, 6: false, 7: false}
	for st, want := range states {
		if got := windowsStateUp(st); got != want {
			t.Errorf("estado %d: up = %v, want %v", st, got, want)
		}
	}
	errs := map[uintptr]error{5: ErrServiceDenied, 1060: ErrServiceNotFound, 123: ErrServiceNotFound, 1078: ErrServiceNotFound, 6: ErrServiceUnsupported}
	for code, want := range errs {
		if got := mapWindowsErr(code); !errors.Is(got, want) {
			t.Errorf("codigo %d: got %v, want %v", code, got, want)
		}
	}
}

func TestValidServiceName(t *testing.T) {
	t.Parallel()
	valid := []string{"nginx", "nginx.service", "user@1000.service", "a:b_c-d", "Spooler"}
	invalid := []string{"", "-x", "--now", "a b", "a;b", "a/b", "a\nb", "$(id)", string(make([]byte, 65))}
	for _, s := range valid {
		if !validServiceName(s) {
			t.Errorf("%q deberia ser valido", s)
		}
	}
	for _, s := range invalid {
		if validServiceName(s) {
			t.Errorf("%q deberia ser invalido", s)
		}
	}
}
