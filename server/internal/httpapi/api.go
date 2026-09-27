// Package httpapi expone la API HTTP de argos (dashboard y agentes).
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"argos/server/internal/agentdist"
	"argos/server/internal/auth"
	"argos/server/internal/notify"
	"argos/server/internal/store"
)

// Store es lo que la API necesita de la persistencia. Lo implementa *store.Store;
// se define aca (donde se consume) pa poder probar los handlers con un falso.
type Store interface {
	Ping(ctx context.Context) error
	SystemStats(ctx context.Context, deadSince time.Time) (store.SystemStats, error)
	// Hosts y agentes.
	CreateEnrollToken(ctx context.Context, tokenHash, hostname, createdBy string, groupID *string, expiresAt time.Time) error
	ListPendingEnrollTokens(ctx context.Context) ([]store.PendingEnrollToken, error)
	RevokeEnrollToken(ctx context.Context, id string) (hostname string, err error)
	EnrollHost(ctx context.Context, enrollTokenHash, osName, arch, agentTokenHash string) (hostID string, err error)
	HostIDByTokenHash(ctx context.Context, tokenHash string) (hostID string, err error)
	IngestMetrics(ctx context.Context, hostID string, s store.Sample) (store.IngestResult, error)
	ListHosts(ctx context.Context, scope store.Scope, f store.HostFilter) (store.HostPage, error)
	HostByID(ctx context.Context, scope store.Scope, hostID string) (store.HostDetail, error)
	HostDisks(ctx context.Context, hostID string, now time.Time) ([]store.DiskUsage, error)
	LatestMetrics(ctx context.Context, hostID string) (*store.MetricPoint, error)
	MetricSeries(ctx context.Context, hostID string, from time.Time, step time.Duration) ([]store.MetricPoint, error)
	MetricSeriesHourly(ctx context.Context, hostID string, from time.Time, step time.Duration) ([]store.MetricPoint, error)
	MetricSeriesMixed(ctx context.Context, hostID string, from time.Time, step time.Duration) ([]store.MetricPoint, error)
	// Historial por fecha (detalle comprimido, resumenes por hora y discos).
	DetailSeries(ctx context.Context, hostID string, from, to time.Time, step time.Duration) ([]store.MetricPoint, *time.Time, error)
	MetricSeriesHourlyRange(ctx context.Context, hostID string, from, to time.Time, step time.Duration) ([]store.MetricPoint, *time.Time, error)
	DetailRange(ctx context.Context, hostID string, from, to time.Time, fn func(store.DetailSample) error) error
	HourlyRange(ctx context.Context, hostID string, from, to time.Time, fn func(store.HourlyRow) error) error
	DiskSampleRange(ctx context.Context, hostID string, from, to time.Time, fn func(store.DiskHistoryRow) error) error
	RevokeHost(ctx context.Context, hostID string) error
	Summary(ctx context.Context, scope store.Scope) (store.Summary, error)
	AgentVersionCounts(ctx context.Context, scope store.Scope, includeDisabled bool) ([]store.AgentVersionCount, error)

	// Grupos.
	ListGroups(ctx context.Context, scope store.Scope) (groups []store.Group, ungrouped int, err error)
	CreateGroup(ctx context.Context, name string, downAfterSeconds *int, alwaysOn bool) (store.Group, error)
	UpdateGroup(ctx context.Context, id string, ch store.GroupChange) (store.Group, error)
	DeleteGroup(ctx context.Context, id string) error
	SetHostGroup(ctx context.Context, hostID string, groupID *string) (store.HostGroupResult, error)
	RenameHost(ctx context.Context, hostID, name string) error
	// Retencion del detalle de metricas (global, por grupo y por host).
	RetentionSettings(ctx context.Context) (store.RetentionSettings, error)
	SetGlobalDetailDays(ctx context.Context, days int, userID string) error
	SetHostDetailDays(ctx context.Context, hostID string, days *int) error
	EffectiveDetailDays(ctx context.Context, hostID string) (days int, source string, err error)
	PreviewRetention(ctx context.Context, t store.RetentionTarget, newDays *int, today time.Time) (store.ShrinkPreview, error)
	BulkHosts(ctx context.Context, b store.Bulk) (int, error)
	// Baja de un cliente: purga de un host o de un grupo entero.
	PurgeHost(ctx context.Context, hostID, confirmHostname string) (store.PurgeCounts, error)
	PurgeGroup(ctx context.Context, groupID string, o store.PurgeGroupOpts) (store.PurgeGroupResult, error)

	// Alertas, reglas, mantenimiento y eventos de host.
	ListAlerts(ctx context.Context, scope store.Scope, f store.AlertFilter) (store.AlertPage, error)
	AlertByID(ctx context.Context, scope store.Scope, id string) (store.AlertDetail, error)
	AckAlert(ctx context.Context, scope store.Scope, id, userID, username string) (store.Alert, error)
	ResolveAlert(ctx context.Context, id, userID, username string) (store.Alert, error)
	AlertSummary(ctx context.Context, scope store.Scope) (store.AlertSummary, error)
	ListHostEvents(ctx context.Context, scope store.Scope, hostID string, after *store.EventCursor, limit int) (store.EventPage, error)
	ListAlertRules(ctx context.Context, scope store.Scope) ([]store.AlertRule, error)
	AlertRuleByID(ctx context.Context, scope store.Scope, id string) (store.AlertRule, error)
	CreateAlertRule(ctx context.Context, r store.AlertRule, createdBy string) (store.AlertRule, error)
	UpdateAlertRule(ctx context.Context, id string, mutate func(*store.AlertRule) error) (store.AlertRule, error)
	DeleteAlertRule(ctx context.Context, id string) (name string, err error)
	AvailabilityReport(ctx context.Context, scope store.Scope, groupID *string, from, to time.Time) ([]store.HostAvailability, error)
	ServiceChecksReport(ctx context.Context, scope store.Scope, groupID *string, from, to time.Time) ([]store.CheckAvailability, error)
	ExportAlerts(ctx context.Context, scope store.Scope, f store.AlertExportFilter, fn func(store.AlertExportRow) error) (int, error)
	ListMaintenance(ctx context.Context, scope store.Scope) ([]store.MaintenanceWindow, error)
	MaintenanceByID(ctx context.Context, scope store.Scope, id string) (store.MaintenanceWindow, error)
	CreateMaintenance(ctx context.Context, w store.MaintenanceWindow, userID, username string) (store.MaintenanceWindow, error)
	UpdateMaintenance(ctx context.Context, id string, mutate func(*store.MaintenanceWindow) error) (store.MaintenanceWindow, error)
	DeleteMaintenance(ctx context.Context, id string) (name string, err error)

	// Canales de notificacion y su asignacion a reglas.
	ListChannels(ctx context.Context) ([]store.Channel, error)
	ChannelByID(ctx context.Context, id string) (store.Channel, error)
	CreateChannel(ctx context.Context, c store.Channel, createdBy string) (store.Channel, error)
	UpdateChannel(ctx context.Context, id string, mutate func(*store.Channel) error) (store.Channel, error)
	DeleteChannel(ctx context.Context, id string) (name, kind string, err error)
	SetRuleChannels(ctx context.Context, ruleID string, channelIDs []string) error

	// Chequeos de servicio (definiciones, estado por host y eventos).
	ListServiceChecks(ctx context.Context, scope store.Scope) ([]store.ServiceCheckDef, error)
	ServiceCheckByID(ctx context.Context, scope store.Scope, id string) (store.ServiceCheckDef, error)
	CreateServiceCheck(ctx context.Context, d store.ServiceCheckDef, createdBy string) (store.ServiceCheckDef, error)
	UpdateServiceCheck(ctx context.Context, id string, mutate func(*store.ServiceCheckDef) error) (store.ServiceCheckDef, error)
	DeleteServiceCheck(ctx context.Context, id string) (name, typ string, err error)
	HostChecks(ctx context.Context, hostID string) ([]store.ServiceCheckDef, error)
	HostServiceChecks(ctx context.Context, scope store.Scope, hostID string) ([]store.HostCheck, error)
	ListCheckEvents(ctx context.Context, scope store.Scope, hostID, checkID string, after *store.EventCursor, limit int) (store.CheckEventPage, error)

	// Dispositivos sin agente (kind device).
	ListDevices(ctx context.Context, scope store.Scope, f store.HostFilter, after *store.HostCursor, limit int) (store.DevicePage, error)
	DeviceByID(ctx context.Context, scope store.Scope, id string) (store.Device, error)
	CreateDevice(ctx context.Context, name string, addr netip.Addr, collectorID string, groupID *string) (store.Device, error)
	UpdateDevice(ctx context.Context, id string, ch store.DeviceChange) (store.Device, error)
	DeleteDevice(ctx context.Context, id string) (name string, err error)
	CollectorPings(ctx context.Context, collectorID string) ([]store.PingAssignment, error)
	CollectorSNMP(ctx context.Context, collectorID string) ([]store.SNMPAssignment, error)

	// Perfiles SNMP (credenciales reutilizables de los dispositivos).
	ListSNMPProfiles(ctx context.Context) ([]store.SNMPProfile, error)
	SNMPProfileByID(ctx context.Context, id string) (store.SNMPProfile, error)
	CreateSNMPProfile(ctx context.Context, p store.SNMPProfile, createdBy string) (store.SNMPProfile, error)
	UpdateSNMPProfile(ctx context.Context, id string, mutate func(*store.SNMPProfile) error) (store.SNMPProfile, error)
	DeleteSNMPProfile(ctx context.Context, id string) (name string, err error)

	// Usuarios y sesiones.
	UserByUsername(ctx context.Context, username string) (store.User, error)
	UserByID(ctx context.Context, id string) (store.User, error)
	ListUsers(ctx context.Context, limit int) ([]store.User, error)
	CreateUser(ctx context.Context, n store.NewUser) (store.User, error)
	DeleteUser(ctx context.Context, id, actorID string) (store.User, error)
	UpdateUser(ctx context.Context, id string, ch store.UserChange, now time.Time) (store.User, error)
	RegisterLoginFailure(ctx context.Context, userID string, now time.Time, maxFails int, lockFor time.Duration) (locked bool, err error)
	RegisterLoginSuccess(ctx context.Context, userID string, now time.Time) error
	UpdatePasswordHash(ctx context.Context, userID, oldHash, newHash string) error
	ChangePassword(ctx context.Context, userID, newHash string, keepSession []byte, now time.Time) error
	CreateSession(ctx context.Context, sess store.Session) error
	SessionByHash(ctx context.Context, idHash []byte) (store.SessionInfo, error)
	TouchSession(ctx context.Context, idHash []byte, now time.Time) error
	DeleteSession(ctx context.Context, idHash []byte) error

	// Auditoria.
	InsertAudit(ctx context.Context, e store.AuditEntry) error
	ListAudit(ctx context.Context, f store.AuditFilter) (store.AuditPage, error)
	AuditEvents(ctx context.Context) ([]string, error)

	// Inventario del host (ingest del agente).
	IngestInventory(ctx context.Context, hostID string, in store.InventoryFull, now time.Time) error
	InventoryHeartbeat(ctx context.Context, hostID, module, rev string, now time.Time) error
	IngestUpdates(ctx context.Context, hostID string, in store.UpdatesSnapshot, now time.Time) error
	// Parches del SO (lectura, con ambito).
	HostUpdates(ctx context.Context, scope store.Scope, hostID string) (store.UpdatesCurrent, bool, error)
	HostUpdatesHistory(ctx context.Context, scope store.Scope, hostID string, limit int) ([]store.UpdatesPoint, error)
	HostOSEOL(ctx context.Context, scope store.Scope, hostID string) (store.OSEOL, bool, error)
	ListUpdatesFleet(ctx context.Context, scope store.Scope, f store.UpdatesFleetFilter) (store.UpdatesFleetPage, error)
	// Inventario del host (lectura, con ambito).
	InventoryOverview(ctx context.Context, scope store.Scope, hostID string) ([]store.InventoryMeta, error)
	InventoryItems(ctx context.Context, scope store.Scope, hostID, module string) ([]json.RawMessage, error)
	ListInventoryChanges(ctx context.Context, scope store.Scope, hostID, module, kind string, after *store.EventCursor, limit int) (store.InventoryChangePage, error)
}

