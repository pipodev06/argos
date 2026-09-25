package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const aptFixture = `NOTE: This is only a simulation!
      apt-get needs root privileges for real execution.
Reading package lists...
Building dependency tree...
The following packages will be upgraded:
  libc6 openssl vim
3 upgraded, 0 newly installed, 0 to remove and 1 not upgraded.
Inst libc6 [2.35-0ubuntu3.6] (2.35-0ubuntu3.7 Ubuntu:22.04/jammy-updates, Ubuntu:22.04/jammy-security [amd64])
Inst openssl [3.0.2-0ubuntu1.14] (3.0.2-0ubuntu1.15 Ubuntu:22.04/jammy-updates [amd64])
Inst vim [2:8.2.3995-1ubuntu2.15] (2:8.2.3995-1ubuntu2.16 Debian-Security:11/oldstable-security [amd64])
Conf libc6 (2.35-0ubuntu3.7 Ubuntu:22.04/jammy-updates [amd64])
`

func TestParseAptSim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		in              string
		total, security int
	}{
		{"real", aptFixture, 3, 2},
		{"vacio", "Reading package lists...\n0 upgraded, 0 newly installed.\n", 0, 0},
		{"locale es (Inst no se traduce; texto libre se ignora)", "Leyendo lista de paquetes...\nInst a [1] (2 x/y-security [amd64])\n", 1, 1},
		{"hostil: basura y linea gigante", "Inst\x00 " + strings.Repeat("A", 100000) + "\nInst ok [1] (2 o/p [i])\n\xff\xfe Inst no\n", 1, 0},
		{"truncada a media linea", "Inst a [1] (2 o/p-security [i])\nIns", 1, 1},
	}
	for _, tt := range tests {
		total, sec := parseAptSim([]byte(tt.in))
		if total != tt.total || sec != tt.security {
			t.Errorf("%s: got %d/%d, want %d/%d", tt.name, total, sec, tt.total, tt.security)
		}
	}
}

func TestParseDnf(t *testing.T) {
	t.Parallel()
	check := "\nkernel.x86_64   5.14.0-427.el9   baseos\nopenssl.x86_64  3.0.7-28.el9  baseos\n\nObsoleting Packages\nfoo.x86_64  1  repo\n"
	if n := parseDnfCheck([]byte(check)); n != 2 {
		t.Errorf("check-update: got %d, want 2 (corta en Obsoleting)", n)
	}
	if n := parseDnfCheck([]byte("Last metadata expiration check: 1:00:00 ago on Mon.\nlinea rara\n")); n != 0 {
		t.Errorf("check-update ruido: got %d, want 0", n)
	}
	sec := "RHSA-2024:1234 Important/Sec. kernel-5.14.0-427.el9.x86_64\nRHSA-2024:1234 Important/Sec. kernel-core-5.14.0-427.el9.x86_64\nRHBA-2024:9 bugfix openssl-3.0.7-28.el9.x86_64\nFEDORA-2024-abcd security foo-1.2-3.fc40.noarch\n"
	pk, ids := parseDnfSecurity([]byte(sec))
	if pk != 3 || !reflect.DeepEqual(ids, []string{"RHSA-2024:1234", "RHSA-2024:1234", "FEDORA-2024-abcd"}) {
		t.Errorf("updateinfo: got %d %v", pk, ids)
	}
}

const zypperFixture = `<?xml version='1.0'?>
<stream>
<update-status version="0.6"><update-list>
<update name="openSUSE-SU-2024:0123-1" kind="patch" edition="1" category="security" severity="important"/>
<update name="kernel-default" kind="package" edition="6.4.0-2"/>
<update name="vim" kind="package" edition="9.1"/>
</update-list></update-status>
</stream>`

