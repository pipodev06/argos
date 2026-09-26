// Comando server: servidor central de argos (API HTTP + Postgres).
//
// Uso:
//
//	argos-server                       arranca el servidor
//	argos-server create-admin [--must-change] [user]  crea un usuario admin (contraseña por terminal o stdin; con
//	                                   --must-change debe cambiarla en su primer login)
//	argos-server reset-password <user> fija una contraseña nueva, desbloquea la cuenta y cierra sus sesiones
//	argos-server migrate               aplica las migraciones con ARGOS_MIGRATE_DATABASE_URL (o DATABASE_URL) y, si
//	                                   ARGOS_APP_ROLE esta definida, deja los permisos del rol de runtime
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"argos/server/internal/agentdist"
	"argos/server/internal/auth"
	"argos/server/internal/config"
	"argos/server/internal/httpapi"
	"argos/server/internal/notify"
	"argos/server/internal/retention"
	"argos/server/internal/store"
)

const (
	shutdownTimeout = 10 * time.Second
	// Argon2id usa ~19 MiB por hash: 4 concurrentes acotan la RAM a ~76 MiB.
	maxConcurrentHashes = 4
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// os.Exit solo aca, despues de que run() retorno y corrieron sus defers.
	if err := run(logger, os.Args[1:]); err != nil {
		logger.Error("argos-server termino con error", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, args []string) error {
	if len(args) > 0 && args[0] == "migrate" {
		return runMigrate(logger)
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("configuracion: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("conectando a postgres: %w", err)
	}
	defer pool.Close()

	if cfg.AutoMigrate {
		if err := store.Migrate(ctx, pool); err != nil {
			return err
		}
	} else if pending, err := store.SchemaPending(ctx, pool); err != nil {
		return err
	} else if pending {
		return errors.New("hay migraciones pendientes y ARGOS_AUTO_MIGRATE=false: ejecuta `argos-server migrate` (en el deploy: sudo argos-admin migrate)")
	}
	detailSource := store.SourceDefault
	if cfg.MetricsRetentionFromEnv {
		detailSource = store.SourceEnv
	}
	st := store.New(pool).WithRetention(store.RetentionDefaults{
		DetailDays: cfg.MetricsRetentionDays, DetailSource: detailSource, HotDays: cfg.MetricsHotDays, HourlyDays: cfg.MetricsHourlyRetentionDays,
	})

	hasher, err := auth.NewHasher(auth.DefaultParams, maxConcurrentHashes)
	if err != nil {
		return fmt.Errorf("creando hasher: %w", err)
	}

	if len(args) > 0 {
		return runCommand(ctx, st, hasher, args)
	}
	logger.Info("postgres listo, migraciones aplicadas")
	// Un fallo del diagnostico no impide arrancar: es solo un aviso para el admin.
	if n, err := st.CollectorGroupMismatches(ctx); err != nil {
		logger.Warn("no se pudo comprobar el grupo de los colectores", "err", err)
	} else if n > 0 {
		logger.Warn("dispositivos con un colector de otro grupo: un agente sondea equipos de otro cliente; corrigelos (GET /api/v1/devices, collector_group_mismatch)", "count", n)
	}
	return serve(ctx, logger, cfg, pool, st, hasher)
}

// runMigrate aplica las migraciones con el rol dueño del esquema (ARGOS_MIGRATE_DATABASE_URL; si falta, DATABASE_URL)
// y, con ARGOS_APP_ROLE, deja listos los permisos del rol de runtime. No lee el resto de la config del server.
func runMigrate(logger *slog.Logger) error {
	url := os.Getenv("ARGOS_MIGRATE_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		return errors.New("migrate: falta ARGOS_MIGRATE_DATABASE_URL (o DATABASE_URL)")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := store.Connect(ctx, url)
	if err != nil {
		return fmt.Errorf("conectando a postgres: %w", err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		return err
	}
	role := os.Getenv("ARGOS_APP_ROLE")
	if role != "" {
		if err := store.PrepareRuntimeRole(ctx, pool, role); err != nil {
			return err
		}
	}
	logger.Info("migraciones aplicadas", "app_role", role)
	return nil
}

// days convierte dias enteros a Duration.
func days(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

// loadDist carga el directorio de binarios del agente. Sin configurar devuelve nil en silencio; si esta
// roto (falta, manifiesto invalido, sha256 distinto) deja un error fuerte y devuelve nil: las descargas
// responden 404 pero el server sigue en pie.
func loadDist(logger *slog.Logger, dir string) *agentdist.Catalog {
	if dir == "" {
		return nil
	}
	cat, err := agentdist.Load(dir)
	if err != nil {
		logger.Error("ARGOS_AGENT_DIST_DIR invalido: /dl e /install.* responderan 404 hasta corregirlo y reiniciar", "dir", dir, "err", err)
		return nil
	}
	logger.Info("binarios del agente publicados", "version", cat.Version)
	return cat
}

func serve(ctx context.Context, logger *slog.Logger, cfg config.Config, pool *pgxpool.Pool, st *store.Store, hasher *auth.Hasher) error {
	nd, err := setupNotify(ctx, cfg, st)
	if err != nil {
		return err
	}
	dist := loadDist(logger, cfg.AgentDistDir)
	var web fs.FS
	if cfg.WebDir != "" {
		root, err := httpapi.OpenWebDir(cfg.WebDir)
		if err != nil {
			return err
		}
		defer func() {
			if err := root.Close(); err != nil {
				logger.Warn("cerrando ARGOS_WEB_DIR", "err", err)
			}
		}()
		web = root.FS()
		logger.Info("frontend servido desde ARGOS_WEB_DIR", "dir", cfg.WebDir)
	}
	api := httpapi.New(st, hasher, logger, time.Now, httpapi.Options{
		PublicURL:       cfg.PublicURL,
		TrustedProxies:  cfg.TrustedProxies,
		InsecureCookies: cfg.InsecureCookies,
		HostDownAfter:   cfg.HostDownAfter,
		Dist:            dist,
		Secrets:         nd.box,
		Webhook:         nd.client,
		Web:             web,
	})
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	useTLS := cfg.TLSCert != ""
	if useTLS {
		// Se carga el par al arrancar: un certificado ilegible falla rapido y con mensaje claro.
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return fmt.Errorf("cargando ARGOS_TLS_CERT/ARGOS_TLS_KEY: %w", err)
		}
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	} else if cfg.PublicURL != nil && cfg.PublicURL.Scheme == "https" {
		logger.Info("sin TLS propio: se asume un proxy inverso que termina HTTPS (ver docs/decisiones.md)")
	}

	// El mantenimiento (purga, volcado de auditoria), el de particiones y resumenes de metricas y el
	// detector de hosts caidos y el evaluador de alertas tienen su propio contexto: el de la senal se cancela antes que el apagado ordenado, y quiero que
	// el ultimo volcado de auditoria corra despues.
	maintCtx, stopMaint := context.WithCancel(context.WithoutCancel(ctx))
	var maint sync.WaitGroup
	maint.Go(func() {
		runMaintenance(maintCtx, logger, st, api, retentionPolicy{
			Audit:         days(cfg.AuditRetentionDays),
			HostEvents:    days(cfg.HostEventsRetentionDays),
			Alerts:        days(cfg.AlertsRetentionDays),
			Notifications: days(cfg.NotificationsRetentionDays),
			CheckEvents:   days(cfg.ServiceEventsRetentionDays),
			Inventory:     days(cfg.InventoryRetentionDays),
			Updates:       days(cfg.UpdatesRetentionDays),
		})
	})
	metricsMaint := retention.New(pool, logger, retention.Config{
		HotDays:           cfg.MetricsHotDays,
		DetailDays:        cfg.MetricsRetentionDays,
		HourlyDays:        cfg.MetricsHourlyRetentionDays,
		ServiceChecksDays: cfg.ServiceChecksRetentionDays,
		DiskDays:          cfg.DiskRetentionDays,
		Interval:          cfg.MetricsMaintenanceInterval,
	}, time.Now)
	maint.Go(func() { metricsMaint.Run(maintCtx) })
	maint.Go(func() {
		runHostDownDetector(maintCtx, logger, st, cfg.HostDownAfter, cfg.HostCheckInterval)
	})
	maint.Go(func() {
		runAlertEvaluator(maintCtx, logger, st, cfg.AlertEvalInterval, cfg.HostDownAfter)
	})
	baseURL := ""
	if cfg.PublicURL != nil {
		baseURL = cfg.PublicURL.String()
	}
	notifier := notify.NewWorker(st, nd.box, nd.client, logger, baseURL)
	maint.Go(func() { notifier.Run(maintCtx, cfg.NotifyInterval) })
	defer func() {
		stopMaint()
		maint.Wait()
	}()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("argos server escuchando", "addr", cfg.ListenAddr, "tls", useTLS)
		if useTLS {
			serveErr <- srv.ListenAndServeTLS("", "")
			return
		}
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("servidor http: %w", err)
	case <-ctx.Done():
		logger.Info("senal de apagado recibida")
	}

	// El ctx de la senal ya esta cancelado: el apagado usa uno nuevo con timeout.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("apagando servidor http: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("servidor http: %w", err)
	}
	logger.Info("servidor apagado")
	return nil
}
