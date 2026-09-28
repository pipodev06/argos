package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"argos/server/internal/notify"
	"argos/server/internal/store"
)

const (
	maxChannelNameLen = 80
	maxHMACSecretLen  = 256
	// channelTestsPerMinute limita POST /channels/{id}/test por usuario: cada uso hace una peticion saliente.
	channelTestsPerMinute = 5
)

// errSecretKeyMissing: sin ARGOS_SECRET_KEY no se pueden guardar ni usar secretos de canales.
var errSecretKeyMissing = &clientError{status: http.StatusServiceUnavailable, code: "secret_key_missing",
	msg: "define ARGOS_SECRET_KEY (base64 de 32 bytes) y reinicia el server pa usar canales"}

// --- DTOs ---

// channelDTO es lo unico que ve un viewer: nunca la configuracion, destinatarios ni URL.
type channelDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	MinSeverity string `json:"min_severity"`
	Enabled     bool   `json:"enabled"`
	HasSecret   bool   `json:"has_secret"`
	// Dueño del canal: null = canal global del admin (usable por cualquier regla); con grupo, solo por reglas de ese grupo.
	GroupID   *string `json:"group_id"`
	GroupName *string `json:"group_name"`
}

// channelConfig es la parte no secreta que se guarda en la columna config: solo el host del webhook y si se
// firma. Nunca la URL completa (puede llevar un token).
type channelConfig struct {
	Host   string `json:"host"`
	Signed bool   `json:"signed"`
}

