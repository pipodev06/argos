package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"argos/server/internal/store"
)

// Perfiles SNMP: credenciales reutilizables que el admin asigna a dispositivos. Todo el CRUD es solo admin (incluida
// la lectura) y ninguna respuesta, log ni entrada de auditoria lleva contrasenas ni comunidad.
const (
	maxProfileNameLen   = 80
	maxProfileUserLen   = 64
	minProfileAuthPass  = 8
	maxProfileAuthPass  = 128
	maxProfileCommunity = 64
	// kindSNMPProfile es el AAD (tipo) con el que se cifra el secreto de un perfil.
	kindSNMPProfile = "snmp_profile"
	levelAuthPriv   = "authPriv"
)

var (
	profileUserRe = regexp.MustCompile(`^[A-Za-z0-9._@:-]{1,64}$`)
	// Solo protocolos fuertes: MD5, SHA-1 y DES se rechazan.
	validAuthProtos = map[string]bool{"SHA256": true, "SHA512": true}
	validPrivProtos = map[string]bool{"AES128": true, "AES256": true}
)

// profileSecret es el contenido del blob cifrado: contrasenas v3 o comunidad v2c.
type profileSecret struct {
	AuthPass  string `json:"auth_pass,omitempty"`
	PrivPass  string `json:"priv_pass,omitempty"`
	Community string `json:"community,omitempty"`
}

func (a *API) sealProfileSecret(profileID string, sec profileSecret) ([]byte, string, error) {
	raw, err := json.Marshal(sec)
	if err != nil {
		return nil, "", fmt.Errorf("serializando secreto del perfil: %w", err)
	}
	blob, err := a.opts.Secrets.Seal(profileID, kindSNMPProfile, raw)
	if err != nil {
		return nil, "", fmt.Errorf("cifrando secreto del perfil: %w", err)
	}
	return blob, a.opts.Secrets.KeyID(), nil
}

// openProfileSecret descifra el secreto de un perfil (para editarlo o entregarlo al colector).
func (a *API) openProfileSecret(p store.SNMPProfile) (profileSecret, error) {
	if a.opts.Secrets == nil {
		return profileSecret{}, errors.New("no se puede abrir el secreto del perfil: falta ARGOS_SECRET_KEY")
	}
	raw, err := a.opts.Secrets.Open(p.ID, p.SecretKeyID, kindSNMPProfile, p.SecretEnc)
	if err != nil {
		return profileSecret{}, fmt.Errorf("abriendo secreto del perfil %s: %w", p.ID, err)
	}
	var sec profileSecret
	if err := json.Unmarshal(raw, &sec); err != nil {
		return profileSecret{}, fmt.Errorf("leyendo secreto del perfil %s: %w", p.ID, err)
	}
	return sec, nil
}

// --- DTO ---

