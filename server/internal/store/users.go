package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Roles de usuario. Deben coincidir con el CHECK de la migracion 00004.
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

var (
	// ErrUserExists: el username ya esta en uso (sin distinguir mayusculas).
	ErrUserExists = errors.New("usuario ya existe")
	// ErrLastAdmin: la operacion dejaria al sistema sin ningun admin activo.
	ErrLastAdmin = errors.New("no se puede dejar el sistema sin admin activo")
)

// User es un usuario del dashboard. PasswordHash nunca sale de la capa HTTP.
type User struct {
	ID                 string
	Username           string
	PasswordHash       string
	Role               string
	MustChangePassword bool
	FailedLogins       int
	LockedUntil        *time.Time
	DisabledAt         *time.Time
	LastLoginAt        *time.Time
	CreatedAt          time.Time
}

const userColumns = `id, username, password_hash, role, must_change_password, failed_logins,
	locked_until, disabled_at, last_login_at, created_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.MustChangePassword, &u.FailedLogins,
		&u.LockedUntil, &u.DisabledAt, &u.LastLoginAt, &u.CreatedAt)
	return u, err
}

// UserByUsername busca por username sin distinguir mayusculas. ErrNotFound si no existe.
func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE lower(username) = lower($1)`, username))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("buscando usuario por nombre: %w", err)
	}
	return u, nil
}

// UserByID busca por id. ErrNotFound si no existe.
func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("buscando usuario por id: %w", err)
	}
	return u, nil
}

// ListUsers devuelve hasta limit usuarios, por nombre.
func (s *Store) ListUsers(ctx context.Context, limit int) ([]User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY lower(username) LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("listando usuarios: %w", err)
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("leyendo usuario: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando usuarios: %w", err)
	}
	return users, nil
}

// CreateUser inserta un usuario. passwordHash ya viene hasheado (Argon2id).
// Devuelve ErrUserExists si el username ya esta tomado.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash, role string, mustChange bool) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash, role, must_change_password)
		 VALUES ($1, $2, $3, $4) RETURNING `+userColumns,
		username, passwordHash, role, mustChange))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, ErrUserExists
	}
	if err != nil {
		return User{}, fmt.Errorf("creando usuario: %w", err)
	}
	return u, nil
}

// UserChange describe los cambios de un admin sobre otro usuario; nil = sin cambio.
type UserChange struct {
	Role     *string
	Disabled *bool
	// PasswordHash, si no es nil, resetea la contraseña: exige cambiarla al proximo login.
	PasswordHash *string
}

// UpdateUser aplica los cambios en una transaccion. Deshabilitar o resetear la
// contraseña revoca todas las sesiones del usuario; un reset ademas lo desbloquea.
// Devuelve ErrNotFound o ErrLastAdmin (si dejaria el sistema sin admin activo).
func (s *Store) UpdateUser(ctx context.Context, id string, ch UserChange, now time.Time) (User, error) {
	var out User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Bloquea todos los admins activos (orden estable: sin deadlock) mas el objetivo:
		// dos demociones concurrentes no pueden dejar el sistema sin admin.
		if err := lockActiveAdmins(ctx, tx); err != nil {
			return err
		}
		cur, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1 FOR UPDATE`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("leyendo usuario: %w", err)
		}

		newRole := cur.Role
		if ch.Role != nil {
			newRole = *ch.Role
		}
		newDisabled := cur.DisabledAt != nil
		if ch.Disabled != nil {
			newDisabled = *ch.Disabled
		}
		wasActiveAdmin := cur.Role == RoleAdmin && cur.DisabledAt == nil
		if wasActiveAdmin && (newRole != RoleAdmin || newDisabled) {
			var others int
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM users WHERE role = 'admin' AND disabled_at IS NULL AND id <> $1`, id,
			).Scan(&others); err != nil {
				return fmt.Errorf("contando admins: %w", err)
			}
			if others == 0 {
				return ErrLastAdmin
			}
		}

		var disabledAt *time.Time
		if newDisabled {
			disabledAt = cur.DisabledAt
			if disabledAt == nil {
				disabledAt = &now
			}
		}
		u, err := scanUser(tx.QueryRow(ctx,
			`UPDATE users SET role = $2, disabled_at = $3 WHERE id = $1 RETURNING `+userColumns,
			id, newRole, disabledAt))
		if err != nil {
			return fmt.Errorf("actualizando usuario: %w", err)
		}

		if ch.PasswordHash != nil {
			u, err = scanUser(tx.QueryRow(ctx,
				`UPDATE users SET password_hash = $2, must_change_password = true, password_changed_at = $3,
				        failed_logins = 0, locked_until = NULL
				  WHERE id = $1 RETURNING `+userColumns,
				id, *ch.PasswordHash, now))
			if err != nil {
				return fmt.Errorf("reseteando contraseña: %w", err)
			}
		}
		if ch.PasswordHash != nil || newDisabled {
			if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
				return fmt.Errorf("revocando sesiones: %w", err)
			}
		}
		out = u
		return nil
	})
	if err != nil {
		return User{}, err
	}
	return out, nil
}

func lockActiveAdmins(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT id FROM users WHERE role = 'admin' AND disabled_at IS NULL ORDER BY id FOR UPDATE`)
	if err != nil {
		return fmt.Errorf("bloqueando admins: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		// Solo interesa tomar los locks de fila: se recorre todo pa que el server los tome.
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("bloqueando admins: %w", err)
	}
	return nil
}

