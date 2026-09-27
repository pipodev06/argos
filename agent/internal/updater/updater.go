// Package updater implementa `argos-agent update`: baja del servidor el binario de la ultima version
// publicada, verifica su SHA-256 contra el manifiesto y lo deja como binario del servicio.
//
// H7 (firma del manifiesto, docs/decisiones/2026-09-26-seguridad-multicliente.md #11): el SHA-256 del
// manifiesto solo detecta descargas corruptas, no protege de un servidor (o tunel) comprometido que sirva
// un manifiesto y un binario falsos a la vez. Por eso, antes de aceptar el manifiesto, se verifica su
// firma ed25519 (manifest.json.sig) con la clave publica compilada dentro de este binario
// (-ldflags "-X main.publicKey=..."): esa clave nunca se descarga del server, asi que un server
// comprometido no puede forjarla. Sin clave publica embebida (build de desarrollo) el update falla
// cerrado: ver ErrNoPublicKey.
package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"argos/agent/internal/agentsig"
)

const (
	// maxManifestBytes acota el manifiesto (el real ocupa ~1 KiB).
	maxManifestBytes = 64 << 10
	// maxBinaryBytes acota la descarga (los binarios del agente rondan 15 MiB).
	maxBinaryBytes = 200 << 20
	// maxSigBytes acota la firma (128 hex + salto de linea; sobra margen).
	maxSigBytes = 1 << 10
)

