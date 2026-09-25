package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrReportTooLarge: el informe abarca mas hosts (o filas) que MaxReportHosts / MaxReportRows.
	ErrReportTooLarge = errors.New("informe demasiado grande")
	// ErrReportTimeout: el informe supero ReportStatementTimeout.
	ErrReportTimeout = errors.New("el informe tardo demasiado")
	// ErrReportRange: el rango pedido es vacio o supera MaxReportSpan.
	ErrReportRange = errors.New("rango de informe invalido")
)

// Topes de los informes de disponibilidad.
const (
	MaxReportHosts         = 2000
	MaxReportRows          = 5000 // filas (host x chequeo) del informe de chequeos
	MaxReportSpan          = 91 * 24 * time.Hour
	ReportStatementTimeout = 10 * time.Second
)

// HostAvailability son los segundos por estado de un host en [from, to). DownS es todo el tiempo caido y
// DownMaintS la parte que cayo dentro de una ventana de mantenimiento (no penaliza). Los segundos son enteros:
// las fracciones se redondean.
type HostAvailability struct {
	HostID      string
	Hostname    string
	GroupID     *string
	GroupName   *string
	UpS         int64
	DownS       int64
	DownMaintS  int64
	OfflineS    int64
	Incidents   int
	TrackedFrom *time.Time // primer evento conocido del host; nil si no tiene historial
}

// CheckAvailability son los segundos por estado de un chequeo en un host. Solo cuenta el tiempo en que el host
// estaba `up`: UpS, WarnS y DownS ya vienen recortados a ese tiempo. warn cuenta como disponible.
type CheckAvailability struct {
	HostID      string
	Hostname    string
	GroupID     *string
	GroupName   *string
	CheckID     string
	CheckName   string
	UpS         int64
	WarnS       int64
	DownS       int64
	DownMaintS  int64
	Incidents   int
	TrackedFrom *time.Time
}

// AvailabilityTotals es una suma de segundos (grupo o flota). CoveredS es el tiempo con historial conocido.
type AvailabilityTotals struct {
	Hosts      int
	UpS        int64
	DownS      int64
	DownMaintS int64
	OfflineS   int64
	Incidents  int
	CoveredS   int64
}

// availabilityPct = disponible / (disponible + caido fuera de mantenimiento); nil si el denominador es 0.
func availabilityPct(available, down, downMaint int64) *float64 {
	counted := max(down-downMaint, 0)
	if available+counted == 0 {
		return nil
	}
	p := float64(available) / float64(available+counted) * 100
	return &p
}

func coveragePct(coveredS, hosts int64, from, to time.Time) float64 {
	span := int64(to.Sub(from).Seconds())
	if hosts == 0 || span <= 0 {
		return 0
	}
	return math.Min(100, float64(coveredS)/float64(hosts*span)*100)
}

// CoveredSeconds son los segundos del rango con historial (desde tracked, recortado al rango).
func CoveredSeconds(tracked *time.Time, from, to time.Time) int64 {
	if tracked == nil || !tracked.Before(to) {
		return 0
	}
	start := from
	if tracked.After(from) {
		start = *tracked
	}
	return int64(to.Sub(start).Seconds())
}

// Pct es la disponibilidad en % (nil = sin datos, nunca 100 falso).
func (t AvailabilityTotals) Pct() *float64 { return availabilityPct(t.UpS, t.DownS, t.DownMaintS) }

// CoveragePct es el % del rango (promedio por host) que tiene historial.
func (t AvailabilityTotals) CoveragePct(from, to time.Time) float64 {
	return coveragePct(t.CoveredS, int64(t.Hosts), from, to)
}

// Pct de un host.
func (h HostAvailability) Pct() *float64 { return availabilityPct(h.UpS, h.DownS, h.DownMaintS) }

// CoveragePct de un host.
func (h HostAvailability) CoveragePct(from, to time.Time) float64 {
	return coveragePct(CoveredSeconds(h.TrackedFrom, from, to), 1, from, to)
}

// Pct de un chequeo (disponible = up + warn).
func (c CheckAvailability) Pct() *float64 {
	return availabilityPct(c.UpS+c.WarnS, c.DownS, c.DownMaintS)
}

// CoveragePct de un chequeo.
func (c CheckAvailability) CoveragePct(from, to time.Time) float64 {
	return coveragePct(CoveredSeconds(c.TrackedFrom, from, to), 1, from, to)
}

func (t *AvailabilityTotals) add(h HostAvailability, from, to time.Time) {
	t.Hosts++
	t.UpS += h.UpS
	t.DownS += h.DownS
	t.DownMaintS += h.DownMaintS
	t.OfflineS += h.OfflineS
	t.Incidents += h.Incidents
	t.CoveredS += CoveredSeconds(h.TrackedFrom, from, to)
}

