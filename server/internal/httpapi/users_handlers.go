package httpapi

import (
	"net/http"
	"time"

	"argos/server/internal/auth"
	"argos/server/internal/store"
)

const listUsersLimit = 1000

// adminUserDTO es la vista de un usuario pa la gestion de usuarios (solo admin).
type adminUserDTO struct {
	ID                 string     `json:"id"`
	Username           string     `json:"username"`
	Role               string     `json:"role"`
	MustChangePassword bool       `json:"must_change_password"`
	Disabled           bool       `json:"disabled"`
	LastLoginAt        *time.Time `json:"last_login_at"`
	CreatedAt          time.Time  `json:"created_at"`
}

func toAdminUserDTO(u store.User) adminUserDTO {
	return adminUserDTO{
		ID: u.ID, Username: u.Username, Role: u.Role, MustChangePassword: u.MustChangePassword,
		Disabled: u.DisabledAt != nil, LastLoginAt: u.LastLoginAt, CreatedAt: u.CreatedAt,
	}
}

func validRole(role string) bool { return role == store.RoleAdmin || role == store.RoleViewer }

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) error {
	users, err := a.store.ListUsers(r.Context(), listUsersLimit)
	if err != nil {
		return err
	}
	items := make([]adminUserDTO, 0, len(users))
	for _, u := range users {
		items = append(items, toAdminUserDTO(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type createUserReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// createUser da de alta un usuario con una contraseña inicial que debe cambiar en su primer login.
func (a *API) createUser(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	admin := currentUser(r)

	var req createUserReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if err := auth.ValidateUsername(req.Username); err != nil {
		return badRequest("%s", err)
	}
	if !validRole(req.Role) {
		return badRequest("role debe ser admin o viewer")
	}
	if err := auth.CheckPassword(req.Password); err != nil {
		return err
	}
	hash, err := a.hasher.Hash(ctx, req.Password)
	if err != nil {
		return err
	}
	u, err := a.store.CreateUser(ctx, req.Username, hash, req.Role, true)
	if err != nil {
		return err
	}

	e := actor(admin)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evUserCreated, store.OutcomeOK, "user", u.ID
	a.audit(r, e, map[string]any{"role": u.Role})

	writeJSON(w, http.StatusCreated, map[string]any{"user": toAdminUserDTO(u)})
	return nil
}

type updateUserReq struct {
	Role        *string `json:"role"`
	Disabled    *bool   `json:"disabled"`
	NewPassword *string `json:"new_password"`
}

// updateUser cambia rol, habilita/deshabilita o resetea la contraseña de otro usuario.
// Deshabilitar o resetear revoca sus sesiones; el reset le exige cambiar la contraseña al entrar.
func (a *API) updateUser(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	admin := currentUser(r)

	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	var req updateUserReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if req.Role == nil && req.Disabled == nil && req.NewPassword == nil {
		return badRequest("nada que cambiar: envia role, disabled o new_password")
	}
	if req.Role != nil && !validRole(*req.Role) {
		return badRequest("role debe ser admin o viewer")
	}
	change := store.UserChange{Role: req.Role, Disabled: req.Disabled}
	if req.NewPassword != nil {
		if id == admin.ID {
			return badRequest("tu propia contraseña se cambia en /auth/password")
		}
		if err := auth.CheckPassword(*req.NewPassword); err != nil {
			return err
		}
		hash, err := a.hasher.Hash(ctx, *req.NewPassword)
		if err != nil {
			return err
		}
		change.PasswordHash = &hash
	}

	before, err := a.store.UserByID(ctx, id)
	if err != nil {
		return err
	}
	u, err := a.store.UpdateUser(ctx, id, change, a.now())
	if err != nil {
		return err
	}

	base := actor(admin)
	base.Outcome, base.TargetType, base.TargetID = store.OutcomeOK, "user", u.ID
	emit := func(event string, detail map[string]any) {
		e := base
		e.Event = event
		a.audit(r, e, detail)
	}
	if req.Role != nil && *req.Role != before.Role {
		emit(evUserRoleChanged, map[string]any{"role": u.Role})
	}
	if req.Disabled != nil && (before.DisabledAt != nil) != (u.DisabledAt != nil) {
		if u.DisabledAt != nil {
			emit(evUserDisabled, nil)
		} else {
			emit(evUserEnabled, nil)
		}
	}
	if req.NewPassword != nil {
		emit(evUserPasswordReset, nil)
	}

	writeJSON(w, http.StatusOK, map[string]any{"user": toAdminUserDTO(u)})
	return nil
}

// isUUID valida el formato 8-4-4-4-12 en hex: un id mal formado llegaria a Postgres
// como error de cast (500) en vez de un 400.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHexDigit(c) {
				return false
			}
		}
	}
	return true
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
