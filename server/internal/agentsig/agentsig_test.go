package agentsig

import "testing"

func TestGenerateSignVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	priv, pub, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	manifest := []byte(`{"version":"1.2.3","files":[]}`)
	sig := Sign(priv, manifest)
	if !Verify(pub, manifest, sig) {
		t.Fatal("Verify rechazo una firma valida")
	}
	if Verify(pub, []byte(`{"version":"1.2.4","files":[]}`), sig) {
		t.Error("Verify acepto un manifiesto alterado")
	}
	_, pub2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if Verify(pub2, manifest, sig) {
		t.Error("Verify acepto la firma con la clave publica incorrecta")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	priv, pub, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	priv2, err := DecodePrivate(EncodePrivate(priv))
	if err != nil {
		t.Fatalf("DecodePrivate: %v", err)
	}
	if !priv2.Equal(priv) {
		t.Error("DecodePrivate(EncodePrivate(priv)) != priv")
	}
	pub2, err := DecodePublic(EncodePublic(pub))
	if err != nil {
		t.Fatalf("DecodePublic: %v", err)
	}
	if !pub2.Equal(pub) {
		t.Error("DecodePublic(EncodePublic(pub)) != pub")
	}
	sig := Sign(priv, []byte("hola"))
	sig2, err := DecodeSignature(EncodeSignature(sig))
	if err != nil {
		t.Fatalf("DecodeSignature: %v", err)
	}
	if string(sig2) != string(sig) {
		t.Error("DecodeSignature(EncodeSignature(sig)) != sig")
	}
}

func TestDecodeRejectsInvalid(t *testing.T) {
	t.Parallel()
	if _, err := DecodePrivate("no-hex"); err == nil {
		t.Error("DecodePrivate acepto hex invalido")
	}
	if _, err := DecodePrivate("00"); err == nil {
		t.Error("DecodePrivate acepto una semilla corta")
	}
	if _, err := DecodePublic("00"); err == nil {
		t.Error("DecodePublic acepto una clave corta")
	}
	if _, err := DecodeSignature("00"); err == nil {
		t.Error("DecodeSignature acepto una firma corta")
	}
}
