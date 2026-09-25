package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"argos/server/internal/store"
)

// Rangos de los campos de un chequeo: alineados con los CHECK de la migracion 00017.
const (
	maxCheckNameLen     = 80
	minCheckInterval    = 30
	maxCheckInterval    = 86400
	minCheckTimeoutMs   = 200
	maxCheckTimeoutMs   = 30000
	maxCheckRetries     = 3
	maxCheckTargetHost  = 253
	maxCheckURLLen      = 512
	maxCheckExpectText  = 100
	maxCheckServiceName = 64
	maxCheckLatencyMs   = 60000
	maxCheckWarnDays    = 365
	maxCheckSecretLen   = 512
	maxSNMPIfIndex      = 65535
	defaultCheckStatus  = "200-399"
)

var (
	checkHostRe    = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	checkServiceRe = regexp.MustCompile(`^[A-Za-z0-9@._:-]{1,64}$`)
	checkStatusRe  = regexp.MustCompile(`^([0-9]{3})(?:-([0-9]{3}))?$`)

	validCheckTypes   = map[string]bool{store.CheckTCP: true, store.CheckHTTP: true, store.CheckTLSCert: true, store.CheckService: true, store.CheckPing: true, store.CheckSNMP: true}
	validCheckHeaders = map[string]bool{"Authorization": true, "X-API-Key": true, "X-Auth-Token": true}

	// secretQueryKeys son fragmentos de nombre de parametro de query que delatan una credencial: el token va en el
	// campo secreto, nunca en la URL (que se guarda en claro, se lista y se audita por host).
	secretQueryKeys = []string{"token", "key", "secret", "pass", "pwd", "auth", "sig", "cred", "bearer", "jwt", "session", "sid"}
)

// kindServiceCheck es el AAD (tipo) con el que se cifra el secreto de un chequeo.
const kindServiceCheck = "service_check"

// --- DTOs ---

