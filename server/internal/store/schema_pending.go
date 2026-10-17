package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SchemaPending dice si hay migraciones sin aplicar. Con ARGOS_AUTO_MIGRATE=false el server no migra: lo usa
// para negarse a arrancar sobre un esquema atrasado (las migraciones las corre `argos-server migrate`).
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
