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
	// ErrInvalidScope: el cambio de permisos por grupo es incoherente (restringir a un admin, o dar group_ids
	// a quien ve todos los grupos).
	ErrInvalidScope = errors.New("permisos por grupo invalidos")
	// ErrSelfDelete: un admin no puede borrarse a si mismo.
	ErrSelfDelete = errors.New("un usuario no puede borrarse a si mismo")
)

// Constraint de user_groups.group_id (nombre autogenerado por Postgres).
const userGroupFKey = "user_groups_group_id_fkey"

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

	// Permisos por grupo (solo aplican a viewers; un admin siempre ve todo, ver Scope).
	AllGroups    bool
	GroupIDs     []string // grupos visibles si !AllGroups, ordenados
	CanInventory bool
	CanUpdates   bool // permiso de leer parches (ver Scope)
	CanAck       bool // permiso de reconocer alertas de su ambito
}

// Scope es el ambito de hosts del usuario: los admins y los viewers con all_groups ven todo.
func (u User) Scope() Scope {
	if u.Role == RoleAdmin || u.AllGroups {
		return AllScope()
	}
	return GroupScope(u.GroupIDs)
}

// userColumns se usa siempre sobre la tabla users sin alias (SELECT, INSERT y UPDATE ... RETURNING).
const userColumns = `id, username, password_hash, role, must_change_password, failed_logins,
	locked_until, disabled_at, last_login_at, created_at, all_groups,
	ARRAY(SELECT ug.group_id::text FROM user_groups ug WHERE ug.user_id = users.id ORDER BY ug.group_id),
	can_inventory, can_updates, can_ack`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.MustChangePassword, &u.FailedLogins,
		&u.LockedUntil, &u.DisabledAt, &u.LastLoginAt, &u.CreatedAt, &u.AllGroups, &u.GroupIDs, &u.CanInventory, &u.CanUpdates, &u.CanAck)
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

// NewUser son los datos de un alta. PasswordHash ya viene hasheado (Argon2id). Los permisos por grupo solo
// aplican a viewers; un admin siempre queda con ambito total y sin permisos sueltos.
type NewUser struct {
	Username     string
	PasswordHash string
	Role         string
	MustChange   bool
	AllGroups    bool
	GroupIDs     []string // ids ya validados y sin repetir; exige AllGroups=false
	CanInventory bool
	CanUpdates   bool
	CanAck       bool
}