// WebhookClient es lo que la API necesita del cliente webhook (lo implementa *notify.Client).
type WebhookClient interface {
	CheckURL(ctx context.Context, raw string) error
	Send(ctx context.Context, t notify.Target, body []byte) (httpStatus int, err error)
}

// Options son los ajustes de despliegue de la API.
type Options struct {
	// PublicURL es la URL publica del dashboard; nil = solo localhost.
	PublicURL *url.URL
	// TrustedProxies son los proxies de los que se acepta X-Forwarded-For.
	TrustedProxies []netip.Prefix
	// InsecureCookies quita __Host- y Secure de la cookie de sesion (solo dev sobre http).
	InsecureCookies bool
	// HostDownAfter es el umbral global de hosts caidos (ARGOS_HOST_DOWN_AFTER); GET /groups lo informa
	// como el valor por defecto de los grupos sin umbral propio.
	HostDownAfter time.Duration
	// Dist es el catalogo verificado de binarios del agente (ARGOS_AGENT_DIST_DIR); nil = /dl e /install.*
	// responden 404 y el dashboard no ofrece comandos de instalacion.
	Dist *agentdist.Catalog
	// Secrets cifra los secretos de los canales (ARGOS_SECRET_KEY); nil = crear o editar secretos responde 503.
	Secrets *notify.Box
	// Webhook valida y envia los webhooks (con la politica anti-SSRF); nil = igual que Secrets nil.
	Webhook WebhookClient
	// Web es el build del frontend (ARGOS_WEB_DIR, ya acotado por os.Root); nil = el server no sirve la SPA.
	Web fs.FS
	// Status alimenta GET /system/status (version, arranque y latidos de las tareas periodicas).
	Status StatusSources
}