func TestParseZypperXML(t *testing.T) {
	t.Parallel()
	n, names, err := parseZypperXML([]byte(zypperFixture), "package")
	if err != nil || n != 2 || !reflect.DeepEqual(names, []string{"kernel-default", "vim"}) {
		t.Errorf("packages: %d %v %v", n, names, err)
	}
	n, names, err = parseZypperXML([]byte(zypperFixture), "patch")
	if err != nil || n != 1 || names[0] != "openSUSE-SU-2024:0123-1" {
		t.Errorf("patches: %d %v %v", n, names, err)
	}
	for _, bad := range []string{"", "no es xml", "<stream><update name='a'", `<!DOCTYPE x [<!ENTITY a "aaaa">]><stream>&a;</stream>`} {
		if _, _, err := parseZypperXML([]byte(bad), "package"); err == nil {
			t.Errorf("xml hostil/truncado %q: esperaba error (desconocido, nunca 0)", bad)
		}
	}
}

func TestParseSoftwareUpdate(t *testing.T) {
	t.Parallel()
	real := "Software Update Tool\n\nFinding available software\nSoftware Update found the following new or updated software:\n* Label: macOS Sonoma 14.5-23F79\n\tTitle: macOS Sonoma 14.5, Version: 14.5, Size: 1234K, Recommended: YES, Action: restart,\n* Label: Safari17.5\n\tTitle: Safari, Version: 17.5, Size: 100K, Recommended: YES,\n"
	if n, restart, known := parseSoftwareUpdate([]byte(real)); n != 2 || !restart || !known {
		t.Errorf("real: %d %t %t", n, restart, known)
	}
	if n, restart, known := parseSoftwareUpdate([]byte("Software Update Tool\n\nFinding available software\nNo new software available.\n")); n != 0 || restart || !known {
		t.Errorf("sin pendientes: %d %t %t", n, restart, known)
	}
	if _, _, known := parseSoftwareUpdate([]byte("No hay software nuevo disponible.\n")); known {
		t.Error("otro idioma: no debe afirmar 0 pendientes (known=false)")
	}
}

func TestParseHistoryAndRPM(t *testing.T) {
	t.Parallel()
	hist := "Start-Date: 2026-09-01  10:00:00\nCommandline: apt install x\nInstall: x:amd64 (1)\nEnd-Date: 2026-09-01  10:01:00\n\nStart-Date: 2026-09-20  08:00:00\nRemove: y:amd64 (1)\n\nStart-Date: 2026-09-10  08:00:00\nUpgrade: z:amd64 (1, 2)\n"
	if got := parseAptHistory([]byte(hist)); got != "2026-09-10" {
		t.Errorf("history: got %q, want 2026-09-10 (un Remove no cuenta)", got)
	}
	if got := parseAptHistory([]byte("basura")); got != "" {
		t.Errorf("history basura: %q", got)
	}
	if got := parseRPMInstallTimes([]byte("1726000000\n1758000000\nxx\n-5\n")); got != "2025-09-16" {
		t.Errorf("rpm: got %q", got)
	}
}

func TestParseWUA(t *testing.T) {
	t.Parallel()
	u, err := parseWUA([]byte("ruido\n" + `{"total":3,"security":1,"kbs":"KB5012345,KB999,evil;rm -rf,KB5000001","reboot":true,"last":"2026-09-24","cache_age_s":600}` + "\r\n"))
	if err != nil || *u.PendingTotal != 3 || *u.PendingSecurity != 1 || !*u.RebootRequired || *u.LastInstalled != "2026-09-24" || u.Source != "wua" {
		t.Fatalf("got %+v %v", u, err)
	}
	if !reflect.DeepEqual(u.Advisories, []string{"KB5012345", "KB5000001"}) {
		t.Errorf("solo KB validos: %v", u.Advisories)
	}
	u, err = parseWUA([]byte(`{"total":null,"security":null,"kbs":null,"reboot":null,"last":null,"cache_age_s":null}`))
	if err != nil || u.PendingTotal != nil || u.PendingSecurity != nil || u.RebootRequired != nil || u.LastInstalled != nil {
		t.Errorf("null debe quedar desconocido: %+v %v", u, err)
	}
	for _, bad := range []string{"", "sin json", `{"total":`, `{"total":"x"}`} {
		if _, err := parseWUA([]byte(bad)); err == nil {
			t.Errorf("%q: esperaba error", bad)
		}
	}
}

