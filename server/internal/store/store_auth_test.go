package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestCreateUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, NewUser{Username: "Alice", PasswordHash: "$argon2id$hash", Role: RoleAdmin, MustChange: true})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == "" || u.Username != "Alice" || u.Role != RoleAdmin || !u.MustChangePassword {
		t.Errorf("usuario creado inesperado: %+v", u)
	}
	if _, err := s.CreateUser(ctx, NewUser{Username: "alice", PasswordHash: "$argon2id$hash", Role: RoleViewer}); !errors.Is(err, ErrUserExists) {
		t.Errorf("username repetido (otra capitalizacion) err got %v, want ErrUserExists", err)
	}
	got, err := s.UserByUsername(ctx, "ALICE")
	if err != nil || got.ID != u.ID {
		t.Errorf("UserByUsername sin distinguir mayusculas got (%+v, %v)", got, err)
	}
	if _, err := s.UserByUsername(ctx, "nadie"); !errors.Is(err, ErrNotFound) {
		t.Errorf("usuario inexistente err got %v, want ErrNotFound", err)
	}
}

func TestLoginFailureLocksAfterMax(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := mkUser(t, s, "alice", RoleViewer)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

	for i := 1; i <= 9; i++ {
		locked, err := s.RegisterLoginFailure(ctx, id, now, 10, 15*time.Minute)
		if err != nil || locked {
			t.Fatalf("fallo %d: got (locked=%v, %v), want (false, nil)", i, locked, err)
		}
	}
	locked, err := s.RegisterLoginFailure(ctx, id, now, 10, 15*time.Minute)
	if err != nil || !locked {
		t.Fatalf("fallo 10: got (locked=%v, %v), want (true, nil)", locked, err)
	}
	u, err := s.UserByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if u.LockedUntil == nil || !u.LockedUntil.Equal(now.Add(15*time.Minute)) {
		t.Errorf("locked_until got %v, want %v", u.LockedUntil, now.Add(15*time.Minute))
	}
	if u.FailedLogins != 0 {
		t.Errorf("failed_logins tras bloquear got %d, want 0 (contador reiniciado)", u.FailedLogins)
	}

	if err := s.RegisterLoginSuccess(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	u, _ = s.UserByID(ctx, id)
	if u.LockedUntil != nil || u.LastLoginAt == nil {
		t.Errorf("el login exitoso debe limpiar el bloqueo y anotar last_login_at: %+v", u)
	}
}

func TestUpdateUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	admin1 := mkUser(t, s, "admin1", RoleAdmin)
	admin2 := mkUser(t, s, "admin2", RoleAdmin)
	viewer := mkUser(t, s, "viewer", RoleViewer)

	seedSession := func(userID string, seed byte) []byte {
		h := make([]byte, 32)
		h[0] = seed
		if err := s.CreateSession(ctx, Session{IDHash: h, UserID: userID, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		return h
	}

	t.Run("cambiar rol", func(t *testing.T) {
		role := RoleAdmin
		u, err := s.UpdateUser(ctx, viewer, UserChange{Role: &role}, now)
		if err != nil || u.Role != RoleAdmin {
			t.Fatalf("got (%+v, %v), want rol admin", u, err)
		}
		role = RoleViewer
		if _, err := s.UpdateUser(ctx, viewer, UserChange{Role: &role}, now); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("deshabilitar revoca sesiones", func(t *testing.T) {
		seedSession(viewer, 1)
		disabled := true
		u, err := s.UpdateUser(ctx, viewer, UserChange{Disabled: &disabled}, now)
		if err != nil || u.DisabledAt == nil {
			t.Fatalf("got (%+v, %v), want deshabilitado", u, err)
		}
		if got := count(t, `SELECT count(*) FROM sessions WHERE user_id = $1`, viewer); got != 0 {
			t.Errorf("sesiones tras deshabilitar got %d, want 0", got)
		}
		disabled = false
		if u, err = s.UpdateUser(ctx, viewer, UserChange{Disabled: &disabled}, now); err != nil || u.DisabledAt != nil {
			t.Fatalf("rehabilitar got (%+v, %v)", u, err)
		}
	})

	t.Run("reset de contraseña", func(t *testing.T) {
		seedSession(viewer, 2)
		mustExec(t, `UPDATE users SET failed_logins = 5, locked_until = now() + interval '1 hour' WHERE id = $1`, viewer)
		hash := "$argon2id$nuevo"
		u, err := s.UpdateUser(ctx, viewer, UserChange{PasswordHash: &hash}, now)
		if err != nil {
			t.Fatal(err)
		}
		if u.PasswordHash != hash || !u.MustChangePassword || u.LockedUntil != nil || u.FailedLogins != 0 {
			t.Errorf("reset no aplico: %+v", u)
		}
		if got := count(t, `SELECT count(*) FROM sessions WHERE user_id = $1`, viewer); got != 0 {
			t.Errorf("sesiones tras reset got %d, want 0", got)
		}
	})

	t.Run("usuario inexistente", func(t *testing.T) {
		role := RoleViewer
		_, err := s.UpdateUser(ctx, "00000000-0000-0000-0000-000000000000", UserChange{Role: &role}, now)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err got %v, want ErrNotFound", err)
		}
	})

	t.Run("no deja el sistema sin admin", func(t *testing.T) {
		viewerRole, disabled := RoleViewer, true
		if _, err := s.UpdateUser(ctx, admin2, UserChange{Role: &viewerRole}, now); err != nil {
			t.Fatalf("degradar con otro admin activo: %v", err)
		}
		if _, err := s.UpdateUser(ctx, admin1, UserChange{Role: &viewerRole}, now); !errors.Is(err, ErrLastAdmin) {
			t.Errorf("degradar al ultimo admin err got %v, want ErrLastAdmin", err)
		}
		if _, err := s.UpdateUser(ctx, admin1, UserChange{Disabled: &disabled}, now); !errors.Is(err, ErrLastAdmin) {
			t.Errorf("deshabilitar al ultimo admin err got %v, want ErrLastAdmin", err)
		}
	})
}

func TestUpdateUserConcurrentDemotionsKeepOneAdmin(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := mkUser(t, s, "admin1", RoleAdmin)
	b := mkUser(t, s, "admin2", RoleAdmin)

	viewer := RoleViewer
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, id := range []string{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.UpdateUser(ctx, id, UserChange{Role: &viewer}, time.Now())
		}()
	}
	wg.Wait()

	if got := count(t, `SELECT count(*) FROM users WHERE role = 'admin' AND disabled_at IS NULL`); got != 1 {
		t.Errorf("admins activos got %d, want 1 (errs: %v)", got, errs)
	}
	wins, blocked := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrLastAdmin):
			blocked++
		default:
			t.Errorf("error inesperado: %v", err)
		}
	}
	if wins != 1 || blocked != 1 {
		t.Errorf("degradaciones ok=%d bloqueadas=%d, want 1 y 1", wins, blocked)
	}
}

