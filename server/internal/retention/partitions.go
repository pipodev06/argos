package retention

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

// Tablas particionadas por dia UTC.
const (
	tableMetrics       = "metrics"
	tableServiceChecks = "service_checks"
	tableDiskSamples   = "disk_samples"
)

// partitionedTables son las tablas que el mantenimiento crea y borra por dia.
var partitionedTables = []string{tableMetrics, tableServiceChecks, tableDiskSamples}

// partition es una particion hija con su rango [lower, upper). nil = sin cota (MINVALUE/MAXVALUE),
// como la particion legacy que adopto la tabla original en la migracion 00006.
type partition struct {
	name          string
	lower, upper  *time.Time
	detachPending bool
}

// boundRe reconoce `FOR VALUES FROM (<cota>) TO (<cota>)` con la salida de pg_get_expr.
var boundRe = regexp.MustCompile(`^FOR VALUES FROM \((MINVALUE|'([^']+)')\) TO \((MAXVALUE|'([^']+)')\)$`)

// boundLayout es el formato de timestamptz de pg_get_expr con timezone UTC (+00).
const boundLayout = "2006-01-02 15:04:05.999999-07"

// parseBound interpreta una cota: MINVALUE/MAXVALUE (literal sin comillas) -> nil.
func parseBound(quoted string) (*time.Time, error) {
	if quoted == "" {
		return nil, nil
	}
	t, err := time.Parse(boundLayout, quoted)
	if err != nil {
		return nil, fmt.Errorf("cota %q: %w", quoted, err)
	}
	t = t.UTC()
	return &t, nil
}

// listPartitions devuelve las particiones de parent con sus cotas en UTC. La transaccion fija
// timezone=UTC solo pa esta lectura: pg_get_expr formatea los timestamptz con la zona de la
// sesion, y el resultado no debe depender de ella. Las particiones cuya cota no se reconoce
// (p. ej. una DEFAULT puesta a mano) se omiten con un aviso: nunca se borran.
func (m *Maintainer) listPartitions(ctx context.Context, conn *pgx.Conn, parent string) ([]partition, error) {
	var parts []partition
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL timezone = 'UTC'`); err != nil {
			return fmt.Errorf("fijando timezone: %w", err)
		}
		rows, err := tx.Query(ctx,
			`SELECT c.relname, i.inhdetachpending, pg_get_expr(c.relpartbound, c.oid)
			   FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
			  WHERE i.inhparent = $1::regclass
			  ORDER BY c.relname`, pgx.Identifier{parent}.Sanitize())
		if err != nil {
			return fmt.Errorf("consultando particiones: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var name, expr string
			var pending bool
			if err := rows.Scan(&name, &pending, &expr); err != nil {
				return fmt.Errorf("leyendo particion: %w", err)
			}
			p, ok := m.parsePartition(parent, name, pending, expr)
			if ok {
				parts = append(parts, p)
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterando particiones: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listando particiones de %s: %w", parent, err)
	}
	return parts, nil
}

func (m *Maintainer) parsePartition(parent, name string, pending bool, expr string) (partition, bool) {
	match := boundRe.FindStringSubmatch(expr)
	if match == nil {
		m.log.Warn("particion con cota no reconocida: se ignora", "table", parent, "partition", name)
		return partition{}, false
	}
	lower, err1 := parseBound(match[2])
	upper, err2 := parseBound(match[4])
	if err := errors.Join(err1, err2); err != nil {
		m.log.Warn("particion con cota ilegible: se ignora", "table", parent, "partition", name, "err", err)
		return partition{}, false
	}
	return partition{name: name, lower: lower, upper: upper, detachPending: pending}, true
}

// covers indica si alguna particion ocupa parte del rango [from, to).
func covers(parts []partition, from, to time.Time) bool {
	for _, p := range parts {
		if (p.lower == nil || p.lower.Before(to)) && (p.upper == nil || p.upper.After(from)) {
			return true
		}
	}
	return false
}

// partitionName es el nombre de la particion diaria de table pa el dia UTC day.
func partitionName(table string, day time.Time) string {
	return table + "_p" + day.UTC().Format("20060102")
}

// createPartitions asegura las particiones de hoy a hoy+createAheadDays (UTC) en cada tabla.
// Es idempotente: los dias ya cubiertos (incluido por la particion legacy) se saltan. Un fallo
// en un dia no impide intentar los demas.
func (m *Maintainer) createPartitions(ctx context.Context, conn *pgx.Conn, now time.Time) error {
	var errs []error
	today := utcDay(now)
	for _, table := range partitionedTables {
		existing, err := m.listPartitions(ctx, conn, table)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for i := 0; i <= createAheadDays; i++ {
			day := today.AddDate(0, 0, i)
			next := day.AddDate(0, 0, 1)
			if covers(existing, day, next) {
				continue
			}
			if err := createPartition(ctx, conn, table, day); err != nil {
				errs = append(errs, err)
				continue
			}
			m.log.Info("particion creada", "table", table, "partition", partitionName(table, day), "day", day.Format(time.DateOnly))
		}
	}
	return errors.Join(errs...)
}

// createPartition crea la particion diaria como tabla suelta (LIKE) y luego la adjunta.
// A diferencia de CREATE TABLE ... PARTITION OF, ATTACH solo toma SHARE UPDATE EXCLUSIVE sobre
// el padre (no bloquea los INSERT del ingest); el ATTACH crea sus indices y FK. Ambas sentencias
// van en una transaccion: si el ATTACH falla (lock_timeout) no queda una tabla huerfana.
func createPartition(ctx context.Context, conn *pgx.Conn, table string, day time.Time) error {
	child := pgx.Identifier{partitionName(table, day)}.Sanitize()
	parent := pgx.Identifier{table}.Sanitize()
	// Las cotas llevan offset explicito: no dependen del timezone de la sesion.
	lo := day.UTC().Format("2006-01-02 15:04:05") + "+00"
	hi := day.UTC().AddDate(0, 0, 1).Format("2006-01-02 15:04:05") + "+00"

	// DDL: no admite parametros. Los identificadores salen de constantes + una fecha, sanitizados;
	// las cotas son fechas formateadas por time.Format.
	create := fmt.Sprintf(`CREATE TABLE %s (LIKE %s INCLUDING DEFAULTS INCLUDING CONSTRAINTS)`, child, parent)
	attach := fmt.Sprintf(`ALTER TABLE %s ATTACH PARTITION %s FOR VALUES FROM ('%s') TO ('%s')`, parent, child, lo, hi)
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, create); err != nil {
			return fmt.Errorf("creando tabla: %w", err)
		}
		if _, err := tx.Exec(ctx, attach); err != nil {
			return fmt.Errorf("adjuntando: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("creando particion %s de %s: %w", partitionName(table, day), table, err)
	}
	return nil
}
