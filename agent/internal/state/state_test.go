package state

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const secret = "tok-super-secreto-123"

func newStore(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "argos-agent"), slog.New(slog.DiscardHandler))
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	want := Credentials{HostID: "h-1", AgentToken: secret, ServerURL: "https://argos.example.com"}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSavePermissionsAndNoTempLeftovers(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	if err := s.Save(Credentials{HostID: "h", AgentToken: secret}); err != nil {
		t.Fatal(err)
	}
	// Sobrescribir es atomico y no deja temporales.
	if err := s.Save(Credentials{HostID: "h2", AgentToken: secret + "2"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != fileName {
		t.Errorf("el directorio deberia tener solo %s, got %v", fileName, entries)
	}
	got, err := s.Load()
	if err != nil || got.HostID != "h2" {
		t.Errorf("Load tras sobrescribir got %+v err %v", got, err)
	}

	if runtime.GOOS == "windows" {
		t.Skip("Windows no tiene bits de permiso POSIX: os.FileMode no refleja la ACL; se verifica con icacls en el e2e manual")
	}
	fi, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("archivo perm got %o, want 600", perm)
	}
	di, err := os.Stat(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("directorio perm got %o, want 700", perm)
	}
}

func TestLoadNotEnrolled(t *testing.T) {
	t.Parallel()

	if _, err := newStore(t).Load(); !errors.Is(err, ErrNotEnrolled) {
		t.Errorf("err got %v, want ErrNotEnrolled", err)
	}
}

func TestLoadCorruptOrEmptyToken(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{"corrupto": "{no es json", "sin token": `{"host_id":"h"}`} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newStore(t)
			if err := s.Prepare(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(s.Path(), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := s.Load()
			if err == nil || errors.Is(err, ErrNotEnrolled) {
				t.Fatalf("err got %v, want error de archivo invalido", err)
			}
			if !strings.Contains(err.Error(), "--force") {
				t.Errorf("el error deberia decir como recuperarse: %v", err)
			}
		})
	}
}

func TestResolveFileVsEnv(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	if err := s.Save(Credentials{HostID: "h", AgentToken: "del-archivo", ServerURL: "https://a"}); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve("", s)
	if err != nil || got.AgentToken != "del-archivo" || got.ServerURL != "https://a" {
		t.Errorf("sin env: got %+v err %v, want token del archivo", got, err)
	}
	got, err = Resolve("del-env", s)
	if err != nil || got.AgentToken != "del-env" {
		t.Errorf("con env: got %+v err %v, want el override", got, err)
	}
}

func TestResolveWithoutCredentialsExplainsHowToEnroll(t *testing.T) {
	t.Parallel()

	_, err := Resolve("", newStore(t))
	if !errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("err got %v, want ErrNotEnrolled", err)
	}
	for _, want := range []string{"argos-agent enroll", "--server", "--token", "ARGOS_AGENT_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("el error deberia mencionar %q: %v", want, err)
		}
	}
}

func TestCredentialsNeverPrintTheToken(t *testing.T) {
	t.Parallel()

	c := Credentials{HostID: "h", AgentToken: secret, ServerURL: "https://a"}
	var buf strings.Builder
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.InfoContext(context.Background(), "creds", "c", c)
	for name, out := range map[string]string{
		"slog": buf.String(), "%v": fmt.Sprintf("%v", c), "%+v": fmt.Sprintf("%+v", c), "Sprint": fmt.Sprint(c),
	} {
		if strings.Contains(out, secret) {
			t.Errorf("%s filtra el token: %s", name, out)
		}
	}
}
