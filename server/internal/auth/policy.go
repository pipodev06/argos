package auth

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

// Largos de contraseña (en caracteres, no bytes). Sin reglas de composicion ni caducidad (NIST 800-63B).
const (
	MinPasswordLen = 15
	MaxPasswordLen = 128
)

// WeakPasswordError explica por que una contraseña no cumple la politica.
// Su mensaje es seguro pa mostrarlo al usuario.
type WeakPasswordError struct{ Reason string }

func (e *WeakPasswordError) Error() string { return e.Reason }

// commonPasswordsRaw: SecLists Passwords/Common-Credentials/10k-most-common.txt
// (MIT, (c) 2018 Daniel Miessler). Licencia en common_passwords.LICENSE.txt.
//
//go:embed common_passwords.txt
var commonPasswordsRaw string

var commonPasswords = sync.OnceValue(func() map[string]struct{} {
	lines := strings.Split(commonPasswordsRaw, "\n")
	set := make(map[string]struct{}, len(lines))
	for _, l := range lines {
		if l = strings.ToLower(strings.TrimSpace(l)); l != "" {
			set[l] = struct{}{}
		}
	}
	return set
})

// CheckPassword aplica la politica: 15-128 caracteres, UTF-8 valido, sin NUL
// y fuera de la lista embebida de contraseñas comprometidas. Devuelve un *WeakPasswordError.
func CheckPassword(pw string) error {
	if !utf8.ValidString(pw) || strings.ContainsRune(pw, 0) {
		return &WeakPasswordError{"la contraseña contiene caracteres invalidos"}
	}
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordLen {
		return &WeakPasswordError{fmt.Sprintf("la contraseña debe tener al menos %d caracteres", MinPasswordLen)}
	}
	if n > MaxPasswordLen {
		return &WeakPasswordError{fmt.Sprintf("la contraseña no puede superar %d caracteres", MaxPasswordLen)}
	}
	if _, found := commonPasswords()[strings.ToLower(pw)]; found {
		return &WeakPasswordError{"esa contraseña es demasiado comun, elige otra"}
	}
	return nil
}
