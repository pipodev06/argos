package httpapi

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"argos/server/internal/store"
)

// metricsScrapesPerMinute es el tope por IP de GET /metrics: un scraper usa ~4 por minuto (cada 15 s); el margen cubre
// varios detras del mismo proxy sin dejar que un cliente cargue la BD a peticiones (cada una consulta estado y conteos).
const metricsScrapesPerMinute = 30

const metricsContentType = "text/plain; version=0.0.4; charset=utf-8"

// metrics (GET /metrics) expone en formato de texto Prometheus agregados globales del servicio. Se autentica con un
// bearer estatico (ARGOS_METRICS_TOKEN) porque un scraper no tiene sesion; sin token configurado la ruta no existe (404).
// Solo hay conteos y edades: ningun hostname, grupo, canal ni id sale en un label (no hay fuga entre clientes).
func (a *API) metrics(w http.ResponseWriter, r *http.Request) error {
	token := a.opts.MetricsToken
	if token == "" {
		http.NotFound(w, r)
		return nil
	}
	// El limite va antes del token: tambien frena los intentos de adivinarlo.
	if ok, wait := a.scrapeLimiter.allow(ipKey(a.clientIP(r)), a.now()); !ok {
		return rateLimited(wait)
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		w.WriteHeader(http.StatusUnauthorized)
		return nil
	}
	in, _, err := a.collectStatus(r.Context())
	if err != nil {
		return err
	}
	fleet, err := a.store.FleetCounts(r.Context())
	if err != nil {
		return err
	}

	var b strings.Builder
	metric := func(name, typ, help string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
	}
	metric("argos_build_info", "gauge", "Version del server.")
	fmt.Fprintf(&b, "argos_build_info{version=%q} 1\n", in.src.Version)
	metric("argos_uptime_seconds", "gauge", "Segundos desde el arranque del server.")
	fmt.Fprintf(&b, "argos_uptime_seconds %d\n", int64(max(in.now.Sub(in.src.StartedAt), 0).Seconds()))

	metric("argos_task_last_ok_age_seconds", "gauge", "Segundos desde el ultimo ciclo correcto de una tarea periodica (sin ciclo aun: desde el arranque).")
	for _, t := range []struct {
		name     string
		interval time.Duration
		st       time.Time
	}{
		{"maintenance", in.src.MaintenanceInterval, in.maint.LastOK},
		{"evaluator", in.src.EvalInterval, in.eval.LastOK},
	} {
		if t.interval <= 0 {
			continue
		}
		ref := t.st
		if ref.IsZero() {
			ref = in.src.StartedAt
		}
		fmt.Fprintf(&b, "argos_task_last_ok_age_seconds{task=%q} %d\n", t.name, int64(max(in.now.Sub(ref), 0).Seconds()))
	}

	var oldest int64
	if in.stats.QueueOldestDue != nil {
		oldest = int64(max(in.now.Sub(*in.stats.QueueOldestDue), 0).Seconds())
	}
	metric("argos_notification_queue_pending", "gauge", "Entregas de notificacion pendientes.")
	fmt.Fprintf(&b, "argos_notification_queue_pending %d\n", in.stats.QueuePending)
	metric("argos_notification_queue_oldest_age_seconds", "gauge", "Antiguedad de la entrega pendiente mas vieja (0 si no hay).")
	fmt.Fprintf(&b, "argos_notification_queue_oldest_age_seconds %d\n", oldest)
	metric("argos_notification_dead_24h", "gauge", "Entregas de notificacion perdidas en las ultimas 24 h.")
	fmt.Fprintf(&b, "argos_notification_dead_24h %d\n", in.deadTotal)
	metric("argos_db_size_bytes", "gauge", "Tamano de la base de datos.")
	fmt.Fprintf(&b, "argos_db_size_bytes %d\n", in.stats.DBSizeBytes)

	metric("argos_hosts", "gauge", "Hosts por tipo (agent, device) y estado.")
	for _, k := range []struct {
		kind string
		c    store.StatusCounts
	}{{"agent", fleet.Agents}, {"device", fleet.Devices}} {
		for _, s := range []struct {
			status string
			n      int
		}{{"up", k.c.Up}, {"down", k.c.Down}, {"offline", k.c.Offline}, {"pending", k.c.Pending}, {"disabled", k.c.Disabled}} {
			fmt.Fprintf(&b, "argos_hosts{kind=%q,status=%q} %d\n", k.kind, s.status, s.n)
		}
	}
	metric("argos_alerts_open", "gauge", "Alertas no resueltas por severidad.")
	for _, sev := range []string{"critical", "warn", "info"} {
		fmt.Fprintf(&b, "argos_alerts_open{severity=%q} %d\n", sev, fleet.AlertsOpen[sev])
	}

	w.Header().Set("Content-Type", metricsContentType)
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write([]byte(b.String())); err != nil {
		a.logger.WarnContext(r.Context(), "escribiendo /metrics", "err", err)
	}
	return nil
}
