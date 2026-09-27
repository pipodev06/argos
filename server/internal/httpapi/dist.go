package httpapi

import (
	_ "embed" // go:embed de los scripts de instalacion
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"argos/server/internal/store"
)

// Scripts de instalacion: estaticos (sin token ni URL) y embebidos en el binario del server.
var (
	//go:embed installers/install.sh
	installSH []byte
	//go:embed installers/install.ps1
	installPS1 []byte
)

const (
	// downloadWriteWindow es el tiempo total que se le da a una descarga: el WriteTimeout global (10 s)
	// cortaria conexiones lentas; aca se amplia solo pa este handler.
	downloadWriteWindow = 30 * time.Minute
	distLimitBurst      = 30 // por IP y minuto (un install consume ~3)
)

// originRe es la forma estricta del origen que se incrusta en los comandos (defensa en profundidad: el
// origen sale de ARGOS_PUBLIC_URL, nunca de la solicitud).
var originRe = regexp.MustCompile(`^https?://[A-Za-z0-9.\-]+(:[0-9]{1,5})?$`)

// commandOrigin devuelve el origen pa los comandos de instalacion, o "" si no hay dist configurada o
// PublicURL no es apta: solo https, o http en loopback (desarrollo explicito).
func commandOrigin(u *url.URL, hasDist bool) string {
	if u == nil || !hasDist {
		return ""
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"):
	default:
		return ""
	}
	origin := u.Scheme + "://" + u.Host
	if !originRe.MatchString(origin) {
		return ""
	}
	return origin
}

// installCommands arma los comandos de una linea pa el dashboard. El token es hex de randomToken.
func installCommands(origin, token string) map[string]string {
	proto := "--proto '=https' "
	if strings.HasPrefix(origin, "http://") {
		proto = "" // solo loopback de desarrollo
	}
	linux := fmt.Sprintf("curl %s--tlsv1.2 -fsSL %s/install.sh | sh -s -- --server %s --token %s", proto, origin, origin, token)
	windows := fmt.Sprintf("powershell -NoProfile -ExecutionPolicy Bypass -Command \"[Net.ServicePointManager]::SecurityProtocol = "+
		"[Net.SecurityProtocolType]::Tls12; & ([scriptblock]::Create((Invoke-RestMethod -UseBasicParsing '%s/install.ps1'))) "+
		"-Server '%s' -Token '%s'\"", origin, origin, token)
	return map[string]string{"linux": linux, "windows": windows}
}

// registerDist registra las rutas publicas de instaladores y descargas. Van fuera de /api/ y bajo
// hostAllowlist. HOOK del SPA: el fallback del frontend ("GET /") debe registrarse DESPUES de esta llamada
// (los patrones mas especificos ya ganan, pero asi queda explicito).
func (a *API) registerDist(mux *http.ServeMux) {
	a.route(mux, "GET /install.sh", accessPublic, a.installScript("install.sh", installSH))
	a.route(mux, "GET /install.ps1", accessPublic, a.installScript("install.ps1", installPS1))
	a.route(mux, "GET /dl/latest/manifest.json", accessPublic, a.distManifest)
	a.route(mux, "GET /dl/latest/manifest.json.sig", accessPublic, a.distManifestSig)
	a.route(mux, "GET /dl/{version}/{file}", accessPublic, a.distDownload)
}

// distGuard aplica el limite por IP y el 404 cuando no hay dist valida.
func (a *API) distGuard(r *http.Request) error {
	if ok, wait := a.distLimiter.allow(ipKey(a.clientIP(r)), a.now()); !ok {
		return rateLimited(wait)
	}
	if a.opts.Dist == nil {
		return store.ErrNotFound
	}
	return nil
}

func (a *API) installScript(name string, body []byte) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		if err := a.distGuard(r); err != nil {
			return err
		}
		h := w.Header()
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			if _, err := w.Write(body); err != nil {
				a.logger.DebugContext(r.Context(), "escribiendo script de instalacion", "script", name, "err", err)
			}
		}
		return nil
	}
}

func (a *API) distManifest(w http.ResponseWriter, r *http.Request) error {
	if err := a.distGuard(r); err != nil {
		return err
	}
	body := a.opts.Dist.Manifest()
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		if _, err := w.Write(body); err != nil {
			a.logger.DebugContext(r.Context(), "escribiendo manifiesto", "err", err)
		}
	}
	return nil
}

// distManifestSig sirve la firma ed25519 del manifiesto (manifest.json.sig, hex). El server NUNCA la
// genera ni la valida (no tiene la clave privada ni la publica): solo la sirve tal cual quedo al
// empaquetar (H7, docs/decisiones/2026-09-26-seguridad-multicliente.md #11). La verifica el agente con
// su clave publica embebida.
func (a *API) distManifestSig(w http.ResponseWriter, r *http.Request) error {
	if err := a.distGuard(r); err != nil {
		return err
	}
	body := a.opts.Dist.Signature()
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		if _, err := w.Write(body); err != nil {
			a.logger.DebugContext(r.Context(), "escribiendo firma del manifiesto", "err", err)
		}
	}
	return nil
}

func (a *API) distDownload(w http.ResponseWriter, r *http.Request) error {
	if err := a.distGuard(r); err != nil {
		return err
	}
	dist := a.opts.Dist
	// Mapa cerrado: la version y el nombre solo se comparan, nunca forman parte de una ruta.
	if r.PathValue("version") != dist.Version {
		return store.ErrNotFound
	}
	f, meta, ok, err := dist.Open(r.PathValue("file"))
	if !ok {
		return store.ErrNotFound
	}
	if err != nil {
		a.logger.ErrorContext(r.Context(), "binario del agente no disponible: las descargas responden 404", "file", r.PathValue("file"), "err", err)
		return store.ErrNotFound
	}
	defer func() {
		if err := f.Close(); err != nil {
			a.logger.DebugContext(r.Context(), "cerrando binario del agente", "err", err)
		}
	}()

	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Now().Add(downloadWriteWindow)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		a.logger.WarnContext(r.Context(), "no se pudo ampliar el plazo de escritura de la descarga", "err", err)
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", `attachment; filename="`+meta.Name+`"`)
	h.Set("ETag", `"`+meta.SHA256+`"`)
	h.Set("Cache-Control", "public, max-age=31536000, immutable") // inmutable: la version esta en la URL
	http.ServeContent(w, r, meta.Name, time.Time{}, f)
	return nil
}
