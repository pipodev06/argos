package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// Revision de seguridad multi-cliente (spec G): regresiones de fugas entre clientes que comparten server.

// Un chequeo de alcance `all` se entrega a los agentes de TODOS los clientes: con secreto (cabecera de auth) filtraria
// la credencial y el destino de un cliente al agente de otro. Solo se admite acotado a un grupo o a un host.
func TestCheckSecretRejectedWithScopeAll(t *testing.T) {
	e := newCheckEnv(t)
	g := e.newGroup(e.admin, `{"name":"cliente-a"}`)
	const base = `"type":"http","url":"https://api.example.com/health","header_name":"Authorization","secret":"tok-1"`

	rec := e.do("POST", "/api/v1/service-checks", `{"name":"sin-alcance",`+base+`}`, withCookie(e.admin))
	e.wantStatus(rec, http.StatusBadRequest, "invalid_body")
	rec = e.do("POST", "/api/v1/service-checks", `{"name":"all-explicito",`+base+`,"scope":{"type":"all"}}`, withCookie(e.admin))
	e.wantStatus(rec, http.StatusBadRequest, "invalid_body")

	id := e.create(`{"name":"del-grupo",` + base + `,"scope":{"type":"group","group_id":"` + g + `"}}`)["id"].(string)
	// Ampliarlo a `all` con el secreto puesto tambien se rechaza, y no cambia nada.
	rec = e.do("PATCH", "/api/v1/service-checks/"+id, `{"scope":{"type":"all"}}`, withCookie(e.admin))
	e.wantStatus(rec, http.StatusBadRequest, "invalid_body")
	if n := e.count(`SELECT count(*) FROM service_check_defs WHERE id = $1 AND scope_type = 'group'`, id); n != 1 {
		t.Error("el rechazo no debe cambiar el alcance del chequeo")
	}
	// Quitando el secreto en el mismo PATCH si se puede ampliar.
	rec = e.do("PATCH", "/api/v1/service-checks/"+id, `{"secret":null,"scope":{"type":"all"}}`, withCookie(e.admin))
	e.wantStatus(rec, http.StatusOK, "")
}

// El detalle de una alerta no debe mostrar a un viewer restringido a que canales se notifico (ids y nombres de canales
// de admin, que pueden nombrar a otro cliente).
func TestRestrictedViewerAlertDetailHidesDeliveryChannels(t *testing.T) {
	x := newAlertScopeEnv(t)
	mustExec(t, `INSERT INTO notification_channels (name, kind) VALUES ('Canal-Cliente-B-secreto', 'webhook')`)
	mustExec(t, `INSERT INTO notification_deliveries (alert_id, channel_id, event, status)
	              SELECT $1::uuid, id, 'opened', 'sent' FROM notification_channels WHERE name = 'Canal-Cliente-B-secreto'`, x.alerts[x.hostA])

	path := "/api/v1/alerts/" + x.alerts[x.hostA]
	rec := x.do("GET", path, "", withCookie(x.va))
	x.wantStatus(rec, http.StatusOK, "")
	if strings.Contains(rec.Body.String(), "Canal-Cliente-B") || !strings.Contains(rec.Body.String(), `"deliveries":[]`) {
		t.Errorf("el viewer restringido no debe ver canales ni entregas: %.300s", rec.Body.String())
	}
	// El admin y un viewer con ambito total (que si ve los canales) siguen viendo la entrega.
	for name, c := range map[string]*http.Cookie{"admin": x.admin, "vall": x.vall} {
		if body := x.do("GET", path, "", withCookie(c)).Body.String(); !strings.Contains(body, "Canal-Cliente-B") {
			t.Errorf("%s debe ver la entrega: %.300s", name, body)
		}
	}
}
