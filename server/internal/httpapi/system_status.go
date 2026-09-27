package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"argos/server/internal/beat"
	"argos/server/internal/store"
)

// Umbrales del auto-monitoreo. Cada uno es un multiplo del intervalo de la tarea (o un tiempo fijo pa la cola).
const (
	statusOK   = "ok"
	statusWarn = "warn"
	statusCrit = "crit"

	maintWarnCycles = 2 // mantenimiento sin ciclo OK en mas de 2 intervalos = warn
	maintCritCycles = 5
	evalWarnCycles  = 3 // evaluador sin pasada OK en mas de 3 intervalos = warn
	evalCritCycles  = 10
	queueWarnAge    = 10 * time.Minute // entrega pendiente sin enviar desde hace mas de esto = warn
	queueCritAge    = 30 * time.Minute
	deadCritCount   = 10 // entregas dead en 24 h a partir de las cuales es crit (con 1 o mas es warn)
	deadWindow      = 24 * time.Hour
	maxStatusErrLen = 300
	statusPollEvery = time.Minute
)

// StatusSources son las piezas del proceso que alimentan GET /system/status (las cablea main).
type StatusSources struct {
	// Version es la version del server (ldflags; "dev" si no se inyecto).
	Version string
	// StartedAt es cuando arranco el proceso.
	StartedAt time.Time
	// Maintenance y Evaluator registran el ultimo ciclo del mantenimiento de metricas y del evaluador de alertas.
	Maintenance, Evaluator *beat.Beat
	// MaintenanceInterval y EvalInterval son sus periodos; cero = esa tarea no se monitorea.
	MaintenanceInterval, EvalInterval time.Duration
	// EvalGrace es la espera inicial del evaluador tras el arranque (no juzga antes).
	EvalGrace time.Duration
}

// statusCheck es un chequeo del auto-monitoreo.
type statusCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// statusInput agrupa todo lo que evaluateChecks necesita (funcion pura, probada con valores fijos).
type statusInput struct {
	now       time.Time
	src       StatusSources
	maint     beat.State
	eval      beat.State
	stats     store.SystemStats
	deadTotal int
}

// evaluateChecks aplica los umbrales y devuelve los chequeos en orden fijo.
func evaluateChecks(in statusInput) []statusCheck {
	checks := []statusCheck{}
	if in.src.MaintenanceInterval > 0 {
		checks = append(checks, cycleCheck("maintenance", "mantenimiento de metricas", in.now, in.maint,
			in.src.StartedAt, in.src.MaintenanceInterval, maintWarnCycles, maintCritCycles))
	}
	if in.src.EvalInterval > 0 {
		checks = append(checks, cycleCheck("evaluator", "evaluador de alertas", in.now, in.eval,
			in.src.StartedAt.Add(in.src.EvalGrace), in.src.EvalInterval, evalWarnCycles, evalCritCycles))
	}
	checks = append(checks, queueCheck(in.now, in.stats), deadCheck(in.deadTotal))
	return checks
}

// cycleCheck juzga una tarea periodica: la edad se mide desde su ultimo ciclo OK o, si aun no hubo ninguno, desde
// notBefore (arranque, mas la espera inicial de la tarea).
func cycleCheck(name, label string, now time.Time, st beat.State, notBefore time.Time, interval time.Duration, warnN, critN int) statusCheck {
	ref := st.LastOK
	if ref.IsZero() {
		ref = notBefore
	}
	age := max(now.Sub(ref), 0)
	c := statusCheck{Name: name, Status: statusOK, Detail: fmt.Sprintf("%s: ultimo ciclo correcto hace %s", label, age.Round(time.Second))}
	if st.LastOK.IsZero() {
		c.Detail = fmt.Sprintf("%s: aun sin ciclo correcto desde el arranque (%s)", label, age.Round(time.Second))
	}
	switch {
	case age > time.Duration(critN)*interval:
		c.Status = statusCrit
	case age > time.Duration(warnN)*interval:
		c.Status = statusWarn
	}
	if c.Status != statusOK && st.LastFail.After(st.LastOK) {
		c.Detail += "; el ultimo ciclo fallo (ver el log del server)"
	}
	return c
}

// queueCheck juzga la cola de notificaciones por la antiguedad de la entrega pendiente mas vieja.
func queueCheck(now time.Time, s store.SystemStats) statusCheck {
	c := statusCheck{Name: "notification_queue", Status: statusOK, Detail: fmt.Sprintf("%d pendientes", s.QueuePending)}
	if s.QueueOldestDue == nil {
		return c
	}
	age := max(now.Sub(*s.QueueOldestDue), 0)
	c.Detail = fmt.Sprintf("%d pendientes, la mas antigua esperando hace %s", s.QueuePending, age.Round(time.Second))
	switch {
	case age > queueCritAge:
		c.Status = statusCrit
	case age > queueWarnAge:
		c.Status = statusWarn
	}
	return c
}

// deadCheck avisa de entregas que se dieron por perdidas en las ultimas 24 h.
func deadCheck(total int) statusCheck {
	c := statusCheck{Name: "notification_dead", Status: statusOK, Detail: "sin entregas perdidas en 24 h"}
	if total == 0 {
		return c
	}
	c.Detail = fmt.Sprintf("%d entregas perdidas en 24 h: revisa los canales indicados", total)
	c.Status = statusWarn
	if total >= deadCritCount {
		c.Status = statusCrit
	}
	return c
}

// worstStatus es el peor estado de la lista (ok si esta vacia).
func worstStatus(checks []statusCheck) string {
	worst := statusOK
	for _, c := range checks {
		switch c.Status {
		case statusCrit:
			return statusCrit
		case statusWarn:
			worst = statusWarn
		}
	}
	return worst
}

