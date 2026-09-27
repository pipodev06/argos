package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"argos/server/internal/config"
	"argos/server/internal/notify"
	"argos/server/internal/store"
	"argos/server/internal/testdb"
)

// Este test corre contra una Postgres real: necesita ARGOS_TEST_DATABASE_URL apuntando a una BD cuyo nombre
// termine en _test (recrea el schema public). Comparte BD con los tests de store y httpapi: el lock advisory de
// testdb los serializa. Sin la variable se salta con aviso (igual que el resto del proyecto).
var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	dsn := os.Getenv("ARGOS_TEST_DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "AVISO: ARGOS_TEST_DATABASE_URL no definida, se saltan los tests de cmd/server")
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
	testPool = pool
	return m.Run()
}

func mustRotateStore(t *testing.T) *store.Store {
	t.Helper()
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	if err := testdb.ResetData(context.Background(), testPool); err != nil {
		t.Fatal(err)
	}
	return store.New(testPool)
}

// TestRunRotateSecretKeyRecifraYEsIdempotente cubre el comando de punta a punta: un canal cifrado con una clave
// vieja (en ARGOS_SECRET_KEY_PREVIOUS) queda recifrado con la actual, sigue siendo legible, y repetir el comando
// no lo vuelve a tocar.
func TestRunRotateSecretKeyRecifraYEsIdempotente(t *testing.T) {
	st := mustRotateStore(t)
	ctx := context.Background()

	oldKeyB64 := "MDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDA="
	newKeyB64 := "MTExMTExMTExMTExMTExMTExMTExMTExMTExMTExMTE="
	oldRing, err := notify.NewKeyRing(oldKeyB64, "")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte(`{"url":"https://hooks.example.com/x","hmac":"h"}`)
	channelID := "00000000-0000-4000-8000-100000000001"
	blob, err := oldRing.Seal(channelID, notify.KindWebhook, secret)
	if err != nil {
		t.Fatal(err)
	}
	oldKeyID := oldRing.KeyID()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO notification_channels (id, name, kind, config, secret_enc, secret_key_id, min_severity, enabled)
		 VALUES ($1, 'rotacion', 'webhook', '{}', $2, $3, 'warn', true)`, channelID, blob, oldKeyID); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{SecretKey: newKeyB64, SecretKeyPrevious: oldKeyB64}
	if err := runRotateSecretKey(ctx, st, cfg); err != nil {
		t.Fatalf("runRotateSecretKey: %v", err)
	}

	newRing, err := notify.NewKeyRing(newKeyB64, "")
	if err != nil {
		t.Fatal(err)
	}
	var enc []byte
	var keyID string
	if err := testPool.QueryRow(ctx, `SELECT secret_enc, secret_key_id FROM notification_channels WHERE id = $1`, channelID).
		Scan(&enc, &keyID); err != nil {
		t.Fatal(err)
	}
	if keyID != newRing.KeyID() {
		t.Fatalf("secret_key_id got %q, want %q", keyID, newRing.KeyID())
	}
	got, err := newRing.Open(channelID, keyID, notify.KindWebhook, enc)
	if err != nil || string(got) != string(secret) {
		t.Fatalf("el secreto recifrado no abre con la clave actual: got %q, %v", got, err)
	}

	// Repetirlo no cambia el blob (ya esta en la clave actual).
	if err := runRotateSecretKey(ctx, st, cfg); err != nil {
		t.Fatalf("repetir runRotateSecretKey: %v", err)
	}
	var enc2 []byte
	if err := testPool.QueryRow(ctx, `SELECT secret_enc FROM notification_channels WHERE id = $1`, channelID).Scan(&enc2); err != nil {
		t.Fatal(err)
	}
	if string(enc) != string(enc2) {
		t.Error("repetir el comando debio ser un no-op (el blob no cambia)")
	}

	// Sin ARGOS_SECRET_KEY el comando se niega.
	if err := runRotateSecretKey(ctx, st, config.Config{}); err == nil {
		t.Error("sin ARGOS_SECRET_KEY debio fallar")
	}
}
