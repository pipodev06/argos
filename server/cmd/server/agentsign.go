package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"argos/server/internal/agentdist"
	"argos/server/internal/agentsig"
)

// runAgentSign despacha keygen/pubkey/sign-manifest (H7: firma del manifiesto del agente con una clave
// que NUNCA vive en el server desplegado). Estos subcomandos no tocan la base de datos: se corren en la
// maquina del admin (o en el CI de build), nunca en el server en produccion, porque el archivo de clave
// privada que reciben/producen no debe copiarse ahi.
func runAgentSign(args []string) error {
	switch args[0] {
	case "keygen":
		return cmdKeygen(args[1:], os.Stdout)
	case "pubkey":
		return cmdPubkey(args[1:], os.Stdout)
	case "sign-manifest":
		return cmdSignManifest(args[1:], os.Stdout)
	default:
		return fmt.Errorf("subcomando %q desconocido", args[0])
	}
}

// cmdKeygen genera el par de claves ed25519 que firma el manifiesto del agente. Escribe la clave privada
// (semilla hex) en <outfile> con permisos 0600 y falla si ya existe (una clave nueva no debe pisar una
// vieja por accidente). La clave publica se imprime en stdout: el admin la guarda (deploy/README.md o
// donde documente su despliegue) y la usa al compilar el agente con
// -ldflags "-X main.publicKey=<hex>".
func cmdKeygen(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("uso: argos-server keygen <archivo-clave-privada>")
	}
	outfile := args[0]
	if _, err := os.Stat(outfile); err == nil { //nolint:gosec // ruta la da el operador en su propia maquina
		return fmt.Errorf("%s ya existe: no se pisa una clave existente (usa otro nombre o borrala a proposito)", outfile)
	}
	priv, pub, err := agentsig.GenerateKey()
	if err != nil {
		return err
	}
	if err := os.WriteFile(outfile, []byte(agentsig.EncodePrivate(priv)+"\n"), 0o600); err != nil { //nolint:gosec // ruta la da el operador
		return fmt.Errorf("guardando la clave privada: %w", err)
	}
	fmt.Fprintf(os.Stderr, "clave privada guardada en %s (0600) -- NUNCA la copies al server desplegado ni la subas al repo\n", outfile)
	if _, err := fmt.Fprintf(out, "%s\n", agentsig.EncodePublic(pub)); err != nil {
		return fmt.Errorf("escribiendo la clave publica: %w", err)
	}
	return nil
}

// cmdPubkey deriva y muestra la clave publica de un archivo de clave privada (para automatizar el build
// del agente sin pegar la clave publica a mano cada vez).
func cmdPubkey(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("uso: argos-server pubkey <archivo-clave-privada>")
	}
	priv, err := readPrivateKey(args[0])
	if err != nil {
		return err
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return errors.New("clave privada invalida")
	}
	if _, err := fmt.Fprintf(out, "%s\n", agentsig.EncodePublic(pub)); err != nil {
		return fmt.Errorf("escribiendo la clave publica: %w", err)
	}
	return nil
}

// cmdSignManifest firma manifest.json con la clave privada y escribe la firma (hex) en
// <manifest.json>.sig, en el mismo directorio. Firma la forma CANONICA del manifiesto
// (agentdist.CanonicalManifest), que es exactamente lo que el server sirve por
// /dl/latest/manifest.json: si se firmaran los bytes crudos del archivo y el formato en disco
// difiriera del servido, la firma no verificaria nunca en los agentes.
func cmdSignManifest(args []string, out io.Writer) error {
	if len(args) != 2 {
		return errors.New("uso: argos-server sign-manifest <manifest.json> <archivo-clave-privada>")
	}
	manifestPath, keyPath := args[0], args[1]
	raw, err := os.ReadFile(manifestPath) //nolint:gosec // ruta la da el operador en su propia maquina
	if err != nil {
		return fmt.Errorf("leyendo %s: %w", manifestPath, err)
	}
	canon, err := agentdist.CanonicalManifest(raw)
	if err != nil {
		return fmt.Errorf("manifiesto invalido: %w", err)
	}
	priv, err := readPrivateKey(keyPath)
	if err != nil {
		return err
	}
	sig := agentsig.Sign(priv, canon)
	sigHex := agentsig.EncodeSignature(sig)
	sigPath := manifestPath + ".sig"
	if err := os.WriteFile(sigPath, []byte(sigHex+"\n"), 0o600); err != nil { //nolint:gosec // ruta la da el operador
		return fmt.Errorf("guardando %s: %w", sigPath, err)
	}
	// Hash por canal aparte (H7 punto 5): el admin lo guarda/compara fuera de la petición HTTP al mismo
	// server, junto al de package-server.sh.
	sum := sha256.Sum256([]byte(sigHex + "\n"))
	if _, err := fmt.Fprintf(out, "firmado: %s\nsha256 de %s (guardalo fuera de banda): %s\n", sigPath, sigPath, hex.EncodeToString(sum[:])); err != nil {
		return fmt.Errorf("escribiendo la confirmacion: %w", err)
	}
	return nil
}

// readPrivateKey lee y decodifica un archivo de clave privada (formato de `keygen`: semilla hex, una
// linea).
func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path) //nolint:gosec // ruta la da el operador en su propia maquina
	if err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", path, err)
	}
	priv, err := agentsig.DecodePrivate(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return priv, nil
}
