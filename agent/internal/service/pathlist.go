package service

import "strings"

// pathSep es el separador de entradas del Path de Windows.
const pathSep = ";"

// samePathEntry compara dos entradas de Path sin distinguir mayusculas e ignorando la barra final.
func samePathEntry(a, b string) bool {
	norm := func(s string) string { return strings.ToLower(strings.TrimRight(strings.TrimSpace(s), `\/`)) }
	return norm(a) == norm(b)
}

// addPathEntry agrega dir al final de un Path (lista separada por ";") si no esta ya.
// Devuelve el nuevo valor y si hubo cambio.
func addPathEntry(path, dir string) (string, bool) {
	for _, e := range strings.Split(path, pathSep) {
		if samePathEntry(e, dir) {
			return path, false
		}
	}
	if strings.TrimSpace(path) == "" {
		return dir, true
	}
	return strings.TrimRight(path, pathSep) + pathSep + dir, true
}

// removePathEntry quita dir de un Path. Devuelve el nuevo valor y si hubo cambio.
func removePathEntry(path, dir string) (string, bool) {
	var kept []string
	changed := false
	for _, e := range strings.Split(path, pathSep) {
		if samePathEntry(e, dir) {
			changed = true
			continue
		}
		kept = append(kept, e)
	}
	if !changed {
		return path, false
	}
	return strings.Join(kept, pathSep), true
}
