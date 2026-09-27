package notify

import (
	"errors"
	"fmt"
	"strings"
)

// KeyRing es el conjunto de claves con las que se puede leer un secreto ya cifrado: la actual (cifra y descifra)
// y, mientras dura una rotacion, las previas (ARGOS_SECRET_KEY_PREVIOUS: solo descifran, nunca cifran nada
// nuevo). Permite cambiar ARGOS_SECRET_KEY sin perder acceso a lo que ya esta cifrado con la clave anterior: el
// admin corre `argos-server rotate-secret-key` para recifrarlo todo con la clave actual y despues puede quitar
// la previa. Es inmutable y seguro entre goroutines (como Box).
type KeyRing struct {
	current  *Box
	previous []*Box
}

// NewKeyRing construye el anillo desde ARGOS_SECRET_KEY (currentB64, la unica que cifra) y
// ARGOS_SECRET_KEY_PREVIOUS (previousB64, una o varias claves separadas por coma que solo descifran).
// currentB64 vacia con previousB64 no vacia es un error (las previas no sirven sin una clave actual que las
// reemplace). Ambas vacias devuelve un anillo nil sin error (no hay cifrado configurado).
func NewKeyRing(currentB64, previousB64 string) (*KeyRing, error) {
	if strings.TrimSpace(currentB64) == "" {
		if strings.TrimSpace(previousB64) != "" {
			return nil, errors.New("ARGOS_SECRET_KEY_PREVIOUS definida sin ARGOS_SECRET_KEY: la clave previa no sirve sin una clave actual")
		}
		return nil, nil
	}
	cur, err := NewBox(currentB64)
	if err != nil {
		return nil, fmt.Errorf("ARGOS_SECRET_KEY invalida: %w", err)
	}
	ring := &KeyRing{current: cur}
	for _, raw := range strings.Split(previousB64, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		b, err := NewBox(raw)
		if err != nil {
			return nil, fmt.Errorf("ARGOS_SECRET_KEY_PREVIOUS invalida: %w", err)
		}
		if b.KeyID() == cur.KeyID() {
			return nil, errors.New("ARGOS_SECRET_KEY_PREVIOUS no debe repetir la clave actual (ARGOS_SECRET_KEY)")
		}
		ring.previous = append(ring.previous, b)
	}
	return ring, nil
}

// Seal cifra siempre con la clave actual (las previas nunca cifran nada nuevo).
func (r *KeyRing) Seal(id, kind string, plaintext []byte) ([]byte, error) {
	return r.current.Seal(id, kind, plaintext)
}

// KeyID es el identificador de la clave actual (con la que se cifra).
func (r *KeyRing) KeyID() string { return r.current.KeyID() }

// Open descifra con la clave del anillo (actual o alguna previa) cuyo identificador coincide con keyID. Si
// ninguna coincide devuelve ErrSecretDecrypt, igual que Box.Open con la clave equivocada.
func (r *KeyRing) Open(id, keyID, kind string, blob []byte) ([]byte, error) {
	b := r.boxFor(keyID)
	if b == nil {
		return nil, ErrSecretDecrypt
	}
	return b.Open(id, keyID, kind, blob)
}

// HasKeyID dice si keyID es el de la clave actual o el de alguna previa (lo usa CheckSecretKey al arrancar).
func (r *KeyRing) HasKeyID(keyID string) bool { return r.boxFor(keyID) != nil }

func (r *KeyRing) boxFor(keyID string) *Box {
	if r.current.KeyID() == keyID {
		return r.current
	}
	for _, b := range r.previous {
		if b.KeyID() == keyID {
			return b
		}
	}
	return nil
}

// Rotate descifra enc (cifrado con keyID, una clave del anillo) y lo vuelve a cifrar con la clave actual.
// ok = false si keyID ya es el de la clave actual: no hay nada que recifrar (asi `rotate-secret-key` es
// idempotente sin que quien llama tenga que comparar claves). id y kind son el AAD con el que se cifro (deben
// ser los mismos con los que se cifro originalmente). La firma coincide con store.RotateSecret.
func (r *KeyRing) Rotate(id, kind, keyID string, enc []byte) (newEnc []byte, newKeyID string, ok bool, err error) {
	if keyID == r.KeyID() {
		return nil, "", false, nil
	}
	raw, err := r.Open(id, keyID, kind, enc)
	if err != nil {
		return nil, "", false, err
	}
	newEnc, err = r.Seal(id, kind, raw)
	if err != nil {
		return nil, "", false, err
	}
	return newEnc, r.KeyID(), true, nil
}
