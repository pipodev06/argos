package httpapi

import (
	"net/http"
	"net/netip"
	"strings"
	"time"

	"argos/server/internal/store"
)

const maxDeviceNameLen = 100

// --- DTOs ---

type collectorRefDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// profileRefDTO es la referencia a un perfil SNMP (sin secretos).
type profileRefDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// deviceDTO es un dispositivo sin agente. address y collector solo los ve un admin: son datos de la red interna y
// el colector puede estar fuera del ambito de un viewer restringido. evaluated es false si el colector no esta up
// (el estado del dispositivo no se esta evaluando).
type deviceDTO struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Kind            string           `json:"kind"`
	Address         *string          `json:"address,omitempty"`
	Collector       *collectorRefDTO `json:"collector,omitempty"`
	SNMPProfile     *profileRefDTO   `json:"snmp_profile,omitempty"`
	Group           *groupRefDTO     `json:"group"`
	Status          string           `json:"status"`
	StatusChangedAt time.Time        `json:"status_changed_at"`
	LastSeenAt      *time.Time       `json:"last_seen_at"`
	Evaluated       bool             `json:"evaluated"`
	CreatedAt       time.Time        `json:"created_at"`
}

func toDeviceDTO(u *sessionUser, d store.Device) deviceDTO {
	dto := deviceDTO{
		ID: d.ID, Name: d.Name, Kind: "device", Group: toGroupRefDTO(d.Group), Status: d.Status,
		StatusChangedAt: d.StatusChangedAt.UTC(), LastSeenAt: utcPtr(d.LastSeenAt), Evaluated: d.CollectorReachable,
		CreatedAt: d.CreatedAt.UTC(),
	}
	if u != nil && u.Role == store.RoleAdmin {
		addr := d.Address.String()
		dto.Address = &addr
		dto.Collector = &collectorRefDTO{ID: d.CollectorID, Name: d.CollectorName}
		if d.Profile != nil {
			dto.SNMPProfile = &profileRefDTO{ID: d.Profile.ID, Name: d.Profile.Name, Version: d.Profile.Version}
		}
	}
	return dto
}

// --- validacion ---

// checkDeviceAddress valida una IP literal (nunca un nombre) y la normaliza (IPv4 mapeada en IPv6 pasa a IPv4). Rechaza
// direcciones que jamas son un dispositivo sondeable: no especificada, loopback, link-local (incluye 169.254/16, el
// metadata de la nube), multicast y broadcast.
func checkDeviceAddress(raw string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || addr.Zone() != "" {
		return netip.Addr{}, badRequest("address debe ser una IP literal (IPv4 o IPv6), sin zona ni nombre")
	}
	addr = addr.Unmap()
	if addr.IsUnspecified() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() || addr.IsInterfaceLocalMulticast() || addr == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return netip.Addr{}, badRequest("address no puede ser loopback, link-local, multicast, broadcast ni no especificada")
	}
	return addr, nil
}

// checkUUIDField valida un id que llega en el body y lo normaliza a minusculas.
func checkUUIDField(field, raw string) (string, error) {
	if !isUUID(raw) {
		return "", badRequest("%s invalido", field)
	}
	return strings.ToLower(raw), nil
}

// --- lecturas ---

func (a *API) listDevices(w http.ResponseWriter, r *http.Request) error {
	u := currentUser(r)
	limit, err := parseLimit(r.URL.Query())
	if err != nil {
		return err
	}
	fp := "devices|" + u.Scope.Key()
	var after *store.HostCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		if after, err = a.decodeCursor(raw, fp); err != nil {
			return err
		}
	}
	page, err := a.store.ListDevices(r.Context(), u.Scope, after, limit)
	if err != nil {
		return err
	}
	items := make([]deviceDTO, 0, len(page.Items))
	for _, d := range page.Items {
		items = append(items, toDeviceDTO(u, d))
	}
	var next *string
	if page.NextCursor != nil {
		c := a.encodeCursor(fp, *page.NextCursor)
		next = &c
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next, "total": page.Total})
	return nil
}

func (a *API) getDevice(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return store.ErrNotFound // id malformado y ajeno son indistinguibles
	}
	u := currentUser(r)
	d, err := a.store.DeviceByID(r.Context(), u.Scope, strings.ToLower(id))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"device": toDeviceDTO(u, d)})
	return nil
}

