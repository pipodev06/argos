package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"argos/server/internal/agentdist"
)

const distVersion = "9.9.9"

var distTargets = []struct{ os, arch string }{
	{"windows", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"},
}

// newDist crea un directorio de distribucion valido; bigLinux es el tamaño del binario linux/amd64.
func newDist(t *testing.T, bigLinux int) *agentdist.Catalog {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, distVersion)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var entries []string
	for _, tg := range distTargets {
		name := agentdist.FileName(tg.os, tg.arch)
		body := []byte("binario " + name)
		if name == "argos-agent_linux_amd64" && bigLinux > 0 {
			body = []byte(strings.Repeat("x", bigLinux))
		}
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		entries = append(entries, fmt.Sprintf(`{"name":%q,"os":%q,"arch":%q,"sha256":%q,"size":%d}`,
			name, tg.os, tg.arch, hex.EncodeToString(sum[:]), len(body)))
	}
	manifest := fmt.Sprintf(`{"version":%q,"files":[%s]}`, distVersion, strings.Join(entries, ","))
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "latest"), []byte(distVersion+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := agentdist.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

func newDistAPI(t *testing.T, opts Options) (http.Handler, *logBuffer) {
	t.Helper()
	logs := &logBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	return New(&fakeStore{}, testHasher(), logger, func() time.Time { return fixedNow }, opts).Routes(), logs
}

func get(t *testing.T, h http.Handler, method, path string, mods ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Host = "localhost:8080"
	for _, m := range mods {
		m(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestDistDownload(t *testing.T) {
	t.Parallel()
	h, _ := newDistAPI(t, Options{Dist: newDist(t, 0)})

	const name = "argos-agent_windows_amd64.exe"
	rec := get(t, h, "GET", "/dl/"+distVersion+"/"+name)
	if rec.Code != 200 {
		t.Fatalf("status got %d, want 200", rec.Code)
	}
	body := "binario " + name
	sum := sha256.Sum256([]byte(body))
	if rec.Body.String() != body {
		t.Errorf("body got %q, want %q", rec.Body.String(), body)
	}
	wantHdr := map[string]string{
		"Content-Type":        "application/octet-stream",
		"Content-Disposition": `attachment; filename="` + name + `"`,
		"ETag":                `"` + hex.EncodeToString(sum[:]) + `"`,
		"Cache-Control":       "public, max-age=31536000, immutable",
	}
	for k, want := range wantHdr {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s got %q, want %q", k, got, want)
		}
	}

	rec = get(t, h, "GET", "/dl/"+distVersion+"/"+name, func(r *http.Request) { r.Header.Set("If-None-Match", wantHdr["ETag"]) })
	if rec.Code != http.StatusNotModified {
		t.Errorf("If-None-Match status got %d, want 304", rec.Code)
	}
	rec = get(t, h, "HEAD", "/dl/"+distVersion+"/"+name)
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Errorf("HEAD got %d con %d bytes de body, want 200 sin body", rec.Code, rec.Body.Len())
	}
}

func TestDistManifest(t *testing.T) {
	t.Parallel()
	h, _ := newDistAPI(t, Options{Dist: newDist(t, 0)})
	rec := get(t, h, "GET", "/dl/latest/manifest.json")
	if rec.Code != 200 {
		t.Fatalf("status got %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control got %q, want no-cache", got)
	}
	if !strings.Contains(rec.Body.String(), `"version":"`+distVersion+`",`) || !strings.Contains(rec.Body.String(), `{"name":"argos-agent_linux_arm64"`) {
		t.Errorf("manifiesto inesperado: %s", rec.Body.String())
	}
}

func TestDistScriptsServed(t *testing.T) {
	t.Parallel()
	h, _ := newDistAPI(t, Options{Dist: newDist(t, 0)})
	for path, body := range map[string][]byte{"/install.sh": installSH, "/install.ps1": installPS1} {
		rec := get(t, h, "GET", path)
		if rec.Code != 200 {
			t.Fatalf("%s status got %d, want 200", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("%s Content-Type got %q", path, got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s Cache-Control got %q, want no-cache", path, got)
		}
		if rec.Body.String() != string(body) {
			t.Errorf("%s: el body no es el script embebido", path)
		}
	}
}

func TestDistMethodsNotAllowed(t *testing.T) {
	t.Parallel()
	h, _ := newDistAPI(t, Options{Dist: newDist(t, 0)})
	paths := []string{"/install.sh", "/install.ps1", "/dl/latest/manifest.json", "/dl/" + distVersion + "/argos-agent_linux_amd64"}
	for _, p := range paths {
		for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			if rec := get(t, h, m, p); rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s status got %d, want 405", m, p, rec.Code)
			}
		}
	}
}

func TestDistNotFoundAndTraversal(t *testing.T) {
	t.Parallel()
	h, _ := newDistAPI(t, Options{Dist: newDist(t, 0)})
	paths := []string{
		"/dl/" + distVersion + "/nope",
		"/dl/" + distVersion + "/manifest.json", // existe en disco pero fuera del mapa de binarios
		"/dl/1.0.0/argos-agent_linux_amd64",     // otra version
		"/dl/latest/argos-agent_linux_amd64",
		"/dl/" + distVersion + "/..",
		"/dl/" + distVersion + "/../latest",
		"/dl/../../etc/passwd",
		"/dl/" + distVersion + "/%2e%2e%2flatest",
		"/dl/" + distVersion + "/..%2f..%2fetc%2fpasswd",
		"/dl/" + distVersion + "/%2e%2e",
		"/dl/%2e%2e/" + distVersion + "/argos-agent_linux_amd64",
		"/dl/" + distVersion + "/argos-agent_linux_amd64/",
		"/dl/" + distVersion + "/argos-agent_linux_amd64%00",
		"/dl/" + distVersion + "/..\\latest",
	}
	for _, p := range paths {
		req := httptest.NewRequest("GET", "http://localhost:8080"+p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == 200 {
			t.Errorf("GET %s devolvio 200: %q", p, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "root:") || strings.Contains(rec.Body.String(), distVersion+"\n") {
			t.Errorf("GET %s filtro contenido: %q", p, rec.Body.String())
		}
	}
}

func TestDistRateLimit(t *testing.T) {
	t.Parallel()
	h, _ := newDistAPI(t, Options{Dist: newDist(t, 0)})
	for i := range distLimitBurst {
		if rec := get(t, h, "GET", "/install.sh"); rec.Code != 200 {
			t.Fatalf("solicitud %d status got %d, want 200", i, rec.Code)
		}
	}
	rec := get(t, h, "GET", "/dl/latest/manifest.json")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status got %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("falta Retry-After")
	}
}

func TestDistDisabled(t *testing.T) {
	t.Parallel()
	h, _ := newDistAPI(t, Options{}) // sin ARGOS_AGENT_DIST_DIR (o dist invalida)
	for _, p := range []string{"/install.sh", "/install.ps1", "/dl/latest/manifest.json", "/dl/" + distVersion + "/argos-agent_linux_amd64"} {
		if rec := get(t, h, "GET", p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status got %d, want 404", p, rec.Code)
		}
	}
}

func TestDistFileChangedAfterLoad(t *testing.T) {
	t.Parallel()
	cat := newDist(t, 0)
	h, logs := newDistAPI(t, Options{Dist: cat})
	// Se altera el binario en disco despues del arranque (sin tocar el manifiesto): 404 y log fuerte.
	f, _, ok, err := cat.Open("argos-agent_linux_arm64")
	if err != nil || !ok {
		t.Fatal("Open inicial fallo")
	}
	path := f.Name()
	_ = f.Close()
	if err := os.WriteFile(path, []byte("manipulado, otro tamaño"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "GET", "/dl/"+distVersion+"/argos-agent_linux_arm64")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status got %d, want 404", rec.Code)
	}
	if !strings.Contains(logs.String(), "binario del agente no disponible") {
		t.Errorf("falta el log de error: %s", logs.String())
	}
}

// Una descarga mas lenta que el WriteTimeout global del server debe completarse.
func TestDistSlowDownload(t *testing.T) {
	t.Parallel()
	const size = 48 << 20
	h, _ := newDistAPI(t, Options{Dist: newDist(t, size)})
	srv := httptest.NewUnstartedServer(h)
	srv.Config.WriteTimeout = 300 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)

	req, err := http.NewRequest("GET", srv.URL+"/dl/"+distVersion+"/argos-agent_linux_amd64", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status got %d, want 200", resp.StatusCode)
	}
	first := make([]byte, 1024)
	if _, err := io.ReadFull(resp.Body, first); err != nil {
		t.Fatal(err)
	}
	time.Sleep(900 * time.Millisecond) // el cliente se atasca mas alla del WriteTimeout
	rest, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		t.Fatalf("la descarga lenta se corto: %v", err)
	}
	if got := rest + int64(len(first)); got != size {
		t.Errorf("bytes recibidos got %d, want %d", got, size)
	}
}

func TestInstallCommands(t *testing.T) {
	t.Parallel()
	dist := newDist(t, 0)
	tests := []struct {
		name      string
		publicURL string
		dist      bool
		want      bool
	}{
		{"https con dist", "https://argos.example.com", true, true},
		{"https con puerto", "https://argos.example.com:8443", true, true},
		{"sin dist", "https://argos.example.com", false, false},
		{"http remoto", "http://argos.example.com", true, false},
		{"sin PublicURL", "", true, false},
		{"http localhost (dev)", "http://localhost:8080", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := Options{}
			if tt.publicURL != "" {
				u, err := url.Parse(tt.publicURL)
				if err != nil {
					t.Fatal(err)
				}
				opts.PublicURL = u
			}
			if tt.dist {
				opts.Dist = dist
			}
			h, _ := newDistAPI(t, opts)
			rec, resp := do(t, h, "POST", "/api/v1/hosts/enroll-token", "", `{"hostname":"srv-01"}`, asSession, func(r *http.Request) { r.Host = "localhost:8080" })
			if rec.Code != 201 {
				t.Fatalf("status got %d, want 201 (%s)", rec.Code, rec.Body.String())
			}
			cmds := struct {
				Token    string `json:"enroll_token"`
				Commands *struct {
					Windows string `json:"windows"`
					Linux   string `json:"linux"`
				} `json:"commands"`
			}{}
			if err := json.Unmarshal(resp.Data, &cmds); err != nil {
				t.Fatal(err)
			}
			if !tt.want {
				if cmds.Commands != nil {
					t.Fatalf("commands presente, want omitido: %+v", cmds.Commands)
				}
				return
			}
			if cmds.Commands == nil {
				t.Fatal("falta commands")
			}
			https := strings.HasPrefix(tt.publicURL, "https://")
			for os, c := range map[string]string{"linux": cmds.Commands.Linux, "windows": cmds.Commands.Windows} {
				if !strings.Contains(c, cmds.Token) {
					t.Errorf("%s: el comando no lleva el token", os)
				}
				for _, bad := range []string{"-k", "-L", "-SkipCertificateCheck", "--insecure", "-Insecure"} {
					for _, f := range strings.Fields(strings.NewReplacer("\"", " ", "(", " ", ")", " ").Replace(c)) {
						if f == bad {
							t.Errorf("%s: el comando contiene %s: %s", os, bad, c)
						}
					}
				}
				if https && strings.Contains(c, "http://") {
					t.Errorf("%s: comando https con http://: %s", os, c)
				}
			}
			if https && !strings.Contains(cmds.Commands.Linux, "--proto '=https' --tlsv1.2") {
				t.Errorf("linux sin --proto '=https' --tlsv1.2: %s", cmds.Commands.Linux)
			}
			if !strings.Contains(cmds.Commands.Windows, "Tls12") {
				t.Errorf("windows sin TLS 1.2: %s", cmds.Commands.Windows)
			}
		})
	}
}

