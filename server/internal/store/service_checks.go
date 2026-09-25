package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
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

	checkNameKey       = "scd_name_lower_key"
	checkGroupFKey     = "service_check_defs_scope_group_id_fkey"
	checkScopeHostFKey = "service_check_defs_scope_host_id_fkey"
	ruleCheckFKey      = "alert_rules_check_id_fkey"
	checkStateMaxAge   = 3 * 24 * time.Hour
)

// Tipos de chequeo.
const (
	CheckTCP     = "tcp"
	CheckHTTP    = "http"
	CheckTLSCert = "tls_cert"
	CheckService = "service"
	CheckPing    = "ping"
	CheckSNMP    = "snmp"
)

// Metricas de un chequeo snmp.
const (
	SNMPMetricReachable = "reachable"
	SNMPMetricIfOper    = "if_oper"
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
	// Metric e IfIndex solo existen en los chequeos snmp (reachable | if_oper; if_index solo con if_oper).
	Metric  *string
	IfIndex *int
	Scope   AlertScope
	// GroupName es el nombre del grupo del alcance (solo lectura).
	GroupName *string
	// DeviceName es el nombre del host o dispositivo del alcance (solo lectura).
	DeviceName *string
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const (
	checkDefCols = `d.id, d.name, d.type, d.interval_seconds, d.timeout_ms, d.retries, d.target_host, d.target_port, d.url,
       d.expect_status, d.expect_text, d.service_name, d.warn_ms, d.crit_ms, d.warn_days, d.crit_days,
       d.header_name, d.secret_enc, d.secret_key_id, d.scope_type, d.scope_group_id, g.name, d.scope_host_id, dh.hostname,
       d.enabled, d.created_at, d.updated_at, d.snmp_metric, d.snmp_if_index`
	checkDefFrom = `FROM service_check_defs d LEFT JOIN host_groups g ON g.id = d.scope_group_id
  LEFT JOIN hosts dh ON dh.id = d.scope_host_id`
	checkDefSelect = `SELECT ` + checkDefCols + ` ` + checkDefFrom
)

// scanCheckDef lee las columnas de checkDefCols; extra son columnas adicionales que el SELECT agrega despues.
func scanCheckDef(row pgx.Row, extra ...any) (ServiceCheckDef, error) {
	var d ServiceCheckDef
	dest := []any{&d.ID, &d.Name, &d.Type, &d.IntervalSeconds, &d.TimeoutMs, &d.Retries, &d.TargetHost, &d.TargetPort,
		&d.URL, &d.ExpectStatus, &d.ExpectText, &d.ServiceName, &d.WarnMs, &d.CritMs, &d.WarnDays, &d.CritDays,
		&d.HeaderName, &d.SecretEnc, &d.SecretKeyID, &d.Scope.Type, &d.Scope.GroupID, &d.GroupName, &d.Scope.HostID, &d.DeviceName,
		&d.Enabled, &d.CreatedAt, &d.UpdatedAt, &d.Metric, &d.IfIndex}
	return d, row.Scan(append(dest, extra...)...)
}

// hostInScopeSQL es verdadero si el host h (alias) entra en el alcance del chequeo d (alias). Los chequeos ping y
// snmp solo aplican a dispositivos y el resto solo a agentes.
const hostInScopeSQL = `((d.type IN ('ping', 'snmp')) = (h.kind = 'device') AND (d.scope_type = 'all'
  OR (d.scope_type = 'group' AND d.scope_group_id = h.group_id)
  OR (d.scope_type IN ('device', 'host') AND d.scope_host_id = h.id)))`

// ListServiceChecks devuelve las definiciones por nombre (tope MaxServiceChecks). Un ambito restringido solo ve
// las de alcance `all` y las de sus grupos.
func (s *Store) ListServiceChecks(ctx context.Context, scope Scope) ([]ServiceCheckDef, error) {
	args := argList{MaxServiceChecks}
	cond := scope.configCond("d.scope_type", "d.scope_group_id", &args)
	if !scope.All() {
		// Un chequeo de un host o dispositivo concreto se ve si ese host esta en el ambito.
		cond = "(" + cond + " OR (d.scope_type IN ('device', 'host') AND " + scope.hostCond("d.scope_host_id", &args) + "))"
	}
	rows, err := s.pool.Query(ctx,
		checkDefSelect+` WHERE `+cond+` ORDER BY lower(d.name), d.id LIMIT $1`,
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
		if (d.Scope.Type != ScopeDevice && d.Scope.Type != ScopeHost) || d.Scope.HostID == nil {
			return ServiceCheckDef{}, ErrNotFound
		}
		if err := s.requireHost(ctx, scope, *d.Scope.HostID); err != nil {
			return ServiceCheckDef{}, err
		}
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
	case isFKViolation(err, checkScopeHostFKey):
		// Solo se llega aqui si el host se borro entre la comprobacion y el insert.
		return ErrScopeHostNotFound
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
		if err := requireScopeHost(ctx, tx, d.Scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO service_check_defs (id, name, type, interval_seconds, timeout_ms, retries, target_host, target_port,
			        url, expect_status, expect_text, service_name, warn_ms, crit_ms, warn_days, crit_days, header_name,
			        secret_enc, secret_key_id, scope_type, scope_group_id, scope_host_id, enabled, created_by,
			        snmp_metric, snmp_if_index)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21,
			         $22, $23, NULLIF($24, '')::uuid, $25, $26)`,
			d.ID, d.Name, d.Type, d.IntervalSeconds, d.TimeoutMs, d.Retries, d.TargetHost, d.TargetPort,
			d.URL, d.ExpectStatus, d.ExpectText, d.ServiceName, d.WarnMs, d.CritMs, d.WarnDays, d.CritDays, d.HeaderName,
			d.SecretEnc, d.SecretKeyID, d.Scope.Type, d.Scope.GroupID, d.Scope.HostID, d.Enabled, createdBy, d.Metric, d.IfIndex); err != nil {
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
		if err := requireScopeHost(ctx, tx, d.Scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE service_check_defs SET name = $2, interval_seconds = $3, timeout_ms = $4, retries = $5,
			        target_host = $6, target_port = $7, url = $8, expect_status = $9, expect_text = $10,
			        service_name = $11, warn_ms = $12, crit_ms = $13, warn_days = $14, crit_days = $15,
			        header_name = $16, secret_enc = $17, secret_key_id = $18, scope_type = $19, scope_group_id = $20,
			        scope_host_id = $22, enabled = $21, snmp_metric = $23, snmp_if_index = $24, updated_at = now()
			  WHERE id = $1`,
			id, d.Name, d.IntervalSeconds, d.TimeoutMs, d.Retries, d.TargetHost, d.TargetPort, d.URL, d.ExpectStatus,
			d.ExpectText, d.ServiceName, d.WarnMs, d.CritMs, d.WarnDays, d.CritDays, d.HeaderName, d.SecretEnc,
			d.SecretKeyID, d.Scope.Type, d.Scope.GroupID, d.Enabled, d.Scope.HostID, d.Metric, d.IfIndex); err != nil {
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
		        d.snmp_metric, d.snmp_if_index, st.status, st.status_changed_at, st.ts, st.response_ms, st.value, st.detail
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
			&c.Def.URL, &c.Def.ServiceName, &c.Def.Metric, &c.Def.IfIndex, &status, &changed, &ts, &st.ResponseMs, &st.Value, &st.Detail)
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

// --- ping: chequeos de dispositivos que entrega el colector ---

// PingAssignment es un chequeo ping asignado a un dispositivo cuyo colector es un host concreto. ItemID es el id con
// que el chequeo viaja al colector (uno por par chequeo-dispositivo, ver PingItemID).
type PingAssignment struct {
	Def      ServiceCheckDef
	DeviceID string
	Address  netip.Addr
	ItemID   string
}

// PingItemID devuelve el id con que un chequeo ping viaja al colector para un dispositivo: un chequeo con alcance
// de grupo produce un item por dispositivo y el colector reporta por id de item, asi que cada par necesita el suyo.
// Es un UUID determinista (SHA-256 del par, con bits de version 5 y variante RFC 4122): el servidor no guarda nada
// mas para resolver un resultado.
func PingItemID(checkID, deviceID string) string {
	return deviceItemID("argos.ping:", checkID, deviceID)
}

// SNMPItemID es PingItemID para los chequeos snmp (otro prefijo: un mismo par nunca comparte id con un ping).
func SNMPItemID(checkID, deviceID string) string {
	return deviceItemID("argos.snmp:", checkID, deviceID)
}

func deviceItemID(prefix, checkID, deviceID string) string {
	sum := sha256.Sum256([]byte(prefix + checkID + ":" + deviceID))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// CollectorPings devuelve los chequeos ping habilitados de los dispositivos activos del colector, por (chequeo,
// dispositivo), como maximo MaxChecksPerHost. Es lo que se entrega al colector y lo que valida sus resultados.
func (s *Store) CollectorPings(ctx context.Context, collectorID string) ([]PingAssignment, error) {
	return collectorPings(ctx, s.pool, collectorID)
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func collectorPings(ctx context.Context, q queryer, collectorID string) ([]PingAssignment, error) {
	rows, err := q.Query(ctx,
		`SELECT `+checkDefCols+`, h.id::text, h.address `+checkDefFrom+`
		   JOIN hosts h ON h.kind = 'device' AND h.collector_host_id = $1 AND h.status <> 'disabled'
		  WHERE d.enabled AND d.type = 'ping' AND `+hostInScopeSQL+` ORDER BY d.id, h.id LIMIT $2`,
		collectorID, MaxChecksPerHost)
	if err != nil {
		return nil, fmt.Errorf("resolviendo pings del colector: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PingAssignment, error) {
		var p PingAssignment
		d, err := scanCheckDef(row, &p.DeviceID, &p.Address)
		p.Def = d
		p.ItemID = PingItemID(d.ID, p.DeviceID)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("leyendo pings del colector: %w", err)
	}
	return out, nil
}

// --- ingest de resultados ---

// CheckResult es el resultado de un chequeo reportado por el agente (ya validado por la API). CheckID es el id del
// item entregado: el del chequeo, o PingItemID si es un ping de un dispositivo.
type CheckResult struct {
	CheckID    string
	Status     string
	ResponseMs int
	Value      *float64
	Detail     string
	TS         time.Time
}

// checkTarget es a quien pertenece un resultado: el estado se guarda sobre hostID (el colector para sus propios
// chequeos, el dispositivo para un ping) y el chequeo checkID.
type checkTarget struct {
	hostID, checkID string
}

// checkPlan es lo que un reporte cambia en el estado de los chequeos: un upsert por (host, chequeo), eventos,
// muestras y el resultado de alcanzabilidad de los dispositivos.
type checkPlan struct {
	state   []plannedState
	events  []plannedEvent
	samples []plannedSample
	devices []deviceProbe
}

type plannedState struct {
	t             checkTarget
	r             CheckResult
	statusChanged time.Time
	lastSample    time.Time
}

type plannedEvent struct {
	t        checkTarget
	from, to string
	detail   string
	at       time.Time
}

type plannedSample struct {
	t checkTarget
	r CheckResult
}

// deviceProbe es un resultado aplicado de un ping: alimenta el estado del dispositivo.
type deviceProbe struct {
	deviceID string
	status   string
	ts       time.Time
}

type liveCheckState struct {
	status     string
	changedAt  time.Time
	ts         time.Time
	lastSample time.Time
	known      bool
}

// resolveTargets resuelve a quien pertenece cada id de item del reporte del colector: sus propios chequeos
// habilitados y en alcance, y los pings y chequeos snmp de sus dispositivos. Lo que no esta asignado a este colector
// no aparece. silent marca los items que no dicen nada de la alcanzabilidad del dispositivo (if_oper: una interfaz
// caida no es un equipo caido), asi que no alimentan su estado.
func resolveTargets(ctx context.Context, tx pgx.Tx, collectorID string, ids []string) (map[string]checkTarget, map[string]bool, error) {
	targets, silent := map[string]checkTarget{}, map[string]bool{}
	rows, err := tx.Query(ctx,
		`SELECT d.id::text
		   FROM service_check_defs d JOIN hosts h ON h.id = $1
		  WHERE d.id = ANY($2::uuid[]) AND d.enabled AND `+hostInScopeSQL, collectorID, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("leyendo chequeos del colector: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("leyendo un chequeo del colector: %w", err)
		}
		targets[id] = checkTarget{hostID: collectorID, checkID: id}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterando chequeos del colector: %w", err)
	}
	if len(targets) == len(ids) {
		return targets, silent, nil
	}
	pings, err := collectorPings(ctx, tx, collectorID)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range pings {
		if _, dup := targets[p.ItemID]; !dup {
			targets[p.ItemID] = checkTarget{hostID: p.DeviceID, checkID: p.Def.ID}
		}
	}
	snmps, err := collectorSNMP(ctx, tx, collectorID)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range snmps {
		if _, dup := targets[p.ItemID]; !dup {
			targets[p.ItemID] = checkTarget{hostID: p.DeviceID, checkID: p.Def.ID}
			silent[p.ItemID] = p.Def.Metric != nil && *p.Def.Metric == SNMPMetricIfOper
		}
	}
	return targets, silent, nil
}

// planCheckResults decide, sin escribir, que hacer con los resultados de un reporte: descarta los que no estan
// asignados al host que reporta (o estan deshabilitados) y los que no son mas nuevos que el estado guardado
// (idempotencia ante el reenvio del buffer), y registra transiciones y muestras. Los resultados de un mismo
// chequeo se procesan en orden de ts. Los pings se guardan sobre el dispositivo, no sobre el colector.
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
	targets, silent, err := resolveTargets(ctx, tx, hostID, ids)
	if err != nil {
		return plan, err
	}
	live, err := liveStates(ctx, tx, targets)
	if err != nil {
		return plan, err
	}

	sorted := append([]CheckResult(nil), results...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TS.Before(sorted[j].TS) })
	final := map[checkTarget]*plannedState{}
	var order []checkTarget
	for _, r := range sorted {
		t, ok := targets[r.CheckID]
		if !ok {
			continue
		}
		st := live[t]
		if st.known && !r.TS.After(st.ts) {
			continue
		}
		transition := !st.known || st.status != r.Status
		if transition {
			from := ""
			if st.known {
				from = st.status
			}
			plan.events = append(plan.events, plannedEvent{t: t, from: from, to: r.Status, detail: r.Detail, at: r.TS})
			st.changedAt = r.TS
		}
		if transition || r.TS.Sub(st.lastSample) >= CheckSampleEvery {
			plan.samples = append(plan.samples, plannedSample{t: t, r: r})
			st.lastSample = r.TS
		}
		st.status, st.ts, st.known = r.Status, r.TS, true
		if t.hostID != hostID && !silent[r.CheckID] {
			plan.devices = append(plan.devices, deviceProbe{deviceID: t.hostID, status: r.Status, ts: r.TS})
		}
		if _, dup := final[t]; !dup {
			order = append(order, t)
		}
		final[t] = &plannedState{t: t, r: r, statusChanged: st.changedAt, lastSample: st.lastSample}
	}
	for _, t := range order {
		plan.state = append(plan.state, *final[t])
	}
	return plan, nil
}

// liveStates lee el estado guardado de cada destino (uno sin fila queda con known = false).
func liveStates(ctx context.Context, tx pgx.Tx, targets map[string]checkTarget) (map[checkTarget]*liveCheckState, error) {
	live := map[checkTarget]*liveCheckState{}
	hosts, checks := make([]string, 0, len(targets)), make([]string, 0, len(targets))
	for _, t := range targets {
		if _, ok := live[t]; ok {
			continue
		}
		live[t] = &liveCheckState{}
		hosts, checks = append(hosts, t.hostID), append(checks, t.checkID)
	}
	if len(hosts) == 0 {
		return live, nil
	}
	rows, err := tx.Query(ctx,
		`SELECT st.host_id::text, st.check_id::text, st.status, st.status_changed_at, st.ts, st.last_sample_at
		   FROM service_check_state st
		   JOIN unnest($1::uuid[], $2::uuid[]) AS u(host_id, check_id) USING (host_id, check_id)`, hosts, checks)
	if err != nil {
		return nil, fmt.Errorf("leyendo estado de chequeos: %w", err)
	}
	for rows.Next() {
		var t checkTarget
		var st liveCheckState
		if err := rows.Scan(&t.hostID, &t.checkID, &st.status, &st.changedAt, &st.ts, &st.lastSample); err != nil {
			rows.Close()
			return nil, fmt.Errorf("leyendo estado de un chequeo: %w", err)
		}
		st.known = true
		live[t] = &st
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando estado de chequeos: %w", err)
	}
	return live, nil
}

const (
	upsertCheckStateSQL = `
INSERT INTO service_check_state AS s (host_id, check_id, status, status_changed_at, ts, response_ms, value, detail, last_sample_at)
SELECT u.host_id, u.check_id, u.status, u.changed_at, u.ts, u.response_ms, u.value, NULLIF(u.detail, ''), u.sample_at
  FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::timestamptz[], $5::timestamptz[], $6::int[], $7::float8[], $8::text[], $9::timestamptz[])
       AS u(host_id, check_id, status, changed_at, ts, response_ms, value, detail, sample_at)
ON CONFLICT (host_id, check_id) DO UPDATE SET
  status = EXCLUDED.status, status_changed_at = EXCLUDED.status_changed_at, ts = EXCLUDED.ts,
  response_ms = EXCLUDED.response_ms, value = EXCLUDED.value, detail = EXCLUDED.detail,
  last_sample_at = EXCLUDED.last_sample_at
WHERE s.ts < EXCLUDED.ts`

	insertCheckEventsSQL = `
INSERT INTO service_check_events (host_id, check_id, from_status, to_status, detail, at)
SELECT u.host_id, u.check_id, NULLIF(u.from_status, ''), u.to_status, NULLIF(u.detail, ''), u.at
  FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::text[], $5::text[], $6::timestamptz[]) AS u(host_id, check_id, from_status, to_status, detail, at)`

	insertCheckSamplesSQL = `
INSERT INTO service_checks (host_id, check_id, status, response_ms, value, ts)
SELECT u.host_id, u.check_id, u.status, u.response_ms, u.value, u.ts
  FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::int[], $5::float8[], $6::timestamptz[]) AS u(host_id, check_id, status, response_ms, value, ts)`

	// deviceReachSQL aplica el resultado de los pings al estado del dispositivo (solo resultados de los ultimos 5
	// minutos: un resultado viejo del buffer offline no revive ni tumba un equipo). Responde algun ping: up y
	// last_seen_at = now(). Solo down: un dispositivo pending u offline pasa a down u offline segun always_on de su
	// grupo (igual que el detector); uno up lo deja al detector, que espera el umbral del grupo. Los resultados
	// unknown (bloqueado, no soportado) no cuentan: no hay evaluacion. La transicion va a host_events en la misma
	// sentencia. $4 es el colector: un dispositivo de otro colector no se toca.
	deviceReachSQL = `
WITH agg AS (
  SELECT u.device_id AS id,
         CASE WHEN bool_or(u.status IN ('up', 'warn')) THEN 'up' ELSE 'down' END AS outcome
    FROM unnest($1::uuid[], $2::text[], $3::timestamptz[]) AS u(device_id, status, ts)
   WHERE u.status <> 'unknown' AND u.ts > now() - interval '5 minutes'
   GROUP BY u.device_id
), cand AS (
  SELECT h.id, h.status AS prev, a.outcome,
         CASE WHEN a.outcome = 'up' THEN 'up'
              WHEN h.status IN ('pending', 'offline')
                THEN CASE WHEN COALESCE((SELECT g.always_on FROM host_groups g WHERE g.id = h.group_id), false)
                          THEN 'down' ELSE 'offline' END
              ELSE h.status END AS next
    FROM hosts h JOIN agg a ON a.id = h.id
   WHERE h.kind = 'device' AND h.collector_host_id = $4 AND h.status <> 'disabled'
   ORDER BY h.id FOR UPDATE OF h
), upd AS (
  UPDATE hosts h
     SET status = c.next,
         last_seen_at = CASE WHEN c.outcome = 'up' THEN now() ELSE h.last_seen_at END,
         status_changed_at = CASE WHEN c.next <> c.prev THEN now() ELSE h.status_changed_at END
    FROM cand c WHERE h.id = c.id
  RETURNING h.id, c.prev, c.next
), ev AS (
  INSERT INTO host_events (host_id, from_status, to_status, source)
  SELECT id, prev, next, 'ingest' FROM upd WHERE prev <> next
)
SELECT count(*) FROM upd`
)

// queue agrega al batch las sentencias del plan (nada si el plan esta vacio). collectorID es quien reporta.
func (p checkPlan) queue(b *pgx.Batch, collectorID string) {
	if n := len(p.state); n > 0 {
		hosts, ids, st, changed, ts := make([]string, n), make([]string, n), make([]string, n), make([]time.Time, n), make([]time.Time, n)
		ms, vals, det, sample := make([]int32, n), make([]*float64, n), make([]string, n), make([]time.Time, n)
		for i, s := range p.state {
			hosts[i], ids[i], st[i], changed[i], ts[i] = s.t.hostID, s.t.checkID, s.r.Status, s.statusChanged, s.r.TS
			ms[i], vals[i], det[i], sample[i] = int32(s.r.ResponseMs), s.r.Value, s.r.Detail, s.lastSample //nolint:gosec // response_ms validado 0..600000
		}
		b.Queue(upsertCheckStateSQL, hosts, ids, st, changed, ts, ms, vals, det, sample)
	}
	if n := len(p.events); n > 0 {
		hosts, ids, from, to, det, at := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]time.Time, n)
		for i, e := range p.events {
			hosts[i], ids[i], from[i], to[i], det[i], at[i] = e.t.hostID, e.t.checkID, e.from, e.to, e.detail, e.at
		}
		b.Queue(insertCheckEventsSQL, hosts, ids, from, to, det, at)
	}
	if n := len(p.samples); n > 0 {
		hosts, ids, st, ms, vals, ts := make([]string, n), make([]string, n), make([]string, n), make([]int32, n), make([]*float64, n), make([]time.Time, n)
		for i, s := range p.samples {
			hosts[i], ids[i], st[i], ms[i], vals[i], ts[i] = s.t.hostID, s.t.checkID, s.r.Status, int32(s.r.ResponseMs), s.r.Value, s.r.TS //nolint:gosec // response_ms validado 0..600000
		}
		b.Queue(insertCheckSamplesSQL, hosts, ids, st, ms, vals, ts)
	}
	if n := len(p.devices); n > 0 {
		ids, st, ts := make([]string, n), make([]string, n), make([]time.Time, n)
		for i, d := range p.devices {
			ids[i], st[i], ts[i] = d.deviceID, d.status, d.ts
		}
		b.Queue(deviceReachSQL, ids, st, ts, collectorID)
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
