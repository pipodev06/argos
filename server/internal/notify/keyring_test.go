package notify

import (
	"bytes"
	"errors"
	"testing"
)

func TestNewKeyRingSinClaveActualYConPreviaFalla(t *testing.T) {
	t.Parallel()
	if _, err := NewKeyRing("", testKey(1)); err == nil {
		t.Fatal("debio fallar: previa sin actual")
	}
	r, err := NewKeyRing("", "")
	if err != nil || r != nil {
		t.Fatalf("ambas vacias: got %v, %v; want nil, nil", r, err)
	}
	if _, err := NewKeyRing(testKey(1), testKey(1)); err == nil {
		t.Fatal("debio fallar: la previa repite la actual")
	}
	if _, err := NewKeyRing(testKey(1), "no-es-base64"); err == nil {
		t.Fatal("debio fallar: previa invalida")
	}
}

func TestKeyRingOpenBuscaEnLasPrevias(t *testing.T) {
	t.Parallel()
	oldBox := mustBox(t, 1)
	secret := []byte("secreto")
	blob, err := oldBox.Seal("id-1", "webhook", secret)
	if err != nil {
		t.Fatal(err)
	}
	ring := mustRing(t, 2, 1) // actual = clave 2, previa = clave 1 (la que cifro el blob)
	got, err := ring.Open("id-1", oldBox.KeyID(), "webhook", blob)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("Open con clave previa: got %q, %v; want %q", got, err, secret)
	}
	if _, err := ring.Open("id-1", "clave-inexistente", "webhook", blob); !errors.Is(err, ErrSecretDecrypt) {
		t.Errorf("keyID fuera del anillo: got %v, want ErrSecretDecrypt", err)
	}
}

func TestKeyRingSealSiempreUsaLaClaveActual(t *testing.T) {
	t.Parallel()
	ring := mustRing(t, 2, 1)
	blob, err := ring.Seal("id-1", "webhook", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := mustBox(t, 2).Open("id-1", ring.KeyID(), "webhook", blob)
	if err != nil || string(got) != "x" {
		t.Fatalf("Seal no uso la clave actual: got %q, %v", got, err)
	}
}

func TestKeyRingRotateEsIdempotenteYRecifraConLaClaveActual(t *testing.T) {
	t.Parallel()
	oldBox := mustBox(t, 1)
	secret := []byte("secreto")
	blob, err := oldBox.Seal("id-1", "webhook", secret)
	if err != nil {
		t.Fatal(err)
	}
	ring := mustRing(t, 2, 1)

	newEnc, newKeyID, ok, err := ring.Rotate("id-1", "webhook", oldBox.KeyID(), blob)
	if err != nil || !ok || newKeyID != ring.KeyID() {
		t.Fatalf("Rotate got ok=%v keyID=%q err=%v", ok, newKeyID, err)
	}
	got, err := ring.Open("id-1", newKeyID, "webhook", newEnc)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("el blob recifrado no abre: got %q, %v", got, err)
	}

	// Repetir con el blob ya en la clave actual es un no-op.
	if _, _, ok, err := ring.Rotate("id-1", "webhook", newKeyID, newEnc); err != nil || ok {
		t.Fatalf("Rotate sobre un blob ya actual: got ok=%v, err=%v; want ok=false", ok, err)
	}
}
