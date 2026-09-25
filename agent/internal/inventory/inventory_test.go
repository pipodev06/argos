package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"testing"

	psnet "github.com/shirou/gopsutil/v4/net"
)

func listen(ip string, port uint32, pid int32) psnet.ConnectionStat {
	return psnet.ConnectionStat{Type: syscall.SOCK_STREAM, Status: "LISTEN", Pid: pid, Laddr: psnet.Addr{IP: ip, Port: port}}
}

// fakeSources: el PID 99 no se puede resolver (proceso protegido).
func fakeSources(conns []psnet.ConnectionStat, procs []ProcInfo) Sources {
	return Sources{
		Conns: func(context.Context) ([]psnet.ConnectionStat, error) { return conns, nil },
		ProcName: func(_ context.Context, pid int32) (string, error) {
			if pid == 99 {
				return "", errors.New("acceso denegado")
			}
			return fmt.Sprintf("proc%d.bin", pid), nil
		},
		Procs: func(context.Context) ([]ProcInfo, error) { return procs, nil },
	}
}

func TestParseModules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw     string
		want    []string
		wantErr bool
	}{
		{"", nil, false},
		{" , ", nil, false},
		{"ports", []string{"ports"}, false},
		{"Ports, processes,ports", []string{"ports", "processes"}, false},
		{"ports,cpu", nil, true},
	}
	for _, tt := range tests {
		got, err := ParseModules(tt.raw)
		if (err != nil) != tt.wantErr || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseModules(%q) = %v, %v; want %v (err=%v)", tt.raw, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestPortsHashStableAndOrdered(t *testing.T) {
	t.Parallel()
	a := []psnet.ConnectionStat{listen("0.0.0.0", 80, 1), listen("127.0.0.1", 5432, 2), listen("::", 443, 3)}
	b := []psnet.ConnectionStat{a[2], a[0], a[1]}
	s1, err := collectPorts(t.Context(), fakeSources(a, nil))
	if err != nil {
		t.Fatal(err)
	}
	s2, err := collectPorts(t.Context(), fakeSources(b, nil))
	if err != nil {
		t.Fatal(err)
	}
	if s1.rev != s2.rev {
		t.Errorf("rev depende del orden: %s != %s", s1.rev, s2.rev)
	}
	if !reflect.DeepEqual(s1.items, s2.items) {
		t.Errorf("items no ordenados de forma determinista: %v vs %v", s1.items, s2.items)
	}
	// El PID no entra al hash: un reinicio del proceso (mismo nombre) no es un cambio.
	src := fakeSources(a, nil)
	src.ProcName = func(_ context.Context, pid int32) (string, error) { return fmt.Sprintf("proc%d.bin", pid), nil }
	renum := []psnet.ConnectionStat{listen("0.0.0.0", 80, 1), listen("127.0.0.1", 5432, 2), listen("::", 443, 3)}
	src.Conns = func(context.Context) ([]psnet.ConnectionStat, error) { return renum, nil }
	s3, _ := collectPorts(t.Context(), src)
	if s3.rev != s1.rev {
		t.Errorf("rev distinto con los mismos puertos")
	}
}

func TestPortsDetectChange(t *testing.T) {
	t.Parallel()
	s1, _ := collectPorts(t.Context(), fakeSources([]psnet.ConnectionStat{listen("0.0.0.0", 80, 1)}, nil))
	tests := map[string][]psnet.ConnectionStat{
		"puerto nuevo": {listen("0.0.0.0", 80, 1), listen("0.0.0.0", 81, 1)},
		"otro bind":    {listen("127.0.0.1", 80, 1)},
		"otro dueno":   {listen("0.0.0.0", 80, 2)},
		"sin dueno":    {listen("0.0.0.0", 80, 99)},
	}
	for name, conns := range tests {
		s2, err := collectPorts(t.Context(), fakeSources(conns, nil))
		if err != nil {
			t.Fatal(err)
		}
		if s2.rev == s1.rev {
			t.Errorf("%s: el rev no cambio", name)
		}
	}
}

