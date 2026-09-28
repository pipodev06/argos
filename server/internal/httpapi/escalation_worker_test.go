package httpapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"argos/server/internal/notify"
)

// Escalamiento de punta a punta: la alerta abre y avisa solo a los canales normales; al vencer el plazo sin
// reconocerla, el aviso ESCALADA sale por el worker real solo hacia el canal de escalamiento.
func TestEscalationReachesOnlyTheEscalationChannelThroughTheWorker(t *testing.T) {
	e := newChannelEnv(t)
	normal := e.createChannel(chBody("Normal"))
	esc := e.createChannel(chBody("Escalado"))
	ruleID := e.createRule(strings.Replace(cpuRuleBody, `"severity":"critical"`, `"severity":"warn","escalate_after_seconds":60`, 1))
	e.wantStatus(e.do("PUT", "/api/v1/alert-rules/"+ruleID+"/channels", fmt.Sprintf(`{"channel_ids":[%q]}`, normal), withCookie(e.admin)), http.StatusOK, "")
	e.wantStatus(e.do("PUT", "/api/v1/alert-rules/"+ruleID+"/escalation-channels", fmt.Sprintf(`{"channel_ids":[%q]}`, esc), withCookie(e.admin)), http.StatusOK, "")
	e.host("srv-web-01", 97)
	e.hook.sent, e.hook.bodies = nil, nil

	w := notify.NewWorker(e.st, e.box, e.hook, slog.New(slog.NewTextHandler(io.Discard, nil)), "")
	run := func() notify.Stats {
		st, err := w.RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return st
	}

	e.eval()
	// warn/info esperan unos segundos para agruparse: se adelanta el reloj de la entrega.
	mustExec(t, `UPDATE notification_deliveries SET created_at = now() - interval '1 minute'`)
	if st := run(); st.Sent != 1 {
		t.Fatalf("al abrir got %+v, want 1 envio (solo el canal normal)", st)
	}
	if n := e.count(`SELECT count(*) FROM notification_deliveries WHERE event = 'escalation'`); n != 0 {
		t.Fatalf("no debe haber escalamiento antes del plazo: %d", n)
	}

	mustExec(t, `UPDATE alerts SET opened_at = now() - interval '2 minutes'`)
	e.eval()
	if st := run(); st.Sent != 1 || len(e.hook.sent) != 2 {
		t.Fatalf("tras el plazo got %+v, envios %d, want 2 en total", st, len(e.hook.sent))
	}
	body := string(e.hook.bodies[1])
	for _, want := range []string{`"event":"escalation"`, `"severity":"critical"`, `ESCALADA`} {
		if !strings.Contains(body, want) {
			t.Errorf("el aviso de escalamiento no contiene %s: %s", want, body)
		}
	}
	if n := e.count(`SELECT count(*) FROM notification_deliveries WHERE event = 'escalation' AND channel_id = $1 AND status = 'sent'`, esc); n != 1 {
		t.Errorf("escalation enviada al canal de escalamiento got %d, want 1", n)
	}
	if n := e.count(`SELECT count(*) FROM notification_deliveries WHERE event = 'escalation' AND channel_id = $1`, normal); n != 0 {
		t.Errorf("el canal normal no debe recibir el escalamiento: %d", n)
	}
	e.eval()
	if st := run(); st.Claimed != 0 {
		t.Errorf("no debe escalar dos veces: %+v", st)
	}
}
