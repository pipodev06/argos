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
	if err := testdb.AcquireLock(ctx, lockConn); err != nil {
		fmt.Fprintln(os.Stderr, "tomando el lock de tests:", err)
		return 1
	}
	defer func() {
		if err := testdb.ReleaseLock(ctx, lockConn); err != nil {
			fmt.Fprintln(os.Stderr, "liberando el lock de tests:", err)
		}
	}()

	if err := resetSchema(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "preparando la BD de test:", err)
		return 1
	}
	return m.Run()
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

// mkEnrollToken replica la firma vieja (previa a B16) de CreateEnrollToken: alta normal con hostname
// fijo (o re-enroll con targetHostID), sin alta masiva. Los tests de B16 usan CreateEnrollTokenParams
// directo pa fijar MaxUses.
func mkEnrollToken(ctx context.Context, s *Store, tokenHash, hostname, createdBy string, groupID *string, expiresAt time.Time, targetHostID *string) error {
	return s.CreateEnrollToken(ctx, CreateEnrollTokenParams{
		TokenHash: tokenHash, Hostname: hostname, CreatedBy: createdBy, GroupID: groupID, ExpiresAt: expiresAt, TargetHostID: targetHostID,
	})
}

// enrollHost replica la firma vieja de EnrollHost (sin agentHostname): los tests que no son de B16
// canjean tokens con hostname fijo, donde agentHostname se ignora.
func enrollHost(ctx context.Context, s *Store, tokenHash, osName, arch, agentTokenHash string) (string, bool, error) {
	return s.EnrollHost(ctx, tokenHash, osName, arch, agentTokenHash, "")
}