// --- escrituras (solo admin) ---

type createDeviceReq struct {
	Name        string  `json:"name"`
	Address     string  `json:"address"`
	CollectorID string  `json:"collector_id"`
	GroupID     *string `json:"group_id"`
}

func (a *API) createDevice(w http.ResponseWriter, r *http.Request) error {
	var req createDeviceReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	name, err := checkName("name", req.Name, maxDeviceNameLen)
	if err != nil {
		return err
	}
	addr, err := checkDeviceAddress(req.Address)
	if err != nil {
		return err
	}
	collector, err := checkUUIDField("collector_id", req.CollectorID)
	if err != nil {
		return err
	}
	var groupID *string
	if req.GroupID != nil {
		g, err := checkUUIDField("group_id", *req.GroupID)
		if err != nil {
			return err
		}
		groupID = &g
	}
	d, err := a.store.CreateDevice(r.Context(), name, addr, collector, groupID)
	if err != nil {
		return err
	}
	u := currentUser(r)
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evDeviceCreated, store.OutcomeOK, "device", d.ID
	detail := map[string]any{"name": d.Name, "collector_id": d.CollectorID}
	if d.Group != nil {
		detail["group_id"] = d.Group.ID
	}
	a.audit(r, e, detail)
	writeJSON(w, http.StatusCreated, map[string]any{"device": toDeviceDTO(u, d)})
	return nil
}

type updateDeviceReq struct {
	Name        *string          `json:"name"`
	Address     *string          `json:"address"`
	CollectorID *string          `json:"collector_id"`
	GroupID     optional[string] `json:"group_id"`
	// SNMPProfileID: un id asigna el perfil y null lo quita.
	SNMPProfileID optional[string] `json:"snmp_profile_id"`
}

func (a *API) updateDevice(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return store.ErrNotFound
	}
	var req updateDeviceReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	var ch store.DeviceChange
	var fields []string
	if req.Name != nil {
		name, err := checkName("name", *req.Name, maxDeviceNameLen)
		if err != nil {
			return err
		}
		ch.Name = &name
		fields = append(fields, "name")
	}
	if req.Address != nil {
		addr, err := checkDeviceAddress(*req.Address)
		if err != nil {
			return err
		}
		ch.Address = &addr
		fields = append(fields, "address")
	}
	if req.CollectorID != nil {
		c, err := checkUUIDField("collector_id", *req.CollectorID)
		if err != nil {
			return err
		}
		ch.CollectorID = &c
		fields = append(fields, "collector_id")
	}
	if req.GroupID.Set {
		ch.SetGroup = true
		if req.GroupID.Value != nil {
			g, err := checkUUIDField("group_id", *req.GroupID.Value)
			if err != nil {
				return err
			}
			ch.GroupID = &g
		}
		fields = append(fields, "group_id")
	}
	var profileAudit string
	if req.SNMPProfileID.Set {
		ch.SetProfile = true
		if req.SNMPProfileID.Value != nil {
			pid, err := checkUUIDField("snmp_profile_id", *req.SNMPProfileID.Value)
			if err != nil {
				return err
			}
			ch.ProfileID, profileAudit = &pid, pid
		}
		fields = append(fields, "snmp_profile_id")
	}
	if len(fields) == 0 {
		return badRequest("nada que cambiar: envia name, address, collector_id, group_id o snmp_profile_id")
	}
	d, err := a.store.UpdateDevice(r.Context(), strings.ToLower(id), ch)
	if err != nil {
		return err
	}
	u := currentUser(r)
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evDeviceUpdated, store.OutcomeOK, "device", d.ID
	detail := map[string]any{"name": d.Name, "fields": fields}
	if profileAudit != "" {
		detail["snmp_profile_id"] = profileAudit
	}
	a.audit(r, e, detail)
	writeJSON(w, http.StatusOK, map[string]any{"device": toDeviceDTO(u, d)})
	return nil
}

func (a *API) deleteDevice(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return store.ErrNotFound
	}
	id = strings.ToLower(id)
	name, err := a.store.DeleteDevice(r.Context(), id)
	if err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evDeviceDeleted, store.OutcomeOK, "device", id
	a.audit(r, e, map[string]any{"name": name})
	writeJSON(w, http.StatusOK, map[string]any{})
	return nil
}