// adminChannelDTO agrega lo que solo ve un admin. El secreto (URL y HMAC) no se devuelve nunca.
type adminChannelDTO struct {
	channelDTO
	Config    channelConfig `json:"config"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func toChannelDTO(c store.Channel) channelDTO {
	return channelDTO{ID: c.ID, Name: c.Name, Kind: c.Kind, MinSeverity: c.MinSeverity, Enabled: c.Enabled,
		HasSecret: len(c.SecretEnc) > 0, GroupID: c.GroupID, GroupName: c.GroupName}
}

// channelView devuelve el DTO segun el rol: el viewer nunca recibe config.
func channelView(u *sessionUser, c store.Channel) any {
	if u == nil || u.Role != store.RoleAdmin {
		return toChannelDTO(c)
	}
	var cfg channelConfig
	// Un config ilegible no es un error del cliente: se muestra vacio.
	if err := json.Unmarshal(c.Config, &cfg); err != nil {
		cfg = channelConfig{}
	}
	return adminChannelDTO{channelDTO: toChannelDTO(c), Config: cfg, CreatedAt: c.CreatedAt.UTC(), UpdatedAt: c.UpdatedAt.UTC()}
}

// --- validacion ---

// webhookReady devuelve el error 503 si falta la clave de secretos o el cliente webhook.
func (a *API) webhookReady() error {
	if a.opts.Secrets == nil || a.opts.Webhook == nil {
		return errSecretKeyMissing
	}
	return nil
}

// checkWebhookURL valida la URL con las mismas reglas del envio (esquema, puerto, IPs) antes de guardarla.
// Los errores de politica son 400 sin eco de la URL; un fallo inesperado sube como 500.
func (a *API) checkWebhookURL(r *http.Request, raw string) error {
	err := a.opts.Webhook.CheckURL(r.Context(), raw)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, notify.ErrBlockedDestination):
		return badRequest("la url apunta a un destino no permitido (red interna o reservada)")
	case errors.Is(err, notify.ErrURLInvalid):
		return badRequest("%s", err.Error())
	}
	return err
}

// parseChannelGroup valida el dueño opcional de un canal (nil = global): un uuid bien formado, en minusculas.
func parseChannelGroup(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	if !isUUID(*raw) {
		return nil, badRequest("group_id debe ser un id de grupo o null")
	}
	id := strings.ToLower(*raw)
	return &id, nil
}

func checkHMACSecret(s string) error {
	return checkOptionalText("hmac_secret", s, maxHMACSecretLen)
}

// urlHost devuelve el hostname de una URL ya validada (lo unico de la URL que se guarda en claro).
func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// sealWebhook cifra el secreto de un canal webhook con el id del canal como AAD.
func (a *API) sealWebhook(channelID string, sec notify.WebhookSecret) (blob []byte, keyID string, err error) {
	raw, err := json.Marshal(sec)
	if err != nil {
		return nil, "", fmt.Errorf("serializando secreto del canal: %w", err)
	}
	blob, err = a.opts.Secrets.Seal(channelID, notify.KindWebhook, raw)
	if err != nil {
		return nil, "", fmt.Errorf("cifrando secreto del canal: %w", err)
	}
	return blob, a.opts.Secrets.KeyID(), nil
}

func (a *API) openWebhook(c store.Channel) (notify.WebhookSecret, error) {
	var sec notify.WebhookSecret
	if c.SecretKeyID == nil {
		return sec, errors.New("el canal no tiene secreto")
	}
	raw, err := a.opts.Secrets.Open(c.ID, *c.SecretKeyID, c.Kind, c.SecretEnc)
	if err != nil {
		return sec, err
	}
	if err := json.Unmarshal(raw, &sec); err != nil {
		return sec, fmt.Errorf("leyendo secreto del canal: %w", err)
	}
	return sec, nil
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generando id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func channelConfigJSON(host string, signed bool) ([]byte, error) {
	b, err := json.Marshal(channelConfig{Host: host, Signed: signed})
	if err != nil {
		return nil, fmt.Errorf("serializando config del canal: %w", err)
	}
	return b, nil
}

// --- lecturas ---

func (a *API) listChannels(w http.ResponseWriter, r *http.Request) error {
	chans, err := a.store.ListChannels(r.Context())
	if err != nil {
		return err
	}
	u := currentUser(r)
	items := make([]any, 0, len(chans))
	for _, c := range chans {
		items = append(items, channelView(u, c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

func (a *API) getChannel(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	c, err := a.store.ChannelByID(r.Context(), id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": channelView(currentUser(r), c)})
	return nil
}

// --- escrituras ---

type createChannelReq struct {
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	URL         string  `json:"url"`
	HMACSecret  string  `json:"hmac_secret"`
	MinSeverity string  `json:"min_severity"`
	Enabled     *bool   `json:"enabled"`
	GroupID     *string `json:"group_id"`
}

func (a *API) createChannel(w http.ResponseWriter, r *http.Request) error {
	var req createChannelReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	name, err := checkName("name", req.Name, maxChannelNameLen)
	if err != nil {
		return err
	}
	if req.Kind != notify.KindWebhook {
		return badRequest("kind debe ser webhook")
	}
	if req.MinSeverity == "" {
		req.MinSeverity = "warn"
	}
	if !validAlertSeverity[req.MinSeverity] {
		return badRequest("min_severity debe ser info, warn o critical")
	}
	if err := checkHMACSecret(req.HMACSecret); err != nil {
		return err
	}
	groupID, err := parseChannelGroup(req.GroupID)
	if err != nil {
		return err
	}
	if err := a.webhookReady(); err != nil {
		return err
	}
	if err := a.checkWebhookURL(r, req.URL); err != nil {
		return err
	}

	id, err := newUUID()
	if err != nil {
		return err
	}
	blob, keyID, err := a.sealWebhook(id, notify.WebhookSecret{URL: req.URL, HMAC: req.HMACSecret})
	if err != nil {
		return err
	}
	cfg, err := channelConfigJSON(urlHost(req.URL), req.HMACSecret != "")
	if err != nil {
		return err
	}
	ch := store.Channel{ID: id, Name: name, Kind: req.Kind, Config: cfg, SecretEnc: blob, SecretKeyID: &keyID,
		MinSeverity: req.MinSeverity, Enabled: true, GroupID: groupID}
	if req.Enabled != nil {
		ch.Enabled = *req.Enabled
	}

	u := currentUser(r)
	created, err := a.store.CreateChannel(r.Context(), ch, u.ID)
	if err != nil {
		return err
	}
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evChannelCreated, store.OutcomeOK, "channel", created.ID
	a.audit(r, e, channelAuditDetail(created, urlHost(req.URL), nil))

	writeJSON(w, http.StatusCreated, map[string]any{"channel": channelView(u, created)})
	return nil
}

// updateChannelReq: PATCH parcial. kind no cambia. hmac_secret null o "" quita la firma; ausente la conserva.
type updateChannelReq struct {
	Name        *string          `json:"name"`
	URL         *string          `json:"url"`
	HMACSecret  optional[string] `json:"hmac_secret"`
	MinSeverity *string          `json:"min_severity"`
	Enabled     *bool            `json:"enabled"`
	// GroupID: dueño del canal; null lo vuelve global, ausente lo conserva.
	GroupID optional[string] `json:"group_id"`
}

func (req updateChannelReq) changedFields() []string {
	var out []string
	add := func(present bool, name string) {
		if present {
			out = append(out, name)
		}
	}
	add(req.Name != nil, "name")
	add(req.URL != nil, "url")
	add(req.HMACSecret.Set, "hmac_secret")
	add(req.MinSeverity != nil, "min_severity")
	add(req.Enabled != nil, "enabled")
	add(req.GroupID.Set, "group_id")
	return out
}

func (a *API) updateChannel(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	var req updateChannelReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	fields := req.changedFields()
	if len(fields) == 0 {
		return badRequest("nada que cambiar")
	}
	if req.Name != nil {
		name, err := checkName("name", *req.Name, maxChannelNameLen)
		if err != nil {
			return err
		}
		req.Name = &name
	}
	if req.MinSeverity != nil && !validAlertSeverity[*req.MinSeverity] {
		return badRequest("min_severity debe ser info, warn o critical")
	}
	newHMAC := ""
	if req.HMACSecret.Value != nil {
		newHMAC = *req.HMACSecret.Value
		if err := checkHMACSecret(newHMAC); err != nil {
			return err
		}
	}
	newGroup, err := parseChannelGroup(req.GroupID.Value)
	if err != nil {
		return err
	}
	changesSecret := req.URL != nil || req.HMACSecret.Set
	if changesSecret {
		if err := a.webhookReady(); err != nil {
			return err
		}
	}
	if req.URL != nil {
		if err := a.checkWebhookURL(r, *req.URL); err != nil {
			return err
		}
	}

	updated, err := a.store.UpdateChannel(r.Context(), id, func(c *store.Channel) error {
		if req.Name != nil {
			c.Name = *req.Name
		}
		if req.MinSeverity != nil {
			c.MinSeverity = *req.MinSeverity
		}
		if req.Enabled != nil {
			c.Enabled = *req.Enabled
		}
		if req.GroupID.Set {
			c.GroupID = newGroup
		}
		if !changesSecret {
			return nil
		}
		sec, err := a.openWebhook(*c)
		if err != nil {
			return fmt.Errorf("abriendo el secreto actual del canal %s: %w", c.ID, err)
		}
		if req.URL != nil {
			sec.URL = *req.URL
		}
		if req.HMACSecret.Set {
			sec.HMAC = newHMAC
		}
		blob, keyID, err := a.sealWebhook(c.ID, sec)
		if err != nil {
			return err
		}
		cfg, err := channelConfigJSON(urlHost(sec.URL), sec.HMAC != "")
		if err != nil {
			return err
		}
		c.SecretEnc, c.SecretKeyID, c.Config = blob, &keyID, cfg
		return nil
	})
	if err != nil {
		return err
	}
	host := ""
	if req.URL != nil {
		host = urlHost(*req.URL)
	}
	u := currentUser(r)
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evChannelUpdated, store.OutcomeOK, "channel", updated.ID
	a.audit(r, e, channelAuditDetail(updated, host, fields))

	writeJSON(w, http.StatusOK, map[string]any{"channel": channelView(u, updated)})
	return nil
}

// channelAuditDetail: id (lo lleva el target), nombre, tipo, severidad minima, estado y campos cambiados. Del
// destino solo el host, y solo si cambio; nunca la URL completa ni el secreto HMAC.
func channelAuditDetail(c store.Channel, host string, fields []string) map[string]any {
	d := map[string]any{"name": c.Name, "kind": c.Kind, "severity": c.MinSeverity, "enabled": c.Enabled}
	if c.GroupID != nil {
		d["group_id"] = *c.GroupID
	}
	if host != "" {
		d["host"] = host
	}
	if fields != nil {
		d["fields"] = fields
	}
	return d
}

func (a *API) deleteChannel(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	name, kind, err := a.store.DeleteChannel(r.Context(), id)
	if err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evChannelDeleted, store.OutcomeOK, "channel", id
	a.audit(r, e, map[string]any{"name": name, "kind": kind})
	writeJSON(w, http.StatusOK, map[string]any{})
	return nil
}

// testChannelResult es TODO lo que devuelve POST /channels/{id}/test: sin cuerpo de respuesta, IP ni status HTTP.
type testChannelResult struct {
	OK       bool    `json:"ok"`
	Category *string `json:"category"`
}

func (a *API) testChannel(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	u := currentUser(r)
	if ok, retry := a.testLimiter.allow(u.ID, a.now()); !ok {
		return rateLimited(retry)
	}
	if err := a.webhookReady(); err != nil {
		return err
	}
	c, err := a.store.ChannelByID(r.Context(), id)
	if err != nil {
		return err
	}
	if c.Kind != notify.KindWebhook {
		return badRequest("solo los canales webhook se pueden probar")
	}
	sec, err := a.openWebhook(c)
	if err != nil {
		return fmt.Errorf("abriendo el secreto del canal %s: %w", c.ID, err)
	}
	body, err := notify.BuildTestMessage(a.now())
	if err != nil {
		return fmt.Errorf("armando el mensaje de prueba: %w", err)
	}

	res := testChannelResult{OK: true}
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evChannelTested, store.OutcomeOK, "channel", c.ID
	detail := map[string]any{"name": c.Name, "kind": c.Kind}
	if _, err := a.opts.Webhook.Send(r.Context(), notify.Target{URL: sec.URL, HMACSecret: sec.HMAC}, body); err != nil {
		var se *notify.SendError
		cat := notify.CatConnect
		if errors.As(err, &se) {
			cat = se.Category
		}
		res = testChannelResult{OK: false, Category: &cat}
		e.Outcome = store.OutcomeFail
		detail["reason"] = cat
	}
	a.audit(r, e, detail)
	writeJSON(w, http.StatusOK, res)
	return nil
}

// --- asignacion regla <-> canal ---

type setRuleChannelsReq struct {
	ChannelIDs *[]string `json:"channel_ids"`
}

func (a *API) setRuleChannels(w http.ResponseWriter, r *http.Request) error {
	return a.putRuleChannels(w, r, a.store.SetRuleChannels, "channels")
}

// setRuleEscalationChannels: misma semantica y validaciones que setRuleChannels, sobre los canales de escalamiento.
func (a *API) setRuleEscalationChannels(w http.ResponseWriter, r *http.Request) error {
	return a.putRuleChannels(w, r, a.store.SetRuleEscalationChannels, "escalation_channels")
}

// putRuleChannels valida el body {"channel_ids": [...]}, aplica set y audita (el evento es el mismo; detail.list dice
// cual de las dos listas cambio).
func (a *API) putRuleChannels(w http.ResponseWriter, r *http.Request, set func(context.Context, string, []string) error, list string) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	var req setRuleChannelsReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if req.ChannelIDs == nil {
		return badRequest("channel_ids requerido (lista, puede ir vacia)")
	}
	ids := make([]string, 0, len(*req.ChannelIDs))
	seen := map[string]bool{}
	for _, cid := range *req.ChannelIDs {
		if !isUUID(cid) {
			return badRequest("channel_ids contiene un id invalido")
		}
		cid = strings.ToLower(cid)
		if !seen[cid] {
			seen[cid] = true
			ids = append(ids, cid)
		}
	}
	if err := set(r.Context(), id, ids); err != nil {
		return err
	}
	rule, err := a.store.AlertRuleByID(r.Context(), store.AllScope(), id)
	if err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evRuleChannelsSet, store.OutcomeOK, "alert_rule", id
	a.audit(r, e, map[string]any{"name": rule.Name, "list": list, "count": len(ids)})
	writeJSON(w, http.StatusOK, map[string]any{"channel_ids": ids})
	return nil
}

// ruleRefDTO es una regla afectada en el detalle de channel_scope_mismatch.
type ruleRefDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// channelScopeDetails arma details de channel_scope_mismatch: {"rules": [{"id","name"}]} (hasta 20).
func channelScopeDetails(err error) any {
	var se *store.ChannelScopeError
	if !errors.As(err, &se) {
		return nil
	}
	rules := make([]ruleRefDTO, 0, len(se.Rules))
	for _, r := range se.Rules {
		rules = append(rules, ruleRefDTO{ID: r.ID, Name: r.Name})
	}
	return map[string]any{"rules": rules}
}
