package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"argos/server/internal/testdb"
)

// Estos tests corren contra una Postgres real. Necesitan ARGOS_TEST_DATABASE_URL
// apuntando a una BD cuyo nombre termine en _test (p. ej. argos_test): la suite
// borra y recrea el schema public. Sin la variable, se saltan con aviso.
//
// No usan t.Parallel: comparten una sola BD.

var testPool *pgxpool.Pool

// testLockKey es la clave del lock advisory que serializa los paquetes de test contra
// la misma BD. Debe ser la misma en httpapi (main_test.go).
const testLockKey = 7368291

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	dsn := os.Getenv("ARGOS_TEST_DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "AVISO: ARGOS_TEST_DATABASE_URL no definida, se saltan los tests del store")
		return m.Run()
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(u.Path, "/"), "_test") {
		fmt.Fprintln(os.Stderr, "ARGOS_TEST_DATABASE_URL debe apuntar a una BD cuyo nombre termine en _test")
		return 1
	}

	ctx := context.Background()
	pool, err := Connect(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "conectando a la BD de test:", err)
		return 1
	}
	defer pool.Close()
	testPool = pool

	// Los tests de varios paquetes comparten esta BD y `go test ./...` corre los paquetes
	// en paralelo: un lock advisory de sesion los serializa (lo libera el defer).
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tomando conexion pa el lock de tests:", err)
		return 1
	}
	defer lockConn.Release()
	if err := acquireTestLock(ctx, lockConn); err != nil {
		fmt.Fprintln(os.Stderr, "tomando el lock de tests:", err)
		return 1
	}
	defer func() {
		if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, testLockKey); err != nil {
			fmt.Fprintln(os.Stderr, "liberando el lock de tests:", err)
		}
	}()

	if err := resetSchema(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "preparando la BD de test:", err)
		return 1
	}
	return m.Run()
}

// acquireTestLock espera el lock advisory de los tests sondeando con pg_try_advisory_lock. No usa
// pg_advisory_lock (bloqueante): una sentencia esperando un lock mantiene una transaccion abierta, y el
// DETACH PARTITION de los tests de retention espera a todas las transacciones anteriores de la BD (deadlock).
func acquireTestLock(ctx context.Context, conn *pgxpool.Conn) error {
	deadline := time.Now().Add(10 * time.Minute)
	for {
		var got bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, testLockKey).Scan(&got); err != nil {
			return err
		}
		if got {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timeout esperando el lock de tests")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// resetSchema deja la BD de test vacia y migrada al ultimo esquema.
func resetSchema(ctx context.Context) error {
	if err := dropSchema(ctx); err != nil {
		return err
	}
	return Migrate(ctx, testPool)
}

func dropSchema(ctx context.Context) error {
	if _, err := testPool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		return fmt.Errorf("recreando schema public: %w", err)
	}
	return nil
}

// newTestStore devuelve un Store sobre tablas vacias.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	if err := testdb.ResetData(context.Background(), testPool); err != nil {
		t.Fatalf("limpiando tablas: %v", err)
	}
	return New(testPool)
}

// th devuelve un hash de token de prueba con el formato que exige la BD (sha256 hex).
func th(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

// mkUser inserta un usuario de prueba y devuelve su id.
func mkUser(t *testing.T, s *Store, username, role string) string {
	t.Helper()
	u, err := s.CreateUser(context.Background(), username, "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA", role, false)
	if err != nil {
		t.Fatalf("creando usuario %q: %v", username, err)
	}
	return u.ID
}

func mustExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return n
}

// latestMigration es la version de la ultima migracion embebida.
const latestMigration = 33

func TestMigrateIsIdempotent(t *testing.T) {
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	ctx := context.Background()
	if err := resetSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, testPool); err != nil {
		t.Fatalf("segunda ejecucion de Migrate: %v", err)
	}
	if got := count(t, `SELECT count(*) FROM goose_db_version WHERE version_id > 0`); got != latestMigration {
		t.Errorf("migraciones registradas got %d, want %d", got, latestMigration)
	}
}

