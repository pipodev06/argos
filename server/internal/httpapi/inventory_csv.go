package httpapi

import (
	"net/http"
	"strconv"

	"argos/server/internal/store"
)

// Exportar el inventario (hosts) como CSV: mismos filtros que la lista
// equivalente, sin paginar, topado en store.maxInventoryExportRows filas (corte silencioso, sin error).

var hostsInventoryHeader = []string{"hostname", "ip", "os", "grupo", "estado", "version_agente",
	"cpu_pct", "mem_pct", "disco_pct", "bateria_pct", "visto"}

func hostInventoryRow(h store.HostRow) []string {
	battery := ""
	if h.BatteryPct != nil {
		battery = strconv.Itoa(*h.BatteryPct)
	}
	return []string{csvSafe(h.Hostname), csvStrPtr(firstUsableIP(h.IPs)), csvSafe(h.OS), csvStrPtr(groupName(h.Group)),
		csvSafe(h.Status), csvStrPtr(h.AgentVersion), csvPct(round1(h.CPUPct)), csvPct(hostMemPct(h)), csvPct(round1(h.DiskPct)),
		battery, csvTime(h.LastSeenAt)}
}

// groupName devuelve el nombre del grupo (nil si el host no tiene grupo).
func groupName(g *store.GroupRef) *string {
	if g == nil {
		return nil
	}
	return &g.Name
}

// exportHostsCSV: GET /api/v1/hosts.csv, mismos filtros que GET /hosts salvo cursor/limit/sort/dir.
func (a *API) exportHostsCSV(w http.ResponseWriter, r *http.Request) error {
	q, err := parseHostQuery(r.URL.Query(), currentUser(r).Scope)
	if err != nil {
		return err
	}
	if q.filter.AgentOutdated {
		if err := a.resolveOutdated(r.Context(), currentUser(r).Scope, &q.filter); err != nil {
			return err
		}
	}
	ex := newCSVExport(w, csvFilename("inventario-hosts", "", a.now()), hostsInventoryHeader)
	hosts, err := a.store.ExportHosts(r.Context(), currentUser(r).Scope, q.filter)
	if err == nil {
		for _, h := range hosts {
			if err = ex.row(hostInventoryRow(h)...); err != nil {
				break
			}
		}
	}
	return a.finishInventoryExport(r, "inventario-hosts", ex, err)
}

// auditInventoryExport registra la exportacion de inventario (tipo y filas escritas, nunca contenido); reusa el
// mismo evento que los informes (report_exported).
func (a *API) auditInventoryExport(r *http.Request, kind string, rows int, outcome, reason string) {
	d := map[string]any{"report": kind, "rows": rows}
	if reason != "" {
		d["reason"] = reason
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType = evReportExported, outcome, "report"
	a.audit(r, e, d)
}

// finishInventoryExport cierra el CSV y audita, igual que finishExport pero sin el caso de tope: aqui el corte a
// maxInventoryExportRows ya lo aplico el LIMIT de la consulta, en silencio.
func (a *API) finishInventoryExport(r *http.Request, kind string, ex *csvExport, err error) error {
	if err == nil {
		err = ex.finish()
	}
	switch {
	case err == nil:
		a.auditInventoryExport(r, kind, ex.rows, store.OutcomeOK, "")
		return nil
	case ex.started:
		a.logger.WarnContext(r.Context(), "exportacion de inventario interrumpida", "report", kind, "rows", ex.rows, "err", err)
		a.auditInventoryExport(r, kind, ex.rows, store.OutcomeFail, "aborted")
		return nil
	default:
		return a.reportErr(r, err)
	}
}
