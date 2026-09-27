package httpapi

import (
	"net/http"
	"time"

	"argos/server/internal/store"
)

type summaryGroupDTO struct {
	GroupID  *string `json:"group_id"`
	Name     string  `json:"name"`
	Total    int     `json:"total"`
	Up       int     `json:"up"`
	Down     int     `json:"down"`
	Offline  int     `json:"offline"`
	Pending  int     `json:"pending"`
	Disabled int     `json:"disabled"`
}

// agentVersionDTO cuenta los hosts con agente que informan una version.
type agentVersionDTO struct {
	Version string `json:"version"`
	Hosts   int    `json:"hosts"`
}

type topHostDTO struct {
	ID       string  `json:"id"`
	Hostname string  `json:"hostname"`
	Value    float64 `json:"value"`
	Mount    *string `json:"mount,omitempty"`
}

type recentDownDTO struct {
	ID              string       `json:"id"`
	Hostname        string       `json:"hostname"`
	StatusChangedAt time.Time    `json:"status_changed_at"`
	Group           *groupRefDTO `json:"group"`
}

// ungroupedName es el nombre con el que by_group rotula a los hosts sin grupo (group_id null).
const ungroupedName = "Sin grupo"

func toTopDTOs(in []store.TopHost) []topHostDTO {
	out := make([]topHostDTO, 0, len(in))
	for _, t := range in {
		out = append(out, topHostDTO{ID: t.ID, Hostname: t.Hostname, Value: roundPct(t.Value), Mount: t.Mount})
	}
	return out
}

// summary devuelve la foto de la flota (viewer o admin): conteos por estado y por grupo, rankings de hosts
// up por cpu, memoria y disco, y los ultimos hosts caidos.
func (a *API) summary(w http.ResponseWriter, r *http.Request) error {
	sum, err := a.store.Summary(r.Context(), currentUser(r).Scope)
	if err != nil {
		return err
	}
	byGroup := make([]summaryGroupDTO, 0, len(sum.ByGroup))
	for _, g := range sum.ByGroup {
		dto := summaryGroupDTO{
			Name: ungroupedName, Total: g.Counts.Total(),
			Up: g.Counts.Up, Down: g.Counts.Down, Offline: g.Counts.Offline, Pending: g.Counts.Pending, Disabled: g.Counts.Disabled,
		}
		if g.Group != nil {
			dto.GroupID, dto.Name = &g.Group.ID, g.Group.Name
		}
		byGroup = append(byGroup, dto)
	}
	recent := make([]recentDownDTO, 0, len(sum.RecentlyDown))
	for _, d := range sum.RecentlyDown {
		recent = append(recent, recentDownDTO{ID: d.ID, Hostname: d.Hostname, StatusChangedAt: d.StatusChangedAt.UTC(), Group: toGroupRefDTO(d.Group)})
	}
	versions := make([]agentVersionDTO, 0, len(sum.AgentVersions))
	outdated := 0 // hosts con agente desactualizado (misma regla que agent_outdated); sin catalogo, 0
	for _, v := range sum.AgentVersions {
		versions = append(versions, agentVersionDTO{Version: v.Version, Hosts: v.Hosts})
		if a.isAgentOutdated(&v.Version) {
			outdated += v.Hosts
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": a.now().UTC(),
		"counts": map[string]int{
			"up": sum.Counts.Up, "down": sum.Counts.Down, "offline": sum.Counts.Offline,
			"pending": sum.Counts.Pending, "disabled": sum.Counts.Disabled,
		},
		"devices": map[string]int{
			"up": sum.Devices.Up, "down": sum.Devices.Down, "offline": sum.Devices.Offline,
			"pending": sum.Devices.Pending, "disabled": sum.Devices.Disabled,
		},
		"agent_versions": map[string]any{"latest": a.latestAgentVersion(), "counts": versions, "outdated": outdated},
		"by_group":       byGroup,
		"top_cpu":        toTopDTOs(sum.TopCPU),
		"top_mem":        toTopDTOs(sum.TopMem),
		"top_disk":       toTopDTOs(sum.TopDisk),
		"recently_down":  recent,
	})
	return nil
}