// fakeCmds graba las llamadas y responde segun "ruta arg1 arg2...".
type fakeCmds struct {
	calls []cmdSpec
	resp  map[string]cmdResult
}

func (f *fakeCmds) run(_ context.Context, c cmdSpec) (cmdResult, error) {
	f.calls = append(f.calls, c)
	if r, ok := f.resp[c.path+" "+strings.Join(c.args, " ")]; ok {
		return r, nil
	}
	return cmdResult{code: 127}, nil
}

func testEnv(goos string, f *fakeCmds, files ...string) updEnv {
	return updEnv{
		goos: goos, windowsOnline: WindowsOnlineAuto, run: f.run,
		getenv: func(k string) string {
			if k == "SystemRoot" {
				return `C:\Windows`
			}
			return ""
		},
		exists: func(p string) bool {
			for _, x := range files {
				if x == p {
					return true
				}
			}
			return false
		},
		modTime:  func(string) (time.Time, bool) { return time.Time{}, false },
		readTail: func(string, int64) ([]byte, bool) { return nil, false },
		now:      time.Now,
	}
}

func TestAptArgvExactAndReadOnly(t *testing.T) {
	t.Parallel()
	f := &fakeCmds{resp: map[string]cmdResult{"/usr/bin/apt-get -s -q -o Debug::NoLocking=1 upgrade": {out: []byte(aptFixture)}}}
	e := testEnv("linux", f, aptGetPath, rebootRequiredFile)
	e.readTail = func(p string, _ int64) ([]byte, bool) {
		if p != aptHistoryFile {
			t.Errorf("lee %s", p)
		}
		return []byte("Start-Date: 2026-09-10  08:00:00\nUpgrade: z (1)\n"), true
	}
	e.modTime = func(p string) (time.Time, bool) { return e.now().Add(-2 * time.Hour), p == aptStampFile }
	u, err := e.collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("%d comandos, want 1", len(f.calls))
	}
	c := f.calls[0]
	if c.path != "/usr/bin/apt-get" || !reflect.DeepEqual(c.args, []string{"-s", "-q", "-o", "Debug::NoLocking=1", "upgrade"}) {
		t.Errorf("argv %s %v", c.path, c.args)
	}
	if !reflect.DeepEqual(c.env, []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "LANG=C"}) || c.timeout != 30*time.Second {
		t.Errorf("env/timeout %v %v", c.env, c.timeout)
	}
	if *u.PendingTotal != 3 || *u.PendingSecurity != 2 || !*u.RebootRequired || *u.LastInstalled != "2026-09-10" || u.Source != "apt" || *u.CacheAgeS < 7000 {
		t.Errorf("got %+v", u)
	}
}

func TestAptFailureIsUnknownNeverZero(t *testing.T) {
	t.Parallel()
	for name, r := range map[string]cmdResult{
		"exit 100":  {out: []byte(aptFixture), code: 100},
		"truncada":  {out: []byte("Inst a [1] (2 x)\n"), truncated: true},
		"no existe": {code: 127},
	} {
		f := &fakeCmds{resp: map[string]cmdResult{"/usr/bin/apt-get -s -q -o Debug::NoLocking=1 upgrade": r}}
		u, err := testEnv("linux", f, aptGetPath).collect(t.Context())
		if err != nil || u.PendingTotal != nil || u.PendingSecurity != nil {
			t.Errorf("%s: got %+v %v, want contadores desconocidos", name, u, err)
		}
		if u.RebootRequired == nil || *u.RebootRequired {
			t.Errorf("%s: sin /var/run/reboot-required en apt = false conocido", name)
		}
	}
}

