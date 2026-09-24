package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"argos/server/internal/store"
)

// Limites de los parametros de la lista de hosts.
const (
	maxQueryLen = 128
	// maxFilterValues acota los valores repetidos de group, os y tag.
	maxFilterValues = 20
	maxCursorLen    = 2048
	defaultSort     = "severity"
	minSeenBefore   = 60       // segundos
	maxSeenBefore   = 31536000 // un anio
)

// validHostStatuses es el vocabulario de hosts.status (CHECK de la migracion 00012).
var validHostStatuses = map[string]bool{"pending": true, "up": true, "down": true, "offline": true, "disabled": true}

// validOSes es el vocabulario del filtro os.
var validOSes = map[string]bool{"windows": true, "linux": true, "darwin": true}

var errInvalidCursor = &clientError{status: http.StatusBadRequest, code: "invalid_cursor", msg: "cursor invalido o vencido: vuelve a la primera pagina"}

type diskPctDTO struct {
	Pct   float64 `json:"pct"`
	Mount string  `json:"mount"`
}

type batteryDTO struct {
	Pct      int   `json:"pct"`
	Charging *bool `json:"charging"`
}

type hostItemDTO struct {
	ID              string       `json:"id"`
	Hostname        string       `json:"hostname"`
	Status          string       `json:"status"`
	StatusChangedAt time.Time    `json:"status_changed_at"`
	LastSeenAt      *time.Time   `json:"last_seen_at"`
	Group           *groupRefDTO `json:"group"`
	Tags            []string     `json:"tags"`
	OS              string       `json:"os"`
	OSName          *string      `json:"os_name"`
	OSVersion       *string      `json:"os_version"`
	IP              *string      `json:"ip"`
	CPUPct          *float64     `json:"cpu_pct"`
	MemPct          *float64     `json:"mem_pct"`
	Disk            *diskPctDTO  `json:"disk"`
	BootTime        *time.Time   `json:"boot_time"`
	Battery         *batteryDTO  `json:"battery"`
}

func toHostItemDTO(h store.HostRow) hostItemDTO {
	dto := hostItemDTO{
		ID: h.ID, Hostname: h.Hostname, Status: h.Status, StatusChangedAt: h.StatusChangedAt.UTC(), LastSeenAt: utcPtr(h.LastSeenAt),
		Group: toGroupRefDTO(h.Group), Tags: nonNilTags(h.Tags), OS: h.OS, OSName: h.OSName, OSVersion: h.OSVersion, IP: firstUsableIP(h.IPs),
		CPUPct: round1(h.CPUPct),
	}
	if h.MemUsedMB != nil && h.MemTotalMB != nil && *h.MemTotalMB > 0 {
		pct := float64(*h.MemUsedMB) * 100 / float64(*h.MemTotalMB)
		dto.MemPct = round1(&pct)
	}
	if h.DiskPct != nil && h.DiskMount != nil {
		dto.Disk = &diskPctDTO{Pct: *round1(h.DiskPct), Mount: *h.DiskMount}
	}
	if h.BootTime != nil {
		t := h.BootTime.UTC()
		dto.BootTime = &t
	}
	if h.BatteryPct != nil {
		dto.Battery = &batteryDTO{Pct: *h.BatteryPct, Charging: h.BatteryCharging}
	}
	return dto
}

// utcPtr devuelve la hora en UTC (ISO 8601 con Z), o nil.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// firstUsableIP devuelve la primera IP que no es de loopback, o nil.
func firstUsableIP(ips []netip.Addr) *string {
	for _, ip := range ips {
		if !ip.IsLoopback() {
			s := ip.String()
			return &s
		}
	}
	return nil
}

// hostQuery es la consulta ya validada de GET /hosts.
type hostQuery struct {
	filter store.HostFilter
	// fingerprint identifica filtros + orden + direccion: un cursor solo vale pa la consulta que lo emitio.
	fingerprint string
	cursor      string
}

