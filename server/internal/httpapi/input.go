package httpapi

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// optional distingue un campo ausente del body de uno con null explicito: Set es true si la clave
// vino (con valor o con null) y Value es nil solo con null. Sirve pa los PATCH/PUT donde "null" significa
// algo (volver al valor global, dejar sin grupo).
type optional[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON solo se invoca si la clave viene en el body.
func (o *optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// hasControl reporta si s tiene caracteres de control (incluidos NUL, separadores de linea y parrafo
// Unicode). Es mas estricto que [[:cntrl:]] de Postgres, asi ningun valor valido aqui puede violar un CHECK de la BD.
func hasControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || r == ' ' || r == ' '
	}) >= 0
}

// checkName valida un texto obligatorio recortado de 1..max caracteres sin caracteres de control.
// Devuelve el texto recortado.
func checkName(field, s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", badRequest("%s requerido", field)
	}
	if utf8.RuneCountInString(s) > max {
		return "", badRequest("%s excede %d caracteres", field, max)
	}
	if hasControl(s) {
		return "", badRequest("%s contiene caracteres invalidos", field)
	}
	return s, nil
}

// checkOptionalText valida un texto opcional (vacio permitido) de hasta max caracteres sin caracteres de control.
func checkOptionalText(field, s string, max int) error {
	if utf8.RuneCountInString(s) > max {
		return badRequest("%s excede %d caracteres", field, max)
	}
	if hasControl(s) {
		return badRequest("%s contiene caracteres invalidos", field)
	}
	return nil
}

// Largo maximo del nombre de un grupo (alineado con el CHECK de la migracion 00007).
const maxGroupNameLen = 64
