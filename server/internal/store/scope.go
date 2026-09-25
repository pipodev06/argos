package store

import (
	"slices"
	"strings"
)

// Scope es el ambito de hosts que puede ver quien consulta. El valor cero NO ve nada: ver todo hay que
// pedirlo explicito (AllScope), asi un Scope sin rellenar nunca filtra de mas.
type Scope struct {
	all      bool
	groupIDs []string
}

// AllScope ve todos los hosts, con y sin grupo (admin o viewer con all_groups).
func AllScope() Scope { return Scope{all: true} }

// GroupScope ve solo los hosts de esos grupos; nunca los hosts sin grupo. Sin grupos no ve nada.
func GroupScope(groupIDs []string) Scope {
	ids := slices.Clone(groupIDs)
	slices.Sort(ids)
	return Scope{groupIDs: slices.Compact(ids)}
}

// All dice si el ambito es total.
func (s Scope) All() bool { return s.all }

// Key identifica el ambito de forma estable (mismo ambito, misma clave): entra en la huella de los cursores.
func (s Scope) Key() string {
	if s.all {
		return "all"
	}
	return "groups:" + strings.Join(s.groupIDs, ",")
}

// groupCond devuelve la condicion SQL que limita al ambito una columna con id de grupo (h.group_id, g.id).
// Ambito total: TRUE. Si no: col = ANY($n) (un host sin grupo, NULL, nunca cumple).
func (s Scope) groupCond(column string, args *argList) string {
	if s.all {
		return "TRUE"
	}
	ids := s.groupIDs
	if ids == nil {
		ids = []string{}
	}
	return column + " = ANY(" + args.add(ids) + "::uuid[])"
}

// hostCond limita al ambito una columna con id de host (a.host_id): el host debe estar en un grupo del ambito.
// Ambito total: TRUE (sin subconsulta).
func (s Scope) hostCond(column string, args *argList) string {
	if s.all {
		return "TRUE"
	}
	return column + " IN (SELECT id FROM hosts WHERE " + s.groupCond("group_id", args) + ")"
}

// configCond limita al ambito los objetos de configuracion con alcance (reglas, chequeos, ventanas): se ven los de
// alcance `all` y los de un grupo del ambito; los de un grupo ajeno no existen para el restringido.
func (s Scope) configCond(typeCol, groupCol string, args *argList) string {
	if s.all {
		return "TRUE"
	}
	return "(" + typeCol + " = 'all' OR (" + typeCol + " = 'group' AND " + s.groupCond(groupCol, args) + "))"
}

// sees dice si el ambito ve un objeto de configuracion con ese alcance (equivale a configCond, en memoria).
func (s Scope) sees(a AlertScope) bool {
	if s.all || a.Type == ScopeAll {
		return true
	}
	return a.Type == ScopeGroup && a.GroupID != nil && slices.Contains(s.groupIDs, *a.GroupID)
}
