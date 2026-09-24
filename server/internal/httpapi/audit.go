package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"argos/server/internal/store"
)

// Largos maximos de campos de auditoria y de sesion (alineados con los CHECK de la migracion 00004).
const (
	maxActorUsernameLen = 128
	maxUserAgentLen     = 200
	maxAuditValueLen    = 128
	auditWriteTimeout   = 3 * time.Second
)

// Eventos de auditoria.
const (
	evLoginOK            = "login_ok"
	evLoginFail          = "login_fail"
	evLogout             = "logout"
	evPasswordChanged    = "password_changed"
	evUserCreated        = "user_created"
	evUserRoleChanged    = "user_role_changed"
	evUserDisabled       = "user_disabled"
	evUserEnabled        = "user_enabled"
	evUserPasswordReset  = "user_password_reset"
	evEnrollTokenCreated = "enroll_token_created" //nolint:gosec // nombre de evento, no una credencial
	evHostEnrolled       = "host_enrolled"
	evHostRevoked        = "host_revoked"
	evGroupCreated       = "group_created"
	evGroupUpdated       = "group_updated"
	evGroupDeleted       = "group_deleted"
	evHostGroupSet       = "host_group_set"
	evHostTagsSet        = "host_tags_set"
	evHostsBulk          = "hosts_bulk"
	evAlertAcked         = "alert_acked"
	evAlertResolved      = "alert_resolved"
	evAlertRuleCreated   = "alert_rule_created"
	evAlertRuleUpdated   = "alert_rule_updated"
	evAlertRuleDeleted   = "alert_rule_deleted"
	evChannelCreated     = "channel_created"
	evChannelUpdated     = "channel_updated"
	evChannelDeleted     = "channel_deleted"
	evChannelTested      = "channel_tested"
	evRuleChannelsSet    = "alert_rule_channels_set"
	evMaintenanceCreated = "maintenance_created"
	evMaintenanceUpdated = "maintenance_updated"
	evMaintenanceDeleted = "maintenance_deleted"
	evAuthzDenied        = "authz_denied"
	evAgentAuthFail      = "agent_auth_fail"
	evEnrollFail         = "enroll_fail"
)

// auditDetailKeys es la lista blanca de claves permitidas en audit_log.detail.
// Cualquier otra clave se descarta: asi un detalle nuevo no puede colar un secreto por descuido.
var auditDetailKeys = map[string]bool{
	"reason": true, "locked": true, "role": true, "disabled": true,
	"method": true, "route": true, "count": true, "hostname": true,
	// Grupos, etiquetas y acciones masivas. De las etiquetas se audita su nombre (no es sensible).
	"action": true, "group_id": true, "name": true, "always_on": true, "down_after_seconds": true, "tag": true,
	// Alertas, reglas y mantenimiento: nombres y ajustes, nunca secretos.
	"severity": true, "kind": true, "metric": true, "enabled": true, "scope_type": true,
	"starts_at": true, "ends_at": true, "fields": true,
	// Canales: solo el host del destino (nunca la URL completa, que puede llevar un token, ni el secreto HMAC).
	"host": true,
}

// audit registra un evento sin fallar la solicitud si la escritura falla: se loguea
// el error (sin el detalle) y sigue. Usa un contexto desacoplado de la solicitud pa
// que un cliente que se desconecta no pierda el evento de algo que ya ocurrio.
func (a *API) audit(r *http.Request, e store.AuditEntry, detail map[string]any) {
	e.IP = addrPtr(a.clientIP(r))
	a.writeAudit(r.Context(), e, detail)
}

func (a *API) writeAudit(ctx context.Context, e store.AuditEntry, detail map[string]any) {
	e.ActorUsername = truncRunes(e.ActorUsername, maxActorUsernameLen)
	e.Detail = marshalDetail(detail)

	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditWriteTimeout)
	defer cancel()
	if err := a.store.InsertAudit(wctx, e); err != nil {
		a.logger.Error("no se pudo escribir la auditoria", "event", e.Event, "err", err)
	}
}

func marshalDetail(detail map[string]any) []byte {
	clean := make(map[string]any, len(detail))
	for k, v := range detail {
		if !auditDetailKeys[k] {
			continue
		}
		if s, ok := v.(string); ok {
			v = truncRunes(s, maxAuditValueLen)
		}
		clean[k] = v
	}
	if len(clean) == 0 {
		return nil
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return nil // valores solo string/bool/int: no deberia ocurrir
	}
	return b
}

// truncRunes recorta a n caracteres (no bytes) sin partir UTF-8 y sustituye bytes invalidos.
func truncRunes(s string, n int) string {
	s = strings.ToValidUTF8(s, "�")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// actor rellena los campos de actor de un evento con el usuario de la sesion.
func actor(u *sessionUser) store.AuditEntry {
	return store.AuditEntry{ActorUserID: u.ID, ActorUsername: u.Username}
}

// failCounter agrega fallos de autenticacion de agentes en memoria; un ticker los
// vuelca a audit_log una vez por minuto (un evento por tipo, no uno por request).
type failCounter struct {
	mu     sync.Mutex
	counts map[string]int
}

func (f *failCounter) add(event string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.counts == nil {
		f.counts = make(map[string]int)
	}
	f.counts[event]++
}

func (f *failCounter) drain() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.counts
	f.counts = nil
	return out
}

// FlushAgentFailures vuelca a audit_log los fallos de auth de agente acumulados
// desde la ultima llamada. Lo invoca el ticker de mantenimiento (y al apagar).
func (a *API) FlushAgentFailures(ctx context.Context) {
	for event, n := range a.agentFails.drain() {
		a.writeAudit(ctx, store.AuditEntry{Event: event, Outcome: store.OutcomeFail, TargetType: "agent"},
			map[string]any{"count": n})
	}
}
