package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"argos/server/internal/config"
	"argos/server/internal/notify"
	"argos/server/internal/store"
)

// rotateSecretKeyTimeout acota `rotate-secret-key`: recifra fila por fila (canales, chequeos y perfiles snmp,
// topes MaxChannels+MaxServiceChecks+MaxSNMPProfiles = 350), asi que necesita mas margen que cliTimeout.
const rotateSecretKeyTimeout = 5 * time.Minute

// runRotateSecretKey recifra con la clave actual (ARGOS_SECRET_KEY) los secretos de canales, chequeos de
// servicio y perfiles snmp que todavia estan en una clave de ARGOS_SECRET_KEY_PREVIOUS (o en la actual, en cuyo
// caso no hace nada: repetir el comando es un no-op). Cada fila se recifra en su propia transaccion: si falla a
// mitad, lo ya recifrado queda bien y lo que falta sigue en su clave anterior (se puede volver a correr).
func runRotateSecretKey(ctx context.Context, st *store.Store, cfg config.Config) error {
	ring, err := notify.NewKeyRing(cfg.SecretKey, cfg.SecretKeyPrevious)
	if err != nil {
		return err
	}
	if ring == nil {
		return errors.New("rotate-secret-key: ARGOS_SECRET_KEY no esta definida")
	}

	var channels, checks, profiles int

	chs, err := st.ListChannels(ctx)
	if err != nil {
		return fmt.Errorf("listando canales: %w", err)
	}
	for _, c := range chs {
		changed, err := st.RotateChannelSecret(ctx, c.ID, ring.Rotate)
		if err != nil {
			return fmt.Errorf("recifrando el canal %q: %w", c.Name, err)
		}
		if changed {
			channels++
		}
	}

	scs, err := st.ListServiceChecks(ctx, store.AllScope())
	if err != nil {
		return fmt.Errorf("listando chequeos: %w", err)
	}
	for _, d := range scs {
		changed, err := st.RotateServiceCheckSecret(ctx, d.ID, ring.Rotate)
		if err != nil {
			return fmt.Errorf("recifrando el chequeo %q: %w", d.Name, err)
		}
		if changed {
			checks++
		}
	}

	profs, err := st.ListSNMPProfiles(ctx)
	if err != nil {
		return fmt.Errorf("listando perfiles snmp: %w", err)
	}
	for _, p := range profs {
		changed, err := st.RotateSNMPProfileSecret(ctx, p.ID, ring.Rotate)
		if err != nil {
			return fmt.Errorf("recifrando el perfil snmp %q: %w", p.Name, err)
		}
		if changed {
			profiles++
		}
	}

	total := channels + checks + profiles
	if err := st.InsertAudit(ctx, store.AuditEntry{
		Event: "secret_key_rotated", Outcome: store.OutcomeOK,
		Detail: []byte(fmt.Sprintf(`{"via":"cli","channels":%d,"checks":%d,"snmp_profiles":%d}`, channels, checks, profiles)),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: no se pudo escribir la auditoria: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "secretos recifrados con la clave actual: %d canales, %d chequeos, %d perfiles snmp (%d en total; el resto ya estaba en la clave actual)\n",
		channels, checks, profiles, total)
	return nil
}