func TestPortsFiltersAndUnknownOwner(t *testing.T) {
	t.Parallel()
	conns := []psnet.ConnectionStat{
		listen("0.0.0.0", 80, 1),
		listen("::ffff:10.0.0.1", 8080, 99), // dueno no resoluble
		listen("0.0.0.0", 22, 0),            // PID 0
		{Type: syscall.SOCK_STREAM, Status: "ESTABLISHED", Laddr: psnet.Addr{IP: "10.0.0.1", Port: 50000}}, // saliente
		{Type: syscall.SOCK_DGRAM, Laddr: psnet.Addr{IP: "0.0.0.0", Port: 53}, Pid: 1},                     // UDP en escucha
		{Type: syscall.SOCK_DGRAM, Laddr: psnet.Addr{IP: "10.0.0.1", Port: 4000}, Raddr: psnet.Addr{IP: "1.1.1.1", Port: 53}},
		listen("no-es-ip", 9, 1),
		listen("0.0.0.0", 0, 1),
	}
	s, err := collectPorts(t.Context(), fakeSources(conns, nil))
	if err != nil {
		t.Fatal(err)
	}
	got := s.items.([]Port)
	if len(got) != 4 {
		t.Fatalf("got %d puertos %+v, want 4", len(got), got)
	}
	unknown := 0
	for _, p := range got {
		if p.Port == 8080 && p.Bind != "10.0.0.1" {
			t.Errorf("bind no normalizado: %q", p.Bind)
		}
		if !p.OwnerKnown {
			unknown++
			if p.Proc != "" || p.PID != 0 {
				t.Errorf("dueno desconocido con datos: %+v", p)
			}
		}
	}
	if unknown != 2 {
		t.Errorf("desconocidos = %d, want 2 (PID 0 y proceso protegido)", unknown)
	}
}

func TestPortsCapTruncates(t *testing.T) {
	t.Parallel()
	var conns []psnet.ConnectionStat
	for i := 1; i <= maxPorts+50; i++ {
		conns = append(conns, listen("0.0.0.0", uint32(i), 1))
	}
	s, err := collectPorts(t.Context(), fakeSources(conns, nil))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(s.items.([]Port)); n != maxPorts || !s.truncated {
		t.Errorf("got %d items truncated=%v, want %d true", n, s.truncated, maxPorts)
	}
}

func TestServicesCapsAndSanitizes(t *testing.T) {
	t.Parallel()
	var raw []Service
	for i := 0; i < maxServices+10; i++ {
		raw = append(raw, Service{Name: fmt.Sprintf("svc%04d", i), Display: "d", State: "running", Start: "auto"})
	}
	src := Sources{Services: func(context.Context) ([]Service, error) { return raw, nil }}
	s, err := collectServices(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(s.items.([]Service)); !s.truncated || n != maxServices {
		t.Errorf("tope de servicios no aplicado: %d truncated=%v", n, s.truncated)
	}

	dirty := []Service{{Name: "  ", Display: "x"}, {Name: "a\x00b\nc" + strings.Repeat("z", 100), Display: strings.Repeat("é", 300)}}
	src.Services = func(context.Context) ([]Service, error) { return dirty, nil }
	s, _ = collectServices(t.Context(), src)
	items := s.items.([]Service)
	if len(items) != 1 {
		t.Fatalf("got %d servicios, want 1 (el de nombre vacio se descarta)", len(items))
	}
	sv := items[0]
	if len([]rune(sv.Name)) > maxNameLen || len([]rune(sv.Display)) > maxDisplayLen || strings.ContainsAny(sv.Name, "\x00\n") {
		t.Errorf("nombre sin sanear: %q / %d runas", sv.Name, len([]rune(sv.Display)))
	}
}

func TestServicesBodyByteCap(t *testing.T) {
	t.Parallel()
	var raw []Service
	for i := 0; i < maxServices; i++ {
		// '<' se escapa a 6 bytes en JSON: el peor caso de tamano.
		raw = append(raw, Service{Name: fmt.Sprintf("%04d", i) + strings.Repeat("<", 60), Display: strings.Repeat("<", maxDisplayLen), State: "running", Start: "auto"})
	}
	s, err := collectServices(t.Context(), Sources{Services: func(context.Context) ([]Service, error) { return raw, nil }})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(fullBody{Module: "services", Rev: s.rev, Items: s.items, Truncated: s.truncated})
	if len(b) > maxBodyBytes || !s.truncated {
		t.Errorf("cuerpo de %d bytes (tope %d) truncated=%v", len(b), maxBodyBytes, s.truncated)
	}
}

func TestProcessesGroupAndStableHash(t *testing.T) {
	t.Parallel()
	procs := []ProcInfo{{"chrome", 100}, {"chrome", 50}, {"go", 10}, {"", 5}, {"\x00", 1}}
	s1, err := collectProcesses(t.Context(), fakeSources(nil, procs))
	if err != nil {
		t.Fatal(err)
	}
	got := s1.items.([]Process)
	if len(got) != 2 || got[0].Name != "chrome" || got[0].Count != 2 || got[0].RSSBytes != 150 {
		t.Fatalf("got %+v", got)
	}
	// El RSS cambia todo el tiempo: no debe mover el rev (solo nombres y conteos).
	s2, _ := collectProcesses(t.Context(), fakeSources(nil, []ProcInfo{{"go", 999}, {"chrome", 1}, {"chrome", 2}}))
	if s1.rev != s2.rev {
		t.Errorf("el RSS movio el rev")
	}
	s3, _ := collectProcesses(t.Context(), fakeSources(nil, []ProcInfo{{"go", 1}, {"go", 1}, {"chrome", 1}, {"chrome", 2}}))
	if s3.rev == s1.rev {
		t.Errorf("un conteo distinto no cambio el rev")
	}
}

func TestProcessesTop100ByRSS(t *testing.T) {
	t.Parallel()
	var procs []ProcInfo
	for i := 0; i < 150; i++ {
		procs = append(procs, ProcInfo{Name: fmt.Sprintf("p%03d", i), RSS: uint64(i + 1)})
	}
	s, err := collectProcesses(t.Context(), fakeSources(nil, procs))
	if err != nil {
		t.Fatal(err)
	}
	got := s.items.([]Process)
	if len(got) != maxProcesses || !s.truncated {
		t.Fatalf("got %d truncated=%v", len(got), s.truncated)
	}
	for _, p := range got {
		if p.RSSBytes <= 50 {
			t.Errorf("entro un proceso fuera del top 100: %+v", p)
		}
	}
}

func TestCollectorErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	_, err := collectPorts(t.Context(), Sources{Conns: func(context.Context) ([]psnet.ConnectionStat, error) { return nil, boom }})
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want boom", err)
	}
}