// statusTracker recuerda que chequeos estan en crit pa loguear un solo ERROR por transicion (no uno por consulta).
type statusTracker struct {
	mu   sync.Mutex
	crit map[string]bool
}

// noteTransitions loguea ERROR por cada chequeo que acaba de pasar a crit y INFO por cada uno que se recupero.
func (t *statusTracker) noteTransitions(ctx context.Context, a *API, checks []statusCheck) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.crit == nil {
		t.crit = map[string]bool{}
	}
	for _, c := range checks {
		isCrit := c.Status == statusCrit
		was := t.crit[c.Name]
		t.crit[c.Name] = isCrit
		switch {
		case isCrit && !was:
			a.logger.ErrorContext(ctx, "auto-monitoreo: chequeo en estado critico", "check", c.Name, "detail", c.Detail)
		case !isCrit && was:
			a.logger.InfoContext(ctx, "auto-monitoreo: chequeo recuperado", "check", c.Name, "status", c.Status)
		}
	}
}

type beatDTO struct {
	LastOKAt     *time.Time `json:"last_ok_at"`
	LastErrorAt  *time.Time `json:"last_error_at"`
	LastError    *string    `json:"last_error"`
	AgeSeconds   *int64     `json:"age_seconds"`
	IntervalSecs int64      `json:"interval_seconds"`
	Monitored    bool       `json:"monitored"`
}

type deadChannelDTO struct {
	ChannelID  string       `json:"channel_id"`
	Name       string       `json:"name"`
	OwnerGroup *groupRefDTO `json:"owner_group"`
	Count      int          `json:"count"`
}

func newBeatDTO(st beat.State, now time.Time, interval time.Duration) beatDTO {
	d := beatDTO{IntervalSecs: int64(interval.Seconds()), Monitored: interval > 0}
	if !st.LastOK.IsZero() {
		t := st.LastOK.UTC()
		age := int64(max(now.Sub(st.LastOK), 0).Seconds())
		d.LastOKAt, d.AgeSeconds = &t, &age
	}
	if !st.LastFail.IsZero() {
		t := st.LastFail.UTC()
		d.LastErrorAt = &t
		msg := st.LastErr
		if len(msg) > maxStatusErrLen {
			msg = msg[:maxStatusErrLen] + "..."
		}
		d.LastError = &msg
	}
	return d
}

// collectStatus lee la BD y los latidos y arma los chequeos; tambien anota las transiciones a crit.
func (a *API) collectStatus(ctx context.Context) (statusInput, []statusCheck, error) {
	now := a.now()
	stats, err := a.store.SystemStats(ctx, now.Add(-deadWindow))
	if err != nil {
		return statusInput{}, nil, err
	}
	dead := 0
	for _, d := range stats.Dead {
		dead += d.Count
	}
	src := a.opts.Status
	in := statusInput{now: now, src: src, maint: src.Maintenance.Snapshot(), eval: src.Evaluator.Snapshot(), stats: stats, deadTotal: dead}
	checks := evaluateChecks(in)
	a.status.noteTransitions(ctx, a, checks)
	return in, checks, nil
}

// systemStatus (GET /system/status, solo admin) resume la salud del servicio: tareas periodicas, cola de
// notificaciones, tamaño de la BD y version. /health sigue siendo la sonda barata de liveness.
func (a *API) systemStatus(w http.ResponseWriter, r *http.Request) error {
	in, checks, err := a.collectStatus(r.Context())
	if err != nil {
		return err
	}
	dead := make([]deadChannelDTO, 0, len(in.stats.Dead))
	for _, d := range in.stats.Dead {
		dto := deadChannelDTO{ChannelID: d.ChannelID, Name: d.Name, Count: d.Count}
		if d.GroupID != nil && d.GroupName != nil {
			dto.OwnerGroup = &groupRefDTO{ID: *d.GroupID, Name: *d.GroupName}
		}
		dead = append(dead, dto)
	}
	var oldest *int64
	if in.stats.QueueOldestDue != nil {
		s := int64(max(in.now.Sub(*in.stats.QueueOldestDue), 0).Seconds())
		oldest = &s
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         worstStatus(checks),
		"version":        in.src.Version,
		"started_at":     in.src.StartedAt.UTC(),
		"uptime_seconds": int64(max(in.now.Sub(in.src.StartedAt), 0).Seconds()),
		"maintenance":    newBeatDTO(in.maint, in.now, in.src.MaintenanceInterval),
		"evaluator":      newBeatDTO(in.eval, in.now, in.src.EvalInterval),
		"notifications": map[string]any{
			"pending":                    in.stats.QueuePending,
			"oldest_pending_age_seconds": oldest,
			"dead_24h_total":             in.deadTotal,
			"dead_24h":                   dead,
		},
		"database": map[string]any{"size_bytes": in.stats.DBSizeBytes},
		"checks":   checks,
	})
	return nil
}

// RunStatusMonitor evalua el auto-monitoreo cada minuto hasta que ctx se cancele, pa que un check que pasa a crit
// se loguee (un solo ERROR) aunque nadie tenga abierta la pantalla. Un fallo de la BD ya lo reportan los demas
// componentes: aqui solo se anota en Debug.
func (a *API) RunStatusMonitor(ctx context.Context) {
	ticker := time.NewTicker(statusPollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, _, err := a.collectStatus(ctx); err != nil {
				a.logger.DebugContext(ctx, "auto-monitoreo: no se pudo leer el estado", "err", err)
			}
		}
	}
}
