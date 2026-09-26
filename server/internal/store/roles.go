package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// roleNameRe acota el nombre del rol de runtime (se usa como identificador SQL, siempre entrecomillado).
var roleNameRe = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// SchemaPending dice si hay migraciones sin aplicar. Con ARGOS_AUTO_MIGRATE=false el server no migra: lo usa
// para negarse a arrancar sobre un esquema atrasado (las migraciones las corre `argos-server migrate` con otro rol).
func SchemaPending(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	provider, err := newProvider(pool)
	if err != nil {
		return false, err
	}
	pending, err := provider.HasPending(ctx)
	if err != nil {
		return false, fmt.Errorf("comprobando migraciones pendientes: %w", err)
	}
	return pending, nil
}

// PrepareRuntimeRole deja el esquema listo para que el server opere con un rol SIN privilegios de dueño (separado
// del rol que migra). Se ejecuta con el rol migrador, tras las migraciones, y es idempotente:
//   - DML sobre todas las tablas (y USAGE de secuencias), y lo mismo por defecto para lo que creen migraciones futuras.
//     Sin TRUNCATE, TRIGGER, REFERENCES ni DDL: el rol no puede tocar el trigger append-only de audit_log, y a
//     audit_log ni siquiera se le da UPDATE.
//   - Dueño de las tablas particionadas (metrics, service_checks, disk_samples) y de sus particiones, más CREATE en
//     el esquema public: el mantenimiento crea, ata, desata y borra particiones (ATTACH/DETACH exigen ser dueño), y
//     DETACH CONCURRENTLY no cabe en una funcion SECURITY DEFINER (no corre dentro de una transaccion).
//
// El rol migrador debe ser miembro del rol de runtime (GRANT <runtime> TO <migrador>) pa cederle esas tablas y pa
// seguir pudiendo migrarlas. Si ambos son el mismo rol (desarrollo, un solo usuario) no hace nada.
func PrepareRuntimeRole(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !roleNameRe.MatchString(role) {
		return fmt.Errorf("ARGOS_APP_ROLE %q invalido (minusculas, digitos y _; hasta 63 caracteres)", role)
	}
	var exists, same, member bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1),
		        current_user = $1,
		        COALESCE(pg_has_role(current_user, $1, 'MEMBER'), false)`, role).Scan(&exists, &same, &member)
	if err != nil {
		return fmt.Errorf("consultando roles: %w", err)
	}
	if !exists {
		return fmt.Errorf("el rol %q no existe: creale antes (ver deploy/README.md)", role)
	}
	if same {
		return nil
	}
	if !member {
		return fmt.Errorf("el rol migrador debe ser miembro de %q (GRANT %s TO <migrador>)", role, role)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("iniciando transaccion de permisos: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	r := pgx.Identifier{role}.Sanitize()
	stmts := []string{
		`GRANT USAGE, CREATE ON SCHEMA public TO ` + r,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO ` + r,
		`REVOKE UPDATE ON audit_log FROM ` + r,
		`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO ` + r,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO ` + r,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO ` + r,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(ctx, s); err != nil {
			return fmt.Errorf("aplicando permisos (%s): %w", s, err)
		}
	}

	rows, err := tx.Query(ctx, `
		SELECT n.nspname, c.relname
		  FROM pg_class c
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public'
		   AND c.relowner <> (SELECT oid FROM pg_roles WHERE rolname = $1)
		   AND (c.oid IN (SELECT partrelid FROM pg_partitioned_table)
		        OR c.oid IN (SELECT i.inhrelid FROM pg_inherits i JOIN pg_partitioned_table p ON p.partrelid = i.inhparent))
		 ORDER BY c.relname`, role)
	if err != nil {
		return fmt.Errorf("listando tablas particionadas: %w", err)
	}
	type tbl struct{ schema, name string }
	var tables []tbl
	for rows.Next() {
		var t tbl
		if err := rows.Scan(&t.schema, &t.name); err != nil {
			rows.Close()
			return fmt.Errorf("leyendo tablas particionadas: %w", err)
		}
		tables = append(tables, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("listando tablas particionadas: %w", err)
	}
	for _, t := range tables {
		s := `ALTER TABLE ` + pgx.Identifier{t.schema, t.name}.Sanitize() + ` OWNER TO ` + r
		if _, err := tx.Exec(ctx, s); err != nil {
			return fmt.Errorf("cediendo %s a %s: %w", t.name, role, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.Join(errors.New("confirmando permisos"), err)
	}
	return nil
}