func TestDnfArgvAndUnknowns(t *testing.T) {
	t.Parallel()
	f := &fakeCmds{resp: map[string]cmdResult{
		"/usr/bin/dnf -q --cacheonly check-update":               {code: 100, out: []byte("kernel.x86_64 1 baseos\nvim.x86_64 2 appstream\n")},
		"/usr/bin/dnf -q --cacheonly updateinfo list --security": {out: []byte("RHSA-2024:1 Important/Sec. kernel-1.x86_64\n")},
		"/usr/bin/needs-restarting -r":                           {code: 1},
		`/usr/bin/rpm -qa --qf %{INSTALLTIME}\n`:                 {out: []byte("1758000000\n")},
	}}
	u, err := testEnv("linux", f, dnfPath, needsRestart, rpmPath).collect(t.Context())
	if err != nil || *u.PendingTotal != 2 || *u.PendingSecurity != 1 || !*u.RebootRequired || *u.LastInstalled != "2025-09-16" || !reflect.DeepEqual(u.Advisories, []string{"RHSA-2024:1"}) {
		t.Errorf("got %+v %v", u, err)
	}
	if len(f.calls) != 4 {
		t.Errorf("%d comandos, want 4", len(f.calls))
	}
	// Sin cache (exit 1): desconocido, no 0.
	f = &fakeCmds{resp: map[string]cmdResult{"/usr/bin/dnf -q --cacheonly check-update": {code: 1}}}
	u, _ = testEnv("linux", f, dnfPath).collect(t.Context())
	if u.PendingTotal != nil || u.PendingSecurity != nil || u.RebootRequired != nil {
		t.Errorf("sin cache: got %+v, want todo desconocido", u)
	}
	// check-update 0 = ninguna pendiente (conocido).
	f = &fakeCmds{resp: map[string]cmdResult{"/usr/bin/yum -q --cacheonly check-update": {code: 0}}}
	u, _ = testEnv("linux", f, yumPath).collect(t.Context())
	if u.PendingTotal == nil || *u.PendingTotal != 0 || u.Source != "yum" {
		t.Errorf("yum sin pendientes: %+v", u)
	}
}

func TestZypperArgv(t *testing.T) {
	t.Parallel()
	f := &fakeCmds{resp: map[string]cmdResult{
		"/usr/bin/zypper --non-interactive --no-refresh -x list-updates":                     {out: []byte(zypperFixture)},
		"/usr/bin/zypper --non-interactive --no-refresh -x list-patches --category security": {out: []byte(zypperFixture)},
	}}
	u, err := testEnv("linux", f, zypperPath).collect(t.Context())
	if err != nil || *u.PendingTotal != 2 || *u.PendingSecurity != 1 || u.RebootRequired != nil || u.Source != "zypper" {
		t.Errorf("got %+v %v", u, err)
	}
}

func TestNoManagerIsUnknown(t *testing.T) {
	t.Parallel()
	f := &fakeCmds{}
	u, err := testEnv("linux", f).collect(t.Context())
	if err != nil || u.PendingTotal != nil || u.Source != "none" || len(f.calls) != 0 {
		t.Errorf("got %+v %v calls=%d", u, err, len(f.calls))
	}
}

func TestMacArgv(t *testing.T) {
	t.Parallel()
	f := &fakeCmds{resp: map[string]cmdResult{"/usr/sbin/softwareupdate -l": {out: []byte("Software Update found the following new or updated software:\n* Label: A\n")}}}
	u, err := testEnv("darwin", f).collect(t.Context())
	if err != nil || *u.PendingTotal != 1 || u.PendingSecurity != nil || *u.RebootRequired {
		t.Errorf("got %+v %v", u, err)
	}
	f = &fakeCmds{resp: map[string]cmdResult{"/usr/sbin/softwareupdate -l": {out: []byte("otro idioma\n")}}}
	if _, err := testEnv("darwin", f).collect(t.Context()); err == nil {
		t.Error("formato no reconocido: esperaba error, no 0 pendientes")
	}
}

