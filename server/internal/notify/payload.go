package notify

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxItemsPerMessage acota las alertas detalladas de un mensaje; el resto va como conteo (omitted).
	MaxItemsPerMessage = 20

	maxHostnameLen = 100
	maxRuleLen     = 80
	maxDetailLen   = 200
	payloadVersion = 1
)

// Item es una alerta a notificar. Solo lo que sale al exterior: hostname, regla, severidad, valor, umbral,
// estado y enlace. Nunca etiquetas, IPs ni datos de usuario.
type Item struct {
	AlertID   string
	Event     string // opened | resolved | reminder | escalation
	Hostname  string
	Rule      string
	Kind      string // tipo de regla: decide como se rotula Detail en el texto
	Severity  string
	Value     *float64
	Threshold *float64
	// Detail: texto corto ya acotado (exposed_port: hasta 5 proto/puerto sin datos de proceso; parches: conteos,
	// fecha y version del SO; nunca listas de paquetes ni avisos).
	Detail string
}

type alertJSON struct {
	Event     string   `json:"event"`
	State     string   `json:"state"`
	Hostname  string   `json:"hostname"`
	Rule      string   `json:"rule"`
	Severity  string   `json:"severity"`
	Value     *float64 `json:"value"`
	Threshold *float64 `json:"threshold"`
	Detail    string   `json:"detail,omitempty"`
	URL       string   `json:"url,omitempty"`
}

type messageJSON struct {
	Version int         `json:"version"`
	Kind    string      `json:"kind"`
	SentAt  time.Time   `json:"sent_at"`
	Text    string      `json:"text"`
	Alerts  []alertJSON `json:"alerts"`
	Omitted int         `json:"omitted"`
	// Content y AllowedMentions son para Discord (lee `content`, no `text`); Slack y receptores propios los
	// ignoran. Sin menciones: un hostname o regla con "@everyone" no debe notificar a todo el canal.
	Content         string          `json:"content"`
	AllowedMentions allowedMentions `json:"allowed_mentions"`
}

type allowedMentions struct {
	Parse []string `json:"parse"`
}

// discordMaxContent es el tope de `content` en Discord (mas largo responde 400).
const discordMaxContent = 2000

func newMessage(kind string, now time.Time, text string) messageJSON {
	content := text
	if r := []rune(content); len(r) > discordMaxContent {
		content = string(r[:discordMaxContent-1]) + "…"
	}
	return messageJSON{Version: payloadVersion, Kind: kind, SentAt: now.UTC(), Text: text, Content: content,
		AllowedMentions: allowedMentions{Parse: []string{}}}
}

// Clean deja s apto pa salir en un mensaje: sin caracteres de control ni de formato (incluye bidi y de ancho
// cero), separadores de linea y parrafo pasados a espacio, y recortado a max caracteres.
func Clean(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	var b strings.Builder
	n := 0
	for _, r := range s {
		switch {
		case r == ' ' || r == ' ':
			r = ' '
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError:
			continue
		}
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

func finiteOrNil(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return nil
	}
	return v
}

func stateOf(event string) string {
	if event == "resolved" {
		return "resolved"
	}
	return "firing"
}

func fmtNum(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.4g", *v)
}

// BuildMessage arma el JSON de un mensaje agrupado con json.Marshal de structs. baseURL es ARGOS_PUBLIC_URL
// (vacio = sin enlaces); omitted cuenta las alertas que no caben. No modifica items.
func BuildMessage(items []Item, omitted int, baseURL string, now time.Time) ([]byte, error) {
	if len(items) > MaxItemsPerMessage {
		omitted += len(items) - MaxItemsPerMessage
		items = items[:MaxItemsPerMessage]
	}
	alerts := make([]alertJSON, 0, len(items))
	var lines []string
	for _, it := range items {
		a := alertJSON{
			Event: Clean(it.Event, 16), State: stateOf(it.Event),
			Hostname: Clean(it.Hostname, maxHostnameLen), Rule: Clean(it.Rule, maxRuleLen), Severity: Clean(it.Severity, 16),
			Value: finiteOrNil(it.Value), Threshold: finiteOrNil(it.Threshold), Detail: Clean(it.Detail, maxDetailLen),
		}
		if baseURL != "" && it.AlertID != "" {
			a.URL = strings.TrimRight(baseURL, "/") + "/alertas/" + Clean(it.AlertID, 40)
		}
		alerts = append(alerts, a)
		verb := "ALERTA"
		switch {
		case a.State == "resolved":
			verb = "RESUELTA"
		case it.Event == "escalation":
			verb = "ESCALADA"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s %s en %s (valor %s, umbral %s)",
			strings.ToUpper(a.Severity), verb, a.Rule, a.Hostname, fmtNum(a.Value), fmtNum(a.Threshold)))
		if a.Detail != "" {
			label := " - "
			if it.Kind == "exposed_port" {
				label = " - puertos: "
			}
			lines[len(lines)-1] += label + a.Detail
		}
	}
	if omitted > 0 {
		lines = append(lines, fmt.Sprintf("y %d alertas mas (ver el dashboard)", omitted))
	}
	msg := newMessage("alerts", now, strings.Join(lines, "\n"))
	msg.Alerts, msg.Omitted = alerts, omitted
	return json.Marshal(msg)
}

// BuildTestMessage es el mensaje de POST /channels/{id}/test: sin datos de ninguna alerta real.
func BuildTestMessage(now time.Time) ([]byte, error) {
	msg := newMessage("test", now, "Mensaje de prueba de argos")
	msg.Alerts = []alertJSON{}
	return json.Marshal(msg)
}
