// Package agentdist carga y verifica el directorio de binarios del agente que el server publica en /dl.
//
// Layout de ARGOS_AGENT_DIST_DIR (lo genera scripts/build-agent.sh):
//
//	<dir>/latest             texto: version publicada (una linea)
//	<dir>/<version>/manifest.json
//	<dir>/<version>/argos-agent_<os>_<arch>[.exe]   (los 6 objetivos)
//
// Load verifica al arrancar que el manifiesto sea valido y que cada binario coincida en tamaño y SHA-256.
// El resultado es un mapa cerrado en memoria: nada de lo que llega por HTTP se usa pa construir rutas.
package agentdist

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	latestFile   = "latest"
	manifestFile = "manifest.json"
	maxMetaBytes = 1 << 16 // tope pa `latest` y `manifest.json`
)

// versionRe limita la version a un nombre de directorio seguro (sin separadores ni "..").
var versionRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)

var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// targets son los seis objetivos obligatorios.
var targets = []struct{ os, arch string }{
	{"windows", "amd64"}, {"windows", "arm64"},
	{"linux", "amd64"}, {"linux", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
}

// File es un binario publicado.
type File struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Catalog es el conjunto verificado de binarios de una version.
type Catalog struct {
	Version string
	files   map[string]File // por nombre de archivo
	dir     string          // <dir>/<version>
	// manifest es el manifiesto canonico (una entrada por linea: lo parsean los scripts de instalacion).
	manifest []byte
}

type manifestDoc struct {
	Version string `json:"version"`
	Files   []File `json:"files"`
}

// FileName devuelve el nombre canonico del binario de un objetivo.
func FileName(osName, arch string) string {
	n := "argos-agent_" + osName + "_" + arch
	if osName == "windows" {
		n += ".exe"
	}
	return n
}

// Load lee y verifica el directorio de distribucion. Cualquier discrepancia devuelve error.
func Load(root string) (*Catalog, error) {
	raw, err := readSmall(filepath.Join(root, latestFile))
	if err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", latestFile, err)
	}
	version := strings.TrimSpace(string(raw))
	if !versionRe.MatchString(version) {
		return nil, fmt.Errorf("%s no contiene una version valida", latestFile)
	}
	dir := filepath.Join(root, version)

	raw, err = readSmall(filepath.Join(dir, manifestFile))
	if err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", manifestFile, err)
	}
	docVersion, byName, canon, err := canonicalizeManifest(raw)
	if err != nil {
		return nil, err
	}
	if docVersion != version {
		return nil, fmt.Errorf("el manifiesto declara la version %q, latest apunta a %q", docVersion, version)
	}

	c := &Catalog{Version: version, dir: dir, files: make(map[string]File, len(targets))}
	for _, t := range targets {
		name := FileName(t.os, t.arch)
		f, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("manifiesto: falta %q", name)
		}
		if err := verifyFile(filepath.Join(dir, name), f); err != nil {
			return nil, fmt.Errorf("verificando %s: %w", name, err)
		}
		c.files[name] = f
	}

	c.manifest = canon
	return c, nil
}

// canonicalizeManifest valida un manifest.json (dedup, exactamente los objetivos esperados, formato de
// cada entrada) y devuelve su version, sus archivos por nombre y la forma canonica que el server sirve.
// No toca disco: no verifica que los binarios existan.
func canonicalizeManifest(raw []byte) (version string, byName map[string]File, canon []byte, err error) {
	var doc manifestDoc
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return "", nil, nil, fmt.Errorf("manifiesto invalido: %w", err)
	}
	if !versionRe.MatchString(doc.Version) {
		return "", nil, nil, errors.New("manifiesto con version invalida")
	}
	byName = make(map[string]File, len(doc.Files))
	for _, f := range doc.Files {
		if _, dup := byName[f.Name]; dup {
			return "", nil, nil, fmt.Errorf("manifiesto: %q repetido", f.Name)
		}
		byName[f.Name] = f
	}
	if len(byName) != len(targets) {
		return "", nil, nil, fmt.Errorf("manifiesto: se esperaban %d binarios, hay %d", len(targets), len(byName))
	}
	for _, t := range targets {
		name := FileName(t.os, t.arch)
		f, ok := byName[name]
		if !ok {
			return "", nil, nil, fmt.Errorf("manifiesto: falta %q", name)
		}
		if f.OS != t.os || f.Arch != t.arch || !sha256Re.MatchString(f.SHA256) || f.Size <= 0 {
			return "", nil, nil, fmt.Errorf("manifiesto: entrada invalida pa %q", name)
		}
	}
	return doc.Version, byName, canonicalManifest(doc.Version, doc.Files), nil
}

// Manifest devuelve el manifiesto canonico.
func (c *Catalog) Manifest() []byte { return c.manifest }

// Open abre un binario por nombre exacto. ok=false si no esta en el mapa cerrado. El tamaño en disco se
// re-comprueba: si el archivo cambio despues del arranque, devuelve error (el llamador responde 404).
func (c *Catalog) Open(name string) (f *os.File, meta File, ok bool, err error) {
	meta, ok = c.files[name]
	if !ok {
		return nil, File{}, false, nil
	}
	f, err = os.Open(filepath.Join(c.dir, meta.Name)) //nolint:gosec // meta.Name sale del mapa cerrado verificado
	if err != nil {
		return nil, File{}, true, fmt.Errorf("abriendo %s: %w", name, err)
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() != meta.Size {
		_ = f.Close()
		return nil, File{}, true, fmt.Errorf("%s cambio en disco desde el arranque", name)
	}
	return f, meta, true, nil
}

func canonicalManifest(version string, files []File) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "{\n\"version\":%q,\n\"files\":[\n", version)
	for i, t := range targets {
		for _, f := range files {
			if f.Name != FileName(t.os, t.arch) {
				continue
			}
			line, _ := json.Marshal(f) // struct simple: no puede fallar
			b.Write(line)
			if i < len(targets)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
	}
	b.WriteString("]\n}\n")
	return b.Bytes()
}

func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // ruta armada con la config y nombres validados
	if err != nil {
		return nil, fmt.Errorf("abriendo: %w", err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxMetaBytes+1))
	if err != nil {
		return nil, fmt.Errorf("leyendo: %w", err)
	}
	if len(b) > maxMetaBytes {
		return nil, errors.New("archivo demasiado grande")
	}
	return b, nil
}

func verifyFile(path string, want File) error {
	f, err := os.Open(path) //nolint:gosec // ruta armada con nombres canonicos
	if err != nil {
		return fmt.Errorf("abriendo: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return fmt.Errorf("leyendo: %w", err)
	}
	if n != want.Size {
		return fmt.Errorf("tamaño %d, el manifiesto dice %d", n, want.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want.SHA256 {
		return errors.New("sha256 no coincide con el manifiesto")
	}
	return nil
}