// RegisterLoginFailure cuenta un fallo de login (o de verificacion de contraseña) y,
// al llegar a maxFails consecutivos, bloquea la cuenta lockFor y reinicia el contador.
// Es una sola sentencia atomica. Devuelve si la cuenta quedo bloqueada.
func (s *Store) RegisterLoginFailure(ctx context.Context, userID string, now time.Time, maxFails int, lockFor time.Duration) (locked bool, err error) {
	err = s.pool.QueryRow(ctx,
		`UPDATE users SET
		   failed_logins = CASE WHEN failed_logins + 1 >= $2 THEN 0 ELSE failed_logins + 1 END,
		   locked_until  = CASE WHEN failed_logins + 1 >= $2 THEN $3::timestamptz ELSE locked_until END
		 WHERE id = $1
		 RETURNING failed_logins = 0`,
		userID, maxFails, now.Add(lockFor)).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // el usuario se borro entre medio: nada que contar
	}
	if err != nil {
		return false, fmt.Errorf("registrando fallo de login: %w", err)
	}
	return locked, nil
}

// RegisterLoginSuccess limpia el contador de fallos y el bloqueo, y anota el ultimo login.
func (s *Store) RegisterLoginSuccess(ctx context.Context, userID string, now time.Time) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE users SET failed_logins = 0, locked_until = NULL, last_login_at = $2 WHERE id = $1`,
		userID, now); err != nil {
		return fmt.Errorf("registrando login exitoso: %w", err)
	}
	return nil
}

// UpdatePasswordHash reemplaza el hash (rehash por cambio de parametros) solo si
// sigue siendo oldHash: si la contraseña cambio entre medio, no se pisa.
func (s *Store) UpdatePasswordHash(ctx context.Context, userID, oldHash, newHash string) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash = $3 WHERE id = $1 AND password_hash = $2`,
		userID, oldHash, newHash); err != nil {
		return fmt.Errorf("actualizando hash de contraseña: %w", err)
	}
	return nil
}

// ChangePassword fija la nueva contraseña, baja must_change_password y revoca todas
// las sesiones del usuario salvo keepSession (la que hizo el cambio), en una transaccion.
func (s *Store) ChangePassword(ctx context.Context, userID, newHash string, keepSession []byte, now time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE users SET password_hash = $2, must_change_password = false, password_changed_at = $3,
			        failed_logins = 0, locked_until = NULL
			  WHERE id = $1`, userID, newHash, now)
		if err != nil {
			return fmt.Errorf("actualizando contraseña: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM sessions WHERE user_id = $1 AND id_hash <> $2`, userID, keepSession); err != nil {
			return fmt.Errorf("revocando otras sesiones: %w", err)
		}
		return nil
	})
}

// ResetPasswordByUsername es la recuperacion por CLI (reset-password): fija la
// contraseña, desbloquea la cuenta y revoca todas sus sesiones. ErrNotFound si no existe.
func (s *Store) ResetPasswordByUsername(ctx context.Context, username, newHash string, now time.Time) (User, error) {
	var out User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		u, err := scanUser(tx.QueryRow(ctx,
			`UPDATE users SET password_hash = $2, must_change_password = false, password_changed_at = $3,
			        failed_logins = 0, locked_until = NULL
			  WHERE lower(username) = lower($1) RETURNING `+userColumns, username, newHash, now))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("reseteando contraseña: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, u.ID); err != nil {
			return fmt.Errorf("revocando sesiones: %w", err)
		}
		out = u
		return nil
	})
	if err != nil {
		return User{}, err
	}
	return out, nil
}
