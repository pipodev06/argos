package updater

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Service es lo que el reemplazo necesita del servicio del SO (lo implementa service.Manager).
type Service interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Status(ctx context.Context) (string, error)
	ExePath() (string, error)
}

// FileSwapper es el Swapper real: rename del binario junto al instalado. Sirve en los 3 SO: en Windows
// un exe en uso no se puede sobreescribir pero si renombrar, asi que el actual pasa a <exe>.old, el nuevo
// ocupa su lugar y el servicio se reinicia; en Linux/macOS el rename es atomico.
type FileSwapper struct {
	Svc Service
	// IsRunning interpreta la linea de Status (service.StatusRunning).
	IsRunning func(statusLine string) bool
	Logger    *slog.Logger
	// Settle es cuanto se espera tras el Start antes de dar por bueno el arranque (default 5 s).
	Settle time.Duration
}

func (s FileSwapper) settle() time.Duration {
	if s.Settle > 0 {
		return s.Settle
	}
	return 5 * time.Second
}

// Stage crea un archivo temporal 0755 junto al binario instalado (mismo volumen: el rename no copia).
func (s FileSwapper) Stage() (*os.File, error) {
	exe, err := s.Svc.ExePath()
	if err != nil {
		return nil, fmt.Errorf("ubicando el binario instalado: %w", err)
	}
	if _, err := os.Stat(exe); err != nil {
		return nil, fmt.Errorf("el binario instalado %s no existe (¿esta instalado el servicio?): %w", exe, err)
	}
	f, err := os.CreateTemp(filepath.Dir(exe), ".argos-update-*")
	if err != nil {
		return nil, fmt.Errorf("creando el archivo de descarga junto a %s (requiere root/administrador): %w", exe, err)
	}
	if err := f.Chmod(0o755); err != nil { //nolint:gosec // es un ejecutable: 0755, el directorio solo lo escribe root
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, fmt.Errorf("permisos del binario nuevo: %w", err)
	}
	return f, nil
}

// Swap instala staged como binario del servicio y lo reinicia; si no arranca, restaura el anterior.
func (s FileSwapper) Swap(ctx context.Context, staged string) error {
	exe, err := s.Svc.ExePath()
	if err != nil {
		return fmt.Errorf("ubicando el binario instalado: %w", err)
	}
	old := exe + ".old"
	line, err := s.Svc.Status(ctx)
	if err != nil {
		return fmt.Errorf("consultando el servicio: %w", err)
	}
	wasRunning := s.IsRunning(line)

	if err := os.Remove(old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("borrando el respaldo anterior %s: %w", old, err)
	}
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("guardando el binario actual como %s: %w", old, err)
	}
	if err := os.Rename(staged, exe); err != nil {
		if rbErr := os.Rename(old, exe); rbErr != nil {
			return fmt.Errorf("instalando el binario nuevo: %w (y no se pudo restaurar %s: %w)", err, old, rbErr)
		}
		return fmt.Errorf("instalando el binario nuevo: %w", err)
	}
	if !wasRunning {
		s.Logger.Info("el servicio estaba detenido: binario reemplazado, no se arranca")
		s.cleanupOld(old)
		return nil
	}
	if err := s.restart(ctx); err != nil {
		return s.rollback(exe, old, err)
	}
	s.cleanupOld(old)
	return nil
}

// restart reinicia el servicio y comprueba que siga en ejecucion tras Settle.
func (s FileSwapper) restart(ctx context.Context) error {
	if err := s.Svc.Stop(ctx); err != nil {
		return fmt.Errorf("deteniendo el servicio: %w", err)
	}
	if err := s.Svc.Start(ctx); err != nil {
		return fmt.Errorf("arrancando el servicio nuevo: %w", err)
	}
	t := time.NewTimer(s.settle())
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("espera interrumpida: %w", ctx.Err())
	case <-t.C:
	}
	line, err := s.Svc.Status(ctx)
	if err != nil {
		return fmt.Errorf("consultando el servicio nuevo: %w", err)
	}
	if !s.IsRunning(line) {
		return fmt.Errorf("el servicio nuevo no quedo en ejecucion (%s)", line)
	}
	return nil
}

// rollback devuelve el binario anterior y lo arranca. Usa un contexto propio: si el original se
// cancelo (Ctrl+C a mitad), igual hay que dejar el servicio funcionando.
func (s FileSwapper) rollback(exe, old string, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s.Logger.Error("el servicio nuevo fallo, se restaura la version anterior", "err", cause)
	if err := s.Svc.Stop(ctx); err != nil {
		s.Logger.Warn("no se pudo detener el servicio antes de restaurar", "err", err)
	}
	if err := os.Remove(exe); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w; ademas no se pudo quitar el binario nuevo %s: %w", cause, exe, err)
	}
	if err := os.Rename(old, exe); err != nil {
		return fmt.Errorf("%w; ademas no se pudo restaurar %s: %w", cause, old, err)
	}
	if err := s.Svc.Start(ctx); err != nil {
		return fmt.Errorf("%w; se restauro el binario anterior pero el servicio no arranco: %w", cause, err)
	}
	return fmt.Errorf("la actualizacion fallo y se restauro la version anterior: %w", cause)
}

// cleanupOld borra el respaldo tras una actualizacion buena (best-effort: en Windows puede seguir retenido).
func (s FileSwapper) cleanupOld(old string) {
	if err := os.Remove(old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.Logger.Warn("no se pudo borrar el respaldo del binario anterior", "path", old, "err", err)
	}
}

var _ Swapper = FileSwapper{}
