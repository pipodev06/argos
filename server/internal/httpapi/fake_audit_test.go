package httpapi

import (
	"context"

	"argos/server/internal/store"
)

// La auditoria de solo lectura se prueba contra Postgres real (audit_list_test.go); el falso solo cumple la interfaz.
func (f *fakeStore) ListAudit(context.Context, store.AuditFilter) (store.AuditPage, error) {
	return store.AuditPage{Items: []store.AuditRow{}}, nil
}

func (f *fakeStore) AuditEvents(context.Context) ([]string, error) { return []string{}, nil }