// GroupAvailability es el total de un grupo (GroupID nil = hosts sin grupo). Es ponderado por tiempo: suma de
// segundos, no promedio de porcentajes.
type GroupAvailability struct {
	GroupID   *string
	GroupName *string
	AvailabilityTotals
}

// GroupTotals agrupa hosts por su grupo actual. Orden: por nombre de grupo; los hosts sin grupo al final.
func GroupTotals(hosts []HostAvailability, from, to time.Time) []GroupAvailability {
	idx := map[string]int{}
	var out []GroupAvailability
	for _, h := range hosts {
		key := ""
		if h.GroupID != nil {
			key = *h.GroupID
		}
		i, ok := idx[key]
		if !ok {
			i = len(out)
			idx[key] = i
			out = append(out, GroupAvailability{GroupID: h.GroupID, GroupName: h.GroupName})
		}
		out[i].add(h, from, to)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.GroupID == nil) != (b.GroupID == nil) {
			return b.GroupID == nil
		}
		if a.GroupID == nil {
			return false
		}
		return strings.ToLower(*a.GroupName) < strings.ToLower(*b.GroupName)
	})
	return out
}

// Totals suma todos los hosts.
func Totals(hosts []HostAvailability, from, to time.Time) AvailabilityTotals {
	var t AvailabilityTotals
	for _, h := range hosts {
		t.add(h, from, to)
	}
	return t
}

// durSQL suma la duracion en segundos de un multirango de tiempo.
func durSQL(multirange string) string {
	return `(SELECT COALESCE(sum(extract(epoch FROM upper(r) - lower(r))), 0)::float8 FROM unnest(` + multirange + `) r)`
}

const emptyMulti = `'{}'::tstzmultirange`

// reportCTEs devuelve los CTE comunes: hs (hosts del ambito, con tope MaxReportHosts+1 para detectar el
// exceso), ev/seg/seg1 (tramos de estado de cada host en [from, to), recortados), inc (entradas a down) y mw
// (mantenimiento por host, como multirango). El ambito es obligatorio.
func reportCTEs(scope Scope, groupID *string, fromP, toP string, args *argList) string {
	where := scope.groupCond("h.group_id", args)
	if groupID != nil {
		where += " AND h.group_id = " + args.add(*groupID) + "::uuid"
	}
	return `
hs AS (
  SELECT h.id, h.hostname, h.group_id, g.name AS gname
    FROM hosts h LEFT JOIN host_groups g ON g.id = h.group_id
   WHERE ` + where + `
   ORDER BY lower(h.hostname), h.id LIMIT ` + fmt.Sprint(MaxReportHosts+1) + `
), ev AS (
  SELECT hs.id AS host_id, e.to_status, e.at, e.id
    FROM hs CROSS JOIN LATERAL (
      (SELECT p.to_status, p.at, p.id FROM host_events p WHERE p.host_id = hs.id AND p.at < ` + fromP + `
        ORDER BY p.at DESC, p.id DESC LIMIT 1)
      UNION ALL
      (SELECT r.to_status, r.at, r.id FROM host_events r WHERE r.host_id = hs.id AND r.at >= ` + fromP + ` AND r.at < ` + toP + `)
    ) e
), seg AS (
  SELECT host_id, to_status AS st, greatest(at, ` + fromP + `) AS s,
         COALESCE(lead(at) OVER (PARTITION BY host_id ORDER BY at, id), ` + toP + `) AS e
    FROM ev
), seg1 AS (SELECT * FROM seg WHERE e > s
), inc AS (
  SELECT host_id, count(*) AS n FROM ev WHERE to_status = 'down' AND at >= ` + fromP + ` GROUP BY host_id
), mw AS (
  SELECT hs.id AS host_id, range_agg(tstzrange(greatest(m.starts_at, ` + fromP + `), least(m.ends_at, ` + toP + `))) AS mr
    FROM hs JOIN maintenance_windows m ON m.ends_at > ` + fromP + ` AND m.starts_at < ` + toP + `
     AND (m.scope_type = 'all'
          OR (m.scope_type = 'group' AND m.scope_group_id = hs.group_id))
   GROUP BY hs.id
)`
}

func checkReportRange(from, to time.Time) error {
	if !to.After(from) || to.Sub(from) > MaxReportSpan {
		return ErrReportRange
	}
	return nil
}

