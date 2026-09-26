package retention

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"argos/server/internal/store"
)

const (
	splitDB    = "argos_split_test"
	splitOwner = "argos_t_owner"
	splitApp   = "argos_t_app"
)

// dsnAs devuelve la URL de la BD de test partida, conectada como el rol dado.
func dsnAs(t *testing.T, role string) string {
	t.Helper()
	u, err := url.Parse(testDSN)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(role, role+"_pw")
	u.Path = "/" + splitDB
	return u.String()
}

func connectAs(t *testing.T, role string) *pgxpool.Pool {
	t.Helper()
	pool, err := store.Connect(context.Background(), dsnAs(t, role))
	if err != nil {
		t.Fatalf("conectando como %s: %v (el Postgres de test debe aceptar contrasena en localhost)", role, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// dropSplit borra la BD y los roles del test (idempotente).
func dropSplit(ctx context.Context) {
	_, _ = testPool.Exec(ctx, `DROP DATABASE IF EXISTS `+splitDB+` WITH (FORCE)`)
	_, _ = testPool.Exec(ctx, `DROP ROLE IF EXISTS `+splitApp)
	_, _ = testPool.Exec(ctx, `DROP ROLE IF EXISTS `+splitOwner)
}

// Con el server operando como rol de runtime (sin ser dueño del esquema): todo lo que hace de verdad funciona
// (DML y el mantenimiento de particiones, incluido DETACH CONCURRENTLY y borrar la particion legacy) y NO puede
// tocar el trigger append-only de audit_log ni hacer DDL sobre el resto.
func TestRuntimeRoleSplit(t *testing.T) {
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	ctx := context.Background()
	dropSplit(ctx)
	t.Cleanup(func() { dropSplit(context.Background()) })
	for _, s := range []string{
		`CREATE ROLE ` + splitOwner + ` LOGIN PASSWORD '` + splitOwner + `_pw'`,
		`CREATE ROLE ` + splitApp + ` LOGIN PASSWORD '` + splitApp + `_pw'`,
		`GRANT ` + splitApp + ` TO ` + splitOwner,
		`CREATE DATABASE ` + splitDB + ` OWNER ` + splitOwner,
	} {
		if _, err := testPool.Exec(ctx, s); err != nil {
			t.Fatalf("preparando la BD partida (%s): %v", s, err)
		}
	}

	owner := connectAs(t, splitOwner)
	if err := store.Migrate(ctx, owner); err != nil {
		t.Fatal(err)
	}
	for range 2 { // idempotente
		if err := store.PrepareRuntimeRole(ctx, owner, splitApp); err != nil {
			t.Fatalf("PrepareRuntimeRole: %v", err)
		}
	}
	// Sin ser miembro no se puede: el migrador no puede cederse tablas a un rol ajeno.
	if err := store.PrepareRuntimeRole(ctx, owner, "postgres"); err == nil {
		t.Error("PrepareRuntimeRole con un rol del que no es miembro debe fallar")
	}
	if err := store.PrepareRuntimeRole(ctx, owner, "Robert'; DROP TABLE users;--"); err == nil {
		t.Error("PrepareRuntimeRole con un nombre invalido debe fallar")
	}

	app := connectAs(t, splitApp)
	if pending, err := store.SchemaPending(ctx, app); err != nil || pending {
		t.Fatalf("SchemaPending como runtime = %v, %v; want false, nil", pending, err)
	}

	// DML normal.
	if err := store.New(app).InsertAudit(ctx, store.AuditEntry{Event: "test", Outcome: store.OutcomeOK}); err != nil {
		t.Fatalf("el runtime debe poder insertar en audit_log: %v", err)
	}

	// DDL y manipulacion del trigger: todo denegado con insufficient_privilege.
	denied := []string{
		`DROP TRIGGER audit_log_guard ON audit_log`,
		`ALTER TABLE audit_log DISABLE TRIGGER audit_log_guard`,
		`ALTER TABLE audit_log DISABLE TRIGGER ALL`,
		`ALTER TABLE audit_log DROP COLUMN event`,
		`CREATE OR REPLACE FUNCTION audit_log_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`,
		`CREATE TRIGGER x BEFORE INSERT ON hosts FOR EACH ROW EXECUTE FUNCTION audit_log_guard()`,
		`TRUNCATE audit_log`,
		`UPDATE audit_log SET event = 'x'`,
		`DELETE FROM audit_log`, // el trigger solo deja purgar filas de mas de 30 dias
		`DROP TABLE users`,
		`ALTER TABLE hosts ADD COLUMN x int`,
	}
	for _, s := range denied {
		_, err := app.Exec(ctx, s)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Errorf("como runtime, %q debe fallar con 42501, got %v", s, err)
		}
	}

	// El mantenimiento completo corre con el rol de runtime: crea particiones, resume, hace DETACH ... CONCURRENTLY y
	// borra las vencidas (incluida la legacy, creada por el migrador).
	logs := &logBuf{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	m := New(app, log, testCfg, farNow)
	m.lockTimeout = 7 * time.Second
	if !m.RunOnce(ctx) {
		t.Fatal("RunOnce como runtime got ran=false")
	}
	if out := logs.String(); strings.Contains(out, `"level":"ERROR"`) {
		t.Errorf("el mantenimiento como runtime fallo:\n%s", out)
	}
	var legacy *string
	if err := app.QueryRow(ctx, `SELECT to_regclass('public.metrics_legacy')::text`).Scan(&legacy); err != nil || legacy != nil {
		t.Errorf("la particion legacy debia borrarse como runtime: %v %v", legacy, err)
	}
	var parts int
	if err := app.QueryRow(ctx, `SELECT count(*) FROM pg_inherits WHERE inhparent = 'metrics'::regclass`).Scan(&parts); err != nil || parts == 0 {
		t.Errorf("particiones de metrics tras el mantenimiento = %d (%v), want > 0", parts, err)
	}

	// Una migracion futura sigue siendo posible como migrador (es miembro del rol dueño de las particionadas).
	if _, err := owner.Exec(ctx, `ALTER TABLE metrics ADD COLUMN IF NOT EXISTS zz_futura int`); err != nil {
		t.Errorf("el migrador debe poder alterar las tablas particionadas: %v", err)
	}
}
