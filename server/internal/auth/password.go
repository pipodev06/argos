// Package auth reune las primitivas de autenticacion: hash de contraseñas
// (Argon2id), politica de contraseñas e identificadores de sesion.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params son los costos de Argon2id (OWASP: m=19 MiB, t=2, p=1).
type Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
	SaltLen   uint32
	KeyLen    uint32
}

// DefaultParams son los parametros aprobados. Subirlos provoca rehash transparente en el login.
var DefaultParams = Params{MemoryKiB: 19456, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}

// Limites al parsear un hash guardado: una fila corrupta no puede pedir gigas de RAM.
const (
	maxParseMemoryKiB = 1 << 20 // 1 GiB
	maxParseTime      = 16
	maxParseKeyLen    = 64
	maxParseSaltLen   = 64
)

// ErrInvalidHash: el hash guardado no es un PHC argon2id valido.
var ErrInvalidHash = errors.New("hash de contraseña invalido")

var b64 = base64.RawStdEncoding

// Hasher calcula y verifica hashes Argon2id. Limita a maxConcurrent los hashes
// simultaneos: cada uno usa MemoryKiB de RAM y un flood de logins no debe agotarla.
type Hasher struct {
	params Params
	sem    chan struct{}
	dummy  string
}

// NewHasher crea el hasher y precalcula el hash "dummy" con el que se iguala el
// tiempo cuando el usuario no existe.
func NewHasher(p Params, maxConcurrent int) (*Hasher, error) {
	if maxConcurrent < 1 {
		return nil, errors.New("maxConcurrent debe ser >= 1")
	}
	h := &Hasher{params: p, sem: make(chan struct{}, maxConcurrent)}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return nil, fmt.Errorf("leyendo crypto/rand: %w", err)
	}
	dummy, err := h.Hash(context.Background(), string(random))
	if err != nil {
		return nil, fmt.Errorf("calculando hash dummy: %w", err)
	}
	h.dummy = dummy
	return h, nil
}

// acquire toma un cupo del semaforo o devuelve el error del contexto.
func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("esperando cupo de hash: %w", ctx.Err())
	}
}

func (h *Hasher) release() { <-h.sem }

// Hash devuelve el hash PHC de la contraseña con salt aleatorio.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, h.params.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("leyendo crypto/rand: %w", err)
	}
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	key := argon2.IDKey([]byte(password), salt, h.params.Time, h.params.MemoryKiB, h.params.Threads, h.params.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.MemoryKiB, h.params.Time, h.params.Threads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify compara la contraseña con el hash PHC en tiempo constante.
// needsRehash es true si el hash es correcto pero usa parametros distintos a los vigentes.
func (h *Hasher) Verify(ctx context.Context, password, encoded string) (ok, needsRehash bool, err error) {
	p, salt, want, err := parsePHC(encoded)
	if err != nil {
		return false, false, err
	}
	if err := h.acquire(ctx); err != nil {
		return false, false, err
	}
	defer h.release()
	got := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, p.KeyLen)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	return true, p != h.params, nil
}

// VerifyDummy gasta el mismo costo que una verificacion real y siempre devuelve
// false: se usa cuando el usuario no existe (o esta bloqueado) pa igualar tiempos.
func (h *Hasher) VerifyDummy(ctx context.Context, password string) error {
	_, _, err := h.Verify(ctx, password, h.dummy)
	return err
}

func parsePHC(encoded string) (p Params, salt, key []byte, err error) {
	// $argon2id$v=19$m=..,t=..,p=..$salt$key
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrInvalidHash
	}
	if parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return Params{}, nil, nil, ErrInvalidHash
	}
	var m, t uint32
	var th uint8
	for kv := range strings.SplitSeq(parts[3], ",") {
		k, v, found := strings.Cut(kv, "=")
		if !found {
			return Params{}, nil, nil, ErrInvalidHash
		}
		n, perr := strconv.ParseUint(v, 10, 32)
		if perr != nil {
			return Params{}, nil, nil, ErrInvalidHash
		}
		switch k {
		case "m":
			m = uint32(n) // #nosec G115 -- ParseUint con bitSize 32
		case "t":
			t = uint32(n) // #nosec G115 -- ParseUint con bitSize 32
		case "p":
			if n > 255 {
				return Params{}, nil, nil, ErrInvalidHash
			}
			th = uint8(n) // #nosec G115 -- acotado arriba
		default:
			return Params{}, nil, nil, ErrInvalidHash
		}
	}
	if m == 0 || m > maxParseMemoryKiB || t == 0 || t > maxParseTime || th == 0 {
		return Params{}, nil, nil, ErrInvalidHash
	}
	salt, err = b64.DecodeString(parts[4])
	if err != nil || len(salt) == 0 || len(salt) > maxParseSaltLen {
		return Params{}, nil, nil, ErrInvalidHash
	}
	key, err = b64.DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > maxParseKeyLen {
		return Params{}, nil, nil, ErrInvalidHash
	}
	p = Params{MemoryKiB: m, Time: t, Threads: th, SaltLen: uint32(len(salt)), KeyLen: uint32(len(key))} // #nosec G115 -- largos acotados arriba
	return p, salt, key, nil
}