// forbiddenKeys es lo que jamas puede viajar en el inventario.
var forbiddenKeys = []string{"cmdline", "environ", "env", "exe", "cwd", "username", "user", "path", "cmd"}

func TestPayloadHasNoForbiddenFields(t *testing.T) {
	t.Parallel()
	whitelist := map[reflect.Type][]string{
		reflect.TypeOf(Port{}):    {"proto", "bind", "port", "pid", "proc", "owner_known"},
		reflect.TypeOf(Service{}): {"name", "display", "state", "start"},
		reflect.TypeOf(Process{}): {"name", "count", "rss_bytes"},
	}
	for typ, want := range whitelist {
		var got []string
		for i := 0; i < typ.NumField(); i++ {
			tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
			got = append(got, tag)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: campos %v, lista blanca %v", typ.Name(), got, want)
		}
	}
	// El JSON real de cada modulo, clave por clave.
	ports, _ := collectPorts(t.Context(), fakeSources([]psnet.ConnectionStat{listen("0.0.0.0", 80, 1)}, nil))
	svcs, _ := collectServices(t.Context(), Sources{Services: func(context.Context) ([]Service, error) { return []Service{{Name: "a"}}, nil }})
	procs, _ := collectProcesses(t.Context(), fakeSources(nil, []ProcInfo{{"a", 1}}))
	for _, s := range []snapshot{ports, svcs, procs} {
		b, err := json.Marshal(fullBody{Module: "m", Rev: s.rev, Items: s.items})
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Items) == 0 {
			t.Fatal("payload sin items")
		}
		for _, it := range doc.Items {
			for k := range it {
				if contains(forbiddenKeys, strings.ToLower(k)) {
					t.Errorf("clave prohibida %q en el payload", k)
				}
			}
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestSourceNeverMentionsForbiddenData falla si el codigo del paquete toca lo que no se puede leer.
func TestSourceNeverMentionsForbiddenData(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`(?i)cmdline|environ|username|\bcwd\b|\bexe\b`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := re.Find(b); m != nil {
			t.Errorf("%s menciona %q: campo prohibido del inventario", f, m)
		}
	}
}
