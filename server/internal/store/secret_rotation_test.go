package store

import (
	"context"
	"errors"
	"testing"
)

// fakeRotate simula (*notify.KeyRing).Rotate sin depender de notify (evita el ciclo store->notify): "descifra"
// quitando el prefijo "enc:"+keyID+":" y "cifra" agregando el de newKeyID. ok=false si keyID ya es newKeyID.
func fakeRotate(newKeyID string) RotateSecret {
	return func(id, kind, keyID string, enc []byte) ([]byte, string, bool, error) {
		if keyID == newKeyID {
			return nil, "", false, nil
		}
		return []byte("enc:" + newKeyID + ":" + id + ":" + kind), newKeyID, true, nil
	}
}

func TestRotateChannelSecret(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c := mkChan(t, s, 1, "Slack ops") // secret_key_id = "kid-1"

	changed, err := s.RotateChannelSecret(ctx, c.ID, fakeRotate("kid-2"))
	if err != nil || !changed {
		t.Fatalf("RotateChannelSecret got changed=%v, %v; want true, nil", changed, err)
	}
	got, err := s.ChannelByID(ctx, c.ID)
	if err != nil || got.SecretKeyID == nil || *got.SecretKeyID != "kid-2" || string(got.SecretEnc) != "enc:kid-2:"+c.ID+":webhook" {
		t.Fatalf("canal recifrado got %+v, %v", got, err)
	}

	// Repetir es un no-op: no cambia nada.
	changed, err = s.RotateChannelSecret(ctx, c.ID, fakeRotate("kid-2"))
	if err != nil || changed {
		t.Fatalf("repetir la rotacion got changed=%v, %v; want false, nil", changed, err)
	}
	again, err := s.ChannelByID(ctx, c.ID)
	if err != nil || string(again.SecretEnc) != string(got.SecretEnc) {
		t.Fatalf("repetir la rotacion no debe tocar el blob: got %+v, %v", again, err)
	}

	if _, err := s.RotateChannelSecret(ctx, "00000000-0000-4000-8000-0000000000ff", fakeRotate("kid-2")); !errors.Is(err, ErrNotFound) {
		t.Errorf("canal inexistente err got %v, want ErrNotFound", err)
	}
}

func TestRotateChannelSecretSinSecretoNoHaceNada(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	no := newChan("sin secreto")
	no.ID = "00000000-0000-4000-8000-000000000099"
	c, err := s.CreateChannel(ctx, no, "")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.RotateChannelSecret(ctx, c.ID, fakeRotate("kid-2"))
	if err != nil || changed {
		t.Fatalf("canal sin secreto got changed=%v, %v; want false, nil", changed, err)
	}
}

// mkHTTPCheckWithSecret crea un chequeo http con un secreto (header de autenticacion) cifrado con kid-1.
func mkHTTPCheckWithSecret(t *testing.T, s *Store, name string) ServiceCheckDef {
	t.Helper()
	d, err := s.CreateServiceCheck(context.Background(), ServiceCheckDef{
		ID: newCheckID(t), Name: name, Type: CheckHTTP, IntervalSeconds: 60, TimeoutMs: 5000, Retries: 1,
		URL: sptr("https://api.example.com/health"), HeaderName: sptr("Authorization"),
		SecretEnc: []byte("blob-cifrado"), SecretKeyID: sptr("kid-1"), Scope: AlertScope{Type: ScopeAll}, Enabled: true,
	}, "")
	if err != nil {
		t.Fatalf("creando chequeo http %q: %v", name, err)
	}
	return d
}

func TestRotateServiceCheckSecret(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	d := mkHTTPCheckWithSecret(t, s, "api")

	changed, err := s.RotateServiceCheckSecret(ctx, d.ID, fakeRotate("kid-2"))
	if err != nil || !changed {
		t.Fatalf("RotateServiceCheckSecret got changed=%v, %v; want true, nil", changed, err)
	}
	got, err := s.ServiceCheckByID(ctx, AllScope(), d.ID)
	if err != nil || got.SecretKeyID == nil || *got.SecretKeyID != "kid-2" || string(got.SecretEnc) != "enc:kid-2:"+d.ID+":service_check" {
		t.Fatalf("chequeo recifrado got %+v, %v", got, err)
	}

	// Repetir es un no-op.
	if changed, err := s.RotateServiceCheckSecret(ctx, d.ID, fakeRotate("kid-2")); err != nil || changed {
		t.Fatalf("repetir la rotacion got changed=%v, %v; want false, nil", changed, err)
	}

	// Un chequeo sin secreto (tcp) no hace nada.
	tcp := mkCheck(t, s, "tcp", AlertScope{Type: ScopeAll})
	if changed, err := s.RotateServiceCheckSecret(ctx, tcp.ID, fakeRotate("kid-2")); err != nil || changed {
		t.Fatalf("chequeo sin secreto got changed=%v, %v; want false, nil", changed, err)
	}

	if _, err := s.RotateServiceCheckSecret(ctx, "00000000-0000-4000-8000-0000000000ff", fakeRotate("kid-2")); !errors.Is(err, ErrNotFound) {
		t.Errorf("chequeo inexistente err got %v, want ErrNotFound", err)
	}
}

func TestRotateSNMPProfileSecret(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p, err := s.CreateSNMPProfile(ctx, SNMPProfile{
		ID: "00000000-0000-4000-9000-000000000001", Name: "publica", Version: SNMPv2c,
		SecretEnc: []byte("blob-cifrado"), SecretKeyID: "kid-1",
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	changed, err := s.RotateSNMPProfileSecret(ctx, p.ID, fakeRotate("kid-2"))
	if err != nil || !changed {
		t.Fatalf("RotateSNMPProfileSecret got changed=%v, %v; want true, nil", changed, err)
	}
	got, err := s.SNMPProfileByID(ctx, p.ID)
	if err != nil || got.SecretKeyID != "kid-2" || string(got.SecretEnc) != "enc:kid-2:"+p.ID+":snmp_profile" {
		t.Fatalf("perfil recifrado got %+v, %v", got, err)
	}

	// Repetir es un no-op.
	if changed, err := s.RotateSNMPProfileSecret(ctx, p.ID, fakeRotate("kid-2")); err != nil || changed {
		t.Fatalf("repetir la rotacion got changed=%v, %v; want false, nil", changed, err)
	}

	if _, err := s.RotateSNMPProfileSecret(ctx, "00000000-0000-4000-9000-0000000000ff", fakeRotate("kid-2")); !errors.Is(err, ErrNotFound) {
		t.Errorf("perfil inexistente err got %v, want ErrNotFound", err)
	}
}
