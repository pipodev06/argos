package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"argos/server/internal/store"
)

// Limites de entrada.
const (
	maxSmallBodyBytes = 4 << 10   // enroll-token y enroll
	maxMetricsBytes   = 256 << 10 // ingest de metricas
	maxHostnameLen    = 253       // largo maximo de un FQDN
	maxOSArchLen      = 64
	maxEnrollTokenLen = 128
	maxBearerLen      = 256
	maxServices       = 50
	maxServiceName    = 100

	enrollTokenTTL = 15 * time.Minute

	// Ventana aceptada pa el timestamp del agente: el pasado cubre el buffer offline;
	// el futuro solo tolera desfase de reloj.
	maxTimestampAge  = 7 * 24 * time.Hour
	maxTimestampSkew = 5 * time.Minute
)

// Vocabulario de status de servicio. Debe coincidir con el CHECK de la migracion 00003.
var validServiceStatus = map[string]bool{"up": true, "down": true, "warn": true, "unknown": true}

func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("leyendo crypto/rand: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// hashToken devuelve el SHA-256 hex del token: en la BD solo se guarda el hash.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// --- dashboard (admin): crear enroll token pa un host nuevo ---

type createEnrollTokenReq struct {
	Hostname string `json:"hostname"`
	// GroupID (opcional): grupo con el que queda el host que canjee el token.
	GroupID *string `json:"group_id"`
}

func (a *API) createEnrollToken(w http.ResponseWriter, r *http.Request) error {
	admin := currentUser(r)
	var req createEnrollTokenReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	req.Hostname = strings.TrimSpace(req.Hostname)
	if err := checkText("hostname", req.Hostname, maxHostnameLen); err != nil {
		return err
	}
	if req.GroupID != nil && !isUUID(*req.GroupID) {
		return badRequest("group_id invalido")
	}

	token, err := randomToken(16)
	if err != nil {
		return fmt.Errorf("generando enroll token: %w", err)
	}
	expiresAt := a.now().Add(enrollTokenTTL)
	// En la BD solo va el hash: el token en claro se entrega una unica vez, en esta respuesta.
	if err := a.store.CreateEnrollToken(r.Context(), hashToken(token), req.Hostname, admin.ID, req.GroupID, expiresAt); err != nil {
		return err
	}

	e := actor(admin)
	e.Event, e.Outcome, e.TargetType = evEnrollTokenCreated, store.OutcomeOK, "enroll_token"
	a.audit(r, e, map[string]any{"hostname": req.Hostname, "group_id": req.GroupID})

	resp := map[string]any{
		"enroll_token": token,
		"expires_at":   expiresAt,
	}
	if a.cmdOrigin != "" {
		resp["commands"] = installCommands(a.cmdOrigin, token)
	}
	writeJSON(w, http.StatusCreated, resp)
	return nil
}

// pendingEnrollTokenDTO es un enroll token vigente sin usar. Nunca lleva el token ni su hash (id es un identificador
// aparte, solo pa revocarlo).
type pendingEnrollTokenDTO struct {
	ID        string       `json:"id"`
	Hostname  string       `json:"hostname"`
	Group     *groupRefDTO `json:"group"`
	CreatedBy *string      `json:"created_by"`
	CreatedAt time.Time    `json:"created_at"`
	ExpiresAt time.Time    `json:"expires_at"`
}

// listEnrollTokens lista los enroll tokens vigentes sin usar (solo admin).
func (a *API) listEnrollTokens(w http.ResponseWriter, r *http.Request) error {
	tokens, err := a.store.ListPendingEnrollTokens(r.Context())
	if err != nil {
		return err
	}
	items := make([]pendingEnrollTokenDTO, 0, len(tokens))
	for _, t := range tokens {
		items = append(items, pendingEnrollTokenDTO{ID: t.ID, Hostname: t.Hostname, Group: toGroupRefDTO(t.Group),
			CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt.UTC(), ExpiresAt: t.ExpiresAt.UTC()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

// revokeEnrollToken revoca un enroll token pendiente (solo admin). 404 si no existe, ya se uso o expiro.
func (a *API) revokeEnrollToken(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	hostname, err := a.store.RevokeEnrollToken(r.Context(), id)
	if err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evEnrollTokenRevoked, store.OutcomeOK, "enroll_token", id
	a.audit(r, e, map[string]any{"hostname": hostname})
	writeJSON(w, http.StatusOK, map[string]any{})
	return nil
}

// --- agente: enroll ---

type enrollReq struct {
	EnrollToken string `json:"enroll_token"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
}

func (a *API) agentEnroll(w http.ResponseWriter, r *http.Request) error {
	ip := a.clientIP(r)
	if ok, wait := a.enrollLimiter.allow(ipKey(ip), a.now()); !ok {
		a.agentFails.add(evEnrollFail)
		return rateLimited(wait)
	}

	var req enrollReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if err := checkText("enroll_token", req.EnrollToken, maxEnrollTokenLen); err != nil {
		return err
	}
	// os y arch son opcionales.
	if req.OS != "" {
		if err := checkText("os", req.OS, maxOSArchLen); err != nil {
			return err
		}
	}
	if req.Arch != "" {
		if err := checkText("arch", req.Arch, maxOSArchLen); err != nil {
			return err
		}
	}

	agentToken, err := randomToken(32)
	if err != nil {
		return fmt.Errorf("generando agent token: %w", err)
	}
	hostID, err := a.store.EnrollHost(r.Context(), hashToken(req.EnrollToken), req.OS, req.Arch, hashToken(agentToken))
	if err != nil {
		if reason, ok := enrollRejection(err); ok {
			// Una sola respuesta externa pa token inexistente/usado/expirado: el motivo real solo va al log.
			a.logger.WarnContext(r.Context(), "enroll rechazado", "reason", reason, "ip", ip.String())
			a.agentFails.add(evEnrollFail)
			return errInvalidAgentToken
		}
		return err
	}
	a.audit(r, store.AuditEntry{Event: evHostEnrolled, Outcome: store.OutcomeOK, TargetType: "host", TargetID: hostID}, nil)

	writeJSON(w, http.StatusCreated, map[string]any{
		"host_id":     hostID,
		"agent_token": agentToken,
	})
	return nil
}

// --- agente: autenticacion por bearer token contra hosts.agent_token_hash ---

// enrollRejection reconoce los errores de token de enroll que son culpa del cliente.
func enrollRejection(err error) (reason string, ok bool) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "not_found", true
	case errors.Is(err, store.ErrTokenUsed):
		return "used", true
	case errors.Is(err, store.ErrTokenExpired):
		return "expired", true
	}
	return "", false
}

// authenticateAgent devuelve el host dueno del bearer token. Un token ausente,
// malformado, desconocido o revocado es 401 (y suma al agregado de auditoria);
// cualquier otro error (infraestructura) se propaga y termina en 500.
func (a *API) authenticateAgent(r *http.Request) (string, error) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" || len(token) > maxBearerLen {
		a.agentFails.add(evAgentAuthFail)
		return "", errInvalidAgentToken
	}
	hostID, err := a.store.HostIDByTokenHash(r.Context(), hashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		a.agentFails.add(evAgentAuthFail)
		return "", errInvalidAgentToken
	}
	if err != nil {
		return "", err
	}
	return hostID, nil
}

// --- agente: ingest de metricas ---

// serviceCheckPayload es una entrada de `services`: legada (solo name) o resultado de un chequeo asignado (check_id,
// value, detail, ts). No se confia en ningun host_id del body.
type serviceCheckPayload struct {
	CheckID    string   `json:"check_id"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	ResponseMs int      `json:"response_ms"`
	Value      *float64 `json:"value"`
	Detail     string   `json:"detail"`
	TS         int64    `json:"ts"`
}

// metricsPayload son las metricas de un reporte. Los campos nuevos (swap, load, battery) son punteros:
// ausente = sin dato, nunca cero. Un agente viejo no manda ninguno.
type metricsPayload struct {
	CPUPct     float64 `json:"cpu_pct"`
	MemUsedMB  int     `json:"mem_used_mb"`
	MemTotalMB int     `json:"mem_total_mb"`
	Net        struct {
		RxBytes int64 `json:"rx_bytes"`
		TxBytes int64 `json:"tx_bytes"`
	} `json:"net"`
	Swap *struct {
		UsedMB  *int64 `json:"used_mb"`
		TotalMB *int64 `json:"total_mb"`
	} `json:"swap"`
	Load *struct {
		L1  *float64 `json:"l1"`
		L5  *float64 `json:"l5"`
		L15 *float64 `json:"l15"`
	} `json:"load"`
	Battery *struct {
		Pct      *int64 `json:"pct"`
		Charging *bool  `json:"charging"`
	} `json:"battery"`
}

// metricsIngestReq es el cuerpo de POST /agent/metrics. disks y host_info tambien son opcionales: el agente
// manda disks cada ~5 min y host_info solo cuando quiere; un agente viejo no manda ninguno.
type metricsIngestReq struct {
	Timestamp int64                 `json:"timestamp"`
	Metrics   metricsPayload        `json:"metrics"`
	Disks     []diskPayload         `json:"disks"`
	HostInfo  *hostInfoPayload      `json:"host_info"`
	Services  []serviceCheckPayload `json:"services"`
	// ChecksRev es la revision de la lista de chequeos que tiene el agente ("" = nunca recibio).
	ChecksRev string `json:"checks_rev"`
}

func (a *API) agentMetrics(w http.ResponseWriter, r *http.Request) error {
	// Auth antes de leer el body: un token invalido no cuesta parsear nada.
	hostID, err := a.authenticateAgent(r)
	if err != nil {
		return err
	}
	if ok, wait := a.metricsLimiter.allow(hostID, a.now()); !ok {
		return rateLimited(wait)
	}

	// Sin DisallowUnknownFields a proposito: el agente puede ir un release por delante
	// del server y mandar campos nuevos (agent_version, hostname...) sin perder metricas.
	var req metricsIngestReq
	if err := decodeJSON(w, r, &req, maxMetricsBytes, false); err != nil {
		return err
	}
	sample, err := a.validateSample(&req)
	if err != nil {
		return err
	}
	if a.ingest.tooSoon(hostID, sample.Timestamp) {
		// Se acepta y se descarta en silencio (200): un 4xx/5xx haria que el agente reintente o acumule en su buffer.
		if n := a.ingest.noteDrop(a.now()); n > 0 {
			a.logger.InfoContext(r.Context(), "muestras descartadas por frecuencia excesiva (>1 cada 4 s por host)", "count", n, "window", dropLogEvery.String())
		}
		resp := map[string]any{"received": true, "dropped": true}
		a.attachChecks(r.Context(), hostID, req.ChecksRev, resp)
		writeJSON(w, http.StatusOK, resp)
		return nil
	}
	res, err := a.store.IngestMetrics(r.Context(), hostID, sample)
	if err != nil {
		return err
	}
	a.ingest.accepted(hostID, sample.Timestamp)
	if res.Recovered {
		// Un host down que vuelve es noticia; un offline que vuelve (laptop que despierta) es rutina.
		level := slog.LevelDebug
		if res.PreviousStatus == "down" {
			level = slog.LevelInfo
		}
		a.logger.Log(r.Context(), level, "host recuperado", "host_id", hostID, "hostname", res.Hostname, "previous_status", res.PreviousStatus)
	}

	resp := map[string]any{"received": true}
	a.attachChecks(r.Context(), hostID, req.ChecksRev, resp)
	writeJSON(w, http.StatusOK, resp)
	return nil
}

func (a *API) validateSample(req *metricsIngestReq) (store.Sample, error) {
	now := a.now()
	ts := now
	if req.Timestamp != 0 {
		ts = time.Unix(req.Timestamp, 0)
		if ts.Before(now.Add(-maxTimestampAge)) || ts.After(now.Add(maxTimestampSkew)) {
			return store.Sample{}, badRequest("timestamp fuera de rango")
		}
	}

	m := req.Metrics
	switch {
	case m.CPUPct < 0 || m.CPUPct > 100:
		return store.Sample{}, badRequest("cpu_pct fuera de rango 0-100")
	case m.MemUsedMB < 0 || m.MemUsedMB > math.MaxInt32,
		m.MemTotalMB < 0 || m.MemTotalMB > math.MaxInt32:
		return store.Sample{}, badRequest("mem_used_mb/mem_total_mb fuera de rango")
	case m.MemUsedMB > m.MemTotalMB:
		return store.Sample{}, badRequest("mem_used_mb no puede superar mem_total_mb")
	case m.Net.RxBytes < 0 || m.Net.TxBytes < 0:
		return store.Sample{}, badRequest("net.rx_bytes/net.tx_bytes no pueden ser negativos")
	}

	legacy, results, err := validateServices(req.Services, now)
	if err != nil {
		return store.Sample{}, err
	}

	sample := store.Sample{
		Timestamp:    ts,
		CPUPct:       m.CPUPct,
		MemUsedMB:    m.MemUsedMB,
		MemTotalMB:   m.MemTotalMB,
		NetRxBytes:   m.Net.RxBytes,
		NetTxBytes:   m.Net.TxBytes,
		Services:     legacy,
		CheckResults: results,
	}
	if err := validateExtras(&req.Metrics, &sample); err != nil {
		return store.Sample{}, err
	}
	if sample.Disks, err = validateDisks(req.Disks); err != nil {
		return store.Sample{}, err
	}
	if sample.Info, err = validateHostInfo(req.HostInfo, now); err != nil {
		return store.Sample{}, err
	}
	return sample, nil
}

// revokeHost revoca el token del agente y deshabilita el host (idempotente).
func (a *API) revokeHost(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	if err := a.store.RevokeHost(r.Context(), id); err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evHostRevoked, store.OutcomeOK, "host", id
	a.audit(r, e, nil)
	writeJSON(w, http.StatusOK, map[string]any{})
	return nil
}
