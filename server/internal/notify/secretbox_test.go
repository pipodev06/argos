package notify

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func testKey(fill byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))
}

func mustBox(t *testing.T, fill byte) *Box {
	t.Helper()
	b, err := NewBox(testKey(fill))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNewBoxValidatesTheKey(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"vacia":           "",
		"no es base64":    "%%%no-base64%%%",
		"muy corta":       base64.StdEncoding.EncodeToString(make([]byte, 16)),
		"muy larga":       base64.StdEncoding.EncodeToString(make([]byte, 64)),
		"32 chars crudos": strings.Repeat("a", 32),
	} {
		if _, err := NewBox(raw); err == nil {
			t.Errorf("%s: NewBox debio fallar", name)
		}
	}
	if _, err := NewBox(testKey(1)); err != nil {
		t.Errorf("clave valida: %v", err)
	}
}

func TestSealOpenRoundTripAndAADBinding(t *testing.T) {
	t.Parallel()
	box := mustBox(t, 7)
	secret := []byte(`{"url":"https://hooks.example.com/T/B/tok"}`)
	blob, err := box.Seal("chan-1", "webhook", secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("hooks.example.com")) {
		t.Fatal("el blob contiene el secreto en claro")
	}
	got, err := box.Open("chan-1", box.KeyID(), "webhook", blob)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("Open got %q, %v; want %q", got, err, secret)
	}

	// El mismo blob no descifra con otro canal, otro tipo ni otro key id (AAD).
	for name, args := range map[string][3]string{
		"otro canal":  {"chan-2", box.KeyID(), "webhook"},
		"otro tipo":   {"chan-1", box.KeyID(), "email"},
		"otro key id": {"chan-1", "otra-clave", "webhook"},
	} {
		if _, err := box.Open(args[0], args[1], args[2], blob); !errors.Is(err, ErrSecretDecrypt) {
			t.Errorf("%s: Open err got %v, want ErrSecretDecrypt", name, err)
		}
	}
	// Otra clave tampoco.
	if _, err := mustBox(t, 8).Open("chan-1", box.KeyID(), "webhook", blob); !errors.Is(err, ErrSecretDecrypt) {
		t.Errorf("otra clave: err got %v, want ErrSecretDecrypt", err)
	}
	// Un blob alterado ni uno demasiado corto abren.
	tampered := bytes.Clone(blob)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := box.Open("chan-1", box.KeyID(), "webhook", tampered); !errors.Is(err, ErrSecretDecrypt) {
		t.Errorf("blob alterado: err got %v", err)
	}
	for _, n := range []int{0, 5, nonceBytes, nonceBytes + tagBytes - 1} {
		if _, err := box.Open("chan-1", box.KeyID(), "webhook", blob[:n]); !errors.Is(err, ErrSecretDecrypt) {
			t.Errorf("blob de %d bytes: err got %v", n, err)
		}
	}
}

func TestSealUsesAFreshNonceEveryTime(t *testing.T) {
	t.Parallel()
	box := mustBox(t, 3)
	seen := map[string]bool{}
	for range 200 {
		blob, err := box.Seal("c", "webhook", []byte("mismo texto"))
		if err != nil {
			t.Fatal(err)
		}
		nonce := string(blob[:nonceBytes])
		if seen[nonce] {
			t.Fatal("nonce repetido")
		}
		seen[nonce] = true
	}
}

func TestKeyIDDependsOnTheKeyAndDoesNotLeakIt(t *testing.T) {
	t.Parallel()
	a, b := mustBox(t, 1), mustBox(t, 2)
	if a.KeyID() == b.KeyID() || a.KeyID() != mustBox(t, 1).KeyID() {
		t.Errorf("KeyID debe ser estable por clave y distinto entre claves: %q %q", a.KeyID(), b.KeyID())
	}
	if strings.Contains(testKey(1), a.KeyID()) {
		t.Error("KeyID no debe ser un trozo de la clave")
	}
}

type fakeKeys struct {
	ids []string
	err error
}

func (f fakeKeys) ChannelKeyIDs(context.Context) ([]string, error) { return f.ids, f.err }

func TestCheckSecretKeyAtStartup(t *testing.T) {
	t.Parallel()
	box := mustBox(t, 1)
	tests := []struct {
		name    string
		keys    fakeKeys
		box     *Box
		wantErr string // fragmento del mensaje; "" = sin error
	}{
		{"sin canales cifrados y sin clave", fakeKeys{}, nil, ""},
		{"sin canales cifrados y con clave", fakeKeys{}, box, ""},
		{"canales cifrados y sin clave", fakeKeys{ids: []string{box.KeyID()}}, nil, "ARGOS_SECRET_KEY no esta definida"},
		{"key id distinto", fakeKeys{ids: []string{"otra"}}, box, "no coincide"},
		{"una fila con otro key id", fakeKeys{ids: []string{box.KeyID(), "otra"}}, box, "no coincide"},
		{"coincide", fakeKeys{ids: []string{box.KeyID()}}, box, ""},
		{"falla la lectura", fakeKeys{err: errors.New("db caida")}, box, "db caida"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := CheckSecretKey(context.Background(), tt.keys, tt.box)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("got %v, want nil", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("got %v, want error con %q", err, tt.wantErr)
			}
		})
	}
}