func TestWindowsArgvAndScript(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{WindowsOnlineAuto, WindowsOnlineNever, WindowsOnlineAlways} {
		f := &fakeCmds{resp: map[string]cmdResult{}}
		e := testEnv("windows", f)
		e.windowsOnline = mode
		script := windowsScript(mode)
		f.resp[`C:\Windows\System32\WindowsPowerShell\v1.0\powershell -NoProfile -NonInteractive -NoLogo -Command `+script] = cmdResult{out: []byte(`{"total":0,"security":0,"kbs":"","reboot":false,"last":null,"cache_age_s":5}`)}
		u, err := e.collect(t.Context())
		if err != nil || *u.PendingTotal != 0 || u.LastInstalled != nil {
			t.Errorf("%s: got %+v %v", mode, u, err)
		}
		if len(f.calls) != 1 || f.calls[0].timeout != 90*time.Second || !reflect.DeepEqual(f.calls[0].env, []string{`SystemRoot=C:\Windows`, `windir=C:\Windows`}) {
			t.Errorf("%s: llamada %+v", mode, f.calls)
		}
		if !strings.Contains(script, "$mode='"+mode+"'") {
			t.Errorf("%s: el script no fija el modo", mode)
		}
		if strings.Contains(script, `"`) || strings.Contains(script, "\n") {
			t.Errorf("%s: el script no puede llevar comillas dobles ni saltos de linea", mode)
		}
		if !strings.Contains(script, "Type=''Software''") || !strings.Contains(script, "IsInstalled=0 and IsHidden=0") {
			t.Errorf("%s: falta el filtro Type='Software'", mode)
		}
	}
	if windowsScript("'; calc; '") != windowsScript(WindowsOnlineAuto) {
		t.Error("un modo fuera del enum debe caer en auto (nunca interpolarse)")
	}
	// Script fallido = error (no 0).
	if _, err := testEnv("windows", &fakeCmds{}).collect(t.Context()); err == nil {
		t.Error("codigo de salida != 0 debe ser error")
	}
}

