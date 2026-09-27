// Package agentsig verifica la firma ed25519 del manifiesto del agente (H7: firma que un server
// comprometido no puede forjar porque la clave privada nunca vive ahi). El agente solo necesita
// verificar: no genera claves ni firma nada (eso lo hace `argos-server keygen`/`sign-manifest`, en la
// maquina del admin o del CI de build, ver server/internal/agentsig).
package agentsig

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
)

// DecodePublic reconstruye la clave publica embebida en el binario (-ldflags -X main.publicKey=...).
func DecodePublic(pubHex string) (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(pubHex)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("clave publica invalida: se esperaba hex de %d bytes", ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

// DecodeSignature decodifica la firma servida por /dl/latest/manifest.json.sig.
func DecodeSignature(sigHex string) ([]byte, error) {
	b, err := hex.DecodeString(sigHex)
	if err != nil || len(b) != ed25519.SignatureSize {
		return nil, fmt.Errorf("firma invalida: se esperaba hex de %d bytes", ed25519.SignatureSize)
	}
	return b, nil
}

// Verify comprueba la firma del manifiesto con la clave publica embebida.
func Verify(pub ed25519.PublicKey, manifest, sig []byte) bool {
	return ed25519.Verify(pub, manifest, sig)
}