// th devuelve un hash de token de prueba con el formato que exige la BD (sha256 hex).
func th(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

// mkUser inserta un usuario de prueba y devuelve su id.
func mkUser(t *testing.T, s *Store, username, role string) string {
	t.Helper()
	u, err := s.CreateUser(context.Background(), NewUser{Username: username, PasswordHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA", Role: role, AllGroups: true})
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
const latestMigration = 38

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
		{"username duplicado sin mayusculas", `INSERT INTO users (username, password_hash, role, all_groups) VALUES ('Alice', '$argon2id$x', 'admin', true), ('alice', '$argon2id$x', 'admin', true)`, "23505"},
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
			if err := mkEnrollToken(ctx, s, th("tok"), "srv-01", mkUser(t, s, "alice", RoleAdmin), nil, future, nil); err != nil {
				t.Fatal(err)
			}
		}, token: th("tok")},
		{name: "inexistente", setup: func(*testing.T, *Store) {}, token: th("nope"), wantErr: ErrNotFound},
		{name: "expirado", setup: func(t *testing.T, s *Store) {
			if err := mkEnrollToken(ctx, s, th("tok"), "srv-01", mkUser(t, s, "alice", RoleAdmin), nil, past, nil); err != nil {
				t.Fatal(err)
			}
		}, token: th("tok"), wantErr: ErrTokenExpired},
		{name: "usado", setup: func(t *testing.T, s *Store) {
			if err := mkEnrollToken(ctx, s, th("tok"), "srv-01", mkUser(t, s, "alice", RoleAdmin), nil, future, nil); err != nil {
				t.Fatal(err)
			}
			if _, _, err := enrollHost(ctx, s, th("tok"), "linux", "amd64", "hash-previo"); err != nil {
				t.Fatal(err)
			}
		}, token: th("tok"), wantErr: ErrTokenUsed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			tt.setup(t, s)
			before := count(t, `SELECT count(*) FROM hosts`)

			hostID, _, err := enrollHost(ctx, s, tt.token, "linux", "amd64", "hash-nuevo")
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
	if err := mkEnrollToken(ctx, s, th("tok"), "srv-01", mkUser(t, s, "alice", RoleAdmin), nil, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}

	const n = 10
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = enrollHost(ctx, s, th("tok"), "linux", "amd64", fmt.Sprintf("hash-%d", i))
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

// TestEnrollHostBulkToken cubre B16: un token de grupo (sin hostname fijo, MaxUses=5) acepta 5
// canjes con hostnames distintos, cada uno crea un host propio en el grupo del token, y el 6to falla
// igual que un token usado/expirado (mismo error uniforme, ErrTokenUsed).
func TestEnrollHostBulkToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "alice", RoleAdmin)
	gid, err := s.CreateGroup(ctx, "clientes", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEnrollToken(ctx, CreateEnrollTokenParams{
		TokenHash: th("bulk"), CreatedBy: admin, GroupID: &gid.ID, ExpiresAt: time.Now().Add(time.Hour), MaxUses: 5,
	}); err != nil {
		t.Fatal(err)
	}

	hostnames := []string{"cliente-01", "cliente-02", "cliente-03", "cliente-04", "cliente-05"}
	for i, hn := range hostnames {
		hostID, reenroll, err := s.EnrollHost(ctx, th("bulk"), "linux", "amd64", th(fmt.Sprintf("agent-%d", i)), hn)
		if err != nil {
			t.Fatalf("canje %d (%s): %v", i, hn, err)
		}
		if reenroll {
			t.Errorf("canje %d: reenroll got true, want false", i)
		}
		var gotHostname string
		var gotGroup *string
		if err := testPool.QueryRow(ctx, `SELECT hostname, group_id FROM hosts WHERE id = $1`, hostID).Scan(&gotHostname, &gotGroup); err != nil {
			t.Fatal(err)
		}
		if gotHostname != hn || gotGroup == nil || *gotGroup != gid.ID {
			t.Errorf("canje %d: hostname=%q group=%v, want %q/%q", i, gotHostname, gotGroup, hn, gid.ID)
		}
	}
	if got := count(t, `SELECT count(*) FROM hosts WHERE group_id = $1`, gid.ID); got != len(hostnames) {
		t.Errorf("hosts en el grupo got %d, want %d", got, len(hostnames))
	}
	if got := count(t, `SELECT uses_count FROM enroll_tokens WHERE token_hash = $1 AND used_at IS NOT NULL`, th("bulk")); got != 5 {
		t.Errorf("el token de grupo debe quedar marcado usado (agotado) con uses_count=5, got %d", got)
	}

	// El 6to canje: el mismo error uniforme que un token usado.
	if _, _, err := s.EnrollHost(ctx, th("bulk"), "linux", "amd64", th("agent-6"), "cliente-06"); !errors.Is(err, ErrTokenUsed) {
		t.Errorf("6to canje: err got %v, want ErrTokenUsed", err)
	}
}

// TestEnrollHostBulkTokenRequiresAgentHostname cubre B16: un token de grupo exige que el agente
// mande su hostname; si viene vacio, ErrEnrollHostnameRequired sin consumir el token.
func TestEnrollHostBulkTokenRequiresAgentHostname(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "alice", RoleAdmin)
	gid, err := s.CreateGroup(ctx, "clientes", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEnrollToken(ctx, CreateEnrollTokenParams{
		TokenHash: th("bulk-sin-hostname"), CreatedBy: admin, GroupID: &gid.ID, ExpiresAt: time.Now().Add(time.Hour), MaxUses: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EnrollHost(ctx, th("bulk-sin-hostname"), "linux", "amd64", th("agent"), ""); !errors.Is(err, ErrEnrollHostnameRequired) {
		t.Errorf("err got %v, want ErrEnrollHostnameRequired", err)
	}
	if got := count(t, `SELECT uses_count FROM enroll_tokens WHERE token_hash = $1`, th("bulk-sin-hostname")); got != 0 {
		t.Errorf("un canje rechazado no debe consumir el token, uses_count got %d", got)
	}
}

// TestEnrollHostBulkTokenConcurrentCap cubre B16: N goroutines canjeando el mismo token de grupo
// (MaxUses=5, N=15) no superan el tope aunque compitan a la vez.
func TestEnrollHostBulkTokenConcurrentCap(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "alice", RoleAdmin)
	gid, err := s.CreateGroup(ctx, "clientes", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	const maxUses = 5
	if err := s.CreateEnrollToken(ctx, CreateEnrollTokenParams{
		TokenHash: th("bulk-cap"), CreatedBy: admin, GroupID: &gid.ID, ExpiresAt: time.Now().Add(time.Hour), MaxUses: maxUses,
	}); err != nil {
		t.Fatal(err)
	}

	const n = 15
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = s.EnrollHost(ctx, th("bulk-cap"), "linux", "amd64", th(fmt.Sprintf("agent-%d", i)), fmt.Sprintf("host-%d", i))
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
	if wins != maxUses || used != n-maxUses {
		t.Errorf("ganadores got %d (used %d), want %d (used %d)", wins, used, maxUses, n-maxUses)
	}
	if got := count(t, `SELECT count(*) FROM hosts WHERE group_id = $1`, gid.ID); got != maxUses {
		t.Errorf("hosts got %d, want %d", got, maxUses)
	}
}

// TestCreateEnrollTokenBulkTargetConflict cubre B16: alta masiva (MaxUses > 1) y target_host_id
// (re-enroll/reactivacion) son mutuamente excluyentes.
func TestCreateEnrollTokenBulkTargetConflict(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "alice", RoleAdmin)
	hostID := enrolled(t, s, "hash-target")
	err := s.CreateEnrollToken(ctx, CreateEnrollTokenParams{
		TokenHash: th("conflict"), CreatedBy: admin, ExpiresAt: time.Now().Add(time.Hour), TargetHostID: &hostID, MaxUses: 5,
	})
	if !errors.Is(err, ErrBulkTargetConflict) {
		t.Errorf("err got %v, want ErrBulkTargetConflict", err)
	}
}

// TestListPendingEnrollTokensBulkHalfUsed cubre B16: un token de grupo a medio usar (canjeado, pero
// sin llegar a MaxUses) sigue "pendiente" (lo lista ListPendingEnrollTokens con su avance
// max_uses/uses_count, hostname nil) y sigue siendo revocable.
func TestListPendingEnrollTokensBulkHalfUsed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "alice", RoleAdmin)
	gid, err := s.CreateGroup(ctx, "clientes", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEnrollToken(ctx, CreateEnrollTokenParams{
		TokenHash: th("bulk-pend"), CreatedBy: admin, GroupID: &gid.ID, ExpiresAt: time.Now().Add(time.Hour), MaxUses: 5,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EnrollHost(ctx, th("bulk-pend"), "linux", "amd64", th("agent-0"), "cliente-00"); err != nil {
		t.Fatal(err)
	}

	tokens, err := s.ListPendingEnrollTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 {
		t.Fatalf("pendientes got %d, want 1", len(tokens))
	}
	tok := tokens[0]
	if tok.Hostname != nil {
		t.Errorf("hostname got %v, want nil (token de grupo)", tok.Hostname)
	}
	if tok.MaxUses != 5 || tok.UsesCount != 1 {
		t.Errorf("max_uses/uses_count got %d/%d, want 5/1", tok.MaxUses, tok.UsesCount)
	}

	if hostname, err := s.RevokeEnrollToken(ctx, tok.ID); err != nil || hostname != nil {
		t.Errorf("revocar un token de grupo a medio usar: got (%v, %v), want (nil, nil)", hostname, err)
	}
	if _, _, err := s.EnrollHost(ctx, th("bulk-pend"), "linux", "amd64", th("agent-1"), "cliente-01"); !errors.Is(err, ErrNotFound) {
		t.Errorf("tras revocar, un canje debe fallar con ErrNotFound, got %v", err)
	}
}

// TestEnrollHostWithTargetHostID cubre B14/B15: canjear un enroll token con target_host_id actualiza
// el host existente (conserva id, os/arch/status/last_seen_at nuevos, agent_token_revoked_at limpio,
// group_id sin cambios) en vez de crear uno nuevo, y las metricas viejas del host siguen ligadas a el.
func TestEnrollHostWithTargetHostID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "alice", RoleAdmin)

	gid, err := s.CreateGroup(ctx, "g1", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	hostID := enrolled(t, s, "hash-original")
	if _, err := s.SetHostGroup(ctx, hostID, &gid.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO metrics (host_id, ts, cpu_pct) VALUES ($1, now(), 5)`, hostID); err != nil {
		t.Fatal(err)
	}

	if err := mkEnrollToken(ctx, s, th("re"), "ignorado", admin, nil, time.Now().Add(time.Hour), &hostID); err != nil {
		t.Fatal(err)
	}
	before := count(t, `SELECT count(*) FROM hosts`)
	gotID, reenroll, err := enrollHost(ctx, s, th("re"), "windows", "arm64", "hash-nuevo")
	if err != nil {
		t.Fatal(err)
	}
	if !reenroll {
		t.Error("reenroll got false, want true")
	}
	if gotID != hostID {
		t.Errorf("hostID got %q, want %q (conserva el id)", gotID, hostID)
	}
	if after := count(t, `SELECT count(*) FROM hosts`); after != before {
		t.Errorf("un re-enroll no debe crear un host nuevo: hosts %d->%d", before, after)
	}

	var osName, arch, status string
	var revokedAt *time.Time
	var groupID *string
	if err := testPool.QueryRow(ctx,
		`SELECT os, arch, status, agent_token_revoked_at, group_id FROM hosts WHERE id = $1`, hostID,
	).Scan(&osName, &arch, &status, &revokedAt, &groupID); err != nil {
		t.Fatal(err)
	}
	if osName != "windows" || arch != "arm64" || status != "pending" {
		t.Errorf("host actualizado: os=%q arch=%q status=%q, want windows/arm64/pending", osName, arch, status)
	}
	if revokedAt != nil {
		t.Errorf("agent_token_revoked_at got %v, want nil", revokedAt)
	}
	if groupID == nil || *groupID != gid.ID {
		t.Errorf("group_id got %v, want %q (el re-enroll no debe mover de grupo)", groupID, gid.ID)
	}
	if got, err := s.HostIDByTokenHash(ctx, "hash-nuevo"); err != nil || got != hostID {
		t.Errorf("HostIDByTokenHash con el token nuevo got (%q, %v), want (%q, nil)", got, err, hostID)
	}
	if _, err := s.HostIDByTokenHash(ctx, "hash-original"); !errors.Is(err, ErrNotFound) {
		t.Errorf("el token de agente viejo debe quedar invalido, err got %v", err)
	}
	if got := count(t, `SELECT count(*) FROM metrics WHERE host_id = $1`, hostID); got != 1 {
		t.Errorf("las metricas viejas del host deben seguir ligadas a el, got %d filas", got)
	}
}

// TestEnrollHostReactivatesDisabled cubre B15: reactivar un host disabled via target_host_id lo vuelve
// a dejar autenticable (status pending, token revocado limpio).
func TestEnrollHostReactivatesDisabled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "alice", RoleAdmin)
	hostID := enrolled(t, s, "hash-disabled")
	if err := s.RevokeHost(ctx, hostID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HostIDByTokenHash(ctx, "hash-disabled"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("host revocado no deberia autenticar, err got %v", err)
	}

	if err := mkEnrollToken(ctx, s, th("react"), "ignorado", admin, nil, time.Now().Add(time.Hour), &hostID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := enrollHost(ctx, s, th("react"), "linux", "amd64", "hash-reactivado"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.HostIDByTokenHash(ctx, "hash-reactivado"); err != nil || got != hostID {
		t.Errorf("host reactivado deberia autenticar con el token nuevo, got (%q, %v)", got, err)
	}
}

// TestCreateEnrollTokenTargetHostRejectsDevice cubre B14/B15: un dispositivo no se re-enrola asi.
func TestCreateEnrollTokenTargetHostRejectsDevice(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "alice", RoleAdmin)
	collectorID := enrolled(t, s, "hash-collector")
	var devID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO hosts (hostname, kind, address, collector_host_id) VALUES ('dev-1', 'device', '10.0.0.1', $1) RETURNING id`,
		collectorID,
	).Scan(&devID); err != nil {
		t.Fatal(err)
	}
	if err := mkEnrollToken(ctx, s, th("dev"), "ignorado", admin, nil, time.Now().Add(time.Hour), &devID); !errors.Is(err, ErrNotFound) {
		t.Errorf("err got %v, want ErrNotFound", err)
	}

	missing := "00000000-0000-0000-0000-000000000000"
	if err := mkEnrollToken(ctx, s, th("missing"), "ignorado", admin, nil, time.Now().Add(time.Hour), &missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("host inexistente: err got %v, want ErrNotFound", err)
	}
}

// TestHostConflictByHostname cubre el aviso de B14 (no bloqueante) en el alta normal.
func TestHostConflictByHostname(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, _, found, err := s.HostConflictByHostname(ctx, "srv-01"); err != nil || found {
		t.Fatalf("sin hosts: got found=%v err=%v, want false/nil", found, err)
	}
	hostID := enrolled(t, s, "hash-conflict")
	if _, err := testPool.Exec(ctx, `UPDATE hosts SET hostname = 'SRV-01' WHERE id = $1`, hostID); err != nil {
		t.Fatal(err)
	}
	id, status, found, err := s.HostConflictByHostname(ctx, "srv-01")
	if err != nil || !found || id != hostID || status != "up" {
		t.Errorf("got id=%q status=%q found=%v err=%v, want %q/up/true/nil", id, status, found, err, hostID)
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
	if err := mkEnrollToken(ctx, s, th("tok-"+hash), "srv", admin.ID, nil, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	id, _, err := enrollHost(ctx, s, th("tok-"+hash), "linux", "amd64", hash)
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
