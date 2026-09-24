package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"argos/server/internal/store"
)

// Limites de la entrega y de los resultados de chequeos.
const (
	// maxChecksPayloadBytes acota el JSON de `checks.items` que viaja en la respuesta al reporte.
	maxChecksPayloadBytes = 15 << 10
	// maxCheckResults acota los resultados con check_id de un reporte (el agente manda los acumulados desde el
	// anterior: 50 chequeos x varios ciclos).
	maxCheckResults  = 500
	maxCheckValue    = 1e9
	maxCheckResultMs = 600000
	checksRevLen     = 16
)

// Catalogo cerrado de `detail`: nunca IPs, URLs ni cuerpos de respuesta.
var validCheckDetail = map[string]bool{
	"": true, "timeout": true, "refused": true, "blocked": true, "tls": true, "not_found": true, "denied": true,
	"unsupported": true, "status_mismatch": true, "text_mismatch": true, "expired": true, "ok": true,
}

// --- resultados (reporte del agente) ---

// validateServices separa las entradas de `services` en resultados legados (sin check_id: solo muestra cruda) y
// resultados de chequeos asignados. Un enum, largo o rango invalido es 400; un resultado con ts fuera de la ventana
// aceptada se descarta (un dato viejo del buffer no debe bloquear el resto del reporte).
func validateServices(in []serviceCheckPayload, now time.Time) ([]store.ServiceCheck, []store.CheckResult, error) {
	var legacy []store.ServiceCheck
	var results []store.CheckResult
	for i, svc := range in {
		if svc.CheckID == "" {
			if len(legacy) >= maxServices {
				return nil, nil, badRequest("maximo %d services sin check_id", maxServices)
			}
			if err := checkText(fmt.Sprintf("services[%d].name", i), svc.Name, maxServiceName); err != nil {
				return nil, nil, err
			}
			if !validServiceStatus[svc.Status] {
				return nil, nil, badRequest("services[%d].status invalido", i)
			}
			if svc.ResponseMs < 0 || svc.ResponseMs > math.MaxInt32 {
				return nil, nil, badRequest("services[%d].response_ms fuera de rango", i)
			}
			legacy = append(legacy, store.ServiceCheck{Name: svc.Name, Status: svc.Status, ResponseMs: svc.ResponseMs})
			continue
		}
		if len(results) >= maxCheckResults {
			return nil, nil, badRequest("maximo %d resultados de chequeos por reporte", maxCheckResults)
		}
		res, keep, err := validateCheckResult(i, svc, now)
		if err != nil {
			return nil, nil, err
		}
		if keep {
			results = append(results, res)
		}
	}
	return legacy, results, nil
}

func validateCheckResult(i int, svc serviceCheckPayload, now time.Time) (store.CheckResult, bool, error) {
	if !isUUID(svc.CheckID) {
		return store.CheckResult{}, false, badRequest("services[%d].check_id invalido", i)
	}
	if !validServiceStatus[svc.Status] {
		return store.CheckResult{}, false, badRequest("services[%d].status invalido", i)
	}
	if svc.ResponseMs < 0 || svc.ResponseMs > maxCheckResultMs {
		return store.CheckResult{}, false, badRequest("services[%d].response_ms fuera de rango 0-%d", i, maxCheckResultMs)
	}
	if svc.Value != nil && (math.IsNaN(*svc.Value) || math.Abs(*svc.Value) > maxCheckValue) {
		return store.CheckResult{}, false, badRequest("services[%d].value fuera de rango", i)
	}
	if !validCheckDetail[svc.Detail] {
		return store.CheckResult{}, false, badRequest("services[%d].detail no esta en el catalogo", i)
	}
	if svc.TS <= 0 {
		return store.CheckResult{}, false, badRequest("services[%d].ts requerido", i)
	}
	ts := time.Unix(svc.TS, 0)
	if ts.Before(now.Add(-maxTimestampAge)) || ts.After(now.Add(maxTimestampSkew)) {
		return store.CheckResult{}, false, nil
	}
	return store.CheckResult{CheckID: strings.ToLower(svc.CheckID), Status: svc.Status, ResponseMs: svc.ResponseMs,
		Value: svc.Value, Detail: svc.Detail, TS: ts}, true, nil
}

// --- entrega (respuesta al reporte) ---

