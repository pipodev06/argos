package httpapi

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestRuleEscalationFieldValidation(t *testing.T) {
	e := newChannelEnv(t)
	withField := func(v string) string {
		return strings.Replace(cpuRuleBody, `"severity"`, `"escalate_after_seconds":`+v+`,"severity"`, 1)
	}
	post := func(body string) int {
		return e.do("POST", "/api/v1/alert-rules", body, withCookie(e.admin)).Code
	}
	for _, bad := range []string{"0", "59", "604801", "-1"} {
		if got := post(withField(bad)); got != http.StatusBadRequest {
			t.Errorf("POST escalate_after_seconds=%s got %d, want 400", bad, got)
		}
	}

	rec := e.do("POST", "/api/v1/alert-rules", withField("300"), withCookie(e.admin))
	e.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		Rule struct {
			ID                   string   `json:"id"`
			EscalateAfterSeconds *int     `json:"escalate_after_seconds"`
			EscalationChannelIDs []string `json:"escalation_channel_ids"`
		} `json:"rule"`
	}
	e.dataAs(rec, &d)
	if d.Rule.EscalateAfterSeconds == nil || *d.Rule.EscalateAfterSeconds != 300 || d.Rule.EscalationChannelIDs == nil {
		t.Fatalf("regla creada got %+v, want 300 y arreglo no nulo", d.Rule)
	}

	patch := func(body string) int {
		return e.do("PATCH", "/api/v1/alert-rules/"+d.Rule.ID, body, withCookie(e.admin)).Code
	}
	if got := patch(`{"escalate_after_seconds":30}`); got != http.StatusBadRequest {
		t.Errorf("PATCH fuera de rango got %d, want 400", got)
	}
	if got := patch(`{"escalate_after_seconds":null}`); got != http.StatusOK {
		t.Fatalf("PATCH null got %d, want 200", got)
	}
	if n := e.count(`SELECT count(*) FROM alert_rules WHERE id = $1 AND escalate_after_seconds IS NULL`, d.Rule.ID); n != 1 {
		t.Errorf("escalate_after_seconds tras PATCH null: filas con NULL got %d, want 1", n)
	}
}

func TestSetRuleEscalationChannels(t *testing.T) {
	e := newChannelEnv(t)
	a, b := e.createChannel(chBody("a")), e.createChannel(chBody("b"))
	ruleID := e.createRule(cpuRuleBody)
	put := func(body string) int {
		return e.do("PUT", "/api/v1/alert-rules/"+ruleID+"/escalation-channels", body, withCookie(e.admin)).Code
	}
	if got := put(`{"channel_ids":[` + `"` + a + `","` + b + `"]}`); got != http.StatusOK {
		t.Fatalf("PUT got %d, want 200", got)
	}
	// Un canal puede estar a la vez en la lista normal y en la de escalamiento.
	e.wantStatus(e.do("PUT", "/api/v1/alert-rules/"+ruleID+"/channels", `{"channel_ids":["`+a+`"]}`, withCookie(e.admin)), http.StatusOK, "")

	var got struct {
		Rule struct {
			ChannelIDs           []string `json:"channel_ids"`
			EscalationChannelIDs []string `json:"escalation_channel_ids"`
		} `json:"rule"`
	}
	e.dataAs(e.do("GET", "/api/v1/alert-rules/"+ruleID, "", withCookie(e.admin)), &got)
	want := []string{a, b}
	slices.Sort(want)
	if !slices.Equal(got.Rule.EscalationChannelIDs, want) || !slices.Equal(got.Rule.ChannelIDs, []string{a}) {
		t.Errorf("GET regla got channels=%v escalation=%v, want [%s] y %v", got.Rule.ChannelIDs, got.Rule.EscalationChannelIDs, a, want)
	}

	if got := put(`{"channel_ids":["00000000-0000-4000-8000-0000000000ff"]}`); got != http.StatusUnprocessableEntity {
		t.Errorf("canal inexistente got %d, want 422", got)
	}
	if got := put(`{}`); got != http.StatusBadRequest {
		t.Errorf("sin channel_ids got %d, want 400", got)
	}
	if got := e.do("PUT", "/api/v1/alert-rules/"+missingID+"/escalation-channels", `{"channel_ids":[]}`, withCookie(e.admin)).Code; got != http.StatusNotFound {
		t.Errorf("regla inexistente got %d, want 404", got)
	}
	if got := e.do("PUT", "/api/v1/alert-rules/"+ruleID+"/escalation-channels", `{"channel_ids":[]}`, withCookie(e.viewer)).Code; got != http.StatusForbidden {
		t.Errorf("viewer got %d, want 403", got)
	}
}