// snmpProfileDTO es un perfil. Nunca lleva contrasenas ni comunidad: solo has_secret (v3) o has_community (v2c).
type snmpProfileDTO struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Version      int       `json:"version"`
	AllowV2c     bool      `json:"allow_v2c"`
	Username     *string   `json:"username"`
	Level        *string   `json:"level"`
	AuthProto    *string   `json:"auth_proto"`
	PrivProto    *string   `json:"priv_proto"`
	HasSecret    bool      `json:"has_secret"`
	HasCommunity bool      `json:"has_community"`
	DeviceCount  int       `json:"device_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func toSNMPProfileDTO(p store.SNMPProfile) snmpProfileDTO {
	dto := snmpProfileDTO{
		ID: p.ID, Name: p.Name, Version: p.Version, AllowV2c: p.Version == store.SNMPv2c, Username: p.Username,
		AuthProto: p.AuthProto, PrivProto: p.PrivProto, DeviceCount: p.DeviceCount,
		CreatedAt: p.CreatedAt.UTC(), UpdatedAt: p.UpdatedAt.UTC(),
	}
	if p.Version == store.SNMPv3 {
		lvl := levelAuthPriv
		dto.Level, dto.HasSecret = &lvl, true
	} else {
		dto.HasCommunity = true
	}
	return dto
}

// --- validacion ---

func checkProfileSecret(field, s string, minLen, maxLen int) error {
	if len(s) < minLen || len(s) > maxLen || !utf8.ValidString(s) || hasControl(s) {
		return badRequest("%s debe tener entre %d y %d caracteres sin caracteres de control", field, minLen, maxLen)
	}
	return nil
}

func checkProfileUser(s string) error {
	if !profileUserRe.MatchString(s) {
		return badRequest("username debe tener 1 a %d caracteres A-Z a-z 0-9 . _ @ : -", maxProfileUserLen)
	}
	return nil
}

func checkProfileProto(field, s string, allowed map[string]bool, list string) error {
	if !allowed[s] {
		return badRequest("%s debe ser %s (MD5, SHA-1 y DES no se admiten)", field, list)
	}
	return nil
}

// --- lecturas ---

func (a *API) listSNMPProfiles(w http.ResponseWriter, r *http.Request) error {
	ps, err := a.store.ListSNMPProfiles(r.Context())
	if err != nil {
		return err
	}
	items := make([]snmpProfileDTO, 0, len(ps))
	for _, p := range ps {
		items = append(items, toSNMPProfileDTO(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

func (a *API) getSNMPProfile(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return store.ErrNotFound
	}
	p, err := a.store.SNMPProfileByID(r.Context(), strings.ToLower(id))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": toSNMPProfileDTO(p)})
	return nil
}

// --- escrituras ---

type createSNMPProfileReq struct {
	Name      string  `json:"name"`
	Version   *int    `json:"version"`
	AllowV2c  *bool   `json:"allow_v2c"`
	Username  *string `json:"username"`
	AuthProto *string `json:"auth_proto"`
	AuthPass  *string `json:"auth_pass"`
	PrivProto *string `json:"priv_proto"`
	PrivPass  *string `json:"priv_pass"`
	Community *string `json:"community"`
}

func (a *API) createSNMPProfile(w http.ResponseWriter, r *http.Request) error {
	var req createSNMPProfileReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	name, err := checkName("name", req.Name, maxProfileNameLen)
	if err != nil {
		return err
	}
	if req.Version == nil || (*req.Version != store.SNMPv3 && *req.Version != store.SNMPv2c) {
		return badRequest("version debe ser 3 (authPriv) o 2 (v2c, solo con allow_v2c: true)")
	}
	id, err := newUUID()
	if err != nil {
		return err
	}
	p := store.SNMPProfile{ID: id, Name: name, Version: *req.Version}
	var sec profileSecret
	if p.Version == store.SNMPv3 {
		if sec, err = validateV3Profile(&p, req); err != nil {
			return err
		}
	} else if sec, err = validateV2Profile(req); err != nil {
		return err
	}
	if err := a.secretsReady(); err != nil {
		return err
	}
	if p.SecretEnc, p.SecretKeyID, err = a.sealProfileSecret(id, sec); err != nil {
		return err
	}
	u := currentUser(r)
	created, err := a.store.CreateSNMPProfile(r.Context(), p, u.ID)
	if err != nil {
		return err
	}
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evSNMPProfileCreated, store.OutcomeOK, "snmp_profile", created.ID
	a.audit(r, e, map[string]any{"name": created.Name, "version": created.Version})
	writeJSON(w, http.StatusCreated, map[string]any{"profile": toSNMPProfileDTO(created)})
	return nil
}

// validateV3Profile valida los campos de un perfil v3 (todos obligatorios: el nivel es siempre authPriv), rellena p
// y devuelve el secreto a cifrar.
func validateV3Profile(p *store.SNMPProfile, req createSNMPProfileReq) (profileSecret, error) {
	if req.Community != nil || req.AllowV2c != nil {
		return profileSecret{}, badRequest("community y allow_v2c solo aplican a un perfil version 2")
	}
	if req.Username == nil || req.AuthProto == nil || req.AuthPass == nil || req.PrivProto == nil || req.PrivPass == nil {
		return profileSecret{}, badRequest("un perfil v3 requiere username, auth_proto, auth_pass, priv_proto y priv_pass")
	}
	if err := checkProfileUser(*req.Username); err != nil {
		return profileSecret{}, err
	}
	if err := checkProfileProto("auth_proto", *req.AuthProto, validAuthProtos, "SHA256 o SHA512"); err != nil {
		return profileSecret{}, err
	}
	if err := checkProfileProto("priv_proto", *req.PrivProto, validPrivProtos, "AES128 o AES256"); err != nil {
		return profileSecret{}, err
	}
	if err := checkProfileSecret("auth_pass", *req.AuthPass, minProfileAuthPass, maxProfileAuthPass); err != nil {
		return profileSecret{}, err
	}
	if err := checkProfileSecret("priv_pass", *req.PrivPass, minProfileAuthPass, maxProfileAuthPass); err != nil {
		return profileSecret{}, err
	}
	p.Username, p.AuthProto, p.PrivProto = req.Username, req.AuthProto, req.PrivProto
	return profileSecret{AuthPass: *req.AuthPass, PrivPass: *req.PrivPass}, nil
}

// validateV2Profile valida un perfil v2c: exige la confirmacion explicita allow_v2c: true y solo lleva comunidad.
func validateV2Profile(req createSNMPProfileReq) (profileSecret, error) {
	if req.AllowV2c == nil || !*req.AllowV2c {
		return profileSecret{}, badRequest("un perfil version 2 (v2c) requiere allow_v2c: true; v3 es lo recomendado")
	}
	if req.Username != nil || req.AuthProto != nil || req.AuthPass != nil || req.PrivProto != nil || req.PrivPass != nil {
		return profileSecret{}, badRequest("username, auth_* y priv_* solo aplican a un perfil version 3")
	}
	if req.Community == nil {
		return profileSecret{}, badRequest("un perfil v2c requiere community")
	}
	if err := checkProfileSecret("community", *req.Community, 1, maxProfileCommunity); err != nil {
		return profileSecret{}, err
	}
	return profileSecret{Community: *req.Community}, nil
}

// updateSNMPProfileReq: PATCH parcial. version no cambia. Los campos ausentes se conservan (tampoco hay null: un
// perfil siempre tiene todos sus campos).
type updateSNMPProfileReq struct {
	Name      *string `json:"name"`
	Username  *string `json:"username"`
	AuthProto *string `json:"auth_proto"`
	AuthPass  *string `json:"auth_pass"`
	PrivProto *string `json:"priv_proto"`
	PrivPass  *string `json:"priv_pass"`
	Community *string `json:"community"`
}

func (req updateSNMPProfileReq) changedFields() []string {
	var out []string
	for _, f := range []struct {
		set  bool
		name string
	}{
		{req.Name != nil, "name"}, {req.Username != nil, "username"}, {req.AuthProto != nil, "auth_proto"},
		{req.AuthPass != nil, "auth_pass"}, {req.PrivProto != nil, "priv_proto"}, {req.PrivPass != nil, "priv_pass"},
		{req.Community != nil, "community"},
	} {
		if f.set {
			out = append(out, f.name)
		}
	}
	return out
}

func (a *API) updateSNMPProfile(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return store.ErrNotFound
	}
	id = strings.ToLower(id)
	var req updateSNMPProfileReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	fields := req.changedFields()
	if len(fields) == 0 {
		return badRequest("nada que cambiar")
	}
	newSecret := req.AuthPass != nil || req.PrivPass != nil || req.Community != nil
	if newSecret {
		if err := a.secretsReady(); err != nil {
			return err
		}
	}
	updated, err := a.store.UpdateSNMPProfile(r.Context(), id, func(p *store.SNMPProfile) error {
		return a.applyProfilePatch(p, req, newSecret)
	})
	if err != nil {
		return err
	}
	u := currentUser(r)
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evSNMPProfileUpdated, store.OutcomeOK, "snmp_profile", updated.ID
	a.audit(r, e, map[string]any{"name": updated.Name, "version": updated.Version, "fields": fields})
	writeJSON(w, http.StatusOK, map[string]any{"profile": toSNMPProfileDTO(updated)})
	return nil
}

// applyProfilePatch aplica el PATCH sobre el perfil bloqueado: rechaza los campos de la otra version, valida y, si
// cambia algun secreto, descifra el actual, lo fusiona y lo vuelve a cifrar.
func (a *API) applyProfilePatch(p *store.SNMPProfile, req updateSNMPProfileReq, newSecret bool) error {
	if req.Name != nil {
		name, err := checkName("name", *req.Name, maxProfileNameLen)
		if err != nil {
			return err
		}
		p.Name = name
	}
	v3 := p.Version == store.SNMPv3
	if v3 && req.Community != nil {
		return badRequest("community solo aplica a un perfil version 2")
	}
	if !v3 && (req.Username != nil || req.AuthProto != nil || req.AuthPass != nil || req.PrivProto != nil || req.PrivPass != nil) {
		return badRequest("username, auth_* y priv_* solo aplican a un perfil version 3")
	}
	if err := patchV3Fields(p, req); err != nil {
		return err
	}
	if !newSecret {
		return nil
	}
	sec, err := a.openProfileSecret(*p)
	if err != nil {
		return err
	}
	if req.AuthPass != nil {
		sec.AuthPass = *req.AuthPass
	}
	if req.PrivPass != nil {
		sec.PrivPass = *req.PrivPass
	}
	if req.Community != nil {
		if err := checkProfileSecret("community", *req.Community, 1, maxProfileCommunity); err != nil {
			return err
		}
		sec.Community = *req.Community
	}
	p.SecretEnc, p.SecretKeyID, err = a.sealProfileSecret(p.ID, sec)
	return err
}

func patchV3Fields(p *store.SNMPProfile, req updateSNMPProfileReq) error {
	if req.Username != nil {
		if err := checkProfileUser(*req.Username); err != nil {
			return err
		}
		p.Username = req.Username
	}
	if req.AuthProto != nil {
		if err := checkProfileProto("auth_proto", *req.AuthProto, validAuthProtos, "SHA256 o SHA512"); err != nil {
			return err
		}
		p.AuthProto = req.AuthProto
	}
	if req.PrivProto != nil {
		if err := checkProfileProto("priv_proto", *req.PrivProto, validPrivProtos, "AES128 o AES256"); err != nil {
			return err
		}
		p.PrivProto = req.PrivProto
	}
	for field, v := range map[string]*string{"auth_pass": req.AuthPass, "priv_pass": req.PrivPass} {
		if v != nil {
			if err := checkProfileSecret(field, *v, minProfileAuthPass, maxProfileAuthPass); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *API) deleteSNMPProfile(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return store.ErrNotFound
	}
	id = strings.ToLower(id)
	name, err := a.store.DeleteSNMPProfile(r.Context(), id)
	if err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evSNMPProfileDeleted, store.OutcomeOK, "snmp_profile", id
	a.audit(r, e, map[string]any{"name": name})
	writeJSON(w, http.StatusOK, map[string]any{})
	return nil
}
