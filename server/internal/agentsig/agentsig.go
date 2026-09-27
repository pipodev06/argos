// Package agentsig genera y usa el par de claves ed25519 que firma el manifiesto del agente (H7).
//
// La clave PRIVADA nunca vive en el servidor desplegado: se genera y se usa solo en la maquina del
// admin (o CI de build), al empaquetar una version nueva del agente. La clave PUBLICA se compila dentro
// del binario del agente (ver agent/cmd/agent, -ldflags -X main.publicKey=...); el server no la necesita
// para nada, ni la sirve: si el server (o el tunel) se compromete, quien lo controle puede servir un
// manifiesto y un binario falsos, pero no puede firmarlos, porque no tiene la clave privada.
package agentsig

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateKey crea un par de claves nuevo con crypto/rand.
func GenerateKey() (priv ed25519.PrivateKey, pub ed25519.PublicKey, err error) {
	pub, priv, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generando la clave: %w", err)
	}
	return priv, pub, nil
}

// EncodePrivate/DecodePrivate y EncodePublic/DecodePublic son hex: mismo formato que el resto del
// repo usa para hashes (sha256Re), facil de pegar en un archivo o en -ldflags.

// EncodePrivate hex-codifica la semilla de 32 bytes de la clave privada (no los 64 bytes de
// ed25519.PrivateKey, que incluyen la clave publica derivada: guardar solo la semilla es mas chico y
// es el formato que ed25519.NewKeyFromSeed espera de vuelta).
func EncodePrivate(priv ed25519.PrivateKey) string {
	return hex.EncodeToString(priv.Seed())
}

// DecodePrivate reconstruye la clave privada a partir de su semilla hex.
func DecodePrivate(seedHex string) (ed25519.PrivateKey, error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("clave privada invalida: se esperaba hex de %d bytes", ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// EncodePublic hex-codifica la clave publica (32 bytes).
func EncodePublic(pub ed25519.PublicKey) string {
	return hex.EncodeToString(pub)
}

// DecodePublic reconstruye la clave publica a partir de su hex.
func DecodePublic(pubHex string) (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(pubHex)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("clave publica invalida: se esperaba hex de %d bytes", ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

// Sign firma el manifiesto canonico (agentdist.Catalog.Manifest) con la clave privada.
func Sign(priv ed25519.PrivateKey, manifest []byte) []byte {
	return ed25519.Sign(priv, manifest)
}

// EncodeSignature/DecodeSignature: hex de la firma (64 bytes), lo que se guarda en manifest.json.sig.
func EncodeSignature(sig []byte) string { return hex.EncodeToString(sig) }

func DecodeSignature(sigHex string) ([]byte, error) {
	b, err := hex.DecodeString(sigHex)
	if err != nil || len(b) != ed25519.SignatureSize {
		return nil, fmt.Errorf("firma invalida: se esperaba hex de %d bytes", ed25519.SignatureSize)
	}
	return b, nil
}

// Verify comprueba la firma del manifiesto con la clave publica.
func Verify(pub ed25519.PublicKey, manifest, sig []byte) bool {
	return ed25519.Verify(pub, manifest, sig)
}
