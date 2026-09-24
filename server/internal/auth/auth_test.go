package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestHasher(t *testing.T, max int) *Hasher {
	t.Helper()
	h, err := NewHasher(DefaultParams, max)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestHashVerify(t *testing.T) {
	t.Parallel()
	h := newTestHasher(t, 4)
	ctx := context.Background()

	enc, err := h.Hash(ctx, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("formato PHC inesperado: %q", enc)
	}
	enc2, _ := h.Hash(ctx, "correct horse battery staple")
	if enc == enc2 {
		t.Error("dos hashes de la misma contraseña deben diferir (salt aleatoria)")
	}

	ok, rehash, err := h.Verify(ctx, "correct horse battery staple", enc)
	if err != nil || !ok || rehash {
		t.Errorf("Verify correcta got (%v, %v, %v), want (true, false, nil)", ok, rehash, err)
	}
	ok, _, err = h.Verify(ctx, "otra contraseña larga", enc)
	if err != nil || ok {
		t.Errorf("Verify incorrecta got (%v, %v), want (false, nil)", ok, err)
	}
}

func TestVerifyNeedsRehashWhenParamsChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	old := DefaultParams
	old.Time = 1
	oldHasher, err := NewHasher(old, 1)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := oldHasher.Hash(ctx, "una contraseña bastante larga")
	if err != nil {
		t.Fatal(err)
	}
	ok, rehash, err := newTestHasher(t, 1).Verify(ctx, "una contraseña bastante larga", enc)
	if err != nil || !ok || !rehash {
		t.Errorf("got (%v, %v, %v), want (true, true, nil)", ok, rehash, err)
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	t.Parallel()
	h := newTestHasher(t, 1)
	bad := []string{
		"",
		"plano",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g",
		"$argon2id$v=18$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g",
		"$argon2id$v=19$m=99999999,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g",
		"$argon2id$v=19$m=19456,t=0,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g",
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g",
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$corto",
	}
	for _, enc := range bad {
		if _, _, err := h.Verify(context.Background(), "x", enc); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("Verify(%q) err got %v, want ErrInvalidHash", enc, err)
		}
	}
}

func TestHasherLimitsConcurrency(t *testing.T) {
	t.Parallel()
	h := newTestHasher(t, 1)

	// Con el unico cupo tomado, un Hash con ctx cancelado debe salir con el error del ctx.
	h.sem <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := h.Hash(ctx, "x"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err got %v, want DeadlineExceeded", err)
	}
	<-h.sem

	// Con N goroutines nunca hay mas de max hashes a la vez.
	const max, n = 2, 8
	h2 := newTestHasher(t, max)
	var inFlight, peak atomic.Int32
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Solo medimos el semaforo: acquire/release reales, sin calcular el hash.
			if err := h2.acquire(context.Background()); err != nil {
				t.Error(err)
				return
			}
			cur := inFlight.Add(1)
			for {
				p := peak.Load()
				if cur <= p || peak.CompareAndSwap(p, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			inFlight.Add(-1)
			h2.release()
		}()
	}
	wg.Wait()
	if peak.Load() > max {
		t.Errorf("pico de hashes concurrentes got %d, want <= %d", peak.Load(), max)
	}
}

func TestVerifyDummyNeverSucceeds(t *testing.T) {
	t.Parallel()
	h := newTestHasher(t, 1)
	if err := h.VerifyDummy(context.Background(), "cualquier cosa"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckPassword(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pw      string
		wantErr bool
	}{
		{"valida", "una frase larga y sencilla", false},
		{"14 caracteres", strings.Repeat("a", 14), true},
		{"15 caracteres", strings.Repeat("a", 15), false},
		{"128 caracteres", strings.Repeat("a", 128), false},
		{"129 caracteres", strings.Repeat("a", 129), true},
		{"cuenta caracteres no bytes", strings.Repeat("ñ", 15), false},
		{"con NUL", "una frase larga\x00y sencilla", true},
		{"utf8 invalido", "una frase larga \xff\xfe", true},
		{"sin composicion", "abcdefghijklmno", false},
		{"en la lista embebida (ignora mayusculas)", "FILMS+pic+galeries", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := CheckPassword(tt.pw)
			var weak *WeakPasswordError
			if tt.wantErr != errors.As(err, &weak) {
				t.Errorf("CheckPassword err got %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEmbeddedListLoaded(t *testing.T) {
	t.Parallel()
	if n := len(commonPasswords()); n < 9000 {
		t.Fatalf("lista embebida con %d entradas, se esperaban ~10k", n)
	}
}

func TestSessionID(t *testing.T) {
	t.Parallel()
	id1, h1, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	id2, _, _ := NewSessionID()
	if id1 == id2 {
		t.Error("dos IDs de sesion iguales")
	}
	if len(id1) != 43 || len(id1) > MaxSessionIDChars {
		t.Errorf("largo del ID got %d, want 43", len(id1))
	}
	if len(h1) != 32 || string(h1) != string(HashSessionID(id1)) {
		t.Error("el hash debe ser SHA-256 (32 bytes) reproducible del ID")
	}
	if strings.Contains(string(h1), id1) {
		t.Error("el hash no debe contener el ID")
	}
}