// Politica de sesion y bloqueo (decisiones aprobadas).
const (
	// SessionIdleTimeout es la inactividad maxima de una sesion (la purga horaria lo usa tambien).
	SessionIdleTimeout     = 30 * time.Minute
	sessionAbsoluteTimeout = 8 * time.Hour
	sessionTouchInterval   = time.Minute

	maxFailedLogins = 10
	accountLockTime = 15 * time.Minute
)

// API agrupa las dependencias de los handlers.
type API struct {
	store  Store
	hasher *auth.Hasher
	logger *slog.Logger
	now    func() time.Time
	opts   Options

	cookieName   string
	allowedHosts map[string]bool

	// cursorKey firma los cursores de paginacion (HMAC). Es aleatoria por proceso: un reinicio invalida los
	// cursores en vuelo (el cliente recibe 400 invalid_cursor y vuelve a la primera pagina).
	cursorKey []byte

	cmdOrigin string // origen de los comandos de instalacion ("" = no ofrecerlos)

	loginLimiter       *limiter      // por IP: 20 intentos / 10 min
	distLimiter        *limiter      // por IP: 30 / min (instaladores y descargas)
	enrollLimiter      *limiter      // por IP: 10 / min
	metricsLimiter     *limiter      // por host: 1 cada 2 s, rafaga 5
	inventoryLimiter   *limiter      // por host: 20 / min (3 modulos con minimo de 30 s entre envios completos)
	inventoryReadDedup *limiter      // por (usuario, host, modulo): 1 evento inventory_read cada 5 min
	inventoryDenyDedup *limiter      // por (usuario, ruta): 1 authz_denied cada 5 min por falta de can_inventory o can_updates
	updatesReadDedup   *limiter      // por (usuario, host o flota): 1 evento updates_read cada 5 min
	testLimiter        *limiter      // por usuario: 5 / min (POST /channels/{id}/test)
	reportSem          chan struct{} // cupos de informes de disponibilidad concurrentes
	exportLimiter      *limiter      // por usuario: 6 / min (CSV del historial)
	exports            *exportGate   // cupo propio de exportaciones CSV (1 por usuario, tope global)
	purgeSem           chan struct{} // un solo purge de grupo a la vez
	agentFails         failCounter

	status  statusTracker
	ingest  ingestGate
	limited limitedSampler

	routes []route
	health healthState
}