func parseHostQuery(v url.Values) (hostQuery, error) {
	var f store.HostFilter

	q := strings.TrimSpace(v.Get("q"))
	if utf8.RuneCountInString(q) > maxQueryLen || hasControl(q) {
		return hostQuery{}, badRequest("q excede %d caracteres o tiene caracteres invalidos", maxQueryLen)
	}
	f.Q = q

	for _, raw := range v["status"] {
		for st := range strings.SplitSeq(raw, ",") {
			st = strings.TrimSpace(st)
			if st == "" {
				continue
			}
			if !validHostStatuses[st] {
				return hostQuery{}, badRequest("status invalido %q: usa pending, up, down, offline o disabled", st)
			}
			if !slices.Contains(f.Statuses, st) {
				f.Statuses = append(f.Statuses, st)
			}
		}
	}
	slices.Sort(f.Statuses)

	if err := parseGroupFilter(v["group"], &f); err != nil {
		return hostQuery{}, err
	}
	oses, err := parseOSFilter(v["os"])
	if err != nil {
		return hostQuery{}, err
	}
	f.OSes = oses

	tags, err := parseTagFilter(v["tag"])
	if err != nil {
		return hostQuery{}, err
	}
	f.Tags = tags

	if f.CPUGt, err = parsePctFilter(v, "cpu_gt"); err != nil {
		return hostQuery{}, err
	}
	if f.MemGt, err = parsePctFilter(v, "mem_gt"); err != nil {
		return hostQuery{}, err
	}
	if f.DiskGt, err = parsePctFilter(v, "disk_gt"); err != nil {
		return hostQuery{}, err
	}
	if f.BatteryLt, err = parsePctFilter(v, "battery_lt"); err != nil {
		return hostQuery{}, err
	}
	if raw := v.Get("seen_before"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < minSeenBefore || n > maxSeenBefore {
			return hostQuery{}, invalidFilter("seen_before debe ser un entero de segundos entre %d y %d", minSeenBefore, maxSeenBefore)
		}
		f.SeenBefore = n
	}

	f.Sort = v.Get("sort")
	if f.Sort == "" {
		f.Sort = defaultSort
	}
	defaultDesc, ok := store.HostSortDefaultDesc(f.Sort)
	if !ok {
		return hostQuery{}, &clientError{status: http.StatusBadRequest, code: "invalid_sort",
			msg: "sort debe ser severity, name, status, group, cpu, mem, disk o last_seen"}
	}
	switch d := v.Get("dir"); d {
	case "":
		f.Desc = defaultDesc
	case "asc":
	case "desc":
		f.Desc = true
	default:
		return hostQuery{}, badRequest("dir debe ser asc o desc")
	}

	f.Limit = store.DefaultHostListLimit
	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > store.MaxHostListLimit {
			return hostQuery{}, badRequest("limit debe ser un entero entre 1 y %d", store.MaxHostListLimit)
		}
		f.Limit = n
	}

	cursor := v.Get("cursor")
	if len(cursor) > maxCursorLen {
		return hostQuery{}, errInvalidCursor
	}
	return hostQuery{filter: f, fingerprint: fingerprint(f), cursor: cursor}, nil
}

// parseGroupFilter lee group repetido: ids de grupo y/o none (sin grupo), unicos y ordenados.
func parseGroupFilter(raw []string, f *store.HostFilter) error {
	if len(raw) > maxFilterValues {
		return invalidFilter("maximo %d valores en group", maxFilterValues)
	}
	for _, g := range raw {
		switch {
		case g == "none":
			f.NoGroup = true
		case isUUID(g):
			if id := strings.ToLower(g); !slices.Contains(f.GroupIDs, id) {
				f.GroupIDs = append(f.GroupIDs, id)
			}
		default:
			return invalidFilter("group debe ser un id de grupo o none")
		}
	}
	slices.Sort(f.GroupIDs)
	return nil
}

// parseOSFilter lee os repetido: windows, linux o darwin (sin distinguir mayusculas), unicos y ordenados.
func parseOSFilter(raw []string) ([]string, error) {
	if len(raw) > maxFilterValues {
		return nil, invalidFilter("maximo %d valores en os", maxFilterValues)
	}
	var out []string
	for _, o := range raw {
		o = strings.ToLower(strings.TrimSpace(o))
		if !validOSes[o] {
			return nil, invalidFilter("os debe ser windows, linux o darwin")
		}
		if !slices.Contains(out, o) {
			out = append(out, o)
		}
	}
	slices.Sort(out)
	return out, nil
}

