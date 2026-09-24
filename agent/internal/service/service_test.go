package service

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRenderSystemdUnit(t *testing.T) {
	t.Parallel()

	unit, err := RenderSystemdUnit("/opt/argos-agent/argos-agent")
	if err != nil {
		t.Fatal(err)
	}
	lines := map[string]bool{}
	for _, l := range strings.Split(unit, "\n") {
		lines[l] = true
	}
	// Cada directiva exigida, como linea completa (no como subcadena de otra).
	for _, want := range []string{
		"[Unit]", "[Service]", "[Install]",
		"User=argos-agent", "Group=argos-agent",
		"ExecStart=/opt/argos-agent/argos-agent run --service",
		"Restart=on-failure",
		"StateDirectory=argos-agent", "StateDirectoryMode=0700", "UMask=0077",
		"NoNewPrivileges=true", "ProtectSystem=strict", "ProtectHome=true", "PrivateTmp=true", "PrivateDevices=true",
		"ProtectKernelTunables=true", "ProtectKernelModules=true", "ProtectKernelLogs=true",
		"ProtectControlGroups=true", "ProtectClock=true", "ProtectHostname=true",
		"CapabilityBoundingSet=", "AmbientCapabilities=",
		"RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK",
		"RestrictNamespaces=true", "RestrictRealtime=true", "RestrictSUIDSGID=true",
		"LockPersonality=true", "MemoryDenyWriteExecute=true",
		"SystemCallArchitectures=native", "SystemCallFilter=@system-service",
		"WantedBy=multi-user.target",
	} {
		if !lines[want] {
			t.Errorf("falta la linea %q en la unit:\n%s", want, unit)
		}
	}
	// ProcSubset=pid rompe /proc/stat, /proc/meminfo y /proc/net/dev: prohibido en cualquier forma.
	if strings.Contains(strings.ToLower(unit), "procsubset") {
		t.Errorf("la unit no debe mencionar ProcSubset:\n%s", unit)
	}
	// Sin privilegios de mas: ninguna capacidad ni CapabilityBoundingSet con valores.
	for _, l := range strings.Split(unit, "\n") {
		if strings.HasPrefix(l, "CapabilityBoundingSet=") && l != "CapabilityBoundingSet=" {
			t.Errorf("CapabilityBoundingSet debe quedar vacio: %q", l)
		}
		if strings.HasPrefix(l, "AmbientCapabilities=") && l != "AmbientCapabilities=" {
			t.Errorf("AmbientCapabilities debe quedar vacio: %q", l)
		}
	}
}

func TestRenderRejectsUnsafeExecPaths(t *testing.T) {
	t.Parallel()

	for _, p := range []string{"", "relativo/argos", "/opt/con espacio/argos", "/opt/a\nExecStartPre=/bin/sh", "/opt/%h/argos", "/opt/$x/argos", "/opt/\"a\"/argos", "/opt/<x>/argos"} {
		if _, err := RenderSystemdUnit(p); err == nil {
			t.Errorf("systemd: la ruta %q debio rechazarse", p)
		}
		if _, err := RenderLaunchdPlist(p); err == nil {
			t.Errorf("launchd: la ruta %q debio rechazarse", p)
		}
	}
}

func TestRenderLaunchdPlist(t *testing.T) {
	t.Parallel()

	plist, err := RenderLaunchdPlist("/usr/local/libexec/argos-agent/argos-agent")
	if err != nil {
		t.Fatal(err)
	}
	// Debe ser XML bien formado.
	dec := xml.NewDecoder(strings.NewReader(plist))
	dec.Strict = true
	dec.Entity = xml.HTMLEntity
	var tokens []string
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("plist mal formado: %v\n%s", err, plist)
		}
		if cd, ok := tok.(xml.CharData); ok {
			if s := strings.TrimSpace(string(cd)); s != "" {
				tokens = append(tokens, s)
			}
		}
	}
	// key, valor: los pares del dict, en orden.
	seq := strings.Join(tokens, "|")
	for _, want := range []string{
		"Label|com.argos.agent",
		"ProgramArguments|/usr/local/libexec/argos-agent/argos-agent|run|--service",
		"UserName|_argos|GroupName|_argos",
		"RunAtLoad",
		"KeepAlive|SuccessfulExit", // KeepAlive es un dict que solo reinicia si sale con error
		"ThrottleInterval|30",
		"Umask|63", // 0077 en octal
	} {
		if !strings.Contains(seq, want) {
			t.Errorf("falta %q en el plist:\n%s", want, plist)
		}
	}
	if !strings.Contains(plist, "<key>SuccessfulExit</key>\n\t\t<false/>") {
		t.Errorf("KeepAlive debe ser {SuccessfulExit: false} (sin reinicio agresivo):\n%s", plist)
	}
	if strings.Contains(plist, "<key>KeepAlive</key>\n\t<true/>") {
		t.Errorf("KeepAlive no debe ser true (reinicio agresivo):\n%s", plist)
	}
	if strings.Contains(plist, "<key>UserName</key>\n\t<string>root</string>") {
		t.Errorf("no debe correr como root:\n%s", plist)
	}
}

