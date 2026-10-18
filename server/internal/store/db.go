package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"runtime"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const pingTimeout = 5 * time.Second

// Connect abre un pool configurado y verifica conectividad con un Ping acotado.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parseando DATABASE_URL: %w", err)
	}
	// Tamano del pool: el de DATABASE_URL (?pool_max_conns=N) si viene; si no, 4 por nucleo entre 10 y 50.
	// Con 10 fijas, 5000 agentes reportando a la vez hacian fila por una conexion (medido 2026-10-18:
	// p95 de 6 s y tiempos agotados en el ingest).
	if !strings.Contains(databaseURL, "pool_max_conns") {
		cfg.MaxConns = int32(min(max(4*runtime.NumCPU(), 10), 50)) //nolint:gosec // acotado a 10..50
	}
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.HealthCheckPeriod = time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creando pool de postgres: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping a postgres: %w", err)
	}
	return pool, nil
}

func newProvider(pool *pgxpool.Pool) (*goose.Provider, error) {
	fsys, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("accediendo a migraciones embebidas: %w", err)
	}
	// El *sql.DB es un adaptador sobre el pool: cerrarlo no cierra el pool.
	provider, err := goose.NewProvider(goose.DialectPostgres, stdlib.OpenDBFromPool(pool), fsys)
	if err != nil {
		return nil, fmt.Errorf("creando provider de goose: %w", err)
	}
	return provider, nil
}

// Migrate aplica las migraciones pendientes (goose, embebidas en el binario).
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	provider, err := newProvider(pool)
	if err != nil {
		return err
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("aplicando migraciones: %w", err)
	}
	return nil
}
