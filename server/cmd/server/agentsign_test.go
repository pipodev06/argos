package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"argos/server/internal/agentdist"
	"argos/server/internal/agentsig"
)

func TestKeygenPubkeySignManifestRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyfile := filepath.Join(dir, "manifest-signing.key")

	var out bytes.Buffer
	if err := cmdKeygen([]string{keyfile}, &out); err != nil {
		t.Fatalf("keygen: %v", err)
	}
	pubFromKeygen := strings.TrimSpace(out.String())
	if pubFromKeygen == "" {
		t.Fatal("keygen no imprimio la clave publica")
	}
	// El modo 0600 pasado a WriteFile no se comprueba aqui: en Windows el sistema de archivos no
	// respeta bits POSIX (siempre da otra cosa), asi que esta asercion solo tendria sentido en Unix.
	if _, err := os.Stat(keyfile); err != nil {
		t.Fatalf("stat keyfile: %v", err)
	}

	// keygen no pisa una clave existente.
	if err := cmdKeygen([]string{keyfile}, &out); err == nil {
		t.Error("keygen piso un archivo de clave existente")
	}

	out.Reset()
	if err := cmdPubkey([]string{keyfile}, &out); err != nil {
		t.Fatalf("pubkey: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != pubFromKeygen {
		t.Errorf("pubkey got %q, want %q (la impresa por keygen)", got, pubFromKeygen)
	}

	manifestPath := filepath.Join(dir, "manifest.json")
	raw := []byte(`{"version":"1.2.3","files":[]}`)
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err := cmdSignManifest([]string{manifestPath, keyfile}, &out)
	// El manifiesto de prueba no trae los 6 binarios obligatorios: CanonicalManifest debe rechazarlo.
	if err == nil {
		t.Fatal("sign-manifest acepto un manifiesto sin los binarios obligatorios")
	}

	raw = validTestManifest(t)
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := cmdSignManifest([]string{manifestPath, keyfile}, &out); err != nil {
		t.Fatalf("sign-manifest: %v", err)
	}
	sigPath := manifestPath + ".sig"
	sigRaw, err := os.ReadFile(sigPath)
	if err != nil {
		t.Fatalf("leyendo %s: %v", sigPath, err)
	}
	sig, err := agentsig.DecodeSignature(strings.TrimSpace(string(sigRaw)))
	if err != nil {
		t.Fatalf("firma con formato invalido: %v", err)
	}
	pub, err := agentsig.DecodePublic(pubFromKeygen)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := agentdist.CanonicalManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !agentsig.Verify(pub, canon, sig) {
		t.Error("la firma escrita no verifica contra el manifiesto canonico y la clave publica de keygen")
	}

	// Con otra clave, la misma firma no debe verificar.
	_, otherPub, err := agentsig.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if agentsig.Verify(otherPub, canon, sig) {
		t.Error("la firma verifico con una clave publica distinta")
	}

	// Un manifiesto alterado despues de firmar no debe verificar.
	altered := bytes.Replace(canon, []byte(`"version":"1.2.3"`), []byte(`"version":"1.2.4"`), 1)
	if agentsig.Verify(pub, altered, sig) {
		t.Error("la firma verifico contra un manifiesto alterado")
	}
}

func TestSignManifestRejectsMissingKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, validTestManifest(t), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdSignManifest([]string{manifestPath, filepath.Join(dir, "no-existe.key")}, &out); err == nil {
		t.Error("sign-manifest sin clave privada debio fallar")
	}
}

func TestRunAgentSignUnknownSubcommand(t *testing.T) {
	t.Parallel()
	if err := runAgentSign([]string{"borra-todo"}); err == nil {
		t.Error("runAgentSign acepto un subcomando desconocido")
	}
}

// validTestManifest arma un manifest.json con los 6 binarios obligatorios (agentdist.CanonicalManifest
// los exige), con hashes y tamaños con formato valido (no se verifican contra archivos en disco: eso lo
// hace agentdist.Load, no CanonicalManifest).
func validTestManifest(t *testing.T) []byte {
	t.Helper()
	targets := []struct{ os, arch string }{
		{"windows", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"},
	}
	var entries []string
	for _, tg := range targets {
		name := agentdist.FileName(tg.os, tg.arch)
		entries = append(entries, `{"name":"`+name+`","os":"`+tg.os+`","arch":"`+tg.arch+`","sha256":"`+strings.Repeat("a", 64)+`","size":1}`)
	}
	return []byte(`{"version":"1.2.3","files":[` + strings.Join(entries, ",") + `]}`)
}
