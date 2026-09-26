package httpapi

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"argos/server/internal/store"
	"argos/server/internal/testdb"
)

// Los tests de seguridad (security_test.go) corren contra una Postgres real:
// necesitan ARGOS_TEST_DATABASE_URL apuntando a una BD cuyo nombre termine en _test
// (la suite borra y recrea el schema public). Sin la variable, se saltan con aviso.
// Comparten BD con los tests del paquete store: un lock advisory los serializa.

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	dsn := os.Getenv("ARGOS_TEST_DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "AVISO: ARGOS_TEST_DATABASE_URL no definida, se saltan los tests de seguridad con BD")
		return m.Run()
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(u.Path, "/"), "_test") {
		fmt.Fprintln(os.Stderr, "ARGOS_TEST_DATABASE_URL debe apuntar a una BD cuyo nombre termine en _test")
		return 1
	}

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

	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		fmt.Fprintln(os.Stderr, "recreando schema public:", err)
		return 1
	}
	if err := store.Migrate(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, "migrando la BD de test:", err)
		return 1
	}
	return m.Run()
}
