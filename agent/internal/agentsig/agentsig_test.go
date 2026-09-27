package agentsig

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"version":"1.2.3","files":[]}`)
	sig := ed25519.Sign(priv, manifest)

	gotPub, err := DecodePublic(hex.EncodeToString(pub))
	if err != nil {
		t.Fatalf("DecodePublic: %v", err)
	}
	gotSig, err := DecodeSignature(hex.EncodeToString(sig))
	if err != nil {
		t.Fatalf("DecodeSignature: %v", err)
	}
	if !Verify(gotPub, manifest, gotSig) {
		t.Error("Verify rechazo una firma valida")
	}
	if Verify(gotPub, []byte(`{"version":"1.2.4","files":[]}`), gotSig) {
		t.Error("Verify acepto un manifiesto alterado")
	}
}

func TestDecodeRejectsInvalid(t *testing.T) {
	t.Parallel()
	if _, err := DecodePublic("no-hex"); err == nil {
		t.Error("DecodePublic acepto hex invalido")
	}
	if _, err := DecodePublic("00"); err == nil {
		t.Error("DecodePublic acepto una clave corta")
	}
	if _, err := DecodeSignature("00"); err == nil {
		t.Error("DecodeSignature acepto una firma corta")
	}
}