func TestMigrateDownUp(t *testing.T) {
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	ctx := context.Background()
	if err := resetSchema(ctx); err != nil {
		t.Fatal(err)
	}
	provider, err := newProvider(testPool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	if got := count(t, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename = 'hosts'`); got != 0 {
		t.Fatalf("hosts sigue existiendo tras down")
	}
	if err := Migrate(ctx, testPool); err != nil {
		t.Fatalf("up tras down: %v", err)
	}
}

// legacySchema es el esquema del MVP (antes de goose), tal como lo dejo el server viejo.
const legacySchema = `
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE hosts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  hostname TEXT NOT NULL, os TEXT, arch TEXT,
  agent_token_hash TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  last_seen_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE enroll_tokens (token TEXT PRIMARY KEY, hostname TEXT NOT NULL, expires_at TIMESTAMPTZ NOT NULL, used_at TIMESTAMPTZ);
CREATE TABLE metrics (
  host_id UUID NOT NULL REFERENCES hosts(id), ts TIMESTAMPTZ NOT NULL,
  cpu_pct REAL, mem_used_mb INT, mem_total_mb INT, net_rx_bytes BIGINT, net_tx_bytes BIGINT,
  PRIMARY KEY (host_id, ts)
);
CREATE TABLE service_checks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  host_id UUID NOT NULL REFERENCES hosts(id),
  name TEXT NOT NULL, status TEXT NOT NULL, response_ms INT, ts TIMESTAMPTZ NOT NULL
);`

func TestMigrateAdoptsLegacySchemaKeepingData(t *testing.T) {
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		if err := resetSchema(ctx); err != nil {
			t.Errorf("restaurando schema: %v", err)
		}
	})
	if err := dropSchema(ctx); err != nil {
		t.Fatal(err)
	}
	mustExec(t, legacySchema)
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash, status) VALUES ('legacy-01', 'h1', 'up')`)
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash, status, last_seen_at, created_at)
	             VALUES ('legacy-02', 'h2', 'up', now() - interval '1 hour', now() - interval '1 day')`)
	mustExec(t, `INSERT INTO service_checks (host_id, name, status, ts)
	             SELECT id, 'ssh', 'up', now() FROM hosts WHERE hostname = 'legacy-01'`)
	mustExec(t, `INSERT INTO enroll_tokens (token, hostname, expires_at) VALUES ('token-en-claro', 'legacy-01', now() + interval '1 hour')`)

	if err := Migrate(ctx, testPool); err != nil {
		t.Fatalf("migrate sobre esquema legacy: %v", err)
	}
	if got := count(t, `SELECT count(*) FROM hosts WHERE hostname = 'legacy-01'`); got != 1 {
		t.Errorf("hosts legacy got %d, want 1 (se perdieron datos)", got)
	}
	// 00005: status_changed_at se rellena con COALESCE(last_seen_at, created_at).
	if got := count(t, `SELECT count(*) FROM hosts WHERE (hostname = 'legacy-01' AND status_changed_at = created_at)
	                                                  OR (hostname = 'legacy-02' AND status_changed_at = last_seen_at)`); got != 2 {
		t.Errorf("backfill de status_changed_at correcto en %d de 2 hosts legacy", got)
	}
	if got := count(t, `SELECT count(*) FROM service_checks`); got != 1 {
		t.Errorf("service_checks legacy got %d, want 1", got)
	}
	if got := count(t, `SELECT count(*) FROM pg_indexes WHERE indexname = 'hosts_agent_token_hash_key'`); got != 1 {
		t.Errorf("no se creo el indice unico sobre agent_token_hash")
	}
	// 00004: el token en claro pasa a hash (sigue siendo canjeable) y la columna en claro desaparece.
	if got := count(t, `SELECT count(*) FROM enroll_tokens WHERE token_hash = $1`, th("token-en-claro")); got != 1 {
		t.Errorf("el enroll token legacy no se convirtio a hash sha256")
	}
	if got := count(t, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'enroll_tokens' AND column_name = 'token'`); got != 0 {
		t.Errorf("la columna enroll_tokens.token sigue existiendo")
	}
}