// New crea la API. now permite fijar el reloj en tests (usar time.Now en produccion). Entra en panico solo
// si el sistema no puede dar bytes aleatorios (no hay forma sensata de seguir sin ellos).
func New(s Store, hasher *auth.Hasher, logger *slog.Logger, now func() time.Time, opts Options) *API {
	cursorKey := make([]byte, 32)
	if _, err := rand.Read(cursorKey); err != nil {
		panic(fmt.Sprintf("leyendo crypto/rand: %v", err))
	}
	a := &API{
		cursorKey:          cursorKey,
		store:              s,
		hasher:             hasher,
		logger:             logger,
		now:                now,
		opts:               opts,
		cookieName:         "__Host-argos_sid",
		allowedHosts:       map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true},
		loginLimiter:       newLimiter(20, 10*time.Minute),
		enrollLimiter:      newLimiter(10, time.Minute),
		metricsLimiter:     newLimiter(5, 10*time.Second),
		inventoryLimiter:   newLimiter(20, time.Minute),
		inventoryReadDedup: newLimiter(1, invAuditWindow),
		inventoryDenyDedup: newLimiter(1, invAuditWindow),
		updatesReadDedup:   newLimiter(1, invAuditWindow),
		distLimiter:        newLimiter(distLimitBurst, time.Minute),
		testLimiter:        newLimiter(channelTestsPerMinute, time.Minute),
		reportSem:          make(chan struct{}, maxConcurrentReports),
		exportLimiter:      newLimiter(exportsPerMinute, time.Minute),
		exports:            newExportGate(maxConcurrentExports),
		purgeSem:           make(chan struct{}, 1),
		cmdOrigin:          commandOrigin(opts.PublicURL, opts.Dist != nil),
	}
	if opts.InsecureCookies {
		a.cookieName = "argos_sid"
	}
	if opts.PublicURL != nil {
		a.allowedHosts[normalizeHost(opts.PublicURL.Hostname())] = true
	}
	return a
}

// access declara quien puede llamar a una ruta. No hay valor por defecto: toda
// ruta se registra con uno explicito (deny by default).
type access int

const (
	accessPublic     access = iota // sin credenciales: health, login, enroll de agente
	accessAgent                    // bearer token de agente (nunca cookie)
	accessSessionAny               // sesion valida, incluso con cambio de contraseña pendiente
	accessSession                  // sesion valida (viewer o admin) sin cambio pendiente
	accessAdmin                    // sesion de admin sin cambio pendiente
)

type route struct {
	pattern string
	access  access
}