// El origen de los comandos sale de ARGOS_PUBLIC_URL: un Host malicioso no lo cambia.
func TestInstallCommandsIgnoreHostHeader(t *testing.T) {
	t.Parallel()
	u, err := url.Parse("https://argos.example.com")
	if err != nil {
		t.Fatal(err)
	}
	h, _ := newDistAPI(t, Options{PublicURL: u, Dist: newDist(t, 0)})
	rec, resp := do(t, h, "POST", "/api/v1/hosts/enroll-token", "", `{"hostname":"srv-01"}`, asSession, func(r *http.Request) {
		r.Host = "localhost:8080"
		r.Header.Set("X-Forwarded-Host", "evil.example.net")
	})
	if rec.Code != 201 {
		t.Fatalf("status got %d", rec.Code)
	}
	if strings.Contains(string(resp.Data), "evil.example.net") || !strings.Contains(string(resp.Data), "https://argos.example.com/install.sh") {
		t.Errorf("origen inesperado en commands: %s", resp.Data)
	}
}

func TestCommandOriginRejectsOddHosts(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"https://a'b.example", "https://a b.example", "https://[::1]:8443"} {
		u, err := url.Parse(raw)
		if err != nil {
			continue // ni siquiera parsea: tampoco llega a config
		}
		if got := commandOrigin(u, true); got != "" {
			t.Errorf("commandOrigin(%q) got %q, want vacio", raw, got)
		}
	}
}

