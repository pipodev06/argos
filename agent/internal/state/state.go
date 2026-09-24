// Package state persiste las credenciales del agente (host_id y agent token)
// en un archivo dentro del directorio de estado.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	fileName = "agent.json"
	// CAFileName es la CA privada que `install` deja junto al estado; el agente la usa por defecto.
	CAFileName = "ca.pem"
	// maxFileBytes acota la lectura: el archivo real ocupa unos cientos de bytes.
	maxFileBytes = 64 << 10
)

// ErrNotEnrolled indica que no hay credenciales guardadas.
var ErrNotEnrolled = errors.New("el agente no esta enrolado")

// Credentials es lo que el enroll le entrega al agente.
type Credentials struct {
	HostID     string `json:"host_id"`
	AgentToken string `json:"agent_token"`
	// ServerURL es el servidor con el que se enrolo; ARGOS_SERVER_URL lo pisa.
	ServerURL string `json:"server_url"`
}

// String y LogValue ocultan el token si alguien imprime o loguea la estructura entera.
func (c Credentials) String() string {
	return fmt.Sprintf("Credentials{HostID:%s, ServerURL:%s, AgentToken:[REDACTED]}", c.HostID, c.ServerURL)
}

// LogValue implementa slog.LogValuer sin exponer el token.
func (c Credentials) LogValue() slog.Value {
	return slog.GroupValue(slog.String("host_id", c.HostID), slog.String("server_url", c.ServerURL))
}

// Store lee y escribe el archivo de estado. No es seguro pa uso concurrente
// entre procesos: enroll y run no se ejecutan a la vez sobre el mismo directorio.
type Store struct {
	dir string
	// serviceAccount es la identidad con la que corre el servicio y que debe poder
	// LEER el estado (Linux/macOS: nombre de usuario, dueno del directorio y el
	// archivo; Windows: SID, con acceso de solo lectura). Vacio = uso normal: solo
	// el usuario actual.
	serviceAccount string
	logger         *slog.Logger
	prepared       bool
}

// New crea un Store sobre dir (no toca el disco todavia) accesible solo por el usuario actual.
func New(dir string, logger *slog.Logger) *Store {
	return &Store{dir: dir, logger: logger}
}

// NewForService crea un Store pa el directorio de estado de un servicio: quien
// escribe (root/administrador, al instalar o enrolar) lo deja legible por la
// cuenta del servicio (ver Store.serviceAccount) y por nadie mas.
func NewForService(dir string, logger *slog.Logger, serviceAccount string) *Store {
	return &Store{dir: dir, logger: logger, serviceAccount: serviceAccount}
}

// Path es la ruta del archivo de estado.
func (s *Store) Path() string { return filepath.Join(s.dir, fileName) }

// Prepare crea el directorio (0700 en Unix) y lo restringe (al usuario actual o, en
// modo servicio, a quien administra y a la cuenta del servicio).
// Se llama antes de consumir el enroll token: si el disco no es usable, se
// falla sin quemarlo. En Windows el endurecimiento es best-effort: si icacls
// falla se avisa por log y se sigue (el directorio por defecto, bajo %APPDATA%,
// ya es privado del usuario).
func (s *Store) Prepare() error {
	if s.prepared {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("creando directorio de estado: %w", err)
	}
	if err := restrictDir(s.dir, s.serviceAccount); err != nil {
		s.logger.Warn("no se pudieron restringir los permisos del directorio de estado; revisalos a mano",
			"dir", s.dir, "err", err)
	}
	s.prepared = true
	return nil
}

// Save escribe las credenciales de forma atomica (temp + rename) con permisos 0600.
func (s *Store) Save(c Credentials) error {
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("serializando credenciales: %w", err)
	}
	return s.writeAtomic(fileName, data)
}

// SaveCA guarda la CA privada del servidor como ca.pem (mismas garantias que Save).
func (s *Store) SaveCA(pem []byte) error { return s.writeAtomic(CAFileName, pem) }

// writeAtomic escribe name dentro del directorio de estado (temp + fsync + rename) con permisos restringidos.
func (s *Store) writeAtomic(name string, data []byte) (err error) {
	if err := s.Prepare(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".agent-*.tmp")
	if err != nil {
		return fmt.Errorf("creando archivo temporal: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName) // best-effort: el error real ya viene en err
		}
	}()
	if err = restrictFile(tmpName, s.serviceAccount); err != nil {
		return fmt.Errorf("restringiendo permisos del archivo: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("escribiendo credenciales: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sincronizando credenciales: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("cerrando archivo temporal: %w", err)
	}
	if err = os.Rename(tmpName, filepath.Join(s.dir, name)); err != nil {
		return fmt.Errorf("moviendo credenciales a su lugar: %w", err)
	}
	return nil
}

// Load lee las credenciales. Devuelve ErrNotEnrolled si el archivo no existe.
func (s *Store) Load() (Credentials, error) {
	f, err := os.Open(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, ErrNotEnrolled
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("abriendo %s: %w", s.Path(), err)
	}
	defer func() { _ = f.Close() }() // solo lectura: un error al cerrar no aporta nada

	var c Credentials
	if err := json.NewDecoder(io.LimitReader(f, maxFileBytes)).Decode(&c); err != nil {
		return Credentials{}, fmt.Errorf("archivo de estado %s corrupto (re-enrola con --force): %w", s.Path(), err)
	}
	if c.AgentToken == "" {
		return Credentials{}, fmt.Errorf("archivo de estado %s sin token (re-enrola con --force)", s.Path())
	}
	return c, nil
}

// Resolve decide con que token corre el agente: ARGOS_AGENT_TOKEN (override,
// solo trae el token) o, si esta vacio, el archivo de estado. Sin ninguno,
// el error explica como enrolar.
func Resolve(envToken string, s *Store) (Credentials, error) {
	if envToken != "" {
		return Credentials{AgentToken: envToken}, nil
	}
	c, err := s.Load()
	if errors.Is(err, ErrNotEnrolled) {
		return Credentials{}, fmt.Errorf("%w: no hay ARGOS_AGENT_TOKEN ni archivo %s; enrolalo con: argos-agent enroll --server <URL> --token <enroll-token>",
			ErrNotEnrolled, s.Path())
	}
	if err != nil {
		return Credentials{}, err
	}
	return c, nil
}
