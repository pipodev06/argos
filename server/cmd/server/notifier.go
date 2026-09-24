package main

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"argos/server/internal/config"
	"argos/server/internal/notify"
	"argos/server/internal/store"
)

// notifyDeps son las piezas de notificaciones que se cablean al arrancar.
type notifyDeps struct {
	box    *notify.Box // nil si no hay ARGOS_SECRET_KEY
	client *notify.Client
}

// setupNotify valida la clave de secretos (formato, y coincidencia con los canales ya cifrados: si no cuadra, el
// server no arranca), la allowlist de webhooks y las CAs extra, y crea el cliente webhook.
func setupNotify(ctx context.Context, cfg config.Config, st *store.Store) (notifyDeps, error) {
	var box *notify.Box
	if cfg.SecretKey != "" {
		b, err := notify.NewBox(cfg.SecretKey)
		if err != nil {
			return notifyDeps{}, fmt.Errorf("ARGOS_SECRET_KEY invalida: %w", err)
		}
		box = b
	}
	if err := notify.CheckSecretKey(ctx, st, box); err != nil {
		return notifyDeps{}, err
	}
	policy, err := notify.ParsePolicy(cfg.WebhookAllowHosts)
	if err != nil {
		return notifyDeps{}, fmt.Errorf("ARGOS_WEBHOOK_ALLOW_HOSTS invalida: %w", err)
	}
	var roots *x509.CertPool
	if cfg.WebhookCAFile != "" {
		pem, err := os.ReadFile(cfg.WebhookCAFile)
		if err != nil {
			return notifyDeps{}, fmt.Errorf("leyendo ARGOS_WEBHOOK_CA_FILE: %w", err)
		}
		if roots, err = x509.SystemCertPool(); err != nil {
			return notifyDeps{}, fmt.Errorf("cargando las CAs del sistema: %w", err)
		}
		if !roots.AppendCertsFromPEM(pem) {
			return notifyDeps{}, errors.New("ARGOS_WEBHOOK_CA_FILE no contiene certificados PEM validos")
		}
	}
	return notifyDeps{box: box, client: notify.NewClient(policy, notify.ClientOptions{RootCAs: roots})}, nil
}
