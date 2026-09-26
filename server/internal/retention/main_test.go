package retention

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"argos/server/internal/store"
	"argos/server/internal/testdb"
)

// Estos tests corren contra una Postgres real: necesitan ARGOS_TEST_DATABASE_URL apuntando a
// una BD cuyo nombre termine en _test (cada test recrea el schema public). Sin la variable se
// saltan con aviso. Comparten BD con los tests de store y httpapi: un lock advisory de sesion
// serializa los paquetes. Los tests no usan t.Parallel: comparten una sola BD.

var (
	testPool *pgxpool.Pool
	testDSN  string
)

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	dsn := os.Getenv("ARGOS_TEST_DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "AVISO: ARGOS_TEST_DATABASE_URL no definida, se saltan los tests de retention")
		return m.Run()
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(u.Path, "/"), "_test") {
		fmt.Fprintln(os.Stderr, "ARGOS_TEST_DATABASE_URL debe apuntar a una BD cuyo nombre termine en _test")
		return 1
	}
	testDSN = dsn

	ctx := context.Background()
	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "conectando a la BD de test:", err)
		return 1
	}
	defer pool.Close()
	testPool = pool

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

	if err := resetSchema(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, "preparando la BD de test:", err)
		return 1
	}
	code := m.Run()
	// Deja la BD con el esquema estandar pa el siguiente paquete (cada uno la recrea de todos modos).
	if err := resetSchema(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, "restaurando la BD de test:", err)
	}
	return code
}

func resetSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		return fmt.Errorf("recreando schema public: %w", err)
	}
	return store.Migrate(ctx, pool)
}

// fresh deja la BD con el esquema recien migrado (particion legacy + 7 dias diarios desde hoy+2).
func fresh(t *testing.T) {
	t.Helper()
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	if err := resetSchema(context.Background(), testPool); err != nil {
		t.Fatal(err)
	}
}

// logBuf captura los logs JSON del mantenedor pa asserts.
type logBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func (l *logBuf) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Reset()
}

// noErrors falla si el mantenedor logueo algun ERROR.
func (l *logBuf) noErrors(t *testing.T) {
	t.Helper()
	if s := l.String(); strings.Contains(s, `"level":"ERROR"`) {
		t.Errorf("el mantenedor logueo errores:\n%s", s)
	}
}

// farNow es un "ahora" simulado 100 dias adelante: las particiones de la migracion (hoy+2 dias
// en adelante) y la legacy quedan muy atras y las nuevas no chocan con ellas.
func farNow() time.Time {
	return utcDay(time.Now()).AddDate(0, 0, 100).Add(10*time.Hour + 30*time.Minute)
}

var testCfg = Config{HotDays: 8, DetailDays: 30, HourlyDays: 400, ServiceChecksDays: 14, DiskDays: 30, Interval: time.Minute}

func newMaint(t *testing.T, cfg Config, now time.Time) (*Maintainer, *logBuf) {
	t.Helper()
	logs := &logBuf{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := New(testPool, log, cfg, func() time.Time { return now })
	m.lockTimeout = time.Second
	return m, logs
}

// withConn corre fn con una conexion del pool (pa llamar a los pasos internos sin el lock).
func withConn(t *testing.T, fn func(conn *pgx.Conn)) {
	t.Helper()
	pc, err := testPool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Release()
	fn(pc.Conn())
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

func mkHost(t *testing.T, name string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(),
		`INSERT INTO hosts (hostname, agent_token_hash, status) VALUES ($1, $1, 'up') RETURNING id`, name).Scan(&id)
	if err != nil {
		t.Fatalf("creando host: %v", err)
	}
	return id
}

// partNames devuelve los nombres de las particiones de parent, ordenados.
func partNames(t *testing.T, parent string) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
		  WHERE i.inhparent = $1::regclass ORDER BY 1`, parent)
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func hasPartition(t *testing.T, parent string, day time.Time) bool {
	t.Helper()
	want := partitionName(parent, day)
	for _, n := range partNames(t, parent) {
		if n == want {
			return true
		}
	}
	return false
}

// mkPartition crea la particion diaria de table pa day (como lo hace el mantenimiento).
func mkPartition(t *testing.T, table string, day time.Time) {
	t.Helper()
	withConn(t, func(conn *pgx.Conn) {
		if err := createPartition(context.Background(), conn, table, day); err != nil {
			t.Fatal(err)
		}
	})
}

// sampleRow es una fila de metrics pa CopyFrom.
type sampleRow struct {
	ts     time.Time
	cpu    *float64
	memU   *int
	memT   *int
	rx, tx *int64
}

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }
func ip(v int) *int          { return &v }

func insertRows(t *testing.T, hostID string, rows []sampleRow) {
	t.Helper()
	data := make([][]any, 0, len(rows))
	for _, r := range rows {
		data = append(data, []any{hostID, r.ts, r.cpu, r.memU, r.memT, r.rx, r.tx})
	}
	_, err := testPool.CopyFrom(context.Background(), pgx.Identifier{"metrics"},
		[]string{"host_id", "ts", "cpu_pct", "mem_used_mb", "mem_total_mb", "net_rx_bytes", "net_tx_bytes"}, pgx.CopyFromRows(data))
	if err != nil {
		t.Fatalf("insertando metricas: %v", err)
	}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }
