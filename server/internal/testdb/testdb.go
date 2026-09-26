// Package testdb reune lo que comparten los tests que usan una Postgres real (solo lo importan _test.go).
package testdb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedTables guardan datos de fabrica (migraciones) y no se limpian nunca.
var seedTables = []string{"goose_db_version", "os_eol"}

// truncateAll es la limpieza completa y lenta (~180 ms por los ~180 relaciones y sus archivos). Es el respaldo
// de ResetData y la referencia de lo que debe dejar la limpieza rapida.
const truncateAll = `TRUNCATE service_check_defs, service_check_events, service_checks, metrics, metrics_hourly, metrics_chunks, settings, maintenance_state,
	disk_samples, enroll_tokens, hosts, host_groups, alert_rules, notification_channels, maintenance_windows, sessions,
	audit_log, users CASCADE`

// fastDeletes borra fila a fila (~10 ms con pocos datos, contra ~180 ms del TRUNCATE): el orden respeta las FK que
// no son ON DELETE CASCADE (lo que apunta a hosts y a grupos va antes; los dispositivos antes que sus colectores) y
// borra a mano las tablas que solo tienen ON DELETE SET NULL (un TRUNCATE ... CASCADE las vaciaba igual).
var fastDeletes = []string{
	`DELETE FROM alerts`,
	`DELETE FROM service_check_events`,
	`DELETE FROM service_checks`,
	`DELETE FROM metrics`,
	`DELETE FROM metrics_hourly`,
	`DELETE FROM metrics_chunks`,
	`DELETE FROM disk_samples`,
	`DELETE FROM maintenance_state`,
	`DELETE FROM enroll_tokens`,
	`DELETE FROM service_check_defs`,
	`DELETE FROM hosts WHERE kind = 'device'`,
	`DELETE FROM hosts`,
	`DELETE FROM snmp_profiles`,
	`DELETE FROM host_groups`,
	`DELETE FROM alert_rules`,
	`DELETE FROM notification_channels`,
	`DELETE FROM maintenance_windows`,
	`DELETE FROM sessions`,
	`DELETE FROM users`,
	`DELETE FROM settings`,
}

// ResetData deja las tablas de datos vacias (las de fabrica, no). Usa borrado por filas y solo cae al TRUNCATE
// completo si alguna sentencia falla (una FK nueva sin cubrir). Con ARGOS_TEST_VERIFY_RESET=1 comprueba ademas que no
// quedo ninguna fila fuera de las tablas de fabrica (pa detectar tablas nuevas que la limpieza rapida no cubre).
func ResetData(ctx context.Context, pool *pgxpool.Pool) error {
	if err := fastReset(ctx, pool); err != nil {
		if _, terr := pool.Exec(ctx, truncateAll); terr != nil {
			return errors.Join(fmt.Errorf("limpieza rapida: %w", err), fmt.Errorf("TRUNCATE de respaldo: %w", terr))
		}
	}
	if os.Getenv("ARGOS_TEST_VERIFY_RESET") == "" {
		return nil
	}
	left, err := NonEmptyTables(ctx, pool)
	if err != nil {
		return err
	}
	if len(left) > 0 {
		return fmt.Errorf("la limpieza dejo filas en: %s (agregalas a fastDeletes)", strings.Join(left, ", "))
	}
	return nil
}

func fastReset(ctx context.Context, pool *pgxpool.Pool) error {
	// audit_log es append-only (el trigger bloquea DELETE y UPDATE): solo se vacia con TRUNCATE, que es barato aqui.
	if _, err := pool.Exec(ctx, `TRUNCATE audit_log`); err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, q := range fastDeletes {
		if _, err := tx.Exec(ctx, q); err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}
	return tx.Commit(ctx)
}

// NonEmptyTables lista las tablas (no de fabrica, sin contar particiones) que tienen filas.
func NonEmptyTables(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		WITH t AS MATERIALIZED (
		  SELECT c.relname
		    FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		   WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND NOT c.relispartition AND c.relname <> ALL ($1)
		)
		SELECT relname FROM t
		 WHERE (xpath('/row/c/text()', query_to_xml(format('SELECT count(*) AS c FROM public.%I', relname), false, true, '')))[1]::text::bigint > 0
		 ORDER BY 1`, seedTables)
	if err != nil {
		return nil, fmt.Errorf("buscando tablas con filas: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
