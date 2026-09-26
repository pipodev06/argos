package httpapi

import (
	"net/http"
	"strings"

	"argos/server/internal/store"
)

type retentionDTO struct {
	DetailDays int    `json:"detail_days"`
	Source     string `json:"source"`
	HotDays    int    `json:"hot_days"`
	HourlyDays int    `json:"hourly_days"`
}

func toRetentionDTO(s store.RetentionSettings) retentionDTO {
	return retentionDTO{DetailDays: s.DetailDays, Source: s.Source, HotDays: s.HotDays, HourlyDays: s.HourlyDays}
}

// checkDetailDays valida una retencion de detalle (nil = heredar).
func checkDetailDays(v *int) error {
	if v != nil && (*v < store.MinDetailDays || *v > store.MaxDetailDays) {
		return badRequest("detail_retention_days debe estar entre %d y %d (o null para heredar)", store.MinDetailDays, store.MaxDetailDays)
	}
	return nil
}

// checkShrink exige confirmacion explicita si reducir la retencion borrara detalle que existe hoy: 409 con lo que se
// perderia (dias, host-dias y bytes estimados). Sin nada que perder (o al subir la retencion) no pide nada.
func (a *API) checkShrink(r *http.Request, t store.RetentionTarget, newDays *int, confirm bool) error {
	p, err := a.store.PreviewRetention(r.Context(), t, newDays, a.now())
	if err != nil {
		return err
	}
	if p.Chunks == 0 || confirm {
		return nil
	}
	return &clientError{
		status: http.StatusConflict, code: "shrink_needs_confirm",
		msg:     "reducir la retencion borra detalle existente: repite con confirm_shrink=true para confirmar",
		details: map[string]any{"days_lost": p.DaysLost, "chunks": p.Chunks, "est_bytes": p.EstBytes},
	}
}

// auditRetention registra un cambio de retencion. Solo lleva el ambito y los dias, nunca datos.
func (a *API) auditRetention(r *http.Request, scope, targetID string, days *int) {
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evRetentionUpdated, store.OutcomeOK, scope, targetID
	a.audit(r, e, map[string]any{"scope": scope, "detail_days": days})
}

func (a *API) getRetention(w http.ResponseWriter, r *http.Request) error {
	s, err := a.store.RetentionSettings(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, toRetentionDTO(s))
	return nil
}

type putRetentionReq struct {
	DetailDays    *int `json:"detail_days"`
	ConfirmShrink bool `json:"confirm_shrink"`
}

// putRetention fija la retencion global de detalle (rige al siguiente ciclo del mantenimiento, sin reiniciar).
func (a *API) putRetention(w http.ResponseWriter, r *http.Request) error {
	var req putRetentionReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if req.DetailDays == nil {
		return badRequest("detail_days requerido")
	}
	if *req.DetailDays < store.MinDetailDays || *req.DetailDays > store.MaxDetailDays {
		return badRequest("detail_days debe estar entre %d y %d", store.MinDetailDays, store.MaxDetailDays)
	}
	if err := a.checkShrink(r, store.RetentionTarget{Kind: store.TargetGlobal}, req.DetailDays, req.ConfirmShrink); err != nil {
		return err
	}
	if err := a.store.SetGlobalDetailDays(r.Context(), *req.DetailDays, currentUser(r).ID); err != nil {
		return err
	}
	a.auditRetention(r, store.TargetGlobal, "", req.DetailDays)

	s, err := a.store.RetentionSettings(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, toRetentionDTO(s))
	return nil
}

type putHostRetentionReq struct {
	DetailRetentionDays optional[int] `json:"detail_retention_days"`
	ConfirmShrink       bool          `json:"confirm_shrink"`
}

// setHostRetention fija (o quita, con null) la retencion de detalle propia de un host con agente.
func (a *API) setHostRetention(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return invalidID()
	}
	id = strings.ToLower(id)
	var req putHostRetentionReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if !req.DetailRetentionDays.Set {
		return badRequest("detail_retention_days requerido (null = heredar)")
	}
	days := req.DetailRetentionDays.Value
	if err := checkDetailDays(days); err != nil {
		return err
	}
	if err := a.checkShrink(r, store.RetentionTarget{Kind: store.TargetHost, ID: id}, days, req.ConfirmShrink); err != nil {
		return err
	}
	if err := a.store.SetHostDetailDays(r.Context(), id, days); err != nil {
		return err
	}
	a.auditRetention(r, store.TargetHost, id, days)

	eff, src, err := a.store.EffectiveDetailDays(r.Context(), id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"detail_retention_days": days, "effective_detail_days": eff, "retention_source": src})
	return nil
}
