package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	healthDBTimeout = 2 * time.Second
	// healthTTL acota el ping a la BD: /health es publico, no debe poder usarse pa saturar Postgres.
	healthTTL = 2 * time.Second
	// maxLoggedPath acota el largo de la ruta en el log (una ruta enorme no debe inflar el journal).
	maxLoggedPath = 200
	// slowRequest: una solicitud que tarda mas sale como Warn aunque haya ido bien (senal temprana de BD fria o sin RAM).
	slowRequest = time.Second
)

// healthState recuerda el ultimo resultado del ping a la BD.
type healthState struct {
	mu   sync.Mutex
	at   time.Time
	ok   bool
	seen bool // ya hubo un resultado (pa loguear solo los cambios)
}

// dbHealthy dice si Postgres responde. Reusa el ultimo resultado healthTTL y loguea solo las transiciones
// (nunca el error crudo: puede traer el host o el usuario de la BD).
func (a *API) dbHealthy(ctx context.Context) bool {
	h := &a.health
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.seen && time.Since(h.at) < healthTTL {
		return h.ok
	}
	pingCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), healthDBTimeout)
	defer cancel()
	ok := a.store.Ping(pingCtx) == nil
	if h.seen && ok != h.ok {
		if ok {
			a.logger.Info("la base de datos volvio a responder")
		} else {
			a.logger.Error("la base de datos no responde")
		}
	}
	h.at, h.ok, h.seen = time.Now(), ok, true
	return ok
}

// healthHandler responde 200 {"status":"ok"} con la BD viva y 503 {"status":"unavailable"} si no; sin autenticacion
// y sin detalles internos.
func (a *API) healthHandler(w http.ResponseWriter, r *http.Request) error {
	if !a.dbHealthy(r.Context()) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return nil
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}

// statusWriter captura el codigo y los bytes de la respuesta.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap deja que http.ResponseController llegue al ResponseWriter real (Flush, deadlines).
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// requestLevel da el nivel del registro: los fallos y las solicitudes lentas suben, y el ruido de rutina (sondas,
// agentes que reportan bien, estaticos y descargas) baja a Debug pa no inundar el journal.
func requestLevel(path string, status int, elapsed time.Duration) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400, elapsed >= slowRequest:
		return slog.LevelWarn
	case path == "/health", strings.HasPrefix(path, "/api/v1/agent/"), !strings.HasPrefix(path, "/api/"):
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

// requestLog registra una linea por solicitud: metodo, ruta, estado, duracion, bytes e IP del cliente. NUNCA la
// query, cabeceras, cookies ni cuerpos (pueden traer tokens o contrasenas).
func (a *API) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		elapsed := time.Since(start)
		level := requestLevel(r.URL.Path, status, elapsed)
		if !a.logger.Enabled(r.Context(), level) {
			return
		}
		if status == http.StatusTooManyRequests {
			log, summary := a.limited.observe(a.clientIP(r).String(), time.Now())
			if summary > 0 {
				a.logger.Log(r.Context(), level, "solicitudes 429 suprimidas del log", "count", summary, "window", limitedLogWindow.String())
			}
			if !log {
				return
			}
		}
		path := r.URL.Path
		if len(path) > maxLoggedPath {
			path = path[:maxLoggedPath] + "..."
		}
		a.logger.Log(r.Context(), level, "solicitud",
			"method", r.Method, "path", path, "status", status,
			"ms", elapsed.Milliseconds(), "bytes", sw.bytes, "ip", a.clientIP(r).String())
	})
}

const (
	// limitedLogWindow es la ventana del muestreo de los 429: una linea por IP y ventana, y un resumen del resto.
	limitedLogWindow = 60 * time.Second
	// maxLimitedKeys acota la memoria del muestreo ante un flood desde muchas IP (al llenarse se vacia).
	maxLimitedKeys = 10_000
)

// limitedSampler evita que un flood de 429 deje una linea WARN por solicitud: deja pasar la primera de cada clave
// (IP) por ventana y cuenta las demas pa un resumen agregado.
type limitedSampler struct {
	mu         sync.Mutex
	logged     map[string]time.Time // clave -> cuando se logueo su ultima linea
	suppressed int
	since      time.Time // inicio de la ventana del resumen
}

// observe registra un 429 de key en now. log dice si hay que escribir la linea de esa solicitud; summary es
// cuantas se suprimieron en la ventana que acaba de cerrarse (0 = nada que resumir aun).
func (s *limitedSampler) observe(key string, now time.Time) (log bool, summary int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logged == nil || len(s.logged) >= maxLimitedKeys {
		s.logged = make(map[string]time.Time)
	}
	if s.since.IsZero() {
		s.since = now
	}
	if last, ok := s.logged[key]; !ok || now.Sub(last) >= limitedLogWindow {
		s.logged[key] = now
		log = true
	} else {
		s.suppressed++
	}
	if now.Sub(s.since) >= limitedLogWindow {
		summary = s.suppressed
		s.suppressed, s.since = 0, now
	}
	return log, summary
}
