package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrCheckExists: ya hay un chequeo con ese nombre (sin distinguir mayusculas).
	ErrCheckExists = errors.New("ya existe un chequeo con ese nombre")
	// ErrTooManyChecks: ya hay MaxServiceChecks chequeos.
	ErrTooManyChecks = errors.New("demasiados chequeos de servicio")
	// ErrCheckNotFound: el chequeo referenciado (en el body de una regla) no existe.
	ErrCheckNotFound = errors.New("chequeo de servicio no encontrado")
)

// Topes y politicas de los chequeos de servicio.
const (
	// MaxServiceChecks es el maximo de definiciones.
	MaxServiceChecks = 200
	// MaxChecksPerHost es el maximo de chequeos que se entregan a un host (los demas se truncan por id).
	MaxChecksPerHost = 50
	// CheckSampleEvery es cada cuanto se guarda una muestra de latencia sin transicion.
	CheckSampleEvery = 15 * time.Minute

	checkNameKey     = "scd_name_lower_key"
	checkGroupFKey   = "service_check_defs_scope_group_id_fkey"
	ruleCheckFKey    = "alert_rules_check_id_fkey"
	checkStateMaxAge = 3 * 24 * time.Hour
)

// Tipos de chequeo.
const (
	CheckTCP     = "tcp"
	CheckHTTP    = "http"
	CheckTLSCert = "tls_cert"
	CheckService = "service"
)

