package inventory

import (
	"reflect"
	"strings"
	"testing"
)

func TestSudoWrapsOnlyPrivilegedCommands(t *testing.T) {
	t.Parallel()
	f := &fakeCmds{resp: map[string]cmdResult{
		"/usr/bin/sudo -n -- /usr/bin/dnf -q --cacheonly check-update": {code: 100, out: []byte("openssl.x86_64 3.0 baseos\n")},
	}}
	e := testEnv("linux", f, dnfPath, rpmPath, needsRestart)
	e.sudo = true
	u, err := e.collect(t.Context())
	if err != nil || u.PendingTotal == nil || *u.PendingTotal != 1 {
		t.Fatalf("got %+v %v, want 1 pendiente via sudo", u, err)
	}
	for _, c := range f.calls {
		viaSudo := c.path == sudoPath
		full := strings.Join(append([]string{c.path}, c.args...), " ")
		if !viaSudo {
			if c.path == dnfPath || c.path == yumPath || c.path == zypperPath {
				t.Errorf("%q debio ir por sudo", full)
			}
			continue
		}
		if !reflect.DeepEqual(c.args[:2], []string{"-n", "--"}) || !isPrivilegedCommand(c.args[2], c.args[3:]) {
			t.Errorf("sudo con comando fuera de la lista: %q", full)
		}
	}
	// rpm y needs-restarting NO van por sudo.
	for _, c := range f.calls {
		if c.path == sudoPath && (strings.Contains(strings.Join(c.args, " "), "rpm ") || strings.Contains(strings.Join(c.args, " "), "needs-restarting")) {
			t.Errorf("no debe usar sudo: %v", c.args)
		}
	}
}

func TestNoSudoWithoutPrivilegedMode(t *testing.T) {
	t.Parallel()
	f := &fakeCmds{resp: map[string]cmdResult{"/usr/bin/dnf -q --cacheonly check-update": {code: 0}}}
	if _, err := testEnv("linux", f, dnfPath).collect(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if c.path == sudoPath {
			t.Errorf("sin modo privilegiado no se usa sudo: %v", c.args)
		}
	}
}

func TestPrivilegedCommandsAreExact(t *testing.T) {
	t.Parallel()
	cmds := PrivilegedCommands()
	seen := map[string]bool{}
	for _, c := range cmds {
		line := strings.Join(c, " ")
		if seen[line] {
			t.Errorf("duplicado: %s", line)
		}
		seen[line] = true
		if !strings.HasPrefix(c[0], "/usr/bin/") || strings.ContainsAny(line, "*?[],:=\\") {
			t.Errorf("comando no exacto o con caracteres de sudoers: %q", line)
		}
	}
	if isPrivilegedCommand(dnfPath, []string{"-y", "upgrade"}) || isPrivilegedCommand("/bin/sh", []string{"-c", "id"}) {
		t.Error("un comando fuera de la lista no debe ser privilegiado")
	}
}
