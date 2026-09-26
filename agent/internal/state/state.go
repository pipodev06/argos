// Package state persiste las credenciales del agente (host_id y agent token)
// en un archivo dentro del directorio de estado.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"
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

// WriteFile escribe name dentro del directorio de estado de forma atomica y con permisos restringidos.
func (s *Store) WriteFile(name string, data []byte) error { return s.writeAtomic(name, data) }

// ReadFile lee name del directorio de estado, hasta limit+1 bytes (el llamador detecta si se paso).
// El error cumple errors.Is(err, os.ErrNotExist) si el archivo no existe.
func (s *Store) ReadFile(name string, limit int64) ([]byte, error) {
	f, err := os.Open(filepath.Join(s.dir, name)) //nolint:gosec // nombre fijo bajo el directorio de estado
	if err != nil {
		return nil, fmt.Errorf("abriendo %s: %w", name, err)
	}
	defer func() { _ = f.Close() }() // solo lectura
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", name, err)
	}
	return data, nil
}

// OpenAppend abre (o crea) name en el directorio de estado pa agregar al final, con los mismos
// permisos restringidos que el resto del estado. El llamador cierra el archivo.
func (s *Store) OpenAppend(name string) (*os.File, error) {
	if err := s.Prepare(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600) //nolint:gosec // ruta bajo el directorio de estado
	if err != nil {
		return nil, fmt.Errorf("abriendo %s: %w", name, err)
	}
	if err := restrictFile(path, s.serviceAccount); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("restringiendo permisos de %s: %w", name, err)
	}
	return f, nil
}

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
	if err = renameWithRetry(tmpName, filepath.Join(s.dir, name)); err != nil {
		return fmt.Errorf("moviendo credenciales a su lugar: %w", err)
	}
	return nil
}

// renameWithRetry reemplaza el destino de forma atomica. En Windows el reemplazo falla con "Acceso denegado"
// mientras un antivirus o indexador retiene el archivo unos instantes: se reintenta brevemente solo ante
// errores de permiso (cualquier otro error, o agotar los intentos, se devuelve tal cual).
func renameWithRetry(from, to string) error {
	const attempts = 6
	var err error
	for i := 0; i < attempts; i++ {
		if err = os.Rename(from, to); err == nil || !errors.Is(err, fs.ErrPermission) {
			return err
		}
		time.Sleep(time.Duration(i+1) * 40 * time.Millisecond)
	}
	return err
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
