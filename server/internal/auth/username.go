package auth

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// Largos de username (coinciden con el CHECK de la migracion 00004).
const (
	MinUsernameLen = 3
	MaxUsernameLen = 64
)

// ValidateUsername exige 3-64 caracteres de [A-Za-z0-9._@+-]: sin espacios, controles
// ni homoglifos que confundan a un admin al leer la lista de usuarios o la auditoria.
func ValidateUsername(name string) error {
	n := utf8.RuneCountInString(name)
	if n < MinUsernameLen || n > MaxUsernameLen {
		return fmt.Errorf("el usuario debe tener entre %d y %d caracteres", MinUsernameLen, MaxUsernameLen)
	}
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '.' || r == '_' || r == '@' || r == '+' || r == '-'
		if !ok {
			return errors.New("el usuario solo admite letras, numeros y . _ @ + -")
		}
	}
	return nil
}
