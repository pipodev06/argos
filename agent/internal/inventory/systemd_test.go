package inventory

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const unitsSample = `  accounts-daemon.service          loaded    active   running Accounts Service
● bad.service                      loaded    failed   failed  Broken thing with spaces
  cron.service                     loaded    active   running Regular background program processing daemon
  ssh@.service                     loaded    inactive dead    template
  getty@tty1.service               loaded    active   running Getty on tty1
  oneshot.service                  loaded    active   exited  One shot
  ghost.service                    not-found inactive dead    ghost.service
  masked.service                   masked    inactive dead    masked.service
  apparmor.service                 loaded    inactive dead    Load AppArmor profiles
`

const filesSample = `accounts-daemon.service enabled enabled
bad.service             enabled enabled
cron.service            enabled enabled
ssh@.service            disabled enabled
getty@.service          static   -
oneshot.service         static   -
masked.service          masked   enabled
apparmor.service        enabled  enabled
disabled-only.service   disabled enabled
weird.service           bad      -
`

func fakeRun(units, files string) systemctlRunner {
	return func(_ context.Context, _ string, _ int64, args ...string) ([]byte, error) {
		if args[0] == "list-units" {
			return []byte(units), nil
		}
		return []byte(files), nil
	}
}

func TestSystemdServices(t *testing.T) {
	t.Parallel()
	got, err := systemdServices(t.Context(), fakeRun(unitsSample, filesSample), "/usr/bin/systemctl")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Service{}
	for _, s := range got {
		by[s.Name] = s
	}
	want := []Service{
		{"accounts-daemon", "Accounts Service", "running", "auto"},
		{"bad", "Broken thing with spaces", "stopped", "auto"},
		{"cron", "Regular background program processing daemon", "running", "auto"},
		{"getty@tty1", "Getty on tty1", "running", "other"},
		{"oneshot", "One shot", "other", "manual"},
		{"ghost", "ghost.service", "other", "other"},
		{"masked", "masked.service", "stopped", "disabled"},
		{"apparmor", "Load AppArmor profiles", "stopped", "auto"},
		{"disabled-only", "", "stopped", "disabled"},
		{"weird", "", "stopped", "other"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d servicios, want %d: %+v", len(got), len(want), got)
	}
	for _, w := range want {
		if by[w.Name] != w {
			t.Errorf("%s: got %+v, want %+v", w.Name, by[w.Name], w)
		}
	}
	if _, ok := by["ssh@"]; ok {
		t.Error("las plantillas foo@.service no deben listarse")
	}
}

func TestSystemdServicesEmpty(t *testing.T) {
	t.Parallel()
	got, err := systemdServices(t.Context(), fakeRun("", ""), "/usr/bin/systemctl")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want vacio sin error", got, err)
	}
}

func TestSystemdServicesRunnerError(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	run := func(context.Context, string, int64, ...string) ([]byte, error) { return nil, boom }
	if _, err := systemdServices(t.Context(), run, "/usr/bin/systemctl"); !errors.Is(err, boom) {
		t.Fatalf("got %v, want boom envuelto", err)
	}
}

func TestSystemdServicesFixedArgv(t *testing.T) {
	t.Parallel()
	var calls [][]string
	var bins []string
	run := func(_ context.Context, bin string, limit int64, args ...string) ([]byte, error) {
		bins = append(bins, bin)
		calls = append(calls, args)
		if limit != maxSystemctlOutput {
			t.Errorf("limit = %d, want %d", limit, maxSystemctlOutput)
		}
		return nil, nil
	}
	if _, err := systemdServices(t.Context(), run, "/usr/bin/systemctl"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"list-units", "--type=service", "--all", "--no-legend", "--plain", "--no-pager"},
		{"list-unit-files", "--type=service", "--no-legend", "--plain", "--no-pager"},
	}
	if !slices.EqualFunc(calls, want, slices.Equal[[]string]) {
		t.Fatalf("argv = %v, want %v", calls, want)
	}
	for _, b := range bins {
		if b != "/usr/bin/systemctl" {
			t.Errorf("binario = %q, want /usr/bin/systemctl (sin shell)", b)
		}
	}
}

func TestSystemdServicesCapAndTruncated(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := range maxServices + 50 {
		b.WriteString("svc" + strconv.Itoa(i) + ".service loaded active running d\n")
	}
	src := Sources{Services: func(ctx context.Context) ([]Service, error) {
		return systemdServices(ctx, fakeRun(b.String(), ""), "/usr/bin/systemctl")
	}}
	snap, err := collectServices(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := snap.items.([]Service)
	if !ok || len(items) != maxServices || !snap.truncated {
		t.Fatalf("items=%d truncated=%v, want %d y true", len(items), snap.truncated, maxServices)
	}
}

func TestSystemdServicesHugeOutputBounded(t *testing.T) {
	t.Parallel()
	line := "a.service loaded active running d\n"
	huge := strings.Repeat(line, 3*maxSystemctlOutput/len(line))
	lines := fieldLines([]byte(huge))
	if limit := maxSystemctlOutput/len(line) + 1; len(lines) > limit {
		t.Fatalf("%d lineas, want <= %d", len(lines), limit)
	}
}
