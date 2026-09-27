package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"argos/server/internal/notify"
)

// Lote L, B2: un canal con dueño (grupo) solo se usa en reglas cuyo alcance queda dentro de ese grupo, para que un
// cliente nunca reciba en su webhook datos de otro.

type channelsEnv struct {
	*alertScopeEnv
	hook *fakeWebhook
}

func newChannelsEnv(t *testing.T) *channelsEnv {
	t.Helper()
	box, err := notify.NewKeyRing(channelKey, "")
	if err != nil {
		t.Fatal(err)
	}
	hook := &fakeWebhook{code: 200}
	return &channelsEnv{alertScopeEnv: newAlertScopeEnvWith(t, Options{HostDownAfter: 2 * time.Minute, Secrets: box, Webhook: hook}), hook: hook}
}

// channel crea un canal; groupID vacio = global.
func (x *channelsEnv) channel(name, groupID string) string {
	x.t.Helper()
	body := chBody(name)
	if groupID != "" {
		body = body[:len(body)-1] + `,"group_id":"` + groupID + `"}`
	}
	rec := x.do("POST", "/api/v1/channels", body, withCookie(x.admin))
	x.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		Channel struct {
			ID        string  `json:"id"`
			GroupID   *string `json:"group_id"`
			GroupName *string `json:"group_name"`
		} `json:"channel"`
	}
	x.dataAs(rec, &d)
	if (groupID == "") != (d.Channel.GroupID == nil) || (groupID != "" && *d.Channel.GroupID != groupID) {
		x.t.Fatalf("group_id del canal got %v, want %q", d.Channel.GroupID, groupID)
	}
	if groupID != "" && (d.Channel.GroupName == nil || *d.Channel.GroupName == "") {
		x.t.Fatalf("group_name del canal vacio: %+v", d.Channel)
	}
	return d.Channel.ID
}

func (x *channelsEnv) rule(name, scope string) string {
	x.t.Helper()
	body := fmt.Sprintf(`{"name":%q,"kind":"host_status","duration_seconds":0,"severity":"warn"%s}`, name, scope)
	return x.ae.createRule(body)
}

func (x *channelsEnv) setChannels(ruleID string, status int, code string, channelIDs ...string) string {
	x.t.Helper()
	list := "["
	for i, c := range channelIDs {
		if i > 0 {
			list += ","
		}
		list += `"` + c + `"`
	}
	rec := x.do("PUT", "/api/v1/alert-rules/"+ruleID+"/channels", `{"channel_ids":`+list+`]}`, withCookie(x.admin))
	x.wantStatus(rec, status, code)
	return rec.Body.String()
}