func TestInstallScriptsHardening(t *testing.T) {
	t.Parallel()
	sh, ps := string(installSH), string(installPS1)
	if !strings.HasSuffix(sh, "\nmain \"$@\"\n") {
		t.Error("install.sh debe terminar con main \"$@\" (evita ejecutar un script truncado)")
	}
	if !strings.HasSuffix(ps, "\nMain -Server $Server -Token $Token -Ca $Ca -Inventory $Inventory -ProbeAllow $ProbeAllow -Privileged $Privileged.IsPresent -Insecure $Insecure.IsPresent -Update $Update.IsPresent\n") {
		t.Error("install.ps1 debe terminar con la llamada a Main")
	}
	if strings.Count(sh, "main \"$@\"") != 1 {
		t.Error("install.sh: main solo se invoca una vez, al final")
	}
	if strings.HasPrefix(sh, "\r") || strings.Contains(sh, "\r\n") {
		t.Error("install.sh tiene CRLF: rompe /bin/sh")
	}
	for _, s := range []struct{ name, body string }{{"install.sh", sh}, {"install.ps1", ps}} {
		for _, bad := range []string{"set -x", "set -o xtrace", "curl -k", "-SkipCertificateCheck", "Start-Transcript", "curl -L", "-fsSL -k"} {
			if strings.Contains(s.body, bad) {
				t.Errorf("%s contiene %q", s.name, bad)
			}
		}
		if !strings.Contains(s.body, "--token-stdin") {
			t.Errorf("%s no pasa el token por --token-stdin", s.name)
		}
		if strings.Contains(s.body, "--token \"$token\"") || strings.Contains(s.body, "enroll --token ") {
			t.Errorf("%s pasa el token como argumento del binario", s.name)
		}
	}
	// El token solo se emite hacia el binario por stdin (printf builtin / pipeline), nunca a stdout/stderr/disco.
	for _, bad := range []string{`echo "$token"`, `echo $token`, `>> "$`, "Write-Host $Token", "Write-Output $Token", `"$Token"` + " |", "Set-Content"} {
		if strings.Contains(sh, bad) || strings.Contains(ps, bad) {
			t.Errorf("los scripts contienen %q", bad)
		}
	}
	if !strings.Contains(sh, `printf '%s\n' "$token" | "$dest/argos-agent" enroll`) {
		t.Error("install.sh: el token debe pasar por printf | enroll --token-stdin")
	}
}

