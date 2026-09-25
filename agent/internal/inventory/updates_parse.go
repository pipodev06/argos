package inventory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Los parsers son de lista blanca: lo que no encaja se ignora y nunca se copia texto libre
// a la salida. Reciben salida ya acotada por runCmd.

const maxParseLine = 4096 // lineas mas largas se ignoran

func lines(b []byte) []string {
	var out []string
	r := bufio.NewReaderSize(bytes.NewReader(b), maxParseLine)
	skipping := false // estamos en el resto de una linea demasiado larga
	for {
		l, isPrefix, err := r.ReadLine()
		if err != nil {
			return out
		}
		if !skipping && !isPrefix {
			out = append(out, string(l))
		}
		skipping = isPrefix
	}
}

// parseAptSim cuenta las lineas "Inst" de `apt-get -s upgrade`; es de seguridad si el origen
// (entre parentesis) trae "-security" (Debian y Ubuntu).
func parseAptSim(out []byte) (total, security int) {
	for _, l := range lines(out) {
		if !strings.HasPrefix(l, "Inst ") {
			continue
		}
		total++
		if _, origin, ok := strings.Cut(l, " ("); ok && strings.Contains(origin, "-security") {
			security++
		}
	}
	return total, security
}

// parseDnfCheck cuenta los paquetes de `dnf check-update` (tres campos: nombre.arch version repo);
// corta en la seccion "Obsoleting Packages".
func parseDnfCheck(out []byte) int {
	n := 0
	for _, l := range lines(out) {
		if strings.HasPrefix(l, "Obsoleting") {
			break
		}
		f := strings.Fields(l)
		if len(f) == 3 && strings.Contains(f[0], ".") && !strings.HasSuffix(f[0], ":") {
			n++
		}
	}
	return n
}

// parseDnfSecurity lee `dnf updateinfo list --security` (id, severidad/tipo, paquete): devuelve
// los paquetes distintos con aviso de seguridad y los ids de aviso.
func parseDnfSecurity(out []byte) (packages int, ids []string) {
	pkgs := map[string]bool{}
	for _, l := range lines(out) {
		f := strings.Fields(l)
		if len(f) != 3 || !strings.Contains(strings.ToLower(f[1]), "sec") {
			continue
		}
		pkgs[f[2]] = true
		ids = append(ids, f[0])
	}
	return len(pkgs), ids
}

// parseZypperXML cuenta los <update kind=kind> de `zypper -x` y devuelve sus nombres.
func parseZypperXML(out []byte, kind string) (count int, names []string, err error) {
	dec := xml.NewDecoder(bytes.NewReader(out))
	sawRoot := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, nil, fmt.Errorf("xml de zypper invalido: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "stream" {
			sawRoot = true
		}
		if se.Name.Local != "update" {
			continue
		}
		var name, k string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "name":
				name = a.Value
			case "kind":
				k = a.Value
			}
		}
		if k == kind {
			count++
			names = append(names, name)
		}
	}
	if !sawRoot {
		return 0, nil, errors.New("salida de zypper sin <stream>")
	}
	return count, names, nil
}

// parseSoftwareUpdate lee `softwareupdate -l`. known=false si no reconoce el formato (otro
// idioma o version): entonces no se afirma ni 0 pendientes ni reinicio.
func parseSoftwareUpdate(out []byte) (total int, restart, known bool) {
	for _, l := range lines(out) {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "No new software available"):
			known = true
		case strings.HasPrefix(t, "Software Update found"):
			known = true
		case strings.HasPrefix(t, "* Label:"):
			total++
		case strings.Contains(t, "Action: restart"):
			restart = true
		}
	}
	if total > 0 {
		known = true
	}
	return total, restart, known
}

var startDateRe = regexp.MustCompile(`^Start-Date: (\d{4}-\d{2}-\d{2})`)

// parseAptHistory devuelve la fecha mas reciente de un bloque de /var/log/apt/history.log que
// instalo o actualizo paquetes ("" si no hay).
func parseAptHistory(data []byte) string {
	last, cur := "", ""
	for _, l := range lines(data) {
		if m := startDateRe.FindStringSubmatch(l); m != nil {
			cur = m[1]
			continue
		}
		if cur != "" && (strings.HasPrefix(l, "Upgrade: ") || strings.HasPrefix(l, "Install: ")) && cur > last {
			last = cur
		}
	}
	return last
}

// parseRPMInstallTimes devuelve el INSTALLTIME mas reciente (fecha UTC) de `rpm -qa --qf`.
func parseRPMInstallTimes(out []byte) string {
	var maxT int64
	for _, l := range lines(out) {
		v, err := strconv.ParseInt(strings.TrimSpace(l), 10, 64)
		if err == nil && v > maxT {
			maxT = v
		}
	}
	if maxT <= 0 {
		return ""
	}
	return time.Unix(maxT, 0).UTC().Format(time.DateOnly)
}

var kbRe = regexp.MustCompile(`^KB\d{6,8}$`)

// wuaOut es el JSON que imprime el script de PowerShell (campos null = desconocido).
type wuaOut struct {
	Total     *int    `json:"total"`
	Security  *int    `json:"security"`
	Kbs       *string `json:"kbs"` // "KB1,KB2"
	Reboot    *bool   `json:"reboot"`
	Last      *string `json:"last"`
	CacheAgeS *int64  `json:"cache_age_s"`
}

// parseWUA lee la ultima linea JSON de la salida del script de Windows Update.
func parseWUA(out []byte) (Updates, error) {
	var jsonLine string
	for _, l := range lines(out) {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "{") {
			jsonLine = t
		}
	}
	if jsonLine == "" {
		return Updates{}, errors.New("el script de Windows Update no devolvio JSON")
	}
	var w wuaOut
	if err := json.Unmarshal([]byte(jsonLine), &w); err != nil {
		return Updates{}, fmt.Errorf("json de Windows Update invalido: %w", err)
	}
	u := Updates{Source: "wua", PendingTotal: w.Total, PendingSecurity: w.Security, RebootRequired: w.Reboot, LastInstalled: w.Last, CacheAgeS: w.CacheAgeS}
	if w.Kbs != nil {
		for _, kb := range strings.Split(*w.Kbs, ",") {
			if kbRe.MatchString(kb) {
				u.Advisories = append(u.Advisories, kb)
			}
		}
	}
	return u, nil
}