// invalidFilter es un 400 de un filtro de la lista con valor fuera de formato o rango.
func invalidFilter(format string, args ...any) error {
	return &clientError{status: http.StatusBadRequest, code: "invalid_filter", msg: fmt.Sprintf(format, args...)}
}

// parsePctFilter lee un filtro numerico 0-100 opcional (nil si no viene).
func parsePctFilter(v url.Values, name string) (*float64, error) {
	raw := v.Get(name)
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(n) || n < 0 || n > 100 {
		return nil, invalidFilter("%s debe ser un numero entre 0 y 100", name)
	}
	return &n, nil
}

// fingerprint resume filtros, orden y direccion (no el limite: se puede cambiar entre paginas).
func fingerprint(f store.HostFilter) string {
	b, err := json.Marshal([]any{f.Q, f.Statuses, f.GroupIDs, f.NoGroup, f.OSes, f.Tags, f.Sort, f.Desc,
		f.CPUGt, f.MemGt, f.DiskGt, f.BatteryLt, f.SeenBefore})
	if err != nil {
		return "" // solo strings, bools y listas: no deberia ocurrir
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// cursorPayload es lo que viaja (firmado) en el cursor opaco.
type cursorPayload struct {
	F  string `json:"f"` // huella de la consulta
	NF int    `json:"n"`
	SK string `json:"k"`
	LN string `json:"l"`
	ID string `json:"i"`
}

// encodeCursor codifica y firma (HMAC-SHA256) la posicion. El cliente no puede fabricar ni alterar cursores.
func (a *API) encodeCursor(fp string, c store.HostCursor) string {
	payload, err := json.Marshal(cursorPayload{F: fp, NF: c.NullFlag, SK: c.SortKey, LN: c.LName, ID: c.ID})
	if err != nil {
		return "" // struct de strings/enteros: no deberia ocurrir
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(a.sign(payload))
}

func (a *API) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, a.cursorKey)
	mac.Write(payload)
	return mac.Sum(nil)
}

// decodeCursor valida firma y huella y devuelve la posicion. Cualquier fallo es errInvalidCursor.
func (a *API) decodeCursor(raw, fp string) (*store.HostCursor, error) {
	body, sig, ok := strings.Cut(raw, ".")
	if !ok {
		return nil, errInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, errInvalidCursor
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, a.sign(payload)) {
		return nil, errInvalidCursor
	}
	var p cursorPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.F != fp || !isUUID(p.ID) || (p.NF != 0 && p.NF != 1) {
		return nil, errInvalidCursor
	}
	return &store.HostCursor{NullFlag: p.NF, SortKey: p.SK, LName: p.LN, ID: p.ID}, nil
}

// listHosts devuelve una pagina de hosts (viewer o admin): filtros, orden y paginacion por cursor.
func (a *API) listHosts(w http.ResponseWriter, r *http.Request) error {
	q, err := parseHostQuery(r.URL.Query())
	if err != nil {
		return err
	}
	if q.cursor != "" {
		if q.filter.After, err = a.decodeCursor(q.cursor, q.fingerprint); err != nil {
			return err
		}
	}
	page, err := a.store.ListHosts(r.Context(), q.filter)
	if err != nil {
		return err
	}
	items := make([]hostItemDTO, 0, len(page.Items))
	for _, h := range page.Items {
		items = append(items, toHostItemDTO(h))
	}
	var next *string
	if page.NextCursor != nil {
		c := a.encodeCursor(q.fingerprint, *page.NextCursor)
		next = &c
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next, "total": page.Total})
	return nil
}

// roundPct redondea un porcentaje a 1 decimal (los DTO de resumen usan value ya redondeado).
func roundPct(v float64) float64 { return math.Round(v*10) / 10 }

// parseTagFilter valida los filtros tag repetidos (AND): sin distinguir mayusculas, sin repetidos, ordenados.
func parseTagFilter(raw []string) ([]string, error) {
	if len(raw) > maxFilterValues {
		return nil, invalidFilter("maximo %d filtros tag", maxFilterValues)
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		t, err := checkTag(r)
		if err != nil {
			return nil, invalidFilter("tag invalido")
		}
		out = append(out, strings.ToLower(t))
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// nonNilTags devuelve siempre una lista (nunca null) pa que el cliente no tenga que distinguirlo.
func nonNilTags(t []string) []string {
	if t == nil {
		return []string{}
	}
	return t
}