// ServiceCheckDef es la definicion de un chequeo. SecretEnc es el blob cifrado (la API nunca lo devuelve).
type ServiceCheckDef struct {
	ID              string
	Name            string
	Type            string
	IntervalSeconds int
	TimeoutMs       int
	Retries         int
	TargetHost      *string
	TargetPort      *int
	URL             *string
	ExpectStatus    *string
	ExpectText      *string
	ServiceName     *string
	WarnMs          *int
	CritMs          *int
	WarnDays        *int
	CritDays        *int
	HeaderName      *string
	SecretEnc       []byte
	SecretKeyID     *string
	Scope           AlertScope
	// GroupName es el nombre del grupo del alcance (solo lectura).
	GroupName *string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

const checkDefSelect = `
SELECT d.id, d.name, d.type, d.interval_seconds, d.timeout_ms, d.retries, d.target_host, d.target_port, d.url,
       d.expect_status, d.expect_text, d.service_name, d.warn_ms, d.crit_ms, d.warn_days, d.crit_days,
       d.header_name, d.secret_enc, d.secret_key_id, d.scope_type, d.scope_group_id, g.name,
       d.enabled, d.created_at, d.updated_at
  FROM service_check_defs d LEFT JOIN host_groups g ON g.id = d.scope_group_id`

func scanCheckDef(row pgx.Row) (ServiceCheckDef, error) {
	var d ServiceCheckDef
	err := row.Scan(&d.ID, &d.Name, &d.Type, &d.IntervalSeconds, &d.TimeoutMs, &d.Retries, &d.TargetHost, &d.TargetPort,
		&d.URL, &d.ExpectStatus, &d.ExpectText, &d.ServiceName, &d.WarnMs, &d.CritMs, &d.WarnDays, &d.CritDays,
		&d.HeaderName, &d.SecretEnc, &d.SecretKeyID, &d.Scope.Type, &d.Scope.GroupID, &d.GroupName,
		&d.Enabled, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

// hostInScopeSQL es verdadero si el host h (alias) entra en el alcance del chequeo d (alias).
const hostInScopeSQL = `(d.scope_type = 'all'
  OR (d.scope_type = 'group' AND d.scope_group_id = h.group_id))`

// ListServiceChecks devuelve las definiciones por nombre (tope MaxServiceChecks). Un ambito restringido solo ve
// las de alcance `all` y las de sus grupos.
func (s *Store) ListServiceChecks(ctx context.Context, scope Scope) ([]ServiceCheckDef, error) {
	args := argList{MaxServiceChecks}
	rows, err := s.pool.Query(ctx,
		checkDefSelect+` WHERE `+scope.configCond("d.scope_type", "d.scope_group_id", &args)+` ORDER BY lower(d.name), d.id LIMIT $1`,
		[]any(args)...)
	if err != nil {
		return nil, fmt.Errorf("listando chequeos: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ServiceCheckDef, error) { return scanCheckDef(row) })
	if err != nil {
		return nil, fmt.Errorf("leyendo chequeos: %w", err)
	}
	return out, nil
}

// ServiceCheckByID devuelve un chequeo. ErrNotFound si no existe o queda fuera del ambito.
func (s *Store) ServiceCheckByID(ctx context.Context, scope Scope, id string) (ServiceCheckDef, error) {
	d, err := checkDefByID(ctx, s.pool, id, "")
	if err != nil {
		return ServiceCheckDef{}, err
	}
	if !scope.sees(d.Scope) {
		return ServiceCheckDef{}, ErrNotFound
	}
	return d, nil
}

func checkDefByID(ctx context.Context, q rowQuerier, id, lock string) (ServiceCheckDef, error) {
	d, err := scanCheckDef(q.QueryRow(ctx, checkDefSelect+` WHERE d.id = $1 `+lock, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ServiceCheckDef{}, ErrNotFound
	}
	if err != nil {
		return ServiceCheckDef{}, fmt.Errorf("leyendo chequeo: %w", err)
	}
	return d, nil
}

func mapCheckErr(err error, what string) error {
	switch {
	case isUniqueViolation(err, checkNameKey):
		return ErrCheckExists
	case isFKViolation(err, checkGroupFKey):
		return ErrGroupNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

// CreateServiceCheck inserta un chequeo con el id ya generado (el secreto se cifro con ese id como AAD) y ya
// validado por quien llama. ErrTooManyChecks, ErrCheckExists o ErrGroupNotFound segun el caso.
func (s *Store) CreateServiceCheck(ctx context.Context, d ServiceCheckDef, createdBy string) (ServiceCheckDef, error) {
	var out ServiceCheckDef
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('argos.service_checks.cap'))`); err != nil {
			return fmt.Errorf("lock de chequeos: %w", err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM service_check_defs`).Scan(&n); err != nil {
			return fmt.Errorf("contando chequeos: %w", err)
		}
		if n >= MaxServiceChecks {
			return ErrTooManyChecks
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO service_check_defs (id, name, type, interval_seconds, timeout_ms, retries, target_host, target_port,
			        url, expect_status, expect_text, service_name, warn_ms, crit_ms, warn_days, crit_days, header_name,
			        secret_enc, secret_key_id, scope_type, scope_group_id, enabled, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21,
			         $22, NULLIF($23, '')::uuid)`,
			d.ID, d.Name, d.Type, d.IntervalSeconds, d.TimeoutMs, d.Retries, d.TargetHost, d.TargetPort,
			d.URL, d.ExpectStatus, d.ExpectText, d.ServiceName, d.WarnMs, d.CritMs, d.WarnDays, d.CritDays, d.HeaderName,
			d.SecretEnc, d.SecretKeyID, d.Scope.Type, d.Scope.GroupID, d.Enabled, createdBy); err != nil {
			return mapCheckErr(err, "insertando chequeo")
		}
		var err error
		out, err = checkDefByID(ctx, tx, d.ID, "")
		return err
	})
	if err != nil {
		return ServiceCheckDef{}, err
	}
	return out, nil
}

// UpdateServiceCheck bloquea el chequeo, deja que mutate lo modifique (su error se devuelve tal cual) y guarda
// todos los campos editables (no el tipo). ErrNotFound, ErrCheckExists o ErrGroupNotFound segun el caso.
func (s *Store) UpdateServiceCheck(ctx context.Context, id string, mutate func(*ServiceCheckDef) error) (ServiceCheckDef, error) {
	var out ServiceCheckDef
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		d, err := checkDefByID(ctx, tx, id, "FOR UPDATE OF d")
		if err != nil {
			return err
		}
		if err := mutate(&d); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE service_check_defs SET name = $2, interval_seconds = $3, timeout_ms = $4, retries = $5,
			        target_host = $6, target_port = $7, url = $8, expect_status = $9, expect_text = $10,
			        service_name = $11, warn_ms = $12, crit_ms = $13, warn_days = $14, crit_days = $15,
			        header_name = $16, secret_enc = $17, secret_key_id = $18, scope_type = $19, scope_group_id = $20,
			        enabled = $21, updated_at = now()
			  WHERE id = $1`,
			id, d.Name, d.IntervalSeconds, d.TimeoutMs, d.Retries, d.TargetHost, d.TargetPort, d.URL, d.ExpectStatus,
			d.ExpectText, d.ServiceName, d.WarnMs, d.CritMs, d.WarnDays, d.CritDays, d.HeaderName, d.SecretEnc,
			d.SecretKeyID, d.Scope.Type, d.Scope.GroupID, d.Enabled); err != nil {
			return mapCheckErr(err, "actualizando chequeo")
		}
		out, err = checkDefByID(ctx, tx, id, "")
		return err
	})
	if err != nil {
		return ServiceCheckDef{}, err
	}
	return out, nil
}

// DeleteServiceCheck borra el chequeo (su estado y sus reglas caen en cascada) y devuelve nombre y tipo.
// ErrNotFound si no existe.
func (s *Store) DeleteServiceCheck(ctx context.Context, id string) (name, typ string, err error) {
	err = s.pool.QueryRow(ctx, `DELETE FROM service_check_defs WHERE id = $1 RETURNING name, type`, id).Scan(&name, &typ)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("borrando chequeo: %w", err)
	}
	return name, typ, nil
}

// HostChecks devuelve los chequeos habilitados que alcanzan al host, por id, como maximo MaxChecksPerHost+1
// (el +1 permite a quien llama detectar el truncado). Es la lista que se entrega al agente.
func (s *Store) HostChecks(ctx context.Context, hostID string) ([]ServiceCheckDef, error) {
	rows, err := s.pool.Query(ctx,
		checkDefSelect+` JOIN hosts h ON h.id = $1 WHERE d.enabled AND `+hostInScopeSQL+` ORDER BY d.id LIMIT $2`,
		hostID, MaxChecksPerHost+1)
	if err != nil {
		return nil, fmt.Errorf("resolviendo chequeos del host: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ServiceCheckDef, error) { return scanCheckDef(row) })
	if err != nil {
		return nil, fmt.Errorf("leyendo chequeos del host: %w", err)
	}
	return out, nil
}

// CheckState es el ultimo estado de un chequeo en un host.
type CheckState struct {
	Status          string
	StatusChangedAt time.Time
	TS              time.Time
	ResponseMs      *int
	Value           *float64
	Detail          *string
}

// HostCheck es un chequeo que alcanza a un host con su estado actual (nil si aun no reporto).
type HostCheck struct {
	Def   ServiceCheckDef
	State *CheckState
}

// HostServiceChecks devuelve los chequeos habilitados que alcanzan al host con su estado, por nombre.
// ErrNotFound si el host no existe o esta fuera del ambito.
func (s *Store) HostServiceChecks(ctx context.Context, scope Scope, hostID string) ([]HostCheck, error) {
	if err := s.requireHost(ctx, scope, hostID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT d.id, d.name, d.type, d.interval_seconds, d.target_host, d.target_port, d.url, d.service_name,
		        st.status, st.status_changed_at, st.ts, st.response_ms, st.value, st.detail
		   FROM service_check_defs d JOIN hosts h ON h.id = $1
		   LEFT JOIN service_check_state st ON st.host_id = h.id AND st.check_id = d.id
		  WHERE d.enabled AND `+hostInScopeSQL+` ORDER BY lower(d.name), d.id LIMIT $2`, hostID, MaxChecksPerHost)
	if err != nil {
		return nil, fmt.Errorf("listando chequeos del host: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (HostCheck, error) {
		var c HostCheck
		var status *string
		var changed, ts *time.Time
		var st CheckState
		err := row.Scan(&c.Def.ID, &c.Def.Name, &c.Def.Type, &c.Def.IntervalSeconds, &c.Def.TargetHost, &c.Def.TargetPort,
			&c.Def.URL, &c.Def.ServiceName, &status, &changed, &ts, &st.ResponseMs, &st.Value, &st.Detail)
		if err == nil && status != nil && changed != nil && ts != nil {
			st.Status, st.StatusChangedAt, st.TS = *status, *changed, *ts
			c.State = &st
		}
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("leyendo chequeos del host: %w", err)
	}
	return out, nil
}

// requireHost devuelve ErrNotFound si el host no existe o esta fuera del ambito (indistinguibles).
func (s *Store) requireHost(ctx context.Context, scope Scope, hostID string) error {
	var exists bool
	args := argList{hostID}
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM hosts h WHERE h.id = $1 AND `+scope.groupCond("h.group_id", &args)+`)`,
		[]any(args)...).Scan(&exists); err != nil {
		return fmt.Errorf("buscando host: %w", err)
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

// CheckEvent es una transicion de estado de un chequeo en un host.
type CheckEvent struct {
	ID         int64
	FromStatus *string
	ToStatus   string
	Detail     *string
	At         time.Time
}

// CheckEventPage es una pagina de transiciones, las mas recientes primero.
type CheckEventPage struct {
	Items      []CheckEvent
	NextCursor *EventCursor
}

// ListCheckEvents devuelve las transiciones del chequeo en el host. ErrNotFound si el host no existe o esta fuera
// del ambito. limit debe estar entre 1 y MaxAlertListLimit.
func (s *Store) ListCheckEvents(ctx context.Context, scope Scope, hostID, checkID string, after *EventCursor, limit int) (CheckEventPage, error) {
	if err := s.requireHost(ctx, scope, hostID); err != nil {
		return CheckEventPage{}, err
	}
	sql := `SELECT id, from_status, to_status, detail, at FROM service_check_events WHERE host_id = $1 AND check_id = $2`
	args := []any{hostID, checkID}
	if after != nil {
		args = append(args, after.At, after.ID)
		sql += ` AND (at, id) < ($3, $4)`
	}
	args = append(args, limit+1)
	sql += fmt.Sprintf(` ORDER BY at DESC, id DESC LIMIT $%d`, len(args))

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return CheckEventPage{}, fmt.Errorf("listando eventos del chequeo: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CheckEvent, error) {
		var e CheckEvent
		err := row.Scan(&e.ID, &e.FromStatus, &e.ToStatus, &e.Detail, &e.At)
		return e, err
	})
	if err != nil {
		return CheckEventPage{}, fmt.Errorf("leyendo eventos del chequeo: %w", err)
	}
	var page CheckEventPage
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		page.NextCursor = &EventCursor{At: last.At, ID: last.ID}
	}
	page.Items = items
	return page, nil
}

// --- ingest de resultados ---

// CheckResult es el resultado de un chequeo reportado por el agente (ya validado por la API).
type CheckResult struct {
	CheckID    string
	Status     string
	ResponseMs int
	Value      *float64
	Detail     string
	TS         time.Time
}

// checkPlan es lo que un reporte cambia en el estado de los chequeos: un upsert por chequeo, eventos y muestras.
type checkPlan struct {
	state   []plannedState
	events  []plannedEvent
	samples []CheckResult
}

type plannedState struct {
	r             CheckResult
	statusChanged time.Time
	lastSample    time.Time
}

type plannedEvent struct {
	checkID, from, to, detail string
	at                        time.Time
}

type liveCheckState struct {
	status     string
	changedAt  time.Time
	ts         time.Time
	lastSample time.Time
	known      bool
}

// planCheckResults decide, sin escribir, que hacer con los resultados de un reporte: descarta los chequeos que no
// estan asignados al host (o estan deshabilitados) y los resultados que no son mas nuevos que el estado guardado
// (idempotencia ante el reenvio del buffer), y registra transiciones y muestras. Los resultados de un mismo
// chequeo se procesan en orden de ts.
func planCheckResults(ctx context.Context, tx pgx.Tx, hostID string, results []CheckResult) (checkPlan, error) {
	var plan checkPlan
	if len(results) == 0 {
		return plan, nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('argos.check_state:' || $1::text, 0))`, hostID); err != nil {
		return plan, fmt.Errorf("lock del estado de chequeos: %w", err)
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(results))
	for _, r := range results {
		if !seen[r.CheckID] {
			seen[r.CheckID] = true
			ids = append(ids, r.CheckID)
		}
	}
	rows, err := tx.Query(ctx,
		`SELECT d.id::text, st.status, st.status_changed_at, st.ts, st.last_sample_at
		   FROM service_check_defs d JOIN hosts h ON h.id = $1
		   LEFT JOIN service_check_state st ON st.host_id = h.id AND st.check_id = d.id
		  WHERE d.id = ANY($2::uuid[]) AND d.enabled AND `+hostInScopeSQL, hostID, ids)
	if err != nil {
		return plan, fmt.Errorf("leyendo estado de chequeos: %w", err)
	}
	live := map[string]*liveCheckState{}
	for rows.Next() {
		var id string
		var status *string
		var changed, ts, sample *time.Time
		if err := rows.Scan(&id, &status, &changed, &ts, &sample); err != nil {
			rows.Close()
			return plan, fmt.Errorf("leyendo estado de un chequeo: %w", err)
		}
		st := &liveCheckState{}
		if status != nil && changed != nil && ts != nil && sample != nil {
			*st = liveCheckState{status: *status, changedAt: *changed, ts: *ts, lastSample: *sample, known: true}
		}
		live[id] = st
	}
	if err := rows.Err(); err != nil {
		return plan, fmt.Errorf("iterando estado de chequeos: %w", err)
	}

	sorted := append([]CheckResult(nil), results...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TS.Before(sorted[j].TS) })
	final := map[string]*plannedState{}
	for _, r := range sorted {
		st, ok := live[r.CheckID]
		if !ok || (st.known && !r.TS.After(st.ts)) {
			continue
		}
		transition := !st.known || st.status != r.Status
		if transition {
			from := ""
			if st.known {
				from = st.status
			}
			plan.events = append(plan.events, plannedEvent{checkID: r.CheckID, from: from, to: r.Status, detail: r.Detail, at: r.TS})
			st.changedAt = r.TS
		}
		if transition || r.TS.Sub(st.lastSample) >= CheckSampleEvery {
			plan.samples = append(plan.samples, r)
			st.lastSample = r.TS
		}
		st.status, st.ts, st.known = r.Status, r.TS, true
		final[r.CheckID] = &plannedState{r: r, statusChanged: st.changedAt, lastSample: st.lastSample}
	}
	for _, id := range ids {
		if p, ok := final[id]; ok {
			plan.state = append(plan.state, *p)
		}
	}
	return plan, nil
}

const (
	upsertCheckStateSQL = `
INSERT INTO service_check_state AS s (host_id, check_id, status, status_changed_at, ts, response_ms, value, detail, last_sample_at)
SELECT $1::uuid, u.check_id, u.status, u.changed_at, u.ts, u.response_ms, u.value, NULLIF(u.detail, ''), u.sample_at
  FROM unnest($2::uuid[], $3::text[], $4::timestamptz[], $5::timestamptz[], $6::int[], $7::float8[], $8::text[], $9::timestamptz[])
       AS u(check_id, status, changed_at, ts, response_ms, value, detail, sample_at)
ON CONFLICT (host_id, check_id) DO UPDATE SET
  status = EXCLUDED.status, status_changed_at = EXCLUDED.status_changed_at, ts = EXCLUDED.ts,
  response_ms = EXCLUDED.response_ms, value = EXCLUDED.value, detail = EXCLUDED.detail,
  last_sample_at = EXCLUDED.last_sample_at
WHERE s.ts < EXCLUDED.ts`

	insertCheckEventsSQL = `
INSERT INTO service_check_events (host_id, check_id, from_status, to_status, detail, at)
SELECT $1::uuid, u.check_id, NULLIF(u.from_status, ''), u.to_status, NULLIF(u.detail, ''), u.at
  FROM unnest($2::uuid[], $3::text[], $4::text[], $5::text[], $6::timestamptz[]) AS u(check_id, from_status, to_status, detail, at)`

	insertCheckSamplesSQL = `
INSERT INTO service_checks (host_id, check_id, status, response_ms, value, ts)
SELECT $1::uuid, u.check_id, u.status, u.response_ms, u.value, u.ts
  FROM unnest($2::uuid[], $3::text[], $4::int[], $5::float8[], $6::timestamptz[]) AS u(check_id, status, response_ms, value, ts)`
)

// queue agrega al batch las sentencias del plan (nada si el plan esta vacio).
func (p checkPlan) queue(b *pgx.Batch, hostID string) {
	if n := len(p.state); n > 0 {
		ids, st, changed, ts := make([]string, n), make([]string, n), make([]time.Time, n), make([]time.Time, n)
		ms, vals, det, sample := make([]int32, n), make([]*float64, n), make([]string, n), make([]time.Time, n)
		for i, s := range p.state {
			ids[i], st[i], changed[i], ts[i] = s.r.CheckID, s.r.Status, s.statusChanged, s.r.TS
			ms[i], vals[i], det[i], sample[i] = int32(s.r.ResponseMs), s.r.Value, s.r.Detail, s.lastSample //nolint:gosec // response_ms validado 0..600000
		}
		b.Queue(upsertCheckStateSQL, hostID, ids, st, changed, ts, ms, vals, det, sample)
	}
	if n := len(p.events); n > 0 {
		ids, from, to, det, at := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]time.Time, n)
		for i, e := range p.events {
			ids[i], from[i], to[i], det[i], at[i] = e.checkID, e.from, e.to, e.detail, e.at
		}
		b.Queue(insertCheckEventsSQL, hostID, ids, from, to, det, at)
	}
	if n := len(p.samples); n > 0 {
		ids, st, ms, vals, ts := make([]string, n), make([]string, n), make([]int32, n), make([]*float64, n), make([]time.Time, n)
		for i, r := range p.samples {
			ids[i], st[i], ms[i], vals[i], ts[i] = r.CheckID, r.Status, int32(r.ResponseMs), r.Value, r.TS //nolint:gosec // response_ms validado 0..600000
		}
		b.Queue(insertCheckSamplesSQL, hostID, ids, st, ms, vals, ts)
	}
}

// --- retencion ---

// PurgeCheckEvents borra transiciones de chequeos anteriores a before.
func (s *Store) PurgeCheckEvents(ctx context.Context, before time.Time, batch int) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM service_check_events WHERE id IN (SELECT id FROM service_check_events WHERE at < $1 ORDER BY at LIMIT $2)`,
		before, batch)
	if err != nil {
		return 0, fmt.Errorf("purgando eventos de chequeos: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// PurgeStaleCheckState borra los estados sin actualizar desde antes de now-3 dias (chequeo que ya no llega al host,
// host que dejo de reportar). No borra el estado de una alerta todavia activa: la alerta necesita su estado para
// resolverse cuando el chequeo vuelva.
func (s *Store) PurgeStaleCheckState(ctx context.Context, now time.Time, batch int) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM service_check_state st
		  WHERE (st.host_id, st.check_id) IN (
		    SELECT x.host_id, x.check_id FROM service_check_state x
		     WHERE x.ts < $1
		       AND NOT EXISTS (SELECT 1 FROM alerts a JOIN alert_rules r ON r.id = a.rule_id
		                        WHERE r.check_id = x.check_id AND a.host_id = x.host_id AND a.state <> 'resolved')
		     LIMIT $2)`, now.Add(-checkStateMaxAge), batch)
	if err != nil {
		return 0, fmt.Errorf("purgando estados de chequeos: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