func TestInstallScriptsAgentOptions(t *testing.T) {
	t.Parallel()
	sh, ps := string(installSH), string(installPS1)
	// Cada opcion del agente se acepta y se pasa a `argos-agent install`.
	for _, want := range []string{
		`--inventory)`, `--probe-allow)`, `--privileged)`, `--insecure)`, `--update)`,
		`--inventory "$inventory"`, `--probe-allow "$probe_allow"`, `set -- "$@" --privileged`, `set -- "$@" --insecure`,
	} {
		if !strings.Contains(sh, want) {
			t.Errorf("install.sh no maneja %q", want)
		}
	}
	for _, want := range []string{
		`[string]$Inventory`, `[string]$ProbeAllow`, `[switch]$Privileged`, `[switch]$Insecure`, `[switch]$Update`,
		`'--inventory', $Inventory`, `'--probe-allow', $ProbeAllow`, `'--privileged'`, `'--insecure'`, `@('update')`,
	} {
		if !strings.Contains(ps, want) {
			t.Errorf("install.ps1 no maneja %q", want)
		}
	}
	// Lista blanca de caracteres antes de pasar los valores al agente.
	for _, want := range []string{`*[!a-z,]*)`, `*[!0-9a-fA-F:./,]*)`} {
		if !strings.Contains(sh, want) {
			t.Errorf("install.sh sin la lista blanca %q", want)
		}
	}
	for _, want := range []string{`'^[a-z,]+$'`, `'^[0-9a-fA-F:./,]+$'`} {
		if !strings.Contains(ps, want) {
			t.Errorf("install.ps1 sin la lista blanca %q", want)
		}
	}
	// Nada de eval ni de armar comandos como texto: los valores viajan como argumentos separados.
	for _, bad := range []string{"eval ", "Invoke-Expression", "iex ", "sh -c"} {
		if strings.Contains(sh, bad) || strings.Contains(ps, bad) {
			t.Errorf("los scripts contienen %q (riesgo de inyeccion)", bad)
		}
	}
	// --insecure es del agente: ningun curl del script lo recibe ni desactiva la verificacion TLS.
	for _, line := range strings.Split(sh, "\n") {
		if strings.Contains(line, "curl ") && (strings.Contains(line, "--insecure") || strings.Contains(line, " -k")) {
			t.Errorf("install.sh: curl sin verificar TLS: %q", line)
		}
	}
	// --update no pide token y usa el subcomando del agente instalado.
	if !strings.Contains(sh, `"$exe" update`) || !strings.Contains(sh, `--update no se combina con otras opciones`) {
		t.Error("install.sh --update debe ejecutar `argos-agent update` y no combinarse con otras opciones")
	}
	if !strings.Contains(ps, `Invoke-Agent $exe @('update')`) || !strings.Contains(ps, `-Update no se combina con otras opciones`) {
		t.Error("install.ps1 -Update debe ejecutar `argos-agent update` y no combinarse con otras opciones")
	}
}