// serviceCheckDTO es la definicion de un chequeo. El secreto no se devuelve nunca (solo has_secret). header_name y
// la query de la URL solo los ve un admin.
type serviceCheckDTO struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	IntervalSeconds int       `json:"interval_seconds"`
	TimeoutMs       int       `json:"timeout_ms"`
	Retries         int       `json:"retries"`
	TargetHost      *string   `json:"target_host"`
	TargetPort      *int      `json:"target_port"`
	URL             *string   `json:"url"`
	ExpectStatus    *string   `json:"expect_status"`
	ExpectText      *string   `json:"expect_text"`
	ServiceName     *string   `json:"service_name"`
	WarnMs          *int      `json:"warn_ms"`
	CritMs          *int      `json:"crit_ms"`
	WarnDays        *int      `json:"warn_days"`
	CritDays        *int      `json:"crit_days"`
	HeaderName      *string   `json:"header_name,omitempty"`
	Metric          *string   `json:"metric"`
	IfIndex         *int      `json:"if_index"`
	HasSecret       bool      `json:"has_secret"`
	Scope           scopeDTO  `json:"scope"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func toServiceCheckDTO(u *sessionUser, d store.ServiceCheckDef) serviceCheckDTO {
	admin := u != nil && u.Role == store.RoleAdmin
	dto := serviceCheckDTO{
		ID: d.ID, Name: d.Name, Type: d.Type, IntervalSeconds: d.IntervalSeconds, TimeoutMs: d.TimeoutMs, Retries: d.Retries,
		TargetHost: d.TargetHost, TargetPort: d.TargetPort, URL: d.URL, ExpectStatus: d.ExpectStatus, ExpectText: d.ExpectText,
		ServiceName: d.ServiceName, WarnMs: d.WarnMs, CritMs: d.CritMs, WarnDays: d.WarnDays, CritDays: d.CritDays,
		Metric: d.Metric, IfIndex: d.IfIndex, HasSecret: len(d.SecretEnc) > 0, Scope: toScopeDTO(d.Scope, d.GroupName, d.DeviceName), Enabled: d.Enabled,
		CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	if admin {
		dto.HeaderName = d.HeaderName
	} else if d.URL != nil {
		stripped := stripQuery(*d.URL)
		dto.URL = &stripped
	}
	return dto
}

// stripQuery quita la query y el fragmento de una URL (lo que puede llevar datos sensibles).
func stripQuery(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return u.String()
}

// checkTarget es el destino legible de un chequeo: host:puerto, URL sin query o nombre del servicio.
func checkTarget(d store.ServiceCheckDef) string {
	switch {
	case d.URL != nil:
		return stripQuery(*d.URL)
	case d.TargetHost != nil && d.TargetPort != nil:
		return *d.TargetHost + ":" + strconv.Itoa(*d.TargetPort)
	case d.ServiceName != nil:
		return *d.ServiceName
	}
	return ""
}

// --- validacion ---

// validateCheckDef comprueba una definicion completa (nueva o ya fusionada con un PATCH) con las mismas reglas
// que los CHECK de la BD y normaliza el nombre. Los campos que no son del tipo deben faltar. No mira el secreto.
func validateCheckDef(d *store.ServiceCheckDef) error {
	name, err := checkName("name", d.Name, maxCheckNameLen)
	if err != nil {
		return err
	}
	d.Name = name
	if !validCheckTypes[d.Type] {
		return badRequest("type debe ser tcp, http, tls_cert, service, ping o snmp")
	}
	if d.IntervalSeconds < minCheckInterval || d.IntervalSeconds > maxCheckInterval {
		return badRequest("interval_seconds debe estar entre %d y %d", minCheckInterval, maxCheckInterval)
	}
	if d.TimeoutMs < minCheckTimeoutMs || d.TimeoutMs > maxCheckTimeoutMs {
		return badRequest("timeout_ms debe estar entre %d y %d", minCheckTimeoutMs, maxCheckTimeoutMs)
	}
	if d.TimeoutMs > d.IntervalSeconds*1000 {
		return badRequest("timeout_ms no puede superar el intervalo")
	}
	if d.Retries < 0 || d.Retries > maxCheckRetries {
		return badRequest("retries debe estar entre 0 y %d", maxCheckRetries)
	}
	if err := validateCheckShape(d); err != nil {
		return err
	}
	if d.Scope.Type == store.ScopeDevice && d.Type != store.CheckPing && d.Type != store.CheckSNMP {
		return badRequest("scope device solo aplica a chequeos ping y snmp")
	}
	if d.Scope.Type == store.ScopeHost && (d.Type == store.CheckPing || d.Type == store.CheckSNMP) {
		return badRequest("scope host no aplica a chequeos ping ni snmp (usa scope device)")
	}
	if d.WarnMs != nil && d.CritMs != nil && *d.WarnMs >= *d.CritMs {
		return badRequest("warn_ms debe ser menor que crit_ms")
	}
	return nil
}

func validateCheckShape(d *store.ServiceCheckDef) error {
	absent := func(fields map[string]bool) error {
		for f, present := range fields {
			if present {
				return badRequest("%s no aplica a un chequeo %s", f, d.Type)
			}
		}
		return nil
	}
	target := map[string]bool{"target_host": d.TargetHost != nil, "target_port": d.TargetPort != nil}
	latency := map[string]bool{"warn_ms": d.WarnMs != nil, "crit_ms": d.CritMs != nil}
	days := map[string]bool{"warn_days": d.WarnDays != nil, "crit_days": d.CritDays != nil}
	httpOnly := map[string]bool{"url": d.URL != nil, "expect_status": d.ExpectStatus != nil, "expect_text": d.ExpectText != nil,
		"header_name": d.HeaderName != nil}
	svc := map[string]bool{"service_name": d.ServiceName != nil}

	merge := func(ms ...map[string]bool) map[string]bool {
		out := map[string]bool{}
		for _, m := range ms {
			for k, v := range m {
				out[k] = v
			}
		}
		return out
	}
	if d.Type != store.CheckSNMP && (d.Metric != nil || d.IfIndex != nil) {
		return badRequest("metric e if_index solo aplican a chequeos snmp")
	}
	switch d.Type {
	case store.CheckPing:
		// El destino es la direccion del dispositivo (nunca un campo del chequeo).
		if err := absent(merge(target, days, httpOnly, svc)); err != nil {
			return err
		}
		return validateLatency(d)
	case store.CheckSNMP:
		// El destino es la direccion del dispositivo y las credenciales salen de su perfil SNMP.
		if err := absent(merge(target, latency, days, httpOnly, svc)); err != nil {
			return err
		}
		return validateSNMP(d)
	case store.CheckTCP:
		if err := absent(merge(days, httpOnly, svc)); err != nil {
			return err
		}
		return validateTarget(d)
	case store.CheckHTTP:
		if err := absent(merge(target, days, svc)); err != nil {
			return err
		}
		return validateHTTP(d)
	case store.CheckTLSCert:
		if err := absent(merge(latency, httpOnly, svc)); err != nil {
			return err
		}
		if err := validateTarget(d); err != nil {
			return err
		}
		if d.WarnDays == nil || d.CritDays == nil {
			return badRequest("warn_days y crit_days son obligatorios")
		}
		if *d.WarnDays < 1 || *d.WarnDays > maxCheckWarnDays || *d.CritDays < 0 || *d.CritDays > maxCheckWarnDays {
			return badRequest("warn_days debe estar entre 1 y %d y crit_days entre 0 y %d", maxCheckWarnDays, maxCheckWarnDays)
		}
		if *d.CritDays >= *d.WarnDays {
			return badRequest("crit_days debe ser menor que warn_days")
		}
	default: // service
		if err := absent(merge(target, latency, days, httpOnly)); err != nil {
			return err
		}
		if d.ServiceName == nil || !checkServiceRe.MatchString(*d.ServiceName) {
			return badRequest("service_name requerido: 1 a %d caracteres A-Z a-z 0-9 @ . _ : -", maxCheckServiceName)
		}
	}
	return nil
}

// validateSNMP comprueba la metrica de un chequeo snmp: reachable (sin if_index) o if_oper (con if_index 1-65535).
func validateSNMP(d *store.ServiceCheckDef) error {
	if d.Metric == nil {
		return badRequest("metric requerido: reachable o if_oper")
	}
	switch *d.Metric {
	case store.SNMPMetricReachable:
		if d.IfIndex != nil {
			return badRequest("if_index no aplica a la metrica reachable")
		}
	case store.SNMPMetricIfOper:
		if d.IfIndex == nil || *d.IfIndex < 1 || *d.IfIndex > maxSNMPIfIndex {
			return badRequest("if_index requerido entre 1 y %d con la metrica if_oper", maxSNMPIfIndex)
		}
	default:
		return badRequest("metric debe ser reachable o if_oper")
	}
	return nil
}

func validateTarget(d *store.ServiceCheckDef) error {
	if d.TargetHost == nil || len(*d.TargetHost) == 0 || len(*d.TargetHost) > maxCheckTargetHost || !checkHostRe.MatchString(*d.TargetHost) {
		return badRequest("target_host requerido: 1 a %d caracteres A-Z a-z 0-9 . _ : -", maxCheckTargetHost)
	}
	if d.TargetPort == nil || *d.TargetPort < 1 || *d.TargetPort > 65535 {
		return badRequest("target_port requerido entre 1 y 65535")
	}
	return validateLatency(d)
}

func validateLatency(d *store.ServiceCheckDef) error {
	for name, v := range map[string]*int{"warn_ms": d.WarnMs, "crit_ms": d.CritMs} {
		if v != nil && (*v < 1 || *v > maxCheckLatencyMs) {
			return badRequest("%s debe estar entre 1 y %d", name, maxCheckLatencyMs)
		}
	}
	return nil
}

func validateHTTP(d *store.ServiceCheckDef) error {
	if d.URL == nil {
		return badRequest("url requerida")
	}
	if err := checkCheckURL(*d.URL); err != nil {
		return err
	}
	if d.ExpectStatus == nil {
		def := defaultCheckStatus
		d.ExpectStatus = &def
	}
	if err := checkExpectStatus(*d.ExpectStatus); err != nil {
		return err
	}
	if d.ExpectText != nil {
		if *d.ExpectText == "" {
			return badRequest("expect_text vacio: usa null para quitarlo")
		}
		if err := checkOptionalText("expect_text", *d.ExpectText, maxCheckExpectText); err != nil {
			return err
		}
	}
	return validateLatency(d)
}

func checkExpectStatus(s string) error {
	m := checkStatusRe.FindStringSubmatch(s)
	if m == nil {
		return badRequest(`expect_status debe ser un codigo ("200") o un rango ("200-399")`)
	}
	lo, _ := strconv.Atoi(m[1])
	hi := lo
	if m[2] != "" {
		hi, _ = strconv.Atoi(m[2])
	}
	if lo < 100 || hi > 599 || lo > hi {
		return badRequest("expect_status fuera de rango 100-599")
	}
	return nil
}

// checkCheckURL valida la forma de la URL de un chequeo http: esquema http/https, host, sin credenciales
// (usuario:clave), sin fragmento y sin parametros de query que parezcan una credencial (el token va en `secret`).
// El error nunca repite la URL.
func checkCheckURL(raw string) error {
	if len(raw) < 8 || len(raw) > maxCheckURLLen {
		return badRequest("url debe tener entre 8 y %d caracteres", maxCheckURLLen)
	}
	if strings.IndexFunc(raw, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0 || hasControl(raw) {
		return badRequest("url contiene espacios o caracteres invalidos")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return badRequest("url debe ser http:// o https:// con host")
	}
	if !checkHostRe.MatchString(u.Hostname()) {
		return badRequest("url tiene un host invalido")
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return badRequest("url tiene un puerto invalido")
		}
	}
	if u.User != nil {
		return badRequest("url no puede llevar credenciales: usa el campo secret")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return badRequest("url no puede llevar fragmento (#)")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return badRequest("url tiene una query invalida")
	}
	for k := range q {
		lk := strings.ToLower(k)
		for _, frag := range secretQueryKeys {
			if strings.Contains(lk, frag) {
				return badRequest("la query de la url no puede llevar tokens ni credenciales: usa el campo secret")
			}
		}
	}
	return nil
}

func checkSecret(s string) error {
	if s == "" {
		return badRequest("secret vacio: usa null para quitarlo")
	}
	return checkOptionalText("secret", s, maxCheckSecretLen)
}

// --- secreto cifrado ---

// sealCheckSecret cifra el valor de la cabecera con el id del chequeo como AAD.
func (a *API) sealCheckSecret(checkID, secret string) (blob []byte, keyID string, err error) {
	blob, err = a.opts.Secrets.Seal(checkID, kindServiceCheck, []byte(secret))
	if err != nil {
		return nil, "", fmt.Errorf("cifrando secreto del chequeo: %w", err)
	}
	return blob, a.opts.Secrets.KeyID(), nil
}

// openCheckSecret descifra el secreto de un chequeo (solo para entregarlo al agente).
func (a *API) openCheckSecret(d store.ServiceCheckDef) (string, error) {
	if a.opts.Secrets == nil || d.SecretKeyID == nil {
		return "", errors.New("no se puede abrir el secreto del chequeo")
	}
	raw, err := a.opts.Secrets.Open(d.ID, *d.SecretKeyID, kindServiceCheck, d.SecretEnc)
	if err != nil {
		return "", fmt.Errorf("abriendo secreto del chequeo %s: %w", d.ID, err)
	}
	return string(raw), nil
}

func (a *API) secretsReady() error {
	if a.opts.Secrets == nil {
		return errSecretKeyMissing
	}
	return nil
}

// --- lecturas ---

func (a *API) listServiceChecks(w http.ResponseWriter, r *http.Request) error {
	defs, err := a.store.ListServiceChecks(r.Context(), currentUser(r).Scope)
	if err != nil {
		return err
	}
	u := currentUser(r)
	items := make([]serviceCheckDTO, 0, len(defs))
	for _, d := range defs {
		items = append(items, toServiceCheckDTO(u, d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

func (a *API) getServiceCheck(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	d, err := a.store.ServiceCheckByID(r.Context(), currentUser(r).Scope, strings.ToLower(id))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"check": toServiceCheckDTO(currentUser(r), d)})
	return nil
}

// --- escrituras ---

type createServiceCheckReq struct {
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	IntervalSeconds *int      `json:"interval_seconds"`
	TimeoutMs       *int      `json:"timeout_ms"`
	Retries         *int      `json:"retries"`
	TargetHost      *string   `json:"target_host"`
	TargetPort      *int      `json:"target_port"`
	URL             *string   `json:"url"`
	ExpectStatus    *string   `json:"expect_status"`
	ExpectText      *string   `json:"expect_text"`
	ServiceName     *string   `json:"service_name"`
	WarnMs          *int      `json:"warn_ms"`
	CritMs          *int      `json:"crit_ms"`
	WarnDays        *int      `json:"warn_days"`
	CritDays        *int      `json:"crit_days"`
	HeaderName      *string   `json:"header_name"`
	Secret          *string   `json:"secret"`
	Metric          *string   `json:"metric"`
	IfIndex         *int      `json:"if_index"`
	Scope           *scopeReq `json:"scope"`
	Enabled         *bool     `json:"enabled"`
}

func (a *API) createServiceCheck(w http.ResponseWriter, r *http.Request) error {
	var req createServiceCheckReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	id, err := newUUID()
	if err != nil {
		return err
	}
	d := store.ServiceCheckDef{
		ID: id, Name: req.Name, Type: req.Type, IntervalSeconds: 60, TimeoutMs: 5000, Retries: 1,
		TargetHost: req.TargetHost, TargetPort: req.TargetPort, URL: req.URL, ExpectStatus: req.ExpectStatus,
		ExpectText: req.ExpectText, ServiceName: req.ServiceName, WarnMs: req.WarnMs, CritMs: req.CritMs,
		WarnDays: req.WarnDays, CritDays: req.CritDays, HeaderName: req.HeaderName, Metric: req.Metric, IfIndex: req.IfIndex,
		Scope: store.AlertScope{Type: store.ScopeAll}, Enabled: true,
	}
	if req.IntervalSeconds != nil {
		d.IntervalSeconds = *req.IntervalSeconds
	}
	if req.TimeoutMs != nil {
		d.TimeoutMs = *req.TimeoutMs
	}
	if req.Retries != nil {
		d.Retries = *req.Retries
	}
	if req.Enabled != nil {
		d.Enabled = *req.Enabled
	}
	if req.Scope != nil {
		sc, err := req.Scope.toScope()
		if err != nil {
			return err
		}
		d.Scope = sc
	}
	if err := validateCheckDef(&d); err != nil {
		return err
	}
	if req.Secret != nil {
		if d.Type != store.CheckHTTP {
			return badRequest("secret solo aplica a chequeos http")
		}
		if d.HeaderName == nil {
			return badRequest("secret requiere header_name")
		}
		if err := checkSecret(*req.Secret); err != nil {
			return err
		}
		if err := a.secretsReady(); err != nil {
			return err
		}
		d.SecretEnc, d.SecretKeyID, err = sealedOrErr(a, id, *req.Secret)
		if err != nil {
			return err
		}
	}
	if d.HeaderName != nil && req.Secret == nil {
		return badRequest("header_name requiere secret")
	}
	if d.HeaderName != nil && !validCheckHeaders[*d.HeaderName] {
		return badRequest("header_name debe ser Authorization, X-API-Key o X-Auth-Token")
	}

	u := currentUser(r)
	created, err := a.store.CreateServiceCheck(r.Context(), d, u.ID)
	if err != nil {
		return err
	}
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evServiceCheckCreated, store.OutcomeOK, "service_check", created.ID
	a.audit(r, e, checkAuditDetail(created, nil))

	writeJSON(w, http.StatusCreated, map[string]any{"check": toServiceCheckDTO(u, created)})
	return nil
}

// sealedOrErr cifra el secreto y devuelve el blob y el id de clave listos para guardar.
func sealedOrErr(a *API, checkID, secret string) (blob []byte, keyID *string, err error) {
	b, k, err := a.sealCheckSecret(checkID, secret)
	if err != nil {
		return nil, nil, err
	}
	return b, &k, nil
}

// updateServiceCheckReq: PATCH parcial. type no cambia. Los campos con null lo quitan (expect_text, umbrales,
// header_name); secret null quita el secreto y su cabecera; ausente conserva.
type updateServiceCheckReq struct {
	Name            *string          `json:"name"`
	IntervalSeconds *int             `json:"interval_seconds"`
	TimeoutMs       *int             `json:"timeout_ms"`
	Retries         *int             `json:"retries"`
	TargetHost      *string          `json:"target_host"`
	TargetPort      *int             `json:"target_port"`
	URL             *string          `json:"url"`
	ExpectStatus    optional[string] `json:"expect_status"`
	ExpectText      optional[string] `json:"expect_text"`
	ServiceName     *string          `json:"service_name"`
	WarnMs          optional[int]    `json:"warn_ms"`
	CritMs          optional[int]    `json:"crit_ms"`
	WarnDays        *int             `json:"warn_days"`
	CritDays        *int             `json:"crit_days"`
	HeaderName      *string          `json:"header_name"`
	Secret          optional[string] `json:"secret"`
	Metric          *string          `json:"metric"`
	IfIndex         optional[int]    `json:"if_index"`
	Scope           *scopeReq        `json:"scope"`
	Enabled         *bool            `json:"enabled"`
}

func (req updateServiceCheckReq) changedFields() []string {
	var out []string
	add := func(present bool, name string) {
		if present {
			out = append(out, name)
		}
	}
	add(req.Name != nil, "name")
	add(req.IntervalSeconds != nil, "interval_seconds")
	add(req.TimeoutMs != nil, "timeout_ms")
	add(req.Retries != nil, "retries")
	add(req.TargetHost != nil, "target_host")
	add(req.TargetPort != nil, "target_port")
	add(req.URL != nil, "url")
	add(req.ExpectStatus.Set, "expect_status")
	add(req.ExpectText.Set, "expect_text")
	add(req.ServiceName != nil, "service_name")
	add(req.WarnMs.Set, "warn_ms")
	add(req.CritMs.Set, "crit_ms")
	add(req.WarnDays != nil, "warn_days")
	add(req.CritDays != nil, "crit_days")
	add(req.HeaderName != nil, "header_name")
	add(req.Secret.Set, "secret")
	add(req.Metric != nil, "metric")
	add(req.IfIndex.Set, "if_index")
	add(req.Scope != nil, "scope")
	add(req.Enabled != nil, "enabled")
	return out
}

func applyCheckPatch(d *store.ServiceCheckDef, req updateServiceCheckReq, scope *store.AlertScope) {
	setInt := func(dst **int, src *int) {
		if src != nil {
			*dst = src
		}
	}
	if req.Name != nil {
		d.Name = *req.Name
	}
	if req.IntervalSeconds != nil {
		d.IntervalSeconds = *req.IntervalSeconds
	}
	if req.TimeoutMs != nil {
		d.TimeoutMs = *req.TimeoutMs
	}
	if req.Retries != nil {
		d.Retries = *req.Retries
	}
	if req.TargetHost != nil {
		d.TargetHost = req.TargetHost
	}
	setInt(&d.TargetPort, req.TargetPort)
	if req.URL != nil {
		d.URL = req.URL
	}
	if req.ExpectStatus.Set {
		d.ExpectStatus = req.ExpectStatus.Value
	}
	if req.ExpectText.Set {
		d.ExpectText = req.ExpectText.Value
	}
	if req.ServiceName != nil {
		d.ServiceName = req.ServiceName
	}
	if req.WarnMs.Set {
		d.WarnMs = req.WarnMs.Value
	}
	if req.CritMs.Set {
		d.CritMs = req.CritMs.Value
	}
	setInt(&d.WarnDays, req.WarnDays)
	setInt(&d.CritDays, req.CritDays)
	if req.HeaderName != nil {
		d.HeaderName = req.HeaderName
	}
	if req.Metric != nil {
		d.Metric = req.Metric
	}
	if req.IfIndex.Set {
		d.IfIndex = req.IfIndex.Value
	}
	if scope != nil {
		d.Scope = *scope
	}
	if req.Enabled != nil {
		d.Enabled = *req.Enabled
	}
}

func (a *API) updateServiceCheck(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	id = strings.ToLower(id)
	var req updateServiceCheckReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	fields := req.changedFields()
	if len(fields) == 0 {
		return badRequest("nada que cambiar")
	}
	var newScope *store.AlertScope
	if req.Scope != nil {
		sc, err := req.Scope.toScope()
		if err != nil {
			return err
		}
		newScope = &sc
	}
	if req.Secret.Value != nil {
		if err := checkSecret(*req.Secret.Value); err != nil {
			return err
		}
		if err := a.secretsReady(); err != nil {
			return err
		}
	}

	updated, err := a.store.UpdateServiceCheck(r.Context(), id, func(d *store.ServiceCheckDef) error {
		applyCheckPatch(d, req, newScope)
		if err := validateCheckDef(d); err != nil {
			return err
		}
		return a.applyCheckSecret(d, req)
	})
	if err != nil {
		return err
	}
	u := currentUser(r)
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evServiceCheckUpdated, store.OutcomeOK, "service_check", updated.ID
	a.audit(r, e, checkAuditDetail(updated, fields))

	writeJSON(w, http.StatusOK, map[string]any{"check": toServiceCheckDTO(u, updated)})
	return nil
}

// applyCheckSecret aplica secret/header_name de un PATCH sobre la definicion ya validada y comprueba que la
// cabecera y el secreto queden juntos (o ambos ausentes) y solo en chequeos http.
func (a *API) applyCheckSecret(d *store.ServiceCheckDef, req updateServiceCheckReq) error {
	switch {
	case req.Secret.Set && req.Secret.Value == nil:
		d.SecretEnc, d.SecretKeyID, d.HeaderName = nil, nil, nil
		return nil
	case req.Secret.Value != nil:
		if d.Type != store.CheckHTTP {
			return badRequest("secret solo aplica a chequeos http")
		}
		if d.HeaderName == nil {
			return badRequest("secret requiere header_name")
		}
		var err error
		d.SecretEnc, d.SecretKeyID, err = sealedOrErr(a, d.ID, *req.Secret.Value)
		if err != nil {
			return err
		}
	}
	if d.HeaderName != nil && len(d.SecretEnc) == 0 {
		return badRequest("header_name requiere secret")
	}
	if d.HeaderName == nil && len(d.SecretEnc) > 0 {
		return badRequest("el secreto guardado requiere header_name: manda secret null pa quitarlo")
	}
	if d.HeaderName != nil && !validCheckHeaders[*d.HeaderName] {
		return badRequest("header_name debe ser Authorization, X-API-Key o X-Auth-Token")
	}
	return nil
}

// checkAuditDetail: nombre, tipo, estado, alcance y campos cambiados (solo sus nombres). Nunca el secreto, la
// cabecera ni la URL (puede llevar query).
func checkAuditDetail(d store.ServiceCheckDef, fields []string) map[string]any {
	m := map[string]any{"name": d.Name, "type": d.Type, "enabled": d.Enabled, "scope_type": d.Scope.Type}
	if d.Scope.GroupID != nil {
		m["group_id"] = *d.Scope.GroupID
	}
	if d.Scope.HostID != nil {
		m[map[bool]string{true: "host_id", false: "device_id"}[d.Scope.Type == store.ScopeHost]] = *d.Scope.HostID
	}
	if d.Metric != nil {
		m["metric"] = *d.Metric
	}
	if d.IfIndex != nil {
		m["if_index"] = *d.IfIndex
	}
	if fields != nil {
		m["fields"] = fields
	}
	return m
}

func (a *API) deleteServiceCheck(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	id = strings.ToLower(id)
	name, typ, err := a.store.DeleteServiceCheck(r.Context(), id)
	if err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evServiceCheckDeleted, store.OutcomeOK, "service_check", id
	a.audit(r, e, map[string]any{"name": name, "type": typ})
	writeJSON(w, http.StatusOK, map[string]any{})
	return nil
}

// --- lecturas por host ---

type hostCheckDTO struct {
	Check struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Type      string `json:"type"`
		Target    string `json:"target"`
		IntervalS int    `json:"interval_s"`
		// Metric e IfIndex solo en los chequeos snmp.
		Metric  *string `json:"metric,omitempty"`
		IfIndex *int    `json:"if_index,omitempty"`
	} `json:"check"`
	State *checkStateDTO `json:"state"`
}

type checkStateDTO struct {
	Status          string    `json:"status"`
	StatusChangedAt time.Time `json:"status_changed_at"`
	TS              time.Time `json:"ts"`
	ResponseMs      *int      `json:"response_ms"`
	Value           *float64  `json:"value"`
	Detail          *string   `json:"detail"`
}

func (a *API) hostServiceChecks(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return invalidID()
	}
	checks, err := a.store.HostServiceChecks(r.Context(), currentUser(r).Scope, strings.ToLower(id))
	if err != nil {
		return err
	}
	items := make([]hostCheckDTO, 0, len(checks))
	for _, c := range checks {
		var dto hostCheckDTO
		dto.Check.ID, dto.Check.Name, dto.Check.Type = c.Def.ID, c.Def.Name, c.Def.Type
		dto.Check.Target, dto.Check.IntervalS = checkTarget(c.Def), c.Def.IntervalSeconds
		dto.Check.Metric, dto.Check.IfIndex = c.Def.Metric, c.Def.IfIndex
		if c.State != nil {
			dto.State = &checkStateDTO{Status: c.State.Status, StatusChangedAt: c.State.StatusChangedAt.UTC(), TS: c.State.TS.UTC(),
				ResponseMs: c.State.ResponseMs, Value: c.State.Value, Detail: c.State.Detail}
		}
		items = append(items, dto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type checkEventDTO struct {
	ID         int64     `json:"id"`
	FromStatus *string   `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Detail     *string   `json:"detail"`
	At         time.Time `json:"at"`
}