// reportGroupExists comprueba que el grupo exista y sea del ambito: si no, ErrNotFound (identico en ambos casos).
func reportGroupExists(ctx context.Context, tx pgx.Tx, scope Scope, groupID string) error {
	var args argList
	idP := args.add(groupID)
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM host_groups g WHERE g.id = `+idP+`::uuid AND `+scope.groupCond("g.id", &args),
		[]any(args)...).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("comprobando grupo del informe: %w", err)
	}
	return nil
}

// reportTx abre una transaccion de solo lectura con statement_timeout y ejecuta fn.
func (s *Store) reportTx(ctx context.Context, groupID *string, scope Scope, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("abriendo transaccion de informe: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // tras un error o un Commit el Rollback es inocuo
	if _, err := tx.Exec(ctx, fmt.Sprintf(`SET LOCAL statement_timeout = %d`, ReportStatementTimeout.Milliseconds())); err != nil {
		return fmt.Errorf("fijando statement_timeout: %w", err)
	}
	if groupID != nil {
		if err := reportGroupExists(ctx, tx, scope, *groupID); err != nil {
			return err
		}
	}
	if err := fn(tx); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "57014" && ctx.Err() == nil {
			return ErrReportTimeout
		}
		return err
	}
	return nil
}

func round(f float64) int64 { return int64(math.Round(f)) }

// AvailabilityReport calcula, por host del ambito, los segundos por estado en [from, to). groupID (opcional)
// acota a un grupo: si no existe o no es del ambito, ErrNotFound. Mas de MaxReportHosts hosts: ErrReportTooLarge.
// El grupo y el mantenimiento se toman del estado actual del host.
func (s *Store) AvailabilityReport(ctx context.Context, scope Scope, groupID *string, from, to time.Time) ([]HostAvailability, error) {
	if err := checkReportRange(from, to); err != nil {
		return nil, err
	}
	var args argList
	fromP, toP := args.add(from), args.add(to)
	ctes := reportCTEs(scope, groupID, fromP, toP, &args)
	sql := `WITH ` + ctes + `, seg2 AS (
  SELECT seg1.host_id, seg1.st, extract(epoch FROM seg1.e - seg1.s)::float8 AS dur,
         CASE WHEN seg1.st = 'down' THEN ` +
		durSQL(`tstzmultirange(tstzrange(seg1.s, seg1.e)) * COALESCE(mw.mr, `+emptyMulti+`)`) + ` ELSE 0 END AS mant
    FROM seg1 LEFT JOIN mw ON mw.host_id = seg1.host_id
), agg AS (
  SELECT host_id,
         COALESCE(sum(dur) FILTER (WHERE st = 'up'), 0) AS up_s,
         COALESCE(sum(dur) FILTER (WHERE st = 'down'), 0) AS down_s,
         COALESCE(sum(mant), 0) AS mant_s,
         COALESCE(sum(dur) FILTER (WHERE st = 'offline'), 0) AS off_s
    FROM seg2 GROUP BY host_id
)
SELECT hs.id, hs.hostname, hs.group_id, hs.gname,
       COALESCE(a.up_s, 0), COALESCE(a.down_s, 0), COALESCE(a.mant_s, 0), COALESCE(a.off_s, 0),
       COALESCE(i.n, 0), (SELECT min(t.at) FROM host_events t WHERE t.host_id = hs.id)
  FROM hs LEFT JOIN agg a ON a.host_id = hs.id LEFT JOIN inc i ON i.host_id = hs.id
 ORDER BY lower(hs.hostname), hs.id`

	var out []HostAvailability
	err := s.reportTx(ctx, groupID, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sql, []any(args)...)
		if err != nil {
			return fmt.Errorf("consultando disponibilidad: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (HostAvailability, error) {
			var h HostAvailability
			var up, down, mant, off float64
			var inc int64
			err := row.Scan(&h.HostID, &h.Hostname, &h.GroupID, &h.GroupName, &up, &down, &mant, &off, &inc, &h.TrackedFrom)
			h.UpS, h.DownS, h.DownMaintS, h.OfflineS, h.Incidents = round(up), round(down), round(mant), round(off), int(inc)
			return h, err
		})
		if err != nil {
			return fmt.Errorf("leyendo disponibilidad: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) > MaxReportHosts {
		return nil, ErrReportTooLarge
	}
	return out, nil
}

// ServiceChecksReport calcula, por (host, chequeo) del ambito, los segundos por estado en [from, to), contando
// solo el tiempo en que el host estaba `up`. Solo aparecen los chequeos que existen y son visibles al ambito.
func (s *Store) ServiceChecksReport(ctx context.Context, scope Scope, groupID *string, from, to time.Time) ([]CheckAvailability, error) {
	if err := checkReportRange(from, to); err != nil {
		return nil, err
	}
	var args argList
	fromP, toP := args.add(from), args.add(to)
	ctes := reportCTEs(scope, groupID, fromP, toP, &args)
	cfg := scope.configCond("d.scope_type", "d.scope_group_id", &args)
	sql := `WITH ` + ctes + `, hostup AS (
  SELECT host_id, range_agg(tstzrange(s, e)) AS mr FROM seg1 WHERE st = 'up' GROUP BY host_id
), pairs AS (
  SELECT DISTINCT e.host_id, e.check_id, d.name AS cname
    FROM hs JOIN service_check_events e ON e.host_id = hs.id AND e.at < ` + toP + `
    JOIN service_check_defs d ON d.id = e.check_id
   WHERE ` + cfg + `
), cev AS (
  SELECT p.host_id, p.check_id, x.to_status, x.at, x.id
    FROM pairs p CROSS JOIN LATERAL (
      (SELECT q.to_status, q.at, q.id FROM service_check_events q
        WHERE q.host_id = p.host_id AND q.check_id = p.check_id AND q.at < ` + fromP + ` ORDER BY q.at DESC, q.id DESC LIMIT 1)
      UNION ALL
      (SELECT r.to_status, r.at, r.id FROM service_check_events r
        WHERE r.host_id = p.host_id AND r.check_id = p.check_id AND r.at >= ` + fromP + ` AND r.at < ` + toP + `)
    ) x
), cseg AS (
  SELECT host_id, check_id, to_status AS st, greatest(at, ` + fromP + `) AS s,
         COALESCE(lead(at) OVER (PARTITION BY host_id, check_id ORDER BY at, id), ` + toP + `) AS e
    FROM cev
), cseg1 AS (
  SELECT c.host_id, c.check_id, c.st, tstzmultirange(tstzrange(c.s, c.e)) * COALESCE(u.mr, ` + emptyMulti + `) AS eff
    FROM cseg c LEFT JOIN hostup u ON u.host_id = c.host_id WHERE c.e > c.s
), cseg2 AS (
  SELECT c.host_id, c.check_id, c.st, ` + durSQL(`c.eff`) + ` AS dur,
         CASE WHEN c.st = 'down' THEN ` + durSQL(`c.eff * COALESCE(mw.mr, `+emptyMulti+`)`) + ` ELSE 0 END AS mant
    FROM cseg1 c LEFT JOIN mw ON mw.host_id = c.host_id
), cagg AS (
  SELECT host_id, check_id,
         COALESCE(sum(dur) FILTER (WHERE st = 'up'), 0) AS up_s,
         COALESCE(sum(dur) FILTER (WHERE st = 'warn'), 0) AS warn_s,
         COALESCE(sum(dur) FILTER (WHERE st = 'down'), 0) AS down_s,
         COALESCE(sum(mant), 0) AS mant_s
    FROM cseg2 GROUP BY host_id, check_id
)
SELECT hs.id, hs.hostname, hs.group_id, hs.gname, p.check_id, p.cname,
       COALESCE(a.up_s, 0), COALESCE(a.warn_s, 0), COALESCE(a.down_s, 0), COALESCE(a.mant_s, 0),
       (SELECT count(*) FROM cev v WHERE v.host_id = p.host_id AND v.check_id = p.check_id AND v.to_status = 'down' AND v.at >= ` + fromP + `),
       (SELECT min(t.at) FROM service_check_events t WHERE t.host_id = p.host_id AND t.check_id = p.check_id),
       (SELECT count(*) FROM hs)
  FROM pairs p JOIN hs ON hs.id = p.host_id LEFT JOIN cagg a ON a.host_id = p.host_id AND a.check_id = p.check_id
 ORDER BY lower(hs.hostname), hs.id, lower(p.cname), p.check_id
 LIMIT ` + fmt.Sprint(MaxReportRows+1)

	var out []CheckAvailability
	tooManyHosts := false
	err := s.reportTx(ctx, groupID, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sql, []any(args)...)
		if err != nil {
			return fmt.Errorf("consultando chequeos: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (CheckAvailability, error) {
			var c CheckAvailability
			var up, warn, down, mant float64
			var inc, hosts int64
			err := row.Scan(&c.HostID, &c.Hostname, &c.GroupID, &c.GroupName, &c.CheckID, &c.CheckName,
				&up, &warn, &down, &mant, &inc, &c.TrackedFrom, &hosts)
			c.UpS, c.WarnS, c.DownS, c.DownMaintS, c.Incidents = round(up), round(warn), round(down), round(mant), int(inc)
			tooManyHosts = tooManyHosts || hosts > MaxReportHosts
			return c, err
		})
		if err != nil {
			return fmt.Errorf("leyendo chequeos: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if tooManyHosts || len(out) > MaxReportRows {
		return nil, ErrReportTooLarge
	}
	return out, nil
}