func TestSchemaConstraints(t *testing.T) {
	newTestStore(t)

	tests := []struct {
		name string
		sql  string
		want string // codigo SQLSTATE
	}{
		{"host status invalido", `INSERT INTO hosts (hostname, agent_token_hash, status) VALUES ('a', 'x1', 'roto')`, "23514"},
		{"agent_token_hash duplicado", `INSERT INTO hosts (hostname, agent_token_hash) VALUES ('a', 'dup'), ('b', 'dup')`, "23505"},
		{"enroll token_hash no hex", `INSERT INTO enroll_tokens (token_hash, hostname, expires_at) VALUES ('en-claro', 'a', now())`, "23514"},
		{"host revocado debe estar disabled", `INSERT INTO hosts (hostname, agent_token_hash, status, agent_token_revoked_at) VALUES ('a', 'x2', 'up', now())`, "23514"},
		{"rol invalido", `INSERT INTO users (username, password_hash, role) VALUES ('alice', '$argon2id$x', 'root')`, "23514"},
		{"hash que no es argon2id", `INSERT INTO users (username, password_hash, role) VALUES ('alice', 'plano', 'admin')`, "23514"},
		{"username duplicado sin mayusculas", `INSERT INTO users (username, password_hash, role) VALUES ('Alice', '$argon2id$x', 'admin'), ('alice', '$argon2id$x', 'admin')`, "23505"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testPool.Exec(context.Background(), tt.sql)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tt.want {
				t.Fatalf("err got %v, want SQLSTATE %s", err, tt.want)
			}
		})
	}
}

func TestEnrollHost(t *testing.T) {
	ctx := context.Background()
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)

	tests := []struct {
		name    string
		setup   func(t *testing.T, s *Store)
		token   string
		wantErr error
	}{
		{name: "feliz", setup: func(t *testing.T, s *Store) {
			if err := s.CreateEnrollToken(ctx, th("tok"), "srv-01", mkUser(t, s, "alice", RoleAdmin), nil, future); err != nil {
				t.Fatal(err)
			}
		}, token: th("tok")},
		{name: "inexistente", setup: func(*testing.T, *Store) {}, token: th("nope"), wantErr: ErrNotFound},
		{name: "expirado", setup: func(t *testing.T, s *Store) {
			if err := s.CreateEnrollToken(ctx, th("tok"), "srv-01", mkUser(t, s, "alice", RoleAdmin), nil, past); err != nil {
				t.Fatal(err)
			}
		}, token: th("tok"), wantErr: ErrTokenExpired},
		{name: "usado", setup: func(t *testing.T, s *Store) {
			if err := s.CreateEnrollToken(ctx, th("tok"), "srv-01", mkUser(t, s, "alice", RoleAdmin), nil, future); err != nil {
				t.Fatal(err)
			}
			if _, err := s.EnrollHost(ctx, th("tok"), "linux", "amd64", "hash-previo"); err != nil {
				t.Fatal(err)
			}
		}, token: th("tok"), wantErr: ErrTokenUsed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			tt.setup(t, s)
			before := count(t, `SELECT count(*) FROM hosts`)

			hostID, err := s.EnrollHost(ctx, tt.token, "linux", "amd64", "hash-nuevo")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err got %v, want %v", err, tt.wantErr)
			}
			after := count(t, `SELECT count(*) FROM hosts`)
			if tt.wantErr != nil {
				if hostID != "" || after != before {
					t.Errorf("un enroll fallido no debe crear host (hostID=%q, hosts %d->%d)", hostID, before, after)
				}
				return
			}
			if hostID == "" || after != before+1 {
				t.Errorf("enroll feliz: hostID=%q, hosts %d->%d", hostID, before, after)
			}
			if got := count(t, `SELECT count(*) FROM enroll_tokens WHERE token_hash = $1 AND used_at IS NOT NULL`, tt.token); got != 1 {
				t.Errorf("el token no quedo marcado como usado")
			}
			gotID, err := s.HostIDByTokenHash(ctx, "hash-nuevo")
			if err != nil || gotID != hostID {
				t.Errorf("HostIDByTokenHash got (%q, %v), want (%q, nil)", gotID, err, hostID)
			}
		})
	}
}