func TestChangePasswordKeepsOnlyCurrentSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := mkUser(t, s, "alice", RoleViewer)
	mustExec(t, `UPDATE users SET must_change_password = true WHERE id = $1`, id)
	now := time.Now()

	mk := func(seed byte) []byte {
		h := make([]byte, 32)
		h[0] = seed
		if err := s.CreateSession(ctx, Session{IDHash: h, UserID: id, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		return h
	}
	keep, other := mk(1), mk(2)

	if err := s.ChangePassword(ctx, id, "$argon2id$nueva", keep, now); err != nil {
		t.Fatal(err)
	}
	u, _ := s.UserByID(ctx, id)
	if u.PasswordHash != "$argon2id$nueva" || u.MustChangePassword {
		t.Errorf("contraseña/must_change no actualizados: %+v", u)
	}
	if _, err := s.SessionByHash(ctx, keep); err != nil {
		t.Errorf("la sesion actual debe sobrevivir: %v", err)
	}
	if _, err := s.SessionByHash(ctx, other); !errors.Is(err, ErrNotFound) {
		t.Errorf("las otras sesiones deben revocarse, err got %v", err)
	}
}

func TestResetPasswordByUsernameUnlocksAndRevokes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := mkUser(t, s, "alice", RoleAdmin)
	mustExec(t, `UPDATE users SET failed_logins = 3, locked_until = now() + interval '1 hour' WHERE id = $1`, id)
	now := time.Now()
	h := make([]byte, 32)
	if err := s.CreateSession(ctx, Session{IDHash: h, UserID: id, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	u, err := s.ResetPasswordByUsername(ctx, "ALICE", "$argon2id$nueva", now)
	if err != nil {
		t.Fatal(err)
	}
	if u.LockedUntil != nil || u.FailedLogins != 0 || u.PasswordHash != "$argon2id$nueva" {
		t.Errorf("reset por CLI no desbloqueo: %+v", u)
	}
	if got := count(t, `SELECT count(*) FROM sessions`); got != 0 {
		t.Errorf("sesiones tras reset got %d, want 0", got)
	}
	if _, err := s.ResetPasswordByUsername(ctx, "nadie", "$argon2id$x", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("usuario inexistente err got %v, want ErrNotFound", err)
	}
}

func TestSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := mkUser(t, s, "alice", RoleViewer)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	hash := make([]byte, 32)
	hash[0] = 9

	if _, err := s.SessionByHash(ctx, hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sesion inexistente err got %v, want ErrNotFound", err)
	}
	if err := s.CreateSession(ctx, Session{
		IDHash: hash, UserID: id, CreatedAt: now, ExpiresAt: now.Add(8 * time.Hour), UserAgent: "curl/8",
	}); err != nil {
		t.Fatal(err)
	}
	si, err := s.SessionByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if si.User.ID != id || si.User.Username != "alice" || !si.LastSeenAt.Equal(now) || !si.ExpiresAt.Equal(now.Add(8*time.Hour)) {
		t.Errorf("sesion leida inesperada: %+v", si)
	}
	later := now.Add(5 * time.Minute)
	if err := s.TouchSession(ctx, hash, later); err != nil {
		t.Fatal(err)
	}
	if si, _ = s.SessionByHash(ctx, hash); !si.LastSeenAt.Equal(later) {
		t.Errorf("last_seen_at got %v, want %v", si.LastSeenAt, later)
	}
	if err := s.DeleteSession(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, hash); err != nil {
		t.Errorf("borrar una sesion inexistente no es error: %v", err)
	}
	if _, err := s.SessionByHash(ctx, hash); !errors.Is(err, ErrNotFound) {
		t.Errorf("tras borrar err got %v, want ErrNotFound", err)
	}
}

func TestSessionsSchemaRejectsBadRows(t *testing.T) {
	s := newTestStore(t)
	id := mkUser(t, s, "alice", RoleViewer)
	now := time.Now()
	tests := []struct {
		name string
		sess Session
	}{
		{"id_hash de largo incorrecto", Session{IDHash: []byte("corto"), UserID: id, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}},
		{"expira antes de crearse", Session{IDHash: make([]byte, 32), UserID: id, CreatedAt: now, ExpiresAt: now.Add(-time.Hour)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pgErr *pgconn.PgError
			if err := s.CreateSession(context.Background(), tt.sess); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Errorf("err got %v, want SQLSTATE 23514", err)
			}
		})
	}
}

func TestAuditInsertAndAppendOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := mkUser(t, s, "alice", RoleAdmin)

	if err := s.InsertAudit(ctx, AuditEntry{Event: "login_fail", Outcome: OutcomeFail, Detail: []byte(`{"reason":"unknown_user"}`)}); err != nil {
		t.Fatalf("evento sin actor: %v", err)
	}
	if err := s.InsertAudit(ctx, AuditEntry{
		ActorUserID: id, ActorUsername: "alice", Event: "login_ok", Outcome: OutcomeOK, TargetType: "user", TargetID: id,
	}); err != nil {
		t.Fatalf("evento con actor: %v", err)
	}
	if got := count(t, `SELECT count(*) FROM audit_log WHERE actor_user_id IS NULL AND detail->>'reason' = 'unknown_user'`); got != 1 {
		t.Errorf("evento sin actor no se guardo con actor NULL y detalle")
	}
	if err := s.InsertAudit(ctx, AuditEntry{Event: "x", Outcome: "raro"}); err == nil {
		t.Error("un outcome fuera del vocabulario debe rechazarse")
	}

	// Append-only: no UPDATE, y no DELETE de filas recientes.
	if _, err := testPool.Exec(ctx, `UPDATE audit_log SET event = 'otro'`); err == nil {
		t.Error("UPDATE sobre audit_log debe fallar")
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM audit_log`); err == nil {
		t.Error("DELETE de filas recientes sobre audit_log debe fallar")
	}
}

func TestPurge(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := mkUser(t, s, "alice", RoleViewer)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

	mkSession := func(seed byte, created, lastSeen, expires time.Time) {
		h := make([]byte, 32)
		h[0] = seed
		if err := s.CreateSession(ctx, Session{IDHash: h, UserID: id, CreatedAt: created, ExpiresAt: expires}); err != nil {
			t.Fatal(err)
		}
		mustExec(t, `UPDATE sessions SET last_seen_at = $2 WHERE id_hash = $1`, h, lastSeen)
	}
	mkSession(1, now.Add(-time.Hour), now.Add(-time.Minute), now.Add(time.Hour))     // viva
	mkSession(2, now.Add(-10*time.Hour), now.Add(-time.Minute), now.Add(-time.Hour)) // limite absoluto vencido
	mkSession(3, now.Add(-2*time.Hour), now.Add(-time.Hour), now.Add(time.Hour))     // inactiva > 30 min
	n, err := s.PurgeSessions(ctx, now, 30*time.Minute, 1000)
	if err != nil || n != 2 {
		t.Errorf("PurgeSessions got (%d, %v), want (2, nil)", n, err)
	}
	if got := count(t, `SELECT count(*) FROM sessions`); got != 1 {
		t.Errorf("sesiones restantes got %d, want 1", got)
	}

	// El lote acota lo que se borra por llamada.
	for i := range 5 {
		mustExec(t, `INSERT INTO enroll_tokens (token_hash, hostname, expires_at) VALUES ($1, 'h', $2)`,
			th(fmt.Sprint("viejo", i)), now.Add(-8*24*time.Hour))
	}
	mustExec(t, `INSERT INTO enroll_tokens (token_hash, hostname, expires_at) VALUES ($1, 'h', $2)`, th("reciente"), now.Add(-24*time.Hour))
	if n, err = s.PurgeEnrollTokens(ctx, now.Add(-7*24*time.Hour), 3); err != nil || n != 3 {
		t.Errorf("PurgeEnrollTokens lote got (%d, %v), want (3, nil)", n, err)
	}
	if n, err = s.PurgeEnrollTokens(ctx, now.Add(-7*24*time.Hour), 3); err != nil || n != 2 {
		t.Errorf("PurgeEnrollTokens resto got (%d, %v), want (2, nil)", n, err)
	}
	if got := count(t, `SELECT count(*) FROM enroll_tokens`); got != 1 {
		t.Errorf("enroll tokens restantes got %d, want 1 (el reciente)", got)
	}

	// audit_log: solo se purga lo de mas de 30 dias (trigger); lo mas nuevo queda.
	mustExec(t, `INSERT INTO audit_log (ts, event, outcome) VALUES (now() - interval '200 days', 'viejo', 'ok'), (now(), 'nuevo', 'ok')`)
	if n, err = s.PurgeAudit(ctx, time.Now().Add(-180*24*time.Hour), 1000); err != nil || n != 1 {
		t.Errorf("PurgeAudit got (%d, %v), want (1, nil)", n, err)
	}
	if got := count(t, `SELECT count(*) FROM audit_log WHERE event = 'nuevo'`); got != 1 {
		t.Errorf("la purga borro un evento reciente")
	}
}

func TestEnrollTokenCapAndCreator(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "alice", RoleAdmin)
	future := time.Now().Add(time.Hour)

	// Concurrentes: el tope de 50 activos es exacto aun con carreras.
	const attempts = MaxActiveEnrollTokens + 20
	errs := make([]error, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = s.CreateEnrollToken(ctx, th(fmt.Sprint("t", i)), "srv", admin, nil, future, nil)
		}()
	}
	wg.Wait()

	created, capped := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			created++
		case errors.Is(err, ErrTooManyTokens):
			capped++
		default:
			t.Errorf("error inesperado: %v", err)
		}
	}
	if created != MaxActiveEnrollTokens || capped != attempts-MaxActiveEnrollTokens {
		t.Errorf("creados=%d rechazados=%d, want %d y %d", created, capped, MaxActiveEnrollTokens, attempts-MaxActiveEnrollTokens)
	}
	if got := count(t, `SELECT count(*) FROM enroll_tokens WHERE created_by = $1`, admin); got != MaxActiveEnrollTokens {
		t.Errorf("tokens ligados al admin got %d, want %d", got, MaxActiveEnrollTokens)
	}

	// Los usados y los vencidos no cuentan como activos.
	mustExec(t, `UPDATE enroll_tokens SET used_at = now() WHERE token_hash = (SELECT token_hash FROM enroll_tokens LIMIT 1)`)
	if err := s.CreateEnrollToken(ctx, th("extra"), "srv", admin, nil, future, nil); err != nil {
		t.Errorf("con un token usado debe caber uno mas: %v", err)
	}
}

func TestNoPlaintextEnrollTokenInDB(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "alice", RoleAdmin)
	const plain = "token-que-jamas-debe-quedar-en-claro"
	if err := s.CreateEnrollToken(ctx, th(plain), "srv", admin, nil, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	// Busca el valor en claro en todas las columnas de texto de enroll_tokens.
	if got := count(t, `SELECT count(*) FROM enroll_tokens e WHERE e::text LIKE '%' || $1 || '%'`, plain); got != 0 {
		t.Errorf("el enroll token aparece en claro en la BD")
	}
}

func TestRevokeHost(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hostID := enrolled(t, s, "h1")

	if got, err := s.HostIDByTokenHash(ctx, "h1"); err != nil || got != hostID {
		t.Fatalf("antes de revocar got (%q, %v)", got, err)
	}
	if err := s.RevokeHost(ctx, hostID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HostIDByTokenHash(ctx, "h1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("token revocado err got %v, want ErrNotFound", err)
	}
	var first time.Time
	if err := testPool.QueryRow(ctx, `SELECT agent_token_revoked_at FROM hosts WHERE id = $1`, hostID).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeHost(ctx, hostID); err != nil {
		t.Errorf("revocar dos veces debe ser idempotente: %v", err)
	}
	var second time.Time
	if err := testPool.QueryRow(ctx, `SELECT agent_token_revoked_at FROM hosts WHERE id = $1`, hostID).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if !first.Equal(second) {
		t.Errorf("la segunda revocacion cambio la fecha: %v -> %v", first, second)
	}
	if err := s.RevokeHost(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("host inexistente err got %v, want ErrNotFound", err)
	}
}

func TestDisabledHostIsNotAuthenticatedNorReactivated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hostID := enrolled(t, s, "h1")
	// disabled sin revocar (p. ej. deshabilitado a mano): tampoco autentica.
	mustExec(t, `UPDATE hosts SET status = 'disabled' WHERE id = $1`, hostID)

	if _, err := s.HostIDByTokenHash(ctx, "h1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("host disabled err got %v, want ErrNotFound", err)
	}
	// Aunque un ingest en curso llegue al UPDATE, no reactiva el host.
	res, err := s.IngestMetrics(ctx, hostID, Sample{Timestamp: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Recovered {
		t.Errorf("un host disabled no se recupera")
	}
	if got := count(t, `SELECT count(*) FROM hosts WHERE id = $1 AND status = 'disabled'`, hostID); got != 1 {
		t.Errorf("un ingest reactivo un host disabled")
	}
}
