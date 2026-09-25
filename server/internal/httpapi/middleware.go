package httpapi

import (
	"context"
	"errors"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"argos/server/internal/auth"
	"argos/server/internal/store"
)

// securityHeaders fija las cabeceras de seguridad en toda respuesta.
func (a *API) securityHeaders(next http.Handler) http.Handler {
	hsts := a.opts.PublicURL != nil && a.opts.PublicURL.Scheme == "https"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		if hsts {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowlist rechaza solicitudes cuyo Host no sea el publico ni localhost
// (defensa contra DNS rebinding). Se exceptuan /health y /api/v1/agent/*: los usan
// clientes que no son navegadores (agentes, sondas) y pueden llegar por IP; no
// llevan cookie, asi que el rebinding no les aplica.
func (a *API) hostAllowlist(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/api/v1/agent/") {
			next.ServeHTTP(w, r)
			return
		}
		if !a.allowedHosts[normalizeHost(r.Host)] {
			writeError(w, http.StatusMisdirectedRequest, "invalid_host", "host no permitido")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// normalizeHost deja solo el nombre de host en minusculas, sin puerto ni corchetes IPv6.
func normalizeHost(hostport string) string {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	return strings.ToLower(host)
}

// crossOrigin aplica net/http.CrossOriginProtection (Sec-Fetch-Site / Origin) a las
// solicitudes que modifican estado. Clientes sin cabeceras de navegador (agentes, curl) pasan.
func (a *API) crossOrigin() func(http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	if u := a.opts.PublicURL; u != nil {
		// Detras de un proxy que reescribe Host, el Origin del navegador sigue siendo la URL publica.
		if err := cop.AddTrustedOrigin(u.Scheme + "://" + u.Host); err != nil {
			a.logger.Error("origen publico invalido pa CrossOriginProtection", "err", err)
		}
	}
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusForbidden, "cross_origin", "solicitud entre origenes rechazada")
	}))
	return cop.Handler
}

// requireJSON exige Content-Type application/json en toda escritura de /api con
// body (415 si no): un formulario cross-site no puede fabricar ese Content-Type sin
// preflight. Las escrituras sin body (logout, revoke) no lo necesitan.
func (a *API) requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !isSafeMethod(r.Method) && r.ContentLength != 0 {
			mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type debe ser application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// --- sesion ---

// sessionUser es el usuario autenticado de la solicitud (rol y estado leidos de la BD en cada request).
type sessionUser struct {
	ID                 string
	Username           string
	Role               string
	MustChangePassword bool
	// Scope es el ambito de hosts del usuario, leido de la BD en cada request (nunca cacheado: revocar un
	// grupo aplica en la siguiente peticion). Lo exigen las consultas del store sobre hosts.
	Scope store.Scope
	// CanInventory: permiso (por usuario) de ver procesos y servicios. Un admin no lo necesita: lo ve todo.
	CanInventory bool
	sessionHash  []byte
}

type ctxKey struct{}

func currentUser(r *http.Request) *sessionUser {
	u, _ := r.Context().Value(ctxKey{}).(*sessionUser)
	return u
}

// requireSession valida la cookie de sesion (existencia, inactividad, limite absoluto,
// usuario habilitado) y deja el usuario en el contexto. Con allowPasswordChange=false
// rechaza a quien tiene un cambio de contraseña pendiente.
func (a *API) requireSession(allowPasswordChange bool, next handlerFunc) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		u, err := a.authenticateSession(w, r)
		if err != nil {
			return err
		}
		if u.MustChangePassword && !allowPasswordChange {
			return errMustChangePass
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, u)
		return next(w, r.WithContext(ctx))
	}
}

func (a *API) requireAdmin(next handlerFunc) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		u := currentUser(r)
		if u == nil || u.Role != store.RoleAdmin {
			if u != nil {
				e := actor(u)
				e.Event, e.Outcome = evAuthzDenied, store.OutcomeDenied
				a.audit(r, e, map[string]any{"method": r.Method, "route": r.Pattern})
			}
			return errForbidden
		}
		return next(w, r)
	}
}

// requireFullScope deja pasar solo a quien ve todos los hosts (admin o viewer con all_groups): los canales de
// notificacion no tienen grupo, asi que un viewer restringido no puede leerlos. Responde 403 y audita el intento,
// igual que requireAdmin con un viewer en una ruta de admin.
func (a *API) requireFullScope(next handlerFunc) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		u := currentUser(r)
		if u == nil || !u.Scope.All() {
			if u != nil {
				e := actor(u)
				e.Event, e.Outcome = evAuthzDenied, store.OutcomeDenied
				a.audit(r, e, map[string]any{"method": r.Method, "route": r.Pattern})
			}
			return errForbidden
		}
		return next(w, r)
	}
}

func (a *API) authenticateSession(w http.ResponseWriter, r *http.Request) (*sessionUser, error) {
	c, err := r.Cookie(a.cookieName)
	if err != nil || c.Value == "" || len(c.Value) > auth.MaxSessionIDChars {
		return nil, errUnauthenticated
	}
	hash := auth.HashSessionID(c.Value)
	ctx := r.Context()

	si, err := a.store.SessionByHash(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		a.clearSessionCookie(w)
		return nil, errUnauthenticated
	}
	if err != nil {
		return nil, err // infraestructura: 500, no un 401
	}

	now := a.now()
	if now.After(si.ExpiresAt) || now.Sub(si.LastSeenAt) > SessionIdleTimeout || si.User.DisabledAt != nil {
		if err := a.store.DeleteSession(ctx, hash); err != nil {
			a.logger.WarnContext(ctx, "no se pudo borrar la sesion vencida", "err", err)
		}
		a.clearSessionCookie(w)
		return nil, errUnauthenticated
	}
	if now.Sub(si.LastSeenAt) >= sessionTouchInterval {
		if err := a.store.TouchSession(ctx, hash, now); err != nil {
			a.logger.WarnContext(ctx, "no se pudo actualizar last_seen_at", "err", err)
		}
	}
	return &sessionUser{
		ID: si.User.ID, Username: si.User.Username, Role: si.User.Role,
		MustChangePassword: si.User.MustChangePassword, sessionHash: hash,
		Scope: si.User.Scope(), CanInventory: si.User.CanInventory,
	}, nil
}

// setSessionCookie emite la cookie de sesion: sin Domain ni Max-Age (cookie de sesion
// del navegador; los limites reales los impone el servidor), HttpOnly, SameSite=Strict
// y Secure (requisito del prefijo __Host-) salvo ARGOS_INSECURE_COOKIES en dev.
func (a *API) setSessionCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, a.sessionCookie(id, 0))
}

// clearSessionCookie expira la cookie en el navegador (Max-Age=0).
func (a *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, a.sessionCookie("", -1))
}

func (a *API) sessionCookie(value string, maxAge int) *http.Cookie {
	//nolint:gosec // Secure solo es false con ARGOS_INSECURE_COOKIES (dev sobre http); por defecto siempre true
	return &http.Cookie{
		Name: a.cookieName, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: !a.opts.InsecureCookies, SameSite: http.SameSiteStrictMode,
	}
}

func retryAfterSeconds(d time.Duration) string {
	secs := int((d + time.Second - 1) / time.Second)
	return strconv.Itoa(max(secs, 1))
}