func TestEnrollHostConcurrentSingleWinner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateEnrollToken(ctx, th("tok"), "srv-01", mkUser(t, s, "alice", RoleAdmin), nil, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	const n = 10
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.EnrollHost(ctx, th("tok"), "linux", "amd64", fmt.Sprintf("hash-%d", i))
		}()
	}
	wg.Wait()

	var wins, used int
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrTokenUsed):
			used++
		default:
			t.Errorf("error inesperado: %v", err)
		}
	}
	if wins != 1 || used != n-1 {
		t.Errorf("ganadores got %d (used %d), want 1 (used %d)", wins, used, n-1)
	}
	if got := count(t, `SELECT count(*) FROM hosts`); got != 1 {
		t.Errorf("hosts got %d, want 1", got)
	}
}

func TestHostIDByTokenHashNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.HostIDByTokenHash(context.Background(), "no-existe"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err got %v, want ErrNotFound", err)
	}
}

func enrolled(t *testing.T, s *Store, hash string) string {
	t.Helper()
	ctx := context.Background()
	admin, err := s.UserByUsername(ctx, "enroller")
	if errors.Is(err, ErrNotFound) {
		admin.ID = mkUser(t, s, "enroller", RoleAdmin)
	} else if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEnrollToken(ctx, th("tok-"+hash), "srv", admin.ID, nil, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	id, err := s.EnrollHost(ctx, th("tok-"+hash), "linux", "amd64", hash)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestIngestMetrics(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hostID := enrolled(t, s, "h1")
	mustExec(t, `UPDATE hosts SET status = 'down', last_seen_at = NULL WHERE id = $1`, hostID)

	ts := time.Now().Truncate(time.Second)
	smp := Sample{
		Timestamp: ts, CPUPct: 42.5, MemUsedMB: 100, MemTotalMB: 200, NetRxBytes: 1, NetTxBytes: 2,
		Services: []ServiceCheck{{"nginx", "up", 5}, {"redis", "down", 0}},
	}
	if _, err := s.IngestMetrics(ctx, hostID, smp); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if got := count(t, `SELECT count(*) FROM metrics WHERE host_id = $1 AND cpu_pct = 42.5`, hostID); got != 1 {
		t.Errorf("metrics got %d, want 1", got)
	}
	if got := count(t, `SELECT count(*) FROM service_checks WHERE host_id = $1`, hostID); got != 2 {
		t.Errorf("service_checks got %d, want 2", got)
	}
	if got := count(t, `SELECT count(*) FROM hosts WHERE id = $1 AND status = 'up' AND last_seen_at IS NOT NULL`, hostID); got != 1 {
		t.Errorf("el host no quedo up con last_seen_at")
	}

	// Mismo (host_id, ts): ON CONFLICT DO NOTHING, sin error ni duplicado.
	smp.Services = nil
	if _, err := s.IngestMetrics(ctx, hostID, smp); err != nil {
		t.Fatalf("ingest duplicado: %v", err)
	}
	if got := count(t, `SELECT count(*) FROM metrics`); got != 1 {
		t.Errorf("metrics tras duplicado got %d, want 1", got)
	}
}

func TestIngestMetricsRollsBackOnFailure(t *testing.T) {
	s := newTestStore(t)
	hostID := enrolled(t, s, "h1")

	smp := Sample{
		Timestamp: time.Now(),
		Services:  []ServiceCheck{{"ok", "up", 1}, {"malo", "estado-invalido", 1}},
	}
	if _, err := s.IngestMetrics(context.Background(), hostID, smp); err == nil {
		t.Fatal("se esperaba error por status invalido")
	}
	if m, c := count(t, `SELECT count(*) FROM metrics`), count(t, `SELECT count(*) FROM service_checks`); m != 0 || c != 0 {
		t.Errorf("la transaccion no hizo rollback: metrics=%d service_checks=%d", m, c)
	}
}
