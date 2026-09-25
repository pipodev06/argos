package httpapi

import (
	"context"
	"time"

	"argos/server/internal/store"
)

// fakeExtra son los datos y errores de los metodos del store que no son de detalle/series: lista, resumen,
// grupos, etiquetas y acciones masivas. err se devuelve de todos ellos (un fallo de infraestructura o un
// sentinela de dominio, segun el test).
type fakeExtra struct {
	err error

	page      store.HostPage
	gotFilter store.HostFilter

	summary store.Summary

	groups    []store.Group
	ungrouped int
	group     store.Group // lo que devuelven CreateGroup/UpdateGroup
	gotChange store.GroupChange
	gotName   string
	gotDown   *int
	gotAlways bool
	deleted   string

	hostGroup store.HostGroupResult
	gotHostID string
	gotGroup  *string

	affected int
	gotBulk  store.Bulk

	disks []store.DiskUsage
}

// HostChecks: el fake no tiene chequeos asignados (los tests de chequeos usan la BD real).
func (f *fakeStore) HostChecks(context.Context, string) ([]store.ServiceCheckDef, error) {
	return nil, nil
}

func (f *fakeStore) ListHosts(_ context.Context, _ store.Scope, fl store.HostFilter) (store.HostPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.x.gotFilter = fl
	return f.x.page, f.x.err
}

func (f *fakeStore) HostDisks(context.Context, string, time.Time) ([]store.DiskUsage, error) {
	return f.x.disks, f.x.err
}

func (f *fakeStore) Summary(context.Context, store.Scope) (store.Summary, error) {
	return f.x.summary, f.x.err
}

func (f *fakeStore) ListGroups(context.Context, store.Scope) ([]store.Group, int, error) {
	return f.x.groups, f.x.ungrouped, f.x.err
}

func (f *fakeStore) CreateGroup(_ context.Context, name string, down *int, always bool) (store.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.x.gotName, f.x.gotDown, f.x.gotAlways = name, down, always
	return f.x.group, f.x.err
}

func (f *fakeStore) UpdateGroup(_ context.Context, _ string, ch store.GroupChange) (store.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.x.gotChange = ch
	return f.x.group, f.x.err
}

func (f *fakeStore) DeleteGroup(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.x.deleted = id
	return f.x.err
}

func (f *fakeStore) SetHostGroup(_ context.Context, hostID string, groupID *string) (store.HostGroupResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.x.gotHostID, f.x.gotGroup = hostID, groupID
	return f.x.hostGroup, f.x.err
}

func (f *fakeStore) BulkHosts(_ context.Context, b store.Bulk) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.x.gotBulk = b
	return f.x.affected, f.x.err
}

// Perfiles SNMP y chequeos snmp: el fake no los tiene (sus tests usan la BD real).
func (f *fakeStore) CollectorSNMP(context.Context, string) ([]store.SNMPAssignment, error) {
	return nil, nil
}

func (f *fakeStore) ListSNMPProfiles(context.Context) ([]store.SNMPProfile, error) { return nil, nil }

func (f *fakeStore) SNMPProfileByID(context.Context, string) (store.SNMPProfile, error) {
	return store.SNMPProfile{}, store.ErrNotFound
}

func (f *fakeStore) CreateSNMPProfile(context.Context, store.SNMPProfile, string) (store.SNMPProfile, error) {
	return store.SNMPProfile{}, f.x.err
}

func (f *fakeStore) UpdateSNMPProfile(context.Context, string, func(*store.SNMPProfile) error) (store.SNMPProfile, error) {
	return store.SNMPProfile{}, f.x.err
}

func (f *fakeStore) DeleteSNMPProfile(context.Context, string) (string, error) {
	return "", f.x.err
}

// CollectorPings: el fake no tiene dispositivos (los tests de dispositivos usan la BD real).
func (f *fakeStore) CollectorPings(context.Context, string) ([]store.PingAssignment, error) {
	return nil, nil
}
