package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// SessionIDLen es el largo en bytes (32 = 256 bits) del identificador de sesion.
const SessionIDLen = 32

// MaxSessionIDChars es el largo maximo del ID codificado (43 chars pa 32 bytes en base64url).
const MaxSessionIDChars = 64

// NewSessionID genera un ID opaco de sesion con crypto/rand. id va en la cookie;
// hash (SHA-256) es lo unico que se guarda en la BD.
func NewSessionID() (id string, hash []byte, err error) {
	b := make([]byte, SessionIDLen)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("leyendo crypto/rand: %w", err)
	}
	id = base64.RawURLEncoding.EncodeToString(b)
	return id, HashSessionID(id), nil
}

// HashSessionID devuelve el SHA-256 del ID de sesion (alta entropia: no necesita sal).
func HashSessionID(id string) []byte {
	sum := sha256.Sum256([]byte(id))
	return sum[:]
}
