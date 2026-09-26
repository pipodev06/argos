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

// prepareSplit deja la BD partida con el esquema public vacio. La BD y los roles se crean una sola vez y se
// conservan entre corridas: DROP DATABASE fuerza un checkpoint inmediato que tardaba ~30 s con los datos sucios de los
// tests anteriores. Los roles solo existen en el Postgres de test (nombres argos_t_*).
func prepareSplit(ctx context.Context, t *testing.T) {
	t.Helper()
	for _, s := range []string{
		`DO $$ BEGIN
		   IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '` + splitOwner + `') THEN CREATE ROLE ` + splitOwner + ` LOGIN PASSWORD '` + splitOwner + `_pw'; END IF;
		   IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '` + splitApp + `') THEN CREATE ROLE ` + splitApp + ` LOGIN PASSWORD '` + splitApp + `_pw'; END IF;
		 END $$`,
		`GRANT ` + splitApp + ` TO ` + splitOwner,
	} {
		if _, err := testPool.Exec(ctx, s); err != nil {
			t.Fatalf("preparando los roles (%s): %v", s, err)
		}
	}
	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT FROM pg_database WHERE datname = $1)`, splitDB).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		if _, err := testPool.Exec(ctx, `CREATE DATABASE `+splitDB+` OWNER `+splitOwner); err != nil {
			t.Fatalf("creando la BD partida: %v", err)
		}
	}
	// Esquema vacio como superusuario: quita tambien las particionadas que el runtime hubiera heredado y los
	// permisos por defecto de la corrida anterior.
	su, err := store.Connect(ctx, suDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer su.Close()
	if _, err := su.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public AUTHORIZATION pg_database_owner`); err != nil {
		t.Fatalf("recreando el esquema de la BD partida: %v", err)
	}
}

// suDSN es la URL del superusuario de test (la de ARGOS_TEST_DATABASE_URL) apuntando a la BD partida.
func suDSN(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(testDSN)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + splitDB
	return u.String()
}

// Con el server operando como rol de runtime (sin ser dueño del esquema): todo lo que hace de verdad funciona
// (DML y el mantenimiento de particiones, incluido DETACH CONCURRENTLY y borrar la particion legacy) y NO puede
// tocar el trigger append-only de audit_log ni hacer DDL sobre el resto.
func TestRuntimeRoleSplit(t *testing.T) {
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	ctx := context.Background()
	prepareSplit(ctx, t)

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
	// Datos pa el empaquetado y el recorte como runtime: una muestra en la particion legacy (creada por el migrador), la
	// marca de agua del resumen al dia y un chunk de hace ~800 dias (vencido con cualquier retencion de hasta 730).
	var hostID string
	if err := app.QueryRow(ctx, `INSERT INTO hosts (hostname, agent_token_hash, status) VALUES ('h-roles', 'h-roles', 'up') RETURNING id`).Scan(&hostID); err != nil {
		t.Fatalf("el runtime debe poder crear hosts: %v", err)
	}
	packDay := utcDay(time.Now()).AddDate(0, 0, -5)
	if _, err := app.Exec(ctx, `INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb) VALUES ($1, $2, 50, 1000, 2000)`, hostID, packDay.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Exec(ctx, `INSERT INTO maintenance_state (name, value_ts) VALUES ($1, $2)`, rollupMarker, farNow().Truncate(time.Hour)); err != nil {
		t.Fatal(err)
	}
	expiredDay := utcDay(farNow()).AddDate(0, 0, -800)
	if _, err := app.Exec(ctx, `INSERT INTO metrics_chunks (host_id, day, n, codec, data) VALUES ($1, $2::date, 0, 1, $3)`, hostID, expiredDay, []byte{0}); err != nil {
		t.Fatalf("el runtime debe poder escribir en metrics_chunks: %v", err)
	}

	logs := &logBuf{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	cfg := testCfg
	cfg.DetailDays = 730 // el dia empaquetado queda ~105 dias atras: dentro de la retencion
	m := New(app, log, cfg, farNow)
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
	// Empaquetado (INSERT ... ON CONFLICT) y recorte (DELETE) de metrics_chunks como runtime.
	var packed, expired int
	if err := app.QueryRow(ctx, `SELECT count(*) FROM metrics_chunks WHERE host_id = $1 AND day = $2::date AND n = 1`, hostID, packDay).Scan(&packed); err != nil || packed != 1 {
		t.Errorf("el runtime debia empaquetar la muestra de la particion legacy: chunks=%d (%v)", packed, err)
	}
	if err := app.QueryRow(ctx, `SELECT count(*) FROM metrics_chunks WHERE host_id = $1 AND day = $2::date`, hostID, expiredDay).Scan(&expired); err != nil || expired != 0 {
		t.Errorf("el runtime debia recortar el chunk vencido: chunks=%d (%v)", expired, err)
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