// mismatchRules devuelve los ids de reglas de details.rules de un error channel_scope_mismatch.
func (x *channelsEnv) mismatchRules(body string) []string {
	x.t.Helper()
	var resp struct {
		Error struct {
			Details struct {
				Rules []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"rules"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		x.t.Fatalf("respuesta de error ilegible: %v (%s)", err, body)
	}
	var ids []string
	for _, r := range resp.Error.Details.Rules {
		if r.Name == "" {
			x.t.Errorf("details.rules sin nombre: %s", body)
		}
		ids = append(ids, r.ID)
	}
	return ids
}

func scopeGroup(id string) string { return `,"scope":{"type":"group","group_id":"` + id + `"}` }
func scopeHost(id string) string  { return `,"scope":{"type":"host","host_id":"` + id + `"}` }

func TestChannelOwnerMustCoverTheRuleScope(t *testing.T) {
	x := newChannelsEnv(t)
	chA := x.channel("hook-a", x.groupA)
	chGlobal := x.channel("hook-global", "")
	ruleAll := x.ruleAll // "CPU alta", alcance all
	ruleA := x.rule("hosts-a", scopeGroup(x.groupA))
	ruleB := x.rule("hosts-b", scopeGroup(x.groupB))
	ruleHostA := x.rule("host-a", scopeHost(x.hostA))
	ruleHostB := x.rule("host-b", scopeHost(x.hostB))
	ruleHostNo := x.rule("host-sin-grupo", scopeHost(x.hostNo))

	// Alcance all con el canal de A: el caso de fuga. 422 con las reglas afectadas y NADA queda asignado.
	rec := x.setChannels(ruleAll, http.StatusUnprocessableEntity, "channel_scope_mismatch", chA)
	if ids := x.mismatchRules(rec); !slices.Equal(ids, []string{ruleAll}) {
		t.Errorf("details.rules got %v, want [%s]", ids, ruleAll)
	}
	if n := x.count(`SELECT count(*) FROM rule_channels WHERE rule_id = $1`, ruleAll); n != 0 {
		t.Errorf("la asignacion rechazada dejo %d filas", n)
	}
	for _, bad := range []string{ruleB, ruleHostB, ruleHostNo} {
		x.setChannels(bad, http.StatusUnprocessableEntity, "channel_scope_mismatch", chA)
	}
	// Dentro de A: bien. El canal global sirve a cualquiera, tambien junto al de A.
	x.setChannels(ruleA, http.StatusOK, "", chA)
	x.setChannels(ruleHostA, http.StatusOK, "", chA, chGlobal)
	x.setChannels(ruleAll, http.StatusOK, "", chGlobal)
	x.setChannels(ruleB, http.StatusOK, "", chGlobal)
	// Un canal de A junto a uno global en una regla all sigue siendo fuga.
	x.setChannels(ruleAll, http.StatusUnprocessableEntity, "channel_scope_mismatch", chGlobal, chA)

	// Cambiar el alcance de una regla con canal de A: fuera de A es 422 y no cambia nada; dentro, ok.
	patch := func(rule, scope string, status int, code string) {
		t.Helper()
		x.wantStatus(x.do("PATCH", "/api/v1/alert-rules/"+rule, `{"scope":`+scope+`}`, withCookie(x.admin)), status, code)
	}
	patch(ruleA, `{"type":"all"}`, http.StatusUnprocessableEntity, "channel_scope_mismatch")
	patch(ruleA, `{"type":"group","group_id":"`+x.groupB+`"}`, http.StatusUnprocessableEntity, "channel_scope_mismatch")
	patch(ruleA, `{"type":"host","host_id":"`+x.hostA+`"}`, http.StatusOK, "")
	if n := x.count(`SELECT count(*) FROM alert_rules WHERE id = $1 AND scope_type = 'host'`, ruleA); n != 1 {
		t.Errorf("el PATCH valido no se aplico")
	}
	patch(ruleA, `{"type":"host","host_id":"`+x.hostB+`"}`, http.StatusUnprocessableEntity, "channel_scope_mismatch")
	if n := x.count(`SELECT count(*) FROM alert_rules WHERE id = $1 AND scope_host_id = $2`, ruleA, x.hostA); n != 1 {
		t.Errorf("el PATCH rechazado cambio el alcance")
	}
	// Otros campos de la regla no se ven afectados.
	x.wantStatus(x.do("PATCH", "/api/v1/alert-rules/"+ruleHostA, `{"severity":"critical"}`, withCookie(x.admin)), http.StatusOK, "")
}

func TestChannelOwnerChangeChecksItsRules(t *testing.T) {
	x := newChannelsEnv(t)
	chA := x.channel("hook-a", x.groupA)
	ruleA := x.rule("hosts-a", scopeGroup(x.groupA))
	x.setChannels(ruleA, http.StatusOK, "", chA)

	patch := func(body string, status int, code string) string {
		t.Helper()
		rec := x.do("PATCH", "/api/v1/channels/"+chA, body, withCookie(x.admin))
		x.wantStatus(rec, status, code)
		return rec.Body.String()
	}
	// A otro grupo con reglas de A: 422 y el dueño no cambia.
	rec := patch(`{"group_id":"`+x.groupB+`"}`, http.StatusUnprocessableEntity, "channel_scope_mismatch")
	if ids := x.mismatchRules(rec); !slices.Equal(ids, []string{ruleA}) {
		t.Errorf("details.rules got %v, want [%s]", ids, ruleA)
	}
	if n := x.count(`SELECT count(*) FROM notification_channels WHERE id = $1 AND group_id = $2`, chA, x.groupA); n != 1 {
		t.Errorf("el dueño cambio pese al 422")
	}
	patch(`{"group_id":"`+missingID+`"}`, http.StatusUnprocessableEntity, "group_not_found")
	patch(`{"group_id":"no-uuid"}`, http.StatusBadRequest, "")
	// Otros campos no tocan el dueño; null lo vuelve global (usable por cualquiera).
	patch(`{"min_severity":"critical"}`, http.StatusOK, "")
	if n := x.count(`SELECT count(*) FROM notification_channels WHERE id = $1 AND group_id = $2`, chA, x.groupA); n != 1 {
		t.Errorf("un PATCH sin group_id cambio el dueño")
	}
	patch(`{"group_id":null}`, http.StatusOK, "")
	x.setChannels(x.ruleAll, http.StatusOK, "", chA)
	// Un canal con reglas de alcance all no puede pasar a tener dueño.
	rec = patch(`{"group_id":"`+x.groupA+`"}`, http.StatusUnprocessableEntity, "channel_scope_mismatch")
	if ids := x.mismatchRules(rec); !slices.Contains(ids, x.ruleAll) {
		t.Errorf("details.rules got %v, want incluir %s", ids, x.ruleAll)
	}

	// Solo admin puede fijar dueño.
	x.wantStatus(x.do("POST", "/api/v1/channels", chBody("otro"), withCookie(x.va)), http.StatusForbidden, "forbidden")
	x.wantStatus(x.do("PATCH", "/api/v1/channels/"+chA, `{"group_id":null}`, withCookie(x.va)), http.StatusForbidden, "forbidden")
}

func TestServiceCheckRuleUsesTheCheckScope(t *testing.T) {
	x := newChannelsEnv(t)
	chA := x.channel("hook-a", x.groupA)
	check := x.newCheck(fmt.Sprintf(`{"name":"tcp-a","type":"tcp","target_host":"localhost","target_port":22,"scope":{"type":"group","group_id":%q}}`, x.groupA))
	rule := x.ae.createRule(fmt.Sprintf(`{"name":"svc-a","kind":"service_check","check_id":%q,"duration_seconds":0,"severity":"warn"}`, check))

	// La regla es all, pero su chequeo vive en A: solo hay alertas de hosts de A.
	x.setChannels(rule, http.StatusOK, "", chA)
	// Ampliar el chequeo a todos rompe esa garantia: 422 y el chequeo no cambia.
	x.wantStatus(x.do("PATCH", "/api/v1/service-checks/"+check, `{"scope":{"type":"all"}}`, withCookie(x.admin)),
		http.StatusUnprocessableEntity, "channel_scope_mismatch")
	if n := x.count(`SELECT count(*) FROM service_check_defs WHERE id = $1 AND scope_type = 'group'`, check); n != 1 {
		t.Errorf("el chequeo cambio pese al 422")
	}
	// Un chequeo de otro grupo con la regla all y el canal de A si es fuga.
	checkB := x.newCheck(fmt.Sprintf(`{"name":"tcp-b2","type":"tcp","target_host":"localhost","target_port":23,"scope":{"type":"group","group_id":%q}}`, x.groupB))
	ruleB := x.ae.createRule(fmt.Sprintf(`{"name":"svc-b","kind":"service_check","check_id":%q,"duration_seconds":0,"severity":"warn"}`, checkB))
	x.setChannels(ruleB, http.StatusUnprocessableEntity, "channel_scope_mismatch", chA)
}

// Defensa en profundidad: aunque haya datos previos incoherentes (regla all con el canal de A), el encolado nunca
// manda a un canal con dueño un aviso de un host de otro grupo.
func TestDeliveriesNeverReachAnotherGroupsChannel(t *testing.T) {
	x := newChannelsEnv(t)
	chA := x.channel("hook-a", x.groupA)
	chGlobal := x.channel("hook-global", "")

	// Datos incoherentes que la API ya no permite: se insertan directo.
	mustExec(t, `INSERT INTO rule_channels (rule_id, channel_id) VALUES ($1, $2), ($1, $3)`, x.ruleAll, chA, chGlobal)
	mustExec(t, `UPDATE notification_channels SET min_severity = 'info'`)
	if n, err := x.st.ChannelScopeMismatches(context.Background()); err != nil || n != 1 {
		t.Fatalf("ChannelScopeMismatches got (%d, %v), want (1, nil)", n, err)
	}

	// Las tres alertas del alta se reabren para que el evaluador las vuelva a abrir con los canales ya asignados.
	mustExec(t, `DELETE FROM alerts`)
	x.ae.eval()
	deliveries := func(channel, host string) int {
		return x.count(`SELECT count(*) FROM notification_deliveries d JOIN alerts a ON a.id = d.alert_id
			WHERE d.channel_id = $1 AND a.host_id = $2 AND d.event = 'opened'`, channel, host)
	}
	if n := deliveries(chA, x.hostA); n != 1 {
		t.Errorf("entregas del canal de A por host-a got %d, want 1", n)
	}
	for _, h := range []string{x.hostB, x.hostNo} {
		if n := deliveries(chA, h); n != 0 {
			t.Errorf("el canal de A recibio %d avisos de un host ajeno (%s): fuga", n, h)
		}
	}
	for _, h := range []string{x.hostA, x.hostB, x.hostNo} {
		if n := deliveries(chGlobal, h); n != 1 {
			t.Errorf("el canal global por %s got %d entregas, want 1", h, n)
		}
	}

	// Lo mismo al resolver (a mano) y en los recordatorios: nada sale hacia A por hosts de B.
	x.wantStatus(x.do("POST", "/api/v1/alerts/"+x.alertIDOf(x.hostB)+"/resolve", "", withCookie(x.admin)), http.StatusOK, "")
	x.wantStatus(x.do("POST", "/api/v1/alerts/"+x.alertIDOf(x.hostA)+"/resolve", "", withCookie(x.admin)), http.StatusOK, "")
	if n := x.count(`SELECT count(*) FROM notification_deliveries d JOIN alerts a ON a.id = d.alert_id
		WHERE d.channel_id = $1 AND a.host_id <> $2`, chA, x.hostA); n != 0 {
		t.Errorf("el canal de A recibio %d entregas de otros hosts", n)
	}
	if n := x.count(`SELECT count(*) FROM notification_deliveries d JOIN alerts a ON a.id = d.alert_id
		WHERE d.channel_id = $1 AND a.host_id = $2 AND d.event = 'resolved'`, chA, x.hostA); n != 1 {
		t.Errorf("aviso resolved del canal de A por su host got %d, want 1", n)
	}
}

func (x *channelsEnv) alertIDOf(hostID string) string {
	x.t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `SELECT id::text FROM alerts WHERE host_id = $1 AND state <> 'resolved'`, hostID).Scan(&id); err != nil {
		x.t.Fatal(err)
	}
	return id
}

// Borrar el grupo borra sus canales (ON DELETE CASCADE); las reglas que los usaban quedan sin ese canal.
func TestDeletingGroupDeletesItsChannels(t *testing.T) {
	x := newChannelsEnv(t)
	groupC := x.newGroup(x.admin, `{"name":"C"}`)
	chC := x.channel("hook-c", groupC)
	chGlobal := x.channel("hook-global", "")
	rule := x.rule("hosts-c", scopeGroup(groupC))
	x.setChannels(rule, http.StatusOK, "", chC, chGlobal)

	x.wantStatus(x.do("DELETE", "/api/v1/groups/"+groupC, "", withCookie(x.admin)), http.StatusOK, "")
	if n := x.count(`SELECT count(*) FROM notification_channels WHERE id = $1`, chC); n != 0 {
		t.Errorf("el canal del grupo borrado sigue existiendo")
	}
	if n := x.count(`SELECT count(*) FROM notification_channels WHERE id = $1`, chGlobal); n != 1 {
		t.Errorf("el canal global no debe borrarse")
	}
	if n := x.count(`SELECT count(*) FROM rule_channels WHERE channel_id = $1`, chC); n != 0 {
		t.Errorf("quedaron asignaciones al canal borrado")
	}
	// Las reglas que podian usar el canal estaban dentro del grupo, asi que caen con el (FK del alcance): ninguna
	// regla de otro alcance pierde canales ni queda apuntando a uno inexistente.
	if n := x.count(`SELECT count(*) FROM alert_rules WHERE id = $1`, rule); n != 0 {
		t.Errorf("la regla del grupo borrado sigue existiendo")
	}
	x.setChannels(x.ruleAll, http.StatusOK, "", chGlobal)
}
