package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"argos/server/internal/auth"
	"argos/server/internal/store"
)

// userDTO es la vista de un usuario pa el dashboard (sin hash ni contadores internos).
type userDTO struct {
	ID                 string `json:"id"`
	Username           string `json:"username"`
	Role               string `json:"role"`
	MustChangePassword bool   `json:"must_change_password"`
	// AllGroups: ve todos los hosts (admin o viewer sin restriccion); false = restringido a sus grupos.
	AllGroups    bool `json:"all_groups"`
	CanInventory bool `json:"can_inventory"`
	CanUpdates   bool `json:"can_updates"`
}

func toUserDTO(u *sessionUser) userDTO {
	return userDTO{ID: u.ID, Username: u.Username, Role: u.Role, MustChangePassword: u.MustChangePassword, AllGroups: u.Scope.All(), CanInventory: u.CanInventory, CanUpdates: u.CanUpdates}
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// login autentica con usuario y contraseña y abre una sesion nueva. Todo fallo de
// credenciales (usuario inexistente, contraseña mala, cuenta bloqueada o deshabilitada)
// responde exactamente igual y gasta el mismo costo de hash.
func (a *API) login(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	ip := a.clientIP(r)

	if ok, wait := a.loginLimiter.allow(ipKey(ip), a.now()); !ok {
		a.logger.WarnContext(ctx, "rate limit de login por IP", "ip", ip.String())
		return rateLimited(wait)
	}

	var req loginReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		return badRequest("usuario y contraseña requeridos")
	}
	if utf8.RuneCountInString(req.Username) > auth.MaxUsernameLen || utf8.RuneCountInString(req.Password) > auth.MaxPasswordLen {
		return badRequest("usuario o contraseña con largo invalido")
	}

	now := a.now()
	u, err := a.store.UserByUsername(ctx, req.Username)
	if errors.Is(err, store.ErrNotFound) {
		if err := a.hasher.VerifyDummy(ctx, req.Password); err != nil {
			return err
		}
		// No se audita el usuario escrito: no existe y podria ser una contraseña tipeada en el campo equivocado.
		a.audit(r, store.AuditEntry{Event: evLoginFail, Outcome: store.OutcomeFail}, map[string]any{"reason": "unknown_user"})
		return errInvalidCreds
	}
	if err != nil {
		return err
	}

	fail := actor(&sessionUser{ID: u.ID, Username: u.Username})
	fail.Event, fail.Outcome = evLoginFail, store.OutcomeFail

	if u.DisabledAt != nil || (u.LockedUntil != nil && u.LockedUntil.After(now)) {
		reason := "locked"
		if u.DisabledAt != nil {
			reason = "disabled"
		}
		if err := a.hasher.VerifyDummy(ctx, req.Password); err != nil {
			return err
		}
		a.audit(r, fail, map[string]any{"reason": reason})
		return errInvalidCreds
	}

	ok, rehash, err := a.hasher.Verify(ctx, req.Password, u.PasswordHash)
	if err != nil {
		return fmt.Errorf("verificando contraseña: %w", err)
	}
	if !ok {
		locked, err := a.store.RegisterLoginFailure(ctx, u.ID, now, maxFailedLogins, accountLockTime)
		if err != nil {
			return err
		}
		a.audit(r, fail, map[string]any{"reason": "bad_password", "locked": locked})
		return errInvalidCreds
	}

	if rehash {
		a.rehashPassword(r, u, req.Password)
	}
	if err := a.store.RegisterLoginSuccess(ctx, u.ID, now); err != nil {
		return err
	}

	// ID nuevo en cada login (nunca se reusa uno existente: sin session fixation).
	id, idHash, err := auth.NewSessionID()
	if err != nil {
		return err
	}
	if err := a.store.CreateSession(ctx, store.Session{
		IDHash: idHash, UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(sessionAbsoluteTimeout),
		IP: addrPtr(ip), UserAgent: truncRunes(r.UserAgent(), maxUserAgentLen),
	}); err != nil {
		return err
	}
	a.setSessionCookie(w, id)

	su := &sessionUser{ID: u.ID, Username: u.Username, Role: u.Role, MustChangePassword: u.MustChangePassword}
	e := actor(su)
	e.Event, e.Outcome = evLoginOK, store.OutcomeOK
	a.audit(r, e, nil)

	writeJSON(w, http.StatusOK, map[string]any{"user": toUserDTO(su)})
	return nil
}

