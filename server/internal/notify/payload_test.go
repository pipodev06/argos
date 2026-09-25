package notify

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }

var t0 = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func TestCleanStripsControlAndBidiCharacters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
		max            int
	}{
		{"CRLF", "srv\r\nX-Injected: 1", "srvX-Injected: 1", 100},
		{"NUL y tab", "a\x00b\tc", "abc", 100},
		{"bidi override", "host\u202egnp.exe", "hostgnp.exe", 100},
		{"bidi isolates", "a\u2066b\u2069c\u200fd", "abcd", 100},
		{"ancho cero", "ho\u200bst", "host", 100},
		{"separadores unicode a espacio", "a\u2028b\u2029c", "a b c", 100},
		{"utf8 invalido", "a\xffb", "ab", 100},
		{"recorta por caracteres", "ñññññ", "ñññ", 3},
		{"espacios de los bordes", "  x  ", "x", 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Clean(tt.in, tt.max); got != tt.want {
				t.Errorf("Clean(%q) got %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildMessageIsValidJSONWithOnlyTheAllowedFields(t *testing.T) {
	t.Parallel()
	items := []Item{{
		AlertID: "11111111-1111-4111-8111-111111111111", Event: "opened",
		Hostname: "srv\r\n\"},\"injected\":{\"x\":\"1", Rule: "CPU\u202e alta", Severity: "critical",
		Value: f64(95.5), Threshold: f64(90),
	}}
	raw, err := BuildMessage(items, 0, "https://argos.example.com/", t0)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("JSON invalido: %v\n%s", err, raw)
	}
	for k := range m {
		switch k {
		case "version", "kind", "sent_at", "text", "alerts", "omitted":
		default:
			t.Errorf("campo inesperado en el mensaje: %q", k)
		}
	}
	alert := m["alerts"].([]any)[0].(map[string]any)
	for k := range alert {
		switch k {
		case "event", "state", "hostname", "rule", "severity", "value", "threshold", "detail", "url":
		default:
			t.Errorf("campo inesperado en la alerta: %q", k)
		}
	}
	// La inyeccion queda como texto dentro del string, no como estructura.
	if _, ok := alert["injected"]; ok {
		t.Error("el hostname inyecto una clave")
	}
	if got := alert["url"]; got != "https://argos.example.com/alertas/11111111-1111-4111-8111-111111111111" {
		t.Errorf("url got %v", got)
	}
	if strings.ContainsAny(alert["hostname"].(string), "\r\n") || strings.ContainsRune(alert["rule"].(string), rune(0x202e)) {
		t.Errorf("caracteres de control o bidi en el mensaje: %v", alert)
	}
	if alert["state"] != "firing" {
		t.Errorf("state got %v", alert["state"])
	}
}

func TestBuildMessageCapsItemsAndHandlesOddValues(t *testing.T) {
	t.Parallel()
	items := make([]Item, MaxItemsPerMessage+5)
	for i := range items {
		items[i] = Item{Event: "resolved", Hostname: "h", Rule: "r", Severity: "warn", Value: f64(math.NaN()), Threshold: f64(math.Inf(1))}
	}
	raw, err := BuildMessage(items, 2, "", t0)
	if err != nil {
		t.Fatalf("NaN/Inf no deben romper el JSON: %v", err)
	}
	var m struct {
		Alerts  []map[string]any `json:"alerts"`
		Omitted int              `json:"omitted"`
		Text    string           `json:"text"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Alerts) != MaxItemsPerMessage || m.Omitted != 7 {
		t.Errorf("alerts=%d omitted=%d, want %d y 7", len(m.Alerts), m.Omitted, MaxItemsPerMessage)
	}
	if m.Alerts[0]["value"] != nil || m.Alerts[0]["state"] != "resolved" {
		t.Errorf("alerta got %v", m.Alerts[0])
	}
	if _, has := m.Alerts[0]["url"]; has {
		t.Error("sin ARGOS_PUBLIC_URL no debe haber enlace")
	}
	if !strings.Contains(m.Text, "y 7 alertas mas") {
		t.Errorf("text debe resumir las omitidas: %q", m.Text)
	}
}

func TestBuildTestMessageCarriesNoAlertData(t *testing.T) {
	t.Parallel()
	raw, err := BuildTestMessage(t0)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Kind   string `json:"kind"`
		Alerts []any  `json:"alerts"`
	}
	if err := json.Unmarshal(raw, &m); err != nil || m.Kind != "test" || len(m.Alerts) != 0 {
		t.Errorf("got %+v, %v", m, err)
	}
}

func TestBuildMessageIncludesCleanedDetailInAlertAndText(t *testing.T) {
	t.Parallel()
	raw, err := BuildMessage([]Item{{Event: "opened", Hostname: "srv", Rule: "puertos", Kind: "exposed_port", Severity: "warn", Value: f64(2),
		Detail: "tcp/22,\r\n" + string(rune(0x202e)) + "tcp/80"}}, 0, "", t0)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Text   string `json:"text"`
		Alerts []struct {
			Detail string `json:"detail"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if got := m.Alerts[0].Detail; got != "tcp/22,tcp/80" || strings.ContainsAny(got, "\r\n"+string(rune(0x202e))) {
		t.Errorf("detail got %q, want limpio", got)
	}
	if !strings.Contains(m.Text, "puertos: tcp/22") || strings.Count(m.Text, "\n") != 0 {
		t.Errorf("text got %q", m.Text)
	}
	// Sin detalle, el campo no aparece.
	raw, err = BuildMessage([]Item{{Event: "opened", Hostname: "srv", Rule: "cpu", Severity: "warn"}}, 0, "", t0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"detail"`) {
		t.Errorf("no debe haber detail sin detalle: %s", raw)
	}
}

func TestBuildMessageDetailLabelDependsOnKind(t *testing.T) {
	t.Parallel()
	raw, err := BuildMessage([]Item{{Event: "opened", Hostname: "srv", Rule: "sec", Kind: "pending_updates", Severity: "warn", Value: f64(5),
		Detail: "5 de seguridad, 12 en total"}}, 0, "", t0)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(m.Text, " - 5 de seguridad, 12 en total") || strings.Contains(m.Text, "puertos") {
		t.Errorf("text got %q, want detalle sin el rotulo de puertos", m.Text)
	}
}