func TestServiceSID(t *testing.T) {
	t.Parallel()

	// Valor real de `sc.exe showsid TrustedInstaller` en Windows 11: valida el algoritmo.
	if got, want := ServiceSID("TrustedInstaller"), "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"; got != want {
		t.Errorf("TrustedInstaller got %s, want %s", got, want)
	}
	// Sin distinguir mayusculas, como Windows.
	if ServiceSID("argos-agent") != ServiceSID("ARGOS-AGENT") {
		t.Error("el SID no debe depender de las mayusculas")
	}
	if got, want := ServiceSID("argos-agent"), "S-1-5-80-1350555266-2345545113-3768975185-1389682951-1450743372"; got != want {
		t.Errorf("argos-agent got %s, want %s", got, want)
	}
}

func TestFreeMacID(t *testing.T) {
	t.Parallel()

	users := "root                  0\n_www                 70\ndaemon                1\n_argos_other         300\nfoo   301\n"
	groups := "wheel  0\nadmin  80\n_grp  302\n"
	got, err := freeMacID(users, groups)
	if err != nil || got != 303 {
		t.Errorf("got %d err %v, want 303 (libre en usuarios y grupos)", got, err)
	}
	if got, err = freeMacID("", ""); err != nil || got != macIDMin {
		t.Errorf("vacio: got %d err %v, want %d", got, err, macIDMin)
	}
	var full strings.Builder
	for id := macIDMin; id <= macIDMax; id++ {
		full.WriteString("u " + strconv.Itoa(id) + "\n")
	}
	if _, err := freeMacID(full.String(), ""); err == nil {
		t.Error("sin ids libres se esperaba error")
	}
}

// newTestCA genera una CA autofirmada en PEM.
func newTestCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "argos test ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestLoadCA(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	testCA := newTestCA(t)
	got, err := LoadCA(write("ok.pem", testCA))
	if err != nil || !bytes.Equal(got, []byte(testCA)) {
		t.Fatalf("CA valida: err %v", err)
	}
	if _, err := LoadCA(write("basura.pem", "no es un certificado")); err == nil {
		t.Error("un archivo sin certificados debe rechazarse")
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("no es der")})
	if _, err := LoadCA(write("der-malo.pem", string(block))); err == nil {
		t.Error("un PEM con DER invalido debe rechazarse")
	}
	if _, err := LoadCA(write("grande.pem", testCA+strings.Repeat("#", maxCABytes))); err == nil {
		t.Error("una CA demasiado grande debe rechazarse")
	}
	if _, err := LoadCA(filepath.Join(dir, "no-existe.pem")); err == nil {
		t.Error("una ruta inexistente debe fallar")
	}
}

type fakeSink struct{ got []string }

func (f *fakeSink) Info(id uint32, m string) error {
	f.got = append(f.got, "info:"+strconv.Itoa(int(id))+":"+m)
	return nil
}
func (f *fakeSink) Warning(id uint32, m string) error {
	f.got = append(f.got, "warn:"+strconv.Itoa(int(id))+":"+m)
	return nil
}
func (f *fakeSink) Error(id uint32, m string) error {
	f.got = append(f.got, "error:"+strconv.Itoa(int(id))+":"+m)
	return nil
}

func TestEventLogHandlerMapsLevelsAndKeepsAttrs(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	log := slog.New(NewEventLogHandler(sink, slog.LevelInfo)).With("host_id", "h1")
	ctx := context.Background()
	log.DebugContext(ctx, "no debe salir")
	log.InfoContext(ctx, "iniciado", "version", "0.4.1")
	log.WarnContext(ctx, "reintento")
	log.ErrorContext(ctx, "token rechazado: re-enrola el agente")

	if len(sink.got) != 3 {
		t.Fatalf("eventos got %d (%v), want 3 (Debug filtrado)", len(sink.got), sink.got)
	}
	for i, want := range []string{"info:3:", "warn:2:", "error:1:"} {
		if !strings.HasPrefix(sink.got[i], want) {
			t.Errorf("evento %d got %q, want prefijo %q", i, sink.got[i], want)
		}
	}
	if !strings.Contains(sink.got[0], "msg=iniciado") || !strings.Contains(sink.got[0], "host_id=h1") || !strings.Contains(sink.got[0], "version=0.4.1") {
		t.Errorf("el evento debe llevar mensaje y atributos: %q", sink.got[0])
	}
	if strings.Contains(sink.got[0], "time=") {
		t.Errorf("el evento no debe repetir la hora: %q", sink.got[0])
	}
}