func (a *API) route(mux *http.ServeMux, pattern string, acc access, h handlerFunc) {
	a.routes = append(a.routes, route{pattern, acc})
	switch acc {
	case accessPublic, accessAgent:
	case accessSessionAny:
		h = a.requireSession(true, h)
	case accessSession:
		h = a.requireSession(false, h)
	case accessAdmin:
		h = a.requireSession(false, a.requireAdmin(h))
	}
	mux.HandleFunc(pattern, a.handle(h))
}

// Routes devuelve el handler completo: mux con todas las rutas envuelto en los
// middlewares de seguridad (cabeceras, Host, CSRF, Content-Type).
func (a *API) Routes() http.Handler {
	a.routes = nil
	mux := http.NewServeMux()

	a.route(mux, "GET /health", accessPublic, a.healthHandler)
	a.route(mux, "GET /api/v1/system/status", accessAdmin, a.systemStatus)

	// Agentes.
	a.route(mux, "POST /api/v1/agent/enroll", accessPublic, a.agentEnroll)
	a.route(mux, "POST /api/v1/agent/metrics", accessAgent, a.agentMetrics)
	a.route(mux, "POST /api/v1/agent/inventory", accessAgent, a.agentInventory)

	// Sesion.
	a.route(mux, "POST /api/v1/auth/login", accessPublic, a.login)
	a.route(mux, "POST /api/v1/auth/logout", accessSessionAny, a.logout)
	a.route(mux, "GET /api/v1/auth/me", accessSessionAny, a.me)
	a.route(mux, "POST /api/v1/auth/password", accessSessionAny, a.changePassword)

	// Dashboard.
	a.route(mux, "GET /api/v1/hosts", accessSession, a.listHosts)
	a.route(mux, "GET /api/v1/hosts/{id}", accessSession, a.getHost)
	a.route(mux, "GET /api/v1/hosts/{id}/metrics", accessSession, a.hostMetrics)
	a.route(mux, "GET /api/v1/hosts/{id}/history", accessSession, a.hostHistory)
	a.route(mux, "POST /api/v1/hosts/enroll-token", accessAdmin, a.createEnrollToken)
	a.route(mux, "GET /api/v1/hosts/enroll-tokens", accessAdmin, a.listEnrollTokens)
	a.route(mux, "DELETE /api/v1/hosts/enroll-tokens/{id}", accessAdmin, a.revokeEnrollToken)
	a.route(mux, "POST /api/v1/hosts/bulk", accessAdmin, a.bulkHosts)
	a.route(mux, "POST /api/v1/hosts/{id}/revoke", accessAdmin, a.revokeHost)
	a.route(mux, "POST /api/v1/hosts/{id}/purge", accessAdmin, a.purgeHost)
	a.route(mux, "PUT /api/v1/hosts/{id}/group", accessAdmin, a.setHostGroup)
	a.route(mux, "PUT /api/v1/hosts/{id}/name", accessAdmin, a.renameHost)
	a.route(mux, "PUT /api/v1/hosts/{id}/retention", accessAdmin, a.setHostRetention)
	a.route(mux, "GET /api/v1/settings/retention", accessAdmin, a.getRetention)
	a.route(mux, "PUT /api/v1/settings/retention", accessAdmin, a.putRetention)
	a.route(mux, "GET /api/v1/hosts/{id}/events", accessSession, a.hostEvents)
	a.route(mux, "GET /api/v1/hosts/{id}/inventory", accessSession, a.requireInventory(a.inventoryOverview))
	a.route(mux, "GET /api/v1/hosts/{id}/inventory/ports", accessSession, a.requireInventory(a.inventoryModule(store.InventoryPorts)))
	a.route(mux, "GET /api/v1/hosts/{id}/inventory/services", accessSession, a.requireInventory(a.inventoryModule(store.InventoryServices)))
	a.route(mux, "GET /api/v1/hosts/{id}/inventory/processes", accessSession, a.requireInventory(a.inventoryModule(store.InventoryProcesses)))
	a.route(mux, "GET /api/v1/hosts/{id}/inventory/changes", accessSession, a.requireInventory(a.inventoryChanges))
	a.route(mux, "GET /api/v1/hosts/{id}/updates", accessSession, a.requireUpdates(a.hostUpdates))
	a.route(mux, "GET /api/v1/updates", accessSession, a.requireUpdates(a.updatesFleet))
	a.route(mux, "GET /api/v1/hosts/{id}/service-checks", accessSession, a.hostServiceChecks)
	a.route(mux, "GET /api/v1/hosts/{id}/service-checks/{check_id}/events", accessSession, a.hostCheckEvents)
	a.route(mux, "GET /api/v1/devices", accessSession, a.listDevices)
	a.route(mux, "GET /api/v1/devices/{id}", accessSession, a.getDevice)
	a.route(mux, "POST /api/v1/devices", accessAdmin, a.createDevice)
	a.route(mux, "PATCH /api/v1/devices/{id}", accessAdmin, a.updateDevice)
	a.route(mux, "DELETE /api/v1/devices/{id}", accessAdmin, a.deleteDevice)
	a.route(mux, "GET /api/v1/snmp-profiles", accessAdmin, a.listSNMPProfiles)
	a.route(mux, "GET /api/v1/snmp-profiles/{id}", accessAdmin, a.getSNMPProfile)
	a.route(mux, "POST /api/v1/snmp-profiles", accessAdmin, a.createSNMPProfile)
	a.route(mux, "PATCH /api/v1/snmp-profiles/{id}", accessAdmin, a.updateSNMPProfile)
	a.route(mux, "DELETE /api/v1/snmp-profiles/{id}", accessAdmin, a.deleteSNMPProfile)
	a.route(mux, "GET /api/v1/summary", accessSession, a.summary)
	a.route(mux, "GET /api/v1/groups", accessSession, a.listGroups)
	a.route(mux, "POST /api/v1/groups", accessAdmin, a.createGroup)
	a.route(mux, "PATCH /api/v1/groups/{id}", accessAdmin, a.updateGroup)
	a.route(mux, "DELETE /api/v1/groups/{id}", accessAdmin, a.deleteGroup)
	a.route(mux, "POST /api/v1/groups/{id}/purge", accessAdmin, a.purgeGroup)

	// Alertas (lectura: cualquier sesion; ack: admin o viewer con can_ack; escritura y resolve: solo admin).
	a.route(mux, "GET /api/v1/reports/availability", accessSession, a.availabilityReport)
	a.route(mux, "GET /api/v1/reports/service-checks", accessSession, a.serviceChecksReport)
	a.route(mux, "GET /api/v1/reports/availability.csv", accessSession, a.availabilityCSV)
	a.route(mux, "GET /api/v1/reports/service-checks.csv", accessSession, a.serviceChecksCSV)
	a.route(mux, "GET /api/v1/reports/alerts.csv", accessSession, a.alertsCSV)
	a.route(mux, "GET /api/v1/alerts", accessSession, a.listAlerts)
	a.route(mux, "GET /api/v1/alerts/summary", accessSession, a.alertSummary)
	a.route(mux, "GET /api/v1/alerts/{id}", accessSession, a.getAlert)
	a.route(mux, "POST /api/v1/alerts/{id}/ack", accessSession, a.requireAck(a.ackAlert))
	a.route(mux, "POST /api/v1/alerts/{id}/resolve", accessAdmin, a.resolveAlert)
	a.route(mux, "GET /api/v1/alert-rules", accessSession, a.listAlertRules)
	a.route(mux, "GET /api/v1/alert-rules/{id}", accessSession, a.getAlertRule)
	a.route(mux, "POST /api/v1/alert-rules", accessAdmin, a.createAlertRule)
	a.route(mux, "PATCH /api/v1/alert-rules/{id}", accessAdmin, a.updateAlertRule)
	a.route(mux, "DELETE /api/v1/alert-rules/{id}", accessAdmin, a.deleteAlertRule)
	a.route(mux, "PUT /api/v1/alert-rules/{id}/channels", accessAdmin, a.setRuleChannels)
	a.route(mux, "GET /api/v1/channels", accessSession, a.requireFullScope(a.listChannels))
	a.route(mux, "GET /api/v1/channels/{id}", accessSession, a.requireFullScope(a.getChannel))
	a.route(mux, "POST /api/v1/channels", accessAdmin, a.createChannel)
	a.route(mux, "PATCH /api/v1/channels/{id}", accessAdmin, a.updateChannel)
	a.route(mux, "DELETE /api/v1/channels/{id}", accessAdmin, a.deleteChannel)
	a.route(mux, "POST /api/v1/channels/{id}/test", accessAdmin, a.testChannel)
	a.route(mux, "GET /api/v1/service-checks", accessSession, a.listServiceChecks)
	a.route(mux, "GET /api/v1/service-checks/{id}", accessSession, a.getServiceCheck)
	a.route(mux, "POST /api/v1/service-checks", accessAdmin, a.createServiceCheck)
	a.route(mux, "PATCH /api/v1/service-checks/{id}", accessAdmin, a.updateServiceCheck)
	a.route(mux, "DELETE /api/v1/service-checks/{id}", accessAdmin, a.deleteServiceCheck)
	a.route(mux, "GET /api/v1/maintenance", accessSession, a.listMaintenance)
	a.route(mux, "GET /api/v1/maintenance/{id}", accessSession, a.getMaintenance)
	a.route(mux, "POST /api/v1/maintenance", accessAdmin, a.createMaintenance)
	a.route(mux, "PATCH /api/v1/maintenance/{id}", accessAdmin, a.updateMaintenance)
	a.route(mux, "DELETE /api/v1/maintenance/{id}", accessAdmin, a.deleteMaintenance)

	a.route(mux, "GET /api/v1/users", accessAdmin, a.listUsers)
	a.route(mux, "POST /api/v1/users", accessAdmin, a.createUser)
	a.route(mux, "PATCH /api/v1/users/{id}", accessAdmin, a.updateUser)
	a.route(mux, "DELETE /api/v1/users/{id}", accessAdmin, a.deleteUser)

	a.route(mux, "GET /api/v1/audit", accessAdmin, a.listAudit)
	a.route(mux, "GET /api/v1/audit/events", accessAdmin, a.listAuditEvents)

	// Instaladores y descargas del agente (fuera de /api/).
	a.registerDist(mux)

	// Frontend compilado (ARGOS_WEB_DIR): al final, es el fallback de todo lo que no reclama otra ruta.
	a.registerWeb(mux)

	return a.requestLog(a.securityHeaders(a.hostAllowlist(a.crossOrigin()(a.requireJSON(mux)))))
}