// agentCheck es un chequeo tal como lo recibe el agente. Los campos opcionales del tipo se omiten si faltan.
type agentCheck struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`
	Name         string  `json:"name"`
	IntervalS    int     `json:"interval_s"`
	TimeoutMs    int     `json:"timeout_ms"`
	Retries      int     `json:"retries"`
	TargetHost   *string `json:"target_host,omitempty"`
	TargetPort   *int    `json:"target_port,omitempty"`
	URL          *string `json:"url,omitempty"`
	ExpectStatus *string `json:"expect_status,omitempty"`
	ExpectText   *string `json:"expect_text,omitempty"`
	ServiceName  *string `json:"service_name,omitempty"`
	WarnMs       *int    `json:"warn_ms,omitempty"`
	CritMs       *int    `json:"crit_ms,omitempty"`
	WarnDays     *int    `json:"warn_days,omitempty"`
	CritDays     *int    `json:"crit_days,omitempty"`
	HeaderName   *string `json:"header_name,omitempty"`
	Secret       string  `json:"secret,omitempty"`
}

// checksDelivery es el objeto `checks` de la respuesta.
type checksDelivery struct {
	Rev   string       `json:"rev"`
	Items []agentCheck `json:"items"`
}

// revItem es lo que entra al hash de la revision: el chequeo SIN secreto mas un resumen del blob cifrado (cambia al
// cambiar o rotar el secreto, sin exponerlo ni depender de descifrar).
type revItem struct {
	agentCheck
	SecretRev string `json:"secret_rev,omitempty"`
}

// checksRev es el hash canonico (16 hex) de la lista entregada: estable mientras nada cambie y sensible a cualquier
// cambio de un campo, del alcance resuelto, del orden o del secreto.
func checksRev(items []agentCheck, secretRevs []string) (string, error) {
	rev := make([]revItem, len(items))
	for i, it := range items {
		it.Secret = ""
		rev[i] = revItem{agentCheck: it, SecretRev: secretRevs[i]}
	}
	b, err := json.Marshal(rev)
	if err != nil {
		return "", fmt.Errorf("serializando la revision de chequeos: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:checksRevLen], nil
}

func toAgentCheck(d store.ServiceCheckDef) agentCheck {
	return agentCheck{
		ID: d.ID, Type: d.Type, Name: d.Name, IntervalS: d.IntervalSeconds, TimeoutMs: d.TimeoutMs, Retries: d.Retries,
		TargetHost: d.TargetHost, TargetPort: d.TargetPort, URL: d.URL, ExpectStatus: d.ExpectStatus, ExpectText: d.ExpectText,
		ServiceName: d.ServiceName, WarnMs: d.WarnMs, CritMs: d.CritMs, WarnDays: d.WarnDays, CritDays: d.CritDays,
		HeaderName: d.HeaderName,
	}
}

// buildChecksDelivery resuelve los chequeos del host y arma la entrega: tope MaxChecksPerHost y maxChecksPayloadBytes
// (lo que exceda se trunca por id y se registra); un chequeo cuyo secreto no se puede descifrar se omite y se
// registra. Devuelve tambien la revision de la lista entregada.
func (a *API) buildChecksDelivery(ctx context.Context, hostID string) (checksDelivery, error) {
	defs, err := a.store.HostChecks(ctx, hostID)
	if err != nil {
		return checksDelivery{}, err
	}
	truncated := len(defs) > store.MaxChecksPerHost
	if truncated {
		defs = defs[:store.MaxChecksPerHost]
	}
	items := make([]agentCheck, 0, len(defs))
	secretRevs := make([]string, 0, len(defs))
	size := 2
	for _, d := range defs {
		it := toAgentCheck(d)
		sr := ""
		if len(d.SecretEnc) > 0 {
			secret, err := a.openCheckSecret(d)
			if err != nil {
				a.logger.ErrorContext(ctx, "chequeo omitido: no se pudo descifrar su secreto", "check_id", d.ID, "host_id", hostID, "err", err)
				continue
			}
			it.Secret = secret
			sum := sha256.Sum256(d.SecretEnc)
			sr = hex.EncodeToString(sum[:8])
		}
		b, err := json.Marshal(it) //nolint:gosec // el secreto viaja al agente por diseno (TLS + bearer)
		if err != nil {
			return checksDelivery{}, fmt.Errorf("serializando chequeo %s: %w", d.ID, err)
		}
		if size+len(b)+1 > maxChecksPayloadBytes {
			truncated = true
			break
		}
		size += len(b) + 1
		items = append(items, it)
		secretRevs = append(secretRevs, sr)
	}
	if truncated {
		a.logger.WarnContext(ctx, "chequeos del host truncados por tope", "host_id", hostID, "delivered", len(items))
	}
	rev, err := checksRev(items, secretRevs)
	if err != nil {
		return checksDelivery{}, err
	}
	return checksDelivery{Rev: rev, Items: items}, nil
}

// attachChecks agrega `checks` a la respuesta del reporte si la revision del agente difiere de la actual. Un fallo
// al resolverlos no rompe el reporte (ya se guardo): se registra y el agente lo reintenta en el siguiente.
func (a *API) attachChecks(ctx context.Context, hostID, agentRev string, resp map[string]any) {
	d, err := a.buildChecksDelivery(ctx, hostID)
	if err != nil {
		a.logger.ErrorContext(ctx, "no se pudieron resolver los chequeos del host", "host_id", hostID, "err", err)
		return
	}
	if d.Rev != agentRev {
		resp["checks"] = d
	}
}