// rehashPassword actualiza el hash tras un login correcto cuyo hash usa parametros
// viejos. Es best-effort: si falla el login sigue valido y solo se loguea.
func (a *API) rehashPassword(r *http.Request, u store.User, password string) {
	ctx := r.Context()
	newHash, err := a.hasher.Hash(ctx, password)
	if err == nil {
		err = a.store.UpdatePasswordHash(ctx, u.ID, u.PasswordHash, newHash)
	}
	if err != nil {
		a.logger.WarnContext(ctx, "no se pudo actualizar el hash de contraseña", "user_id", u.ID, "err", err)
	}
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) error {
	u := currentUser(r)
	if err := a.store.DeleteSession(r.Context(), u.sessionHash); err != nil {
		return err
	}
	a.clearSessionCookie(w)
	e := actor(u)
	e.Event, e.Outcome = evLogout, store.OutcomeOK
	a.audit(r, e, nil)
	writeJSON(w, http.StatusOK, map[string]any{})
	return nil
}

func (a *API) me(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserDTO(currentUser(r))})
	return nil
}

type changePasswordReq struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// changePassword cambia la contraseña del usuario de la sesion. Exige la actual
// (un fallo cuenta pa el bloqueo de cuenta, igual que en el login), aplica la politica a
// la nueva, revoca las demas sesiones del usuario y baja must_change_password.
func (a *API) changePassword(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	su := currentUser(r)

	var req changePasswordReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if req.CurrentPassword == "" || utf8.RuneCountInString(req.CurrentPassword) > auth.MaxPasswordLen {
		return badRequest("current_password requerido")
	}

	now := a.now()
	u, err := a.store.UserByID(ctx, su.ID)
	if err != nil {
		return err
	}
	fail := actor(su)
	fail.Event, fail.Outcome, fail.TargetType, fail.TargetID = evPasswordChanged, store.OutcomeFail, "user", su.ID

	if u.LockedUntil != nil && u.LockedUntil.After(now) {
		if err := a.hasher.VerifyDummy(ctx, req.CurrentPassword); err != nil {
			return err
		}
		a.audit(r, fail, map[string]any{"reason": "locked"})
		return errInvalidCreds
	}
	ok, _, err := a.hasher.Verify(ctx, req.CurrentPassword, u.PasswordHash)
	if err != nil {
		return fmt.Errorf("verificando contraseña actual: %w", err)
	}
	if !ok {
		locked, err := a.store.RegisterLoginFailure(ctx, u.ID, now, maxFailedLogins, accountLockTime)
		if err != nil {
			return err
		}
		a.audit(r, fail, map[string]any{"reason": "bad_password", "locked": locked})
		return errInvalidCreds
	}

	if err := auth.CheckPassword(req.NewPassword); err != nil {
		return err
	}
	if req.NewPassword == req.CurrentPassword {
		return &auth.WeakPasswordError{Reason: "la nueva contraseña debe ser distinta de la actual"}
	}
	newHash, err := a.hasher.Hash(ctx, req.NewPassword)
	if err != nil {
		return err
	}
	if err := a.store.ChangePassword(ctx, u.ID, newHash, su.sessionHash, now); err != nil {
		return err
	}

	e := actor(su)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evPasswordChanged, store.OutcomeOK, "user", su.ID
	a.audit(r, e, nil)

	su.MustChangePassword = false
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserDTO(su)})
	return nil
}
