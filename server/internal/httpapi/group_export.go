package httpapi

import (
	"archive/zip"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"argos/server/internal/store"
)

// Exportar el cliente antes de purgarlo (B17): un zip con hosts.csv (hosts y dispositivos del grupo) y
// disponibilidad.csv (mismo informe que ya existe, para el rango por defecto de los informes: 30 dias), para
// conservar un respaldo legible fuera de la base antes de la baja irreversible. Mismo cupo que los informes
// (a.reportSem): es una consulta pesada mas.

// groupExportWriteWindow amplia el plazo de escritura, igual que los otros exports largos (history.go, purge).
const groupExportWriteWindow = 5 * time.Minute

var hostsCSVHeader = []string{"host_id", "hostname", "kind", "status", "os", "ip", "created_at"}

// hostsCSVRow arma las celdas de una fila de hosts.csv.
func hostsCSVRow(h store.GroupExportHost) []string {
	return []string{csvSafe(h.ID), csvSafe(h.Hostname), csvSafe(h.Kind), csvSafe(h.Status), csvStrPtr(h.OS),
		csvStrPtr(h.IP), h.CreatedAt.UTC().Format(time.RFC3339)}
}

// writeHostsCSV escribe hosts.csv (cabecera y una fila por host o dispositivo) en un io.Writer.
func writeHostsCSV(w *zip.Writer, hosts []store.GroupExportHost) (int, error) {
	f, err := w.Create("hosts.csv")
	if err != nil {
		return 0, err
	}
	cw := csv.NewWriter(f)
	if err := cw.Write(hostsCSVHeader); err != nil {
		return 0, err
	}
	rows := 0
	for _, h := range hosts {
		if err := cw.Write(hostsCSVRow(h)); err != nil {
			return rows, err
		}
		rows++
	}
	cw.Flush()
	return rows, cw.Error()
}

// exportZipFilename arma "cliente-<grupo>-AAAAMMDD.zip" con el nombre del grupo saneado (mismo criterio que
// historyFilename: fuera de [A-Za-z0-9._-] pasa a "_").
func exportZipFilename(groupName string, now time.Time) string {
	name := unsafeFileChars.ReplaceAllString(groupName, "_")
	if len(name) > maxFilenameHostLen {
		name = name[:maxFilenameHostLen]
	}
	return "cliente-" + name + "-" + now.UTC().Format("20060102") + ".zip"
}

// exportGroupZip: GET /api/v1/groups/{id}/export.zip (admin) exporta a un zip los hosts/dispositivos del grupo y su
// disponibilidad de los ultimos 30 dias, para conservar el cliente antes de purgarlo. 404 si el grupo no existe.
func (a *API) exportGroupZip(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	id = strings.ToLower(id)

	release, err := a.acquireReport()
	if err != nil {
		return err
	}
	defer release()

	name, err := a.store.GroupName(r.Context(), id)
	if err != nil {
		return err
	}
	hosts, err := a.store.ExportGroupHosts(r.Context(), id)
	if err != nil {
		return err
	}
	to := a.now().UTC().Truncate(time.Second)
	from := to.Add(-reportRanges["30d"])
	avail, err := a.store.AvailabilityReport(r.Context(), store.AllScope(), &id, from, to)
	if err != nil {
		return err
	}

	rc := http.NewResponseController(w)
	// Sin soporte de deadline (p. ej. httptest) no pasa nada: rige el del servidor.
	if err := rc.SetWriteDeadline(time.Now().Add(groupExportWriteWindow)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		a.logger.WarnContext(r.Context(), "no se pudo ampliar el plazo de escritura del export de grupo", "err", err)
	}

	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", `attachment; filename="`+exportZipFilename(name, to)+`"`)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	zw := zip.NewWriter(w)
	hostsRows, err := writeHostsCSV(zw, hosts)
	availRows := 0
	if err == nil {
		var f io.Writer
		f, err = zw.Create("disponibilidad.csv")
		if err == nil {
			availRows, err = writeAvailabilityCSV(f, avail, from, to)
		}
	}
	if err == nil {
		err = zw.Close()
	}

	e := actor(currentUser(r))
	e.Event, e.TargetType, e.TargetID = evGroupExported, "group", id
	detail := map[string]any{"hosts_rows": hostsRows, "availability_rows": availRows}
	if err != nil {
		// Ya se empezo a escribir la respuesta (WriteHeader ya salio): no se puede cambiar el status. Se loguea y
		// queda constancia en la auditoria con lo que se llego a escribir.
		a.logger.WarnContext(r.Context(), "export de grupo interrumpido", "group_id", id, "err", err)
		e.Outcome = store.OutcomeFail
		detail["reason"] = "aborted"
		a.writeAudit(r.Context(), e, detail)
		return nil
	}
	e.Outcome = store.OutcomeOK
	a.audit(r, e, detail)
	return nil
}
