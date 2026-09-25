package httpapi

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"argos/server/internal/store"
)

const (
	webCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
		"font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"
	webIndex = "index.html"
)

// OpenWebDir abre el directorio del build del frontend (ARGOS_WEB_DIR) acotado con os.Root: ni ".." ni
// symlinks pueden salir de el. Exige que exista index.html. El llamador cierra el Root al apagar.
func OpenWebDir(dir string) (*os.Root, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("abriendo ARGOS_WEB_DIR: %w", err)
	}
	st, err := fs.Stat(root.FS(), webIndex)
	if err != nil || !st.Mode().IsRegular() {
		if cerr := root.Close(); cerr != nil {
			err = fmt.Errorf("%w (cerrando: %w)", err, cerr)
		}
		return nil, fmt.Errorf("ARGOS_WEB_DIR %q no contiene %s (falta el build: npm run build en web/): %w", dir, webIndex, err)
	}
	return root, nil
}

// registerWeb sirve el frontend compilado. Va DESPUES de todas las rutas explicitas: "GET /" solo recibe lo
// que ninguna otra ruta reclama (el mux atiende HEAD como GET y responde 405 a los demas metodos).
func (a *API) registerWeb(mux *http.ServeMux) {
	if a.opts.Web == nil {
		return
	}
	a.route(mux, "GET /", accessPublic, a.serveWeb)
}

// reservedPath son los espacios de rutas del server que nunca caen al fallback del SPA.
func reservedPath(p string) bool {
	return p == "/health" || strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/dl/") || strings.HasPrefix(p, "/install.")
}

func (a *API) serveWeb(w http.ResponseWriter, r *http.Request) error {
	p := r.URL.Path
	if reservedPath(p) {
		return store.ErrNotFound
	}
	h := w.Header()
	h.Set("Content-Security-Policy", webCSP)

	name := path.Clean("/" + p)[1:]
	if name == "" || strings.ContainsAny(name, `\`) || !fs.ValidPath(name) {
		name = webIndex
	}
	f, st, err := a.openWebFile(name)
	if err != nil {
		if strings.HasPrefix(p, "/assets/") {
			return store.ErrNotFound // un asset faltante no es una ruta del SPA
		}
		name = webIndex
		if f, st, err = a.openWebFile(name); err != nil {
			return fmt.Errorf("abriendo %s: %w", webIndex, err)
		}
	}
	defer func() {
		if err := f.Close(); err != nil {
			a.logger.DebugContext(r.Context(), "cerrando archivo del frontend", "err", err)
		}
	}()
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable") // nombres con hash de contenido
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		return fmt.Errorf("archivo del frontend %s no es seekable", name)
	}
	http.ServeContent(w, r, name, st.ModTime(), rs)
	return nil
}

// openWebFile abre un archivo regular; los directorios cuentan como inexistentes (sin listado).
func (a *API) openWebFile(name string) (fs.File, fs.FileInfo, error) {
	f, err := a.opts.Web.Open(name)
	if err != nil {
		return nil, nil, fmt.Errorf("abriendo %s: %w", name, err)
	}
	st, err := f.Stat()
	if err == nil && !st.Mode().IsRegular() {
		err = fs.ErrNotExist
	}
	if err != nil {
		_ = f.Close() // ya hay un error que reportar
		return nil, nil, fmt.Errorf("archivo %s: %w", name, err)
	}
	return f, st, nil
}