var (
	// ErrDevBuild: una build sin version inyectada no se actualiza (no se sabe que reemplaza).
	ErrDevBuild = errors.New("este binario es una build de desarrollo (version dev): no se actualiza")
	// ErrNoPublicKey: build sin clave publica embebida (-ldflags -X main.publicKey=...). Se falla
	// cerrado a proposito (H7): sin clave no hay como distinguir un manifiesto legitimo de uno forjado
	// por un server comprometido.
	ErrNoPublicKey = errors.New("este binario no tiene la clave publica de firma embebida: no se actualiza")
	// ErrBadSignature: el manifiesto no trae una firma valida pa la clave publica embebida.
	ErrBadSignature = errors.New("la firma del manifiesto no es valida: no se aplica el binario")

	versionRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)
	sha256Re  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Swapper es la frontera con el sistema: crea el archivo donde se descarga el binario nuevo (en el mismo
// directorio que el instalado, pa que el reemplazo sea un rename) y lo deja como binario del servicio
// reiniciandolo. Es una interfaz pa probar la logica de descarga sin tocar servicios reales.
type Swapper interface {
	// Stage crea el archivo (0755) donde se descarga el binario nuevo. El llamador lo cierra y, si
	// Swap no lo consumio, lo borra.
	Stage() (*os.File, error)
	// Swap deja el binario descargado como el del servicio conservando estado y config, reinicia el
	// servicio y, si el nuevo no arranca, restaura el anterior y devuelve error.
	Swap(ctx context.Context, staged string) error
}

// Options son las entradas de Run.
type Options struct {
	Server    string       // URL base del servidor (https, o http si el operador lo permitio)
	Version   string       // version del binario en ejecucion (main.version)
	PubKeyHex string       // clave publica ed25519 embebida (main.publicKey), hex; "" en builds de dev
	HTTP      *http.Client // con timeout suficiente pa la descarga
	GOOS      string
	GOARCH    string
	Swapper   Swapper
	Out       io.Writer
}

type manifest struct {
	Version string `json:"version"`
	Files   []struct {
		Name   string `json:"name"`
		OS     string `json:"os"`
		Arch   string `json:"arch"`
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	} `json:"files"`
}

// Run actualiza el agente. Devuelve nil tanto si actualizo como si ya estaba en la ultima version.
func Run(ctx context.Context, o Options) error {
	if o.Version == "" || o.Version == "dev" {
		return ErrDevBuild
	}
	if o.PubKeyHex == "" {
		return ErrNoPublicKey
	}
	pub, err := agentsig.DecodePublic(o.PubKeyHex)
	if err != nil {
		return fmt.Errorf("clave publica embebida invalida: %w", err)
	}
	mf, err := fetchManifest(ctx, o, pub)
	if err != nil {
		return err
	}
	if mf.Version == o.Version {
		say(o.Out, "ya tienes la ultima version (%s)\n", o.Version)
		return nil
	}
	if older(mf.Version, o.Version) {
		return fmt.Errorf("el servidor publica %s, mas vieja que la instalada (%s): no se hace downgrade", mf.Version, o.Version)
	}
	name, sum, size, err := pick(mf, o.GOOS, o.GOARCH)
	if err != nil {
		return err
	}
	say(o.Out, "actualizando %s -> %s\n", o.Version, mf.Version)

	f, err := o.Swapper.Stage()
	if err != nil {
		return fmt.Errorf("preparando la descarga: %w", err)
	}
	staged := f.Name()
	defer func() { _ = os.Remove(staged) }() // si Swap lo consumio ya no existe; si no, no queda basura
	dlErr := download(ctx, o, mf.Version, name, sum, size, f)
	if closeErr := f.Close(); dlErr == nil && closeErr != nil {
		dlErr = fmt.Errorf("cerrando la descarga: %w", closeErr)
	}
	if dlErr != nil {
		return dlErr
	}
	if err := o.Swapper.Swap(ctx, staged); err != nil {
		return err
	}
	say(o.Out, "agente actualizado a %s\n", mf.Version)
	return nil
}

func say(w io.Writer, format string, args ...any) {
	if w != nil {
		_, _ = fmt.Fprintf(w, format, args...) // salida informativa: un fallo de escritura no aborta
	}
}

// fetchManifest baja el manifiesto y su firma (H7) y verifica la firma con la clave publica embebida
// ANTES de confiar en el contenido: si no valida, no se decodifica ni se usa nada del manifiesto.
func fetchManifest(ctx context.Context, o Options, pub ed25519.PublicKey) (manifest, error) {
	raw, err := fetchRaw(ctx, o, "/dl/latest/manifest.json", maxManifestBytes)
	if err != nil {
		return manifest{}, fmt.Errorf("descargando el manifiesto: %w", err)
	}
	sigRaw, err := fetchRaw(ctx, o, "/dl/latest/manifest.json.sig", maxSigBytes)
	if err != nil {
		return manifest{}, fmt.Errorf("descargando la firma del manifiesto: %w", err)
	}
	sig, err := agentsig.DecodeSignature(strings.TrimSpace(string(sigRaw)))
	if err != nil {
		return manifest{}, fmt.Errorf("%w (%w)", ErrBadSignature, err)
	}
	if !agentsig.Verify(pub, raw, sig) {
		return manifest{}, ErrBadSignature
	}

	var mf manifest
	if err := json.Unmarshal(raw, &mf); err != nil {
		return manifest{}, fmt.Errorf("manifiesto ilegible: %w", err)
	}
	if !versionRe.MatchString(mf.Version) {
		return manifest{}, errors.New("manifiesto con version invalida")
	}
	return mf, nil
}

func fetchRaw(ctx context.Context, o Options, path string, max int64) ([]byte, error) {
	u, err := url.JoinPath(o.Server, path)
	if err != nil {
		return nil, fmt.Errorf("url: %w", err)
	}
	body, err := get(ctx, o.HTTP, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }() // solo lectura
	return io.ReadAll(io.LimitReader(body, max))
}

// pick busca el binario de este os/arch y valida su hash y tamano.
func pick(mf manifest, goos, goarch string) (name, sum string, size int64, err error) {
	for _, f := range mf.Files {
		if f.OS != goos || f.Arch != goarch {
			continue
		}
		if !sha256Re.MatchString(f.SHA256) || f.Size <= 0 || f.Size > maxBinaryBytes || f.Name == "" || strings.ContainsAny(f.Name, `/\`) {
			return "", "", 0, fmt.Errorf("manifiesto invalido para %s/%s", goos, goarch)
		}
		return f.Name, f.SHA256, f.Size, nil
	}
	return "", "", 0, fmt.Errorf("el servidor no publica un binario para %s/%s", goos, goarch)
}

func download(ctx context.Context, o Options, version, name, wantSum string, size int64, dst io.Writer) error {
	u, err := url.JoinPath(o.Server, "/dl/", version, name)
	if err != nil {
		return fmt.Errorf("url de descarga: %w", err)
	}
	body, err := get(ctx, o.HTTP, u)
	if err != nil {
		return fmt.Errorf("descargando el binario: %w", err)
	}
	defer func() { _ = body.Close() }() // solo lectura
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(body, size+1))
	if err != nil {
		return fmt.Errorf("descargando el binario: %w", err)
	}
	if n != size {
		return fmt.Errorf("tamano descargado %d, el manifiesto dice %d", n, size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSum {
		return errors.New("el SHA-256 del binario descargado no coincide con el del manifiesto: no se instala")
	}
	return nil
}

// get hace un GET y exige 200; el llamador cierra el body.
func get(ctx context.Context, c *http.Client, u string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creando request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("el servidor respondio %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// older dice si a es una version numerica x.y.z estrictamente menor que b. Si alguna no es numerica
// no se puede comparar y devuelve false (se deja actualizar: distinta = otra build publicada).
func older(a, b string) bool {
	pa, okA := numeric(a)
	pb, okB := numeric(b)
	if !okA || !okB {
		return false
	}
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func numeric(v string) ([]int, bool) {
	var out []int
	for part := range strings.SplitSeq(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