// CreateUser inserta un usuario (y sus grupos) en una transaccion. Devuelve ErrUserExists si el username ya esta
// tomado, ErrGroupNotFound si algun grupo no existe e ErrInvalidScope si pide restricciones a un admin o group_ids
// con all_groups. Un viewer con AllGroups=false y sin grupos es valido: no ve ningun host.
func (s *Store) CreateUser(ctx context.Context, n NewUser) (User, error) {
	if n.Role == RoleAdmin {
		if !n.AllGroups && (n.CanInventory || n.CanUpdates || n.CanAck || len(n.GroupIDs) > 0) {
			return User{}, ErrInvalidScope
		}
		n.AllGroups, n.CanInventory, n.CanUpdates, n.CanAck = true, false, false, false
	}
	if n.AllGroups && len(n.GroupIDs) > 0 {
		return User{}, ErrInvalidScope
	}
	var out User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx,
			`INSERT INTO users (username, password_hash, role, must_change_password, all_groups, can_inventory, can_updates, can_ack)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			n.Username, n.PasswordHash, n.Role, n.MustChange, n.AllGroups, n.CanInventory, n.CanUpdates, n.CanAck).Scan(&id)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrUserExists
		}
		if err != nil {
			return fmt.Errorf("creando usuario: %w", err)
		}
		// Un usuario nuevo entra a todos sus grupos por primera vez: cuenta contra la cuota de cada uno.
		for _, gid := range n.GroupIDs {
			if err := checkGroupQuota(ctx, tx, gid, QuotaUsers, 1); err != nil {
				return err
			}
		}
		if err := replaceUserGroups(ctx, tx, id, n.GroupIDs); err != nil {
			return err
		}
		out, err = scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
		if err != nil {
			return fmt.Errorf("leyendo usuario creado: %w", err)
		}
		return nil
	})
	if err != nil {
		return User{}, err
	}
	return out, nil
}

// UserChange describe los cambios de un admin sobre otro usuario; nil = sin cambio.
type UserChange struct {
	Role     *string
	Disabled *bool
	// PasswordHash, si no es nil, resetea la contraseña: exige cambiarla al proximo login.
	PasswordHash *string

	// Permisos por grupo (solo viewers). GroupIDs reemplaza la lista completa (ids ya validados y sin repetir);
	// solo aplica con AllGroups=false. Promover a admin los reinicia (ve todo, sin inventario).
	AllGroups    *bool
	GroupIDs     *[]string
	CanInventory *bool
	CanUpdates   *bool
	CanAck       *bool
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
		allGroups, canInventory, canUpdates, canAck, clearGroups, err := resolveScopeChange(cur, newRole, ch)
		if err != nil {
			return err
		}
		if clearGroups {
			if _, err := tx.Exec(ctx, `DELETE FROM user_groups WHERE user_id = $1`, id); err != nil {
				return fmt.Errorf("limpiando grupos del usuario: %w", err)
			}
		} else if ch.GroupIDs != nil {
			// Solo los grupos NUEVOS de la lista cuentan contra la cuota (los que ya tenia no suman de nuevo).
			had := make(map[string]bool, len(cur.GroupIDs))
			for _, gid := range cur.GroupIDs {
				had[gid] = true
			}
			for _, gid := range *ch.GroupIDs {
				if !had[gid] {
					if err := checkGroupQuota(ctx, tx, gid, QuotaUsers, 1); err != nil {
						return err
					}
				}
			}
			if err := replaceUserGroups(ctx, tx, id, *ch.GroupIDs); err != nil {
				return err
			}
		}
		u, err := scanUser(tx.QueryRow(ctx,
			`UPDATE users SET role = $2, disabled_at = $3, all_groups = $4, can_inventory = $5, can_updates = $6, can_ack = $7
			  WHERE id = $1 RETURNING `+userColumns,
			id, newRole, disabledAt, allGroups, canInventory, canUpdates, canAck))
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

// resolveScopeChange calcula all_groups, can_inventory, can_updates y can_ack finales y si hay que vaciar user_groups.
// Un admin no admite restricciones (se rechazan si el cambio las pide; si solo se le promueve, se reinician) y
// group_ids solo tiene sentido con all_groups=false.
func resolveScopeChange(cur User, newRole string, ch UserChange) (allGroups, canInventory, canUpdates, canAck, clearGroups bool, err error) {
	allGroups, canInventory, canUpdates, canAck = cur.AllGroups, cur.CanInventory, cur.CanUpdates, cur.CanAck
	if ch.AllGroups != nil {
		allGroups = *ch.AllGroups
	}
	if ch.CanInventory != nil {
		canInventory = *ch.CanInventory
	}
	if ch.CanUpdates != nil {
		canUpdates = *ch.CanUpdates
	}
	if ch.CanAck != nil {
		canAck = *ch.CanAck
	}
	withGroups := ch.GroupIDs != nil && len(*ch.GroupIDs) > 0
	if newRole == RoleAdmin {
		if (ch.AllGroups != nil && !*ch.AllGroups) || (ch.CanInventory != nil && *ch.CanInventory) ||
			(ch.CanUpdates != nil && *ch.CanUpdates) || (ch.CanAck != nil && *ch.CanAck) || withGroups {
			return false, false, false, false, false, ErrInvalidScope
		}
		return true, false, false, false, true, nil
	}
	if allGroups {
		if withGroups {
			return false, false, false, false, false, ErrInvalidScope
		}
		return true, canInventory, canUpdates, canAck, true, nil
	}
	return false, canInventory, canUpdates, canAck, false, nil
}

// replaceUserGroups reemplaza los grupos del usuario. ErrGroupNotFound si alguno no existe.
func replaceUserGroups(ctx context.Context, tx pgx.Tx, userID string, groupIDs []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM user_groups WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("limpiando grupos del usuario: %w", err)
	}
	if len(groupIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO user_groups (user_id, group_id) SELECT $1, unnest($2::uuid[])`, userID, groupIDs)
	if isFKViolation(err, userGroupFKey) {
		return ErrGroupNotFound
	}
	if err != nil {
		return fmt.Errorf("asignando grupos al usuario: %w", err)
	}
	return nil
}

// DeleteUser borra un usuario en una transaccion: sus sesiones y pertenencias a grupos caen por ON DELETE CASCADE y
// las columnas created_by/acknowledged_by de otras tablas quedan en NULL (ON DELETE SET NULL; los nombres de quien
// reconocio o resolvio una alerta se conservan en sus columnas *_name). audit_log no tiene FK a users, asi que su
// trigger append-only no interviene y las filas conservan actor_user_id y actor_username. Devuelve el usuario
// borrado. ErrNotFound si no existe; ErrSelfDelete si id es el actor; ErrLastAdmin si dejaria al sistema sin admin.
func (s *Store) DeleteUser(ctx context.Context, id, actorID string) (User, error) {
	if id == actorID {
		return User{}, ErrSelfDelete
	}
	var out User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Mismo bloqueo que UpdateUser: dos bajas o demociones concurrentes no dejan el sistema sin admin.
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
		if cur.Role == RoleAdmin && cur.DisabledAt == nil {
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
		if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id); err != nil {
			return fmt.Errorf("borrando usuario: %w", err)
		}
		out = cur
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