// handlerFunc es un handler que devuelve su error; a.handle lo traduce a HTTP
// en un unico sitio.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

func (a *API) handle(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			a.writeErr(w, r, err)
		}
	}
}

// writeErr traduce un error a la respuesta HTTP. Todo lo desconocido es 500:
// se loguea el error real y al cliente solo va un mensaje generico.
func (a *API) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	var ce *clientError
	var weak *auth.WeakPasswordError
	switch {
	case errors.As(err, &ce):
		if ce.retryAfter > 0 {
			w.Header().Set("Retry-After", retryAfterSeconds(ce.retryAfter))
		}
		writeErrorDetails(w, ce.status, ce.code, ce.msg, ce.details)
	case errors.As(err, &weak):
		writeError(w, http.StatusBadRequest, "weak_password", weak.Reason)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "recurso no encontrado")
	case errors.Is(err, store.ErrNeedFull):
		writeError(w, http.StatusConflict, "need_full", "se necesita el inventario completo")
	case errors.Is(err, store.ErrUserExists):
		writeError(w, http.StatusConflict, "username_taken", "ese nombre de usuario ya existe")
	case errors.Is(err, store.ErrInvalidScope):
		writeError(w, http.StatusBadRequest, "invalid_scope",
			"los permisos por grupo no aplican a admins, y group_ids exige all_groups=false")
	case errors.Is(err, store.ErrLastAdmin):
		writeError(w, http.StatusConflict, "last_admin", "debe quedar al menos un admin activo")
	case errors.Is(err, store.ErrSelfDelete):
		writeError(w, http.StatusConflict, "self_delete", "no puedes borrar tu propia cuenta")
	case errors.Is(err, store.ErrChannelScopeMismatch):
		writeErrorDetails(w, http.StatusUnprocessableEntity, "channel_scope_mismatch",
			"un canal de un grupo solo puede usarse en reglas cuyo alcance quede dentro de ese grupo", channelScopeDetails(err))
	case errors.Is(err, store.ErrTooManyTokens):
		writeError(w, http.StatusConflict, "too_many_tokens", "hay demasiados enroll tokens activos, espera a que expiren")
	case errors.Is(err, store.ErrGroupNotFound):
		writeError(w, http.StatusUnprocessableEntity, "group_not_found", "el grupo indicado no existe")
	case errors.Is(err, store.ErrGroupExists):
		writeError(w, http.StatusConflict, "group_exists", "ya existe un grupo con ese nombre")
	case errors.Is(err, store.ErrGroupNotEmpty):
		writeError(w, http.StatusConflict, "group_not_empty", "el grupo tiene hosts: muevelos a otro grupo antes de borrarlo")
	case errors.Is(err, store.ErrTooManyGroups):
		writeError(w, http.StatusConflict, "too_many_groups", fmt.Sprintf("se alcanzo el maximo de %d grupos", store.MaxGroups))
	case errors.Is(err, store.ErrAlertResolved):
		writeError(w, http.StatusConflict, "alert_resolved", "la alerta ya esta resuelta")
	case errors.Is(err, store.ErrRuleExists):
		writeError(w, http.StatusConflict, "rule_exists", "ya existe una regla con ese nombre")
	case errors.Is(err, store.ErrTooManyRules):
		writeError(w, http.StatusConflict, "too_many_rules", fmt.Sprintf("se alcanzo el maximo de %d reglas", store.MaxAlertRules))
	case errors.Is(err, store.ErrTooManyWindows):
		writeError(w, http.StatusConflict, "too_many_maintenance",
			fmt.Sprintf("se alcanzo el maximo de %d ventanas de mantenimiento activas o programadas", store.MaxActiveMaintenance))
	case errors.Is(err, store.ErrChannelExists):
		writeError(w, http.StatusConflict, "channel_exists", "ya existe un canal con ese nombre")
	case errors.Is(err, store.ErrTooManyChannels):
		writeError(w, http.StatusConflict, "too_many_channels", fmt.Sprintf("se alcanzo el maximo de %d canales", store.MaxChannels))
	case errors.Is(err, store.ErrChannelNotFound):
		writeError(w, http.StatusUnprocessableEntity, "channel_not_found", "alguno de los canales indicados no existe")
	case errors.Is(err, store.ErrTooManyRuleChannels):
		writeError(w, http.StatusConflict, "too_many_rule_channels", fmt.Sprintf("una regla admite como maximo %d canales", store.MaxChannelsPerRule))
	case errors.Is(err, store.ErrCheckExists):
		writeError(w, http.StatusConflict, "check_exists", "ya existe un chequeo con ese nombre")
	case errors.Is(err, store.ErrTooManyChecks):
		writeError(w, http.StatusConflict, "too_many_checks", fmt.Sprintf("se alcanzo el maximo de %d chequeos", store.MaxServiceChecks))
	case errors.Is(err, store.ErrCheckNotFound):
		writeError(w, http.StatusUnprocessableEntity, "check_not_found", "el chequeo indicado no existe")
	case errors.Is(err, store.ErrDeviceNotFound):
		writeError(w, http.StatusUnprocessableEntity, "device_not_found", "el dispositivo indicado no existe")
	case errors.Is(err, store.ErrScopeHostNotFound):
		writeError(w, http.StatusUnprocessableEntity, "host_not_found", "el host indicado no existe o no tiene agente")
	case errors.Is(err, store.ErrCollectorGroupMismatch):
		writeErrorDetails(w, http.StatusUnprocessableEntity, "collector_group_mismatch",
			"el dispositivo y su colector deben estar en el mismo grupo", groupRuleDetails(err))
	case errors.Is(err, store.ErrCollectorHasDevices):
		writeErrorDetails(w, http.StatusConflict, "collector_has_devices",
			"el host es colector de dispositivos: muevelos a la vez, cambiales el colector o borralos antes", groupRuleDetails(err))
	case errors.Is(err, store.ErrPurgeConfirm):
		writeError(w, http.StatusBadRequest, "confirm_mismatch", "el nombre de confirmacion no coincide")
	case errors.Is(err, store.ErrCollectorInvalid):
		writeError(w, http.StatusUnprocessableEntity, "collector_invalid", "el colector debe ser un host con agente activo")
	case errors.Is(err, store.ErrDeviceExists):
		writeError(w, http.StatusConflict, "device_exists", "ya existe un dispositivo con esa direccion en ese colector")
	case errors.Is(err, store.ErrTooManyDevices):
		writeError(w, http.StatusConflict, "too_many_devices", fmt.Sprintf("se alcanzo el maximo de %d dispositivos", store.MaxDevices))
	case errors.Is(err, store.ErrProfileExists):
		writeError(w, http.StatusConflict, "profile_exists", "ya existe un perfil snmp con ese nombre")
	case errors.Is(err, store.ErrTooManyProfiles):
		writeError(w, http.StatusConflict, "too_many_profiles", fmt.Sprintf("se alcanzo el maximo de %d perfiles snmp", store.MaxSNMPProfiles))
	case errors.Is(err, store.ErrProfileNotFound):
		writeError(w, http.StatusUnprocessableEntity, "profile_not_found", "el perfil snmp indicado no existe")
	case errors.Is(err, store.ErrProfileInUse):
		writeError(w, http.StatusConflict, "profile_in_use", "el perfil snmp esta asignado a dispositivos: quitalo de ellos antes de borrarlo")
	case errors.Is(err, store.ErrUnknownHosts):
		writeError(w, http.StatusUnprocessableEntity, "unknown_hosts", "alguno de los hosts indicados no existe")
	case errors.Is(err, store.ErrPartitionMissing):
		// Falta la particion del timestamp (el mantenimiento no la creo): problema del servidor, no de la
		// muestra. 503 pa que el agente reintente; se loguea el error real.
		a.logger.ErrorContext(r.Context(), "sin particion pa el timestamp de la muestra",
			"method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "servicio no disponible temporalmente, reintenta")
	case errors.Is(err, context.Canceled):
		// El cliente corto la conexion: no es un error del servidor.
		a.logger.DebugContext(r.Context(), "solicitud cancelada por el cliente", "method", r.Method, "path", r.URL.Path)
		writeError(w, http.StatusServiceUnavailable, "canceled", "solicitud cancelada")
	default:
		a.logger.ErrorContext(r.Context(), "error interno",
			"method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "error interno del servidor")
	}
}