func TestUpdatesNormalizeAndRev(t *testing.T) {
	t.Parallel()
	base := func() Updates {
		return Updates{PendingTotal: ptr(5), PendingSecurity: ptr(2), RebootRequired: ptr(false), LastInstalled: ptr("2026-09-01"), Source: "apt", CacheAgeS: ptr(int64(100))}
	}
	snap := func(u Updates) snapshot {
		s, err := collectUpdates(t.Context(), Sources{Updates: func(context.Context) (Updates, error) { return u, nil }})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	a := snap(base())
	if b := snap(base()); a.rev != b.rev {
		t.Error("rev no es estable")
	}
	u := base()
	u.CacheAgeS = ptr(int64(999999))
	if snap(u).rev != a.rev {
		t.Error("cache_age_s no debe entrar al rev")
	}
	for name, mod := range map[string]func(*Updates){
		"total":    func(u *Updates) { u.PendingTotal = ptr(6) },
		"security": func(u *Updates) { u.PendingSecurity = ptr(3) },
		"reboot":   func(u *Updates) { u.RebootRequired = ptr(true) },
		"last":     func(u *Updates) { u.LastInstalled = ptr("2026-09-02") },
		"unknown":  func(u *Updates) { u.PendingSecurity = nil },
		"advisory": func(u *Updates) { u.Advisories = []string{"KB1234567"} },
		"source":   func(u *Updates) { u.Source = "dnf" },
	} {
		u := base()
		mod(&u)
		if snap(u).rev == a.rev {
			t.Errorf("cambiar %s debe cambiar el rev", name)
		}
	}
	// El orden y los duplicados de avisos no cambian el rev.
	x, y := base(), base()
	x.Advisories, y.Advisories = []string{"KB1234567", "KB7654321"}, []string{"KB7654321", "KB1234567", "KB1234567"}
	if snap(x).rev != snap(y).rev {
		t.Error("avisos: el orden y duplicados no deben afectar el rev")
	}
	// Sanea: seguridad > total, fecha futura, edad negativa, avisos invalidos y tope.
	bad := Updates{PendingTotal: ptr(1), PendingSecurity: ptr(9), LastInstalled: ptr("2999-01-01"), CacheAgeS: ptr(int64(-1)), Source: "\x00ab"}
	for i := range 300 {
		bad.Advisories = append(bad.Advisories, "RHSA-2024:"+strings.Repeat("1", 6)+string(rune('a'+i%26))+strings.Repeat("0", i/26+1))
	}
	bad.Advisories = append(bad.Advisories, "no valido", strings.Repeat("A", 33), "")
	s := snap(bad)
	got, ok := s.items.(Updates)
	if !ok {
		t.Fatalf("items es %T", s.items)
	}
	if got.PendingSecurity != nil || got.LastInstalled != nil || got.CacheAgeS != nil || got.Source != "ab" || len(got.Advisories) != maxAdvisories || !s.truncated {
		t.Errorf("sin sanear: %+v truncated=%t", got, s.truncated)
	}
	for _, id := range got.Advisories {
		if !advisoryRe.MatchString(id) {
			t.Errorf("aviso invalido pasa: %q", id)
		}
	}
	// El cuerpo cabe con holgura en el tope (200 x 32 B).
	b, err := json.Marshal(fullBody{Module: ModuleUpdates, Rev: s.rev, Items: s.items})
	if err != nil || len(b) > 16<<10 {
		t.Errorf("cuerpo de %d bytes (err=%v)", len(b), err)
	}
}

func TestUpdatesPayloadFieldsWhitelist(t *testing.T) {
	t.Parallel()
	var got []string
	typ := reflect.TypeOf(Updates{})
	for i := 0; i < typ.NumField(); i++ {
		tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		got = append(got, tag)
	}
	want := []string{"pending_total", "pending_security", "reboot_required", "last_installed", "source", "cache_age_s", "advisories"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("campos %v, lista blanca %v", got, want)
	}
}

func TestUpdatesOffMeansZeroCalls(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	src := fakeSources(nil, nil)
	src.Updates = func(context.Context) (Updates, error) { calls.Add(1); return Updates{}, nil }
	// Modulo no listado: no hay worker, no se llama a la fuente.
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	Run(ctx, Options{Endpoint: "http://127.0.0.1:1", Token: "t", Modules: nil, Sources: &src})
	Run(ctx, Options{Endpoint: "http://127.0.0.1:1", Token: "t", Modules: []string{ModulePorts}, Sources: &src})
	if n := calls.Load(); n != 0 {
		t.Errorf("%d llamadas con updates apagado, want 0", n)
	}
	if _, ok := buildModule(ModuleUpdates, Sources{}, UpdatesConfig{}); ok {
		t.Error("sin fuente de parches el modulo no debe estar disponible")
	}
}

func TestUpdatesModuleSchedule(t *testing.T) {
	t.Parallel()
	src := Sources{Updates: func(context.Context) (Updates, error) { return Updates{}, nil }}
	for _, tt := range []struct{ in, want time.Duration }{{0, DefaultUpdatesInterval}, {time.Minute, MinUpdatesInterval}, {100 * time.Hour, MaxUpdatesInterval}, {8 * time.Hour, 8 * time.Hour}} {
		m, ok := buildModule(ModuleUpdates, src, UpdatesConfig{Interval: tt.in})
		want := tt.want
		if runtime.GOOS == "darwin" {
			want = MaxUpdatesInterval
		}
		if !ok || m.interval != want || m.heartbeat != 24*time.Hour || m.start != 10*time.Minute {
			t.Errorf("interval %s: got %+v", tt.in, m)
		}
		w := &worker{mod: m}
		for range 50 {
			if d := w.every(); d < m.interval*8/10 || d > m.interval*12/10 {
				t.Fatalf("jitter fuera de +-20%%: %s de %s", d, m.interval)
			}
		}
	}
}

func TestUnsupportedStatus(t *testing.T) {
	t.Parallel()
	// Modulos de siempre: solo 404/405.
	for code, want := range map[int]bool{404: true, 405: true, 400: false, 413: false, 422: false, 401: false, 403: false, 409: false, 429: false, 500: false, 204: false} {
		if got := unsupportedStatus(code, false); got != want {
			t.Errorf("modulo estrecho, status %d: got %t, want %t", code, got, want)
		}
	}
	// updates: cualquier 4xx no transitorio.
	for code, want := range map[int]bool{400: true, 404: true, 405: true, 413: true, 422: true, 401: false, 403: false, 408: false, 409: false, 429: false, 500: false, 503: false, 204: false} {
		if got := unsupportedStatus(code, true); got != want {
			t.Errorf("updates, status %d: got %t, want %t", code, got, want)
		}
	}
}

func TestParseWindowsOnlineAndModules(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"": "auto", "AUTO": "auto", " never ": "never", "always": "always"} {
		if got, err := ParseWindowsOnline(in); err != nil || got != want {
			t.Errorf("%q: got %q %v", in, got, err)
		}
	}
	if _, err := ParseWindowsOnline("si"); err == nil {
		t.Error("valor invalido debe fallar")
	}
	if got, err := ParseModules("ports, Updates"); err != nil || !reflect.DeepEqual(got, []string{"ports", "updates"}) {
		t.Errorf("ParseModules: %v %v", got, err)
	}
}

