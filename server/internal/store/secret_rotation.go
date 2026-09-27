package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RotateSecret descifra un secreto cifrado con keyID y lo vuelve a cifrar con la clave actual del anillo (lo
// implementa (*notify.KeyRing).Rotate: el store no importa el paquete notify para evitar un ciclo, ya que notify
// ya importa store). id y kind son el AAD con el que se cifro originalmente; ok = false si keyID ya es el de la
// clave actual (no hay nada que hacer).
type RotateSecret func(id, kind, keyID string, enc []byte) (newEnc []byte, newKeyID string, ok bool, err error)

// kindServiceCheck y kindSNMPProfile son el AAD (tipo) con el que se cifran esos secretos: deben coincidir con
// las constantes homonimas de internal/httpapi (que a su vez usan notify.KindServiceCheck/KindSNMPProfile). Se
// duplican aca porque store no puede importar notify ni httpapi.
const (
	kindServiceCheck = "service_check"
	kindSNMPProfile  = "snmp_profile"
)

// RotateChannelSecret bloquea el canal, deja que reencrypt descifre y vuelva a cifrar su secreto y guarda el
// resultado en la misma transaccion. No hace nada (ok=false, sin error) si el canal no tiene secreto o si
// reencrypt dice que ya esta en la clave actual. ErrNotFound si el canal no existe.
func (s *Store) RotateChannelSecret(ctx context.Context, id string, reencrypt RotateSecret) (bool, error) {
	var changed bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var kind string
		var keyID *string
		var enc []byte
		err := tx.QueryRow(ctx, `SELECT kind, secret_key_id, secret_enc FROM notification_channels WHERE id = $1 FOR UPDATE`, id).
			Scan(&kind, &keyID, &enc)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("leyendo canal: %w", err)
		}
		if keyID == nil {
			return nil
		}
		newEnc, newKeyID, ok, err := reencrypt(id, kind, *keyID, enc)
		if err != nil || !ok {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE notification_channels SET secret_enc = $2, secret_key_id = $3 WHERE id = $1`,
			id, newEnc, newKeyID); err != nil {
			return fmt.Errorf("actualizando canal: %w", err)
		}
		changed = true
		return nil
	})
	return changed, err
}

// RotateServiceCheckSecret hace lo mismo que RotateChannelSecret para un chequeo de servicio (solo los de tipo
// http tienen secreto; el resto no hace nada). ErrNotFound si el chequeo no existe.
func (s *Store) RotateServiceCheckSecret(ctx context.Context, id string, reencrypt RotateSecret) (bool, error) {
	var changed bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var keyID *string
		var enc []byte
		err := tx.QueryRow(ctx, `SELECT secret_key_id, secret_enc FROM service_check_defs WHERE id = $1 FOR UPDATE`, id).
			Scan(&keyID, &enc)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("leyendo chequeo: %w", err)
		}
		if keyID == nil {
			return nil
		}
		newEnc, newKeyID, ok, err := reencrypt(id, kindServiceCheck, *keyID, enc)
		if err != nil || !ok {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE service_check_defs SET secret_enc = $2, secret_key_id = $3 WHERE id = $1`,
			id, newEnc, newKeyID); err != nil {
			return fmt.Errorf("actualizando chequeo: %w", err)
		}
		changed = true
		return nil
	})
	return changed, err
}

// RotateSNMPProfileSecret hace lo mismo que RotateChannelSecret para un perfil snmp (siempre tiene secreto).
// ErrNotFound si el perfil no existe.
func (s *Store) RotateSNMPProfileSecret(ctx context.Context, id string, reencrypt RotateSecret) (bool, error) {
	var changed bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var keyID string
		var enc []byte
		err := tx.QueryRow(ctx, `SELECT secret_key_id, secret_enc FROM snmp_profiles WHERE id = $1 FOR UPDATE`, id).
			Scan(&keyID, &enc)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("leyendo perfil snmp: %w", err)
		}
		newEnc, newKeyID, ok, err := reencrypt(id, kindSNMPProfile, keyID, enc)
		if err != nil || !ok {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE snmp_profiles SET secret_enc = $2, secret_key_id = $3 WHERE id = $1`,
			id, newEnc, newKeyID); err != nil {
			return fmt.Errorf("actualizando perfil snmp: %w", err)
		}
		changed = true
		return nil
	})
	return changed, err
}
