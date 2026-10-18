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
	evLoginOK                = "login_ok"
	evLoginFail              = "login_fail"
	evLogout                 = "logout"
	evPasswordChanged        = "password_changed"
	evUserCreated            = "user_created"
	evUserRoleChanged        = "user_role_changed"
	evUserDisabled           = "user_disabled"
	evUserEnabled            = "user_enabled"
	evUserPasswordReset      = "user_password_reset"
	evUserPermissionsChanged = "user_permissions_changed"
	evUserDeleted            = "user_deleted"
	evEnrollTokenCreated     = "enroll_token_created" //nolint:gosec // nombre de evento, no una credencial
	evHostEnrolled           = "host_enrolled"
	evHostRevoked            = "host_revoked"
	evGroupCreated           = "group_created"
	evGroupUpdated           = "group_updated"
	evGroupDeleted           = "group_deleted"
	evHostGroupSet           = "host_group_set"
	evHostRenamed            = "host_renamed"
	evHostsBulk              = "hosts_bulk"
	evHostPurged             = "host_purged"
	evGroupPurged            = "group_purged"
	evRetentionUpdated       = "retention_updated"
	evAlertAcked             = "alert_acked"
	evAlertResolved          = "alert_resolved"
	evAlertRuleCreated       = "alert_rule_created"
	evAlertRuleUpdated       = "alert_rule_updated"
	evAlertRuleDeleted       = "alert_rule_deleted"
	evChannelCreated         = "channel_created"
	evChannelUpdated         = "channel_updated"
	evChannelDeleted         = "channel_deleted"
	evChannelTested          = "channel_tested"
	evRuleChannelsSet        = "alert_rule_channels_set"
	evDeviceCreated          = "device_created"
	evDeviceUpdated          = "device_updated"
	evDeviceDeleted          = "device_deleted"
	evSNMPProfileCreated     = "snmp_profile_created"
	evSNMPProfileUpdated     = "snmp_profile_updated"
	evSNMPProfileDeleted     = "snmp_profile_deleted"
	evServiceCheckCreated    = "service_check_created"
	evServiceCheckUpdated    = "service_check_updated"
	evServiceCheckDeleted    = "service_check_deleted"
	evMaintenanceCreated     = "maintenance_created"
	evMaintenanceUpdated     = "maintenance_updated"
	evMaintenanceDeleted     = "maintenance_deleted"
	evAuthzDenied            = "authz_denied"
	evAgentAuthFail          = "agent_auth_fail"
	evEnrollFail             = "enroll_fail"
	evInventoryRead          = "inventory_read"
	evUpdatesRead            = "updates_read"
	evReportExported         = "report_exported"
	evGroupExported          = "group_exported"
)

// auditDetailKeys es la lista blanca de claves permitidas en audit_log.detail.
// Cualquier otra clave se descarta: asi un detalle nuevo no puede colar un secreto por descuido.
var auditDetailKeys = map[string]bool{
	"reason": true, "locked": true, "role": true, "disabled": true,
	"method": true, "route": true, "count": true, "hostname": true,
	// Permisos por grupo de un usuario: valores antes (prev_*) y despues; group_ids solo si son pocos.
	"all_groups": true, "prev_all_groups": true, "can_inventory": true, "prev_can_inventory": true, "can_updates": true, "prev_can_updates": true, "can_ack": true, "prev_can_ack": true, "username": true,
	"group_ids": true, "group_count": true, "prev_group_count": true,
	// Grupos y acciones masivas.
	"action": true, "group_id": true, "name": true, "always_on": true, "down_after_seconds": true,
	// Alertas, reglas y mantenimiento: nombres y ajustes, nunca secretos.
	"severity": true, "kind": true, "metric": true, "enabled": true, "scope_type": true,
	"starts_at": true, "ends_at": true, "fields": true, "minutes": true,
	// Chequeos de servicio: tipo y ajustes, nunca el secreto, la cabecera ni la URL.
	"type": true, "check_id": true, "device_id": true, "host_id": true,
	// Dispositivos: el colector que los sondea (nunca la direccion IP).
	"collector_id": true,
	// SNMP: version del perfil, perfil asignado a un dispositivo e interfaz del chequeo (nunca contrasenas ni comunidad).
	"version": true, "snmp_profile_id": true, "if_index": true,
	// Canales: solo el host del destino (nunca la URL completa, que puede llevar un token, ni el secreto HMAC).
	"host": true,
	// Lectura de inventario: solo el modulo consultado, nunca contenido.
	"module": true, "scope": true,
	// Exportacion de informes: tipo, rango y filas escritas, nunca contenido.
	"report": true, "range": true, "rows": true,
	// Retencion de detalle: dias fijados (null = heredar); el ambito va en "scope".
	"detail_days": true,
	// Exportacion del historial: rango, resolucion y serie (nunca datos).
	"from": true, "to": true, "resolution": true, "series": true,
	// Purga de un cliente: conteos, nunca nombres ni contenido.
	"agents": true, "devices": true, "metrics_rows": true, "failed": true, "rules": true, "windows": true, "checks": true,
	"users_disabled": true, "users_scope_reduced": true, "group_deleted": true, "enroll_tokens": true,
	"channels": true, "snmp_profiles": true,
	// Exportacion del cliente (zip): filas por cada csv, nunca contenido.
	"hosts_rows": true, "availability_rows": true,
	// Cuotas del cliente (B7): limites fijados (null = sin limite propio).
	"max_hosts": true, "max_devices": true, "max_checks": true, "max_users": true, "max_rules": true, "max_channels": true,
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