// hostCheckEvents devuelve las transiciones de un chequeo en un host, las mas recientes primero.
func (a *API) hostCheckEvents(w http.ResponseWriter, r *http.Request) error {
	id, checkID := r.PathValue("id"), r.PathValue("check_id")
	if !isUUID(id) {
		return invalidID()
	}
	if !isUUID(checkID) {
		return badRequest("check_id invalido")
	}
	id, checkID = strings.ToLower(id), strings.ToLower(checkID)
	v := r.URL.Query()
	limit, err := parseLimit(v)
	if err != nil {
		return err
	}
	scope := currentUser(r).Scope
	fp := fingerprintOf("check_events", id, checkID, scope.Key())
	var after *store.EventCursor
	if raw := v.Get("cursor"); raw != "" {
		p, err := a.decodeKeyset(raw, fp)
		if err != nil {
			return err
		}
		evID, err := strconv.ParseInt(p.I, 10, 64)
		if err != nil || evID < 1 {
			return errInvalidCursor
		}
		after = &store.EventCursor{At: time.UnixMicro(p.T), ID: evID}
	}
	page, err := a.store.ListCheckEvents(r.Context(), scope, id, checkID, after, limit)
	if err != nil {
		return err
	}
	items := make([]checkEventDTO, 0, len(page.Items))
	for _, e := range page.Items {
		items = append(items, checkEventDTO{ID: e.ID, FromStatus: e.FromStatus, ToStatus: e.ToStatus, Detail: e.Detail, At: e.At.UTC()})
	}
	var next *string
	if page.NextCursor != nil {
		c := a.encodeKeyset(fp, page.NextCursor.At, strconv.FormatInt(page.NextCursor.ID, 10))
		next = &c
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
	return nil
}
