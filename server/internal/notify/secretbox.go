// Package notify envia notificaciones de alertas: cifrado de los secretos de los canales, cliente webhook con
// defensas anti-SSRF y el worker que drena la cola de entregas.
package notify

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	keyBytes   = 32
	nonceBytes = 12 // GCM estandar: 96 bits
	tagBytes   = 16
)

// ErrSecretDecrypt: el blob no se pudo abrir (clave distinta, AAD distinta, dato corrupto o demasiado corto).
var ErrSecretDecrypt = errors.New("no se pudo descifrar el secreto")

// Box cifra y descifra secretos de canales con AES-256-GCM. Es inmutable y seguro entre goroutines.
type Box struct {
	aead  cipher.AEAD
	keyID string
}

// NewBox crea un Box desde la clave en base64 (32 bytes, ARGOS_SECRET_KEY). El identificador de clave se
// deriva de la propia clave (no revela la clave): asi una clave distinta nunca se confunde con la anterior.
func NewBox(b64 string) (*Box, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, errors.New("debe ser base64 estandar")
	}
	if len(key) != keyBytes {
		return nil, fmt.Errorf("debe decodificar a %d bytes (tiene %d)", keyBytes, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creando aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creando gcm: %w", err)
	}
	id := sha256.Sum256(append([]byte("argos-secret-key-id\x00"), key...))
	return &Box{aead: aead, keyID: hex.EncodeToString(id[:8])}, nil
}

// KeyID identifica la clave con la que se cifra (se guarda junto al blob).
func (b *Box) KeyID() string { return b.keyID }

// aad ata el blob a su canal, clave y tipo: copiarlo a otra fila no lo hace descifrable.
func aad(channelID, keyID, kind string) []byte {
	return []byte(channelID + "\x00" + keyID + "\x00" + kind)
}

// Seal cifra plaintext con un nonce aleatorio nuevo. Devuelve nonce || ciphertext || tag.
func (b *Box) Seal(channelID, kind string, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, nonceBytes, nonceBytes+len(plaintext)+tagBytes)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generando nonce: %w", err)
	}
	return b.aead.Seal(nonce, nonce, plaintext, aad(channelID, b.keyID, kind)), nil
}

// Open descifra un blob de Seal. keyID es el guardado con la fila: si no es el de esta clave falla sin intentar.
func (b *Box) Open(channelID, keyID, kind string, blob []byte) ([]byte, error) {
	if keyID != b.keyID || len(blob) < nonceBytes+tagBytes {
		return nil, ErrSecretDecrypt
	}
	pt, err := b.aead.Open(nil, blob[:nonceBytes], blob[nonceBytes:], aad(channelID, keyID, kind))
	if err != nil {
		return nil, ErrSecretDecrypt
	}
	return pt, nil
}

// KeyIDLister lista los secret_key_id de los canales que tienen secreto (lo implementa *store.Store).
type KeyIDLister interface {
	ChannelKeyIDs(ctx context.Context) ([]string, error)
}

// CheckSecretKey se llama al arrancar: si hay canales con secreto cifrado exige que box exista y que su
// identificador coincida con el de TODAS las filas. Sin canales cifrados no exige nada (la clave solo se
// necesita al crear el primer canal). El error dice que hacer, sin revelar la clave.
func CheckSecretKey(ctx context.Context, l KeyIDLister, box *Box) error {
	ids, err := l.ChannelKeyIDs(ctx)
	if err != nil {
		return fmt.Errorf("leyendo las claves de los canales: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	if box == nil {
		return errors.New("hay canales de notificacion con secretos cifrados y ARGOS_SECRET_KEY no esta definida: define la clave con la que se cifraron")
	}
	for _, id := range ids {
		if id != box.KeyID() {
			return errors.New("ARGOS_SECRET_KEY no coincide con la clave con la que se cifraron los secretos de los canales (secret_key_id distinto): restaura la clave original o borra los canales")
		}
	}
	return nil
}