// El codigo del modulo jamas usa shell ni comandos que escriban o instalen.
func TestUpdatesSourceHasNoShellNorWrites(t *testing.T) {
	t.Parallel()
	forbidden := regexp.MustCompile(`(?i)"(sh|bash|zsh|cmd|/bin/sh|/bin/bash|-c|/c)"|cmd\.exe|/bin/(ba)?sh|"(update|install|dist-upgrade|remove|erase|refresh|clean|makecache|autoremove|reinstall|downgrade|--refresh|-y)"|Install-|Uninstall-|Remove-|Set-|Add-|New-Item|Out-File|Start-Process|Invoke-|\.Download\(|\.Install\(|\biex\b|DownloadString|-EncodedCommand|Add-Type`)
	for _, f := range []string{"updates.go", "updates_exec.go", "updates_sys.go", "updates_parse.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := forbidden.Find(b); m != nil && f != "updates_parse.go" { // el parser nombra el elemento XML "update"
			t.Errorf("%s contiene %q (shell o comando de escritura)", f, m)
		}
	}
	// `upgrade` solo aparece como simulacion (-s) de apt-get.
	b, err := os.ReadFile("updates_sys.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), `"upgrade"`); n != 1 || !strings.Contains(string(b), `"-s", "-q", "-o", "Debug::NoLocking=1", "upgrade"`) {
		t.Error("apt-get debe ejecutarse solo como simulacion (-s)")
	}
}

// ---- runCmd (proceso real: el propio binario de test como hijo) ----

func TestHelperProcess(t *testing.T) {
	switch os.Getenv("ARGOS_HELPER") {
	case "big":
		_, _ = os.Stdout.Write([]byte(strings.Repeat("x", 100000)))
		os.Exit(0)
	case "exit7":
		_, _ = os.Stdout.WriteString("hola")
		os.Exit(7)
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

func helperSpec(mode string, timeout time.Duration, maxOut int) cmdSpec {
	return cmdSpec{path: os.Args[0], args: []string{"-test.run=TestHelperProcess"}, env: append(os.Environ(), "ARGOS_HELPER="+mode), timeout: timeout, maxOut: maxOut}
}

func TestRunCmd(t *testing.T) {
	t.Parallel()
	r, err := runCmd(t.Context(), helperSpec("big", 30*time.Second, 1000))
	if err != nil || len(r.out) != 1000 || !r.truncated || r.code != 0 {
		t.Errorf("salida acotada: len=%d truncated=%t code=%d err=%v", len(r.out), r.truncated, r.code, err)
	}
	r, err = runCmd(t.Context(), helperSpec("exit7", 30*time.Second, 1000))
	if err != nil || r.code != 7 || !strings.Contains(string(r.out), "hola") {
		t.Errorf("codigo de salida: %+v %v", r, err)
	}
	st := time.Now()
	if _, err = runCmd(t.Context(), helperSpec("sleep", 500*time.Millisecond, 1000)); !errors.Is(err, context.DeadlineExceeded) || time.Since(st) > 10*time.Second {
		t.Errorf("timeout: err=%v en %s", err, time.Since(st))
	}
	if _, err = runCmd(t.Context(), cmdSpec{path: filepath.Join(t.TempDir(), "no-existe"), timeout: time.Second, maxOut: 10, env: []string{"A=b"}}); err == nil {
		t.Error("binario inexistente debe ser error")
	}
}
