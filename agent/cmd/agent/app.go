package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"argos/agent/internal/agent"
	"argos/agent/internal/checks"
	"argos/agent/internal/collector"
	"argos/agent/internal/config"
	"argos/agent/internal/enroll"
	"argos/agent/internal/inventory"
	"argos/agent/internal/sender"
	"argos/agent/internal/service"
	"argos/agent/internal/state"
	"argos/agent/internal/transport"
)

const (
	httpTimeout = 10 * time.Second
	usage       = "uso: argos-agent [run [--service]] | enroll --server <URL> (--token-stdin | --token <token>) [--force] | install [--ca-file <ca.pem>] | uninstall [--keep-state] | start | stop | status"

	// maxTokenBytes acota lo que se lee del token por stdin (un enroll token real ocupa ~50 bytes).
	maxTokenBytes = 4096

	defaultIdleLogEvery = 10 * time.Minute
	defaultIdleRecheck  = 30 * time.Second
)

// serviceManager es lo que la app necesita del servicio del SO (lo implementa service.Manager).
type serviceManager interface {
	Install(ctx context.Context, o service.InstallOptions) error
	Uninstall(ctx context.Context, keepState bool, out io.Writer) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Status(ctx context.Context) (string, error)
	Installed() bool
	StateDir() string
	StateAccount() string
}

// app agrupa las dependencias de los subcomandos (inyectables en tests).
type app struct {
	version string
	getenv  func(string) string
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
	logger  *slog.Logger
	// collector es el medidor; nil = el real (gopsutil). Si tambien implementa
	// agent.DiskCollector / agent.HostInfoCollector, se usan pa discos y host_info.
	collector agent.Collector
	// svc administra el servicio del SO.
	svc serviceManager
	// serviceMode: el proceso corre bajo el gestor de servicios (Windows) y no en primer plano.
	serviceMode bool
	// runAsService, si no es nil, ejecuta run bajo el gestor de servicios (Windows: svc.Run).
	runAsService func(run func(context.Context) error) error

	// Tiempos del modo inactivo (0 = default); los tests los acortan.
	idleLogEvery time.Duration
	idleRecheck  time.Duration
}

// execute despacha el subcomando. Sin argumentos corre `run`.
func (a *app) execute(ctx context.Context, args []string) error {
	cmd := "run"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "run":
		return a.runCommand(ctx, args)
	case "enroll":
		return a.enroll(ctx, args)
	case "install":
		return a.install(ctx, args)
	case "uninstall":
		return a.uninstall(ctx, args)
	case "start":
		return a.start(ctx, args)
	case "stop":
		return a.stop(ctx, args)
	case "status":
		return a.status(ctx, args)
	default:
		return fmt.Errorf("subcomando desconocido %q; %s", cmd, usage)
	}
}

// newFlagSet crea un FlagSet que escribe sus errores en stderr y no llama a os.Exit.
func (a *app) newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	return fs
}

// noArgs valida que un subcomando sin flags no reciba argumentos.
func noArgs(name string, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%s no acepta argumentos; %s", name, usage)
	}
	return nil
}

// stateDirDefault decide el directorio de estado cuando ARGOS_STATE_DIR no esta
// definida: el del servicio si el proceso corre como servicio o el servicio esta
// instalado en este equipo (asi `enroll`, `run` y `start` ven las mismas
// credenciales que el servicio); si no, el del usuario.
func (a *app) stateDirDefault() (string, error) {
	if a.serviceMode || a.svc.Installed() {
		return a.svc.StateDir(), nil
	}
	return config.UserStateDir()
}

func (a *app) loadConfig() (config.Config, error) {
	cfg, err := config.Load(a.getenv, a.stateDirDefault)
	if err != nil {
		return config.Config{}, fmt.Errorf("configuracion: %w", err)
	}
	return cfg, nil
}

// newStore abre el almacen de estado; en el directorio del servicio deja los
// archivos legibles por la cuenta del servicio (ver state.NewForService).
func (a *app) newStore(cfg config.Config) *state.Store {
	if filepath.Clean(cfg.StateDir) == filepath.Clean(a.svc.StateDir()) {
		return state.NewForService(cfg.StateDir, a.logger, a.svc.StateAccount())
	}
	return state.New(cfg.StateDir, a.logger)
}

func (a *app) enroll(ctx context.Context, args []string) error {
	fs := a.newFlagSet("enroll")
	server := fs.String("server", "", "URL base del servidor (o ARGOS_SERVER_URL)")
	token := fs.String("token", "", "enroll token (visible en la lista de procesos: prefiere --token-stdin o ARGOS_ENROLL_TOKEN)")
	tokenStdin := fs.Bool("token-stdin", false, "lee el enroll token de la entrada estandar (primera linea)")
	force := fs.Bool("force", false, "reemplaza un enroll existente (el host anterior queda huerfano en el server)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("argumentos de enroll: %w", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("argumentos inesperados: %v; %s", fs.Args(), usage)
	}
	if *tokenStdin && *token != "" {
		return errors.New("usa --token o --token-stdin, no ambos")
	}

	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	serverURL := firstNonEmpty(*server, cfg.ServerURL)
	if serverURL == "" {
		return errors.New("falta el servidor: usa --server <URL> o ARGOS_SERVER_URL")
	}
	enrollToken := cfg.EnrollToken
	switch {
	case *tokenStdin:
		if enrollToken, err = readTokenLine(a.stdin); err != nil {
			return err
		}
	case *token != "":
		enrollToken = *token
		// Solo el aviso: el valor del token no se imprime jamas.
		if _, err := fmt.Fprintln(a.stderr, "aviso: --token deja el token visible en la lista de procesos y en el historial de la terminal; usa --token-stdin o ARGOS_ENROLL_TOKEN"); err != nil {
			return fmt.Errorf("escribiendo aviso: %w", err)
		}
	}
	if enrollToken == "" {
		return errors.New("falta el enroll token: usa --token-stdin (recomendado), ARGOS_ENROLL_TOKEN o --token (se genera en el dashboard, 'Agregar host')")
	}
	if err := a.checkTransport(serverURL, cfg.AllowInsecure); err != nil {
		return err
	}

	st := a.newStore(cfg)
	if !*force {
		existing, err := st.Load()
		switch {
		case err == nil:
			return fmt.Errorf("este agente ya esta enrolado (host_id %s, %s); usa --force pa reemplazarlo", existing.HostID, st.Path())
		case !errors.Is(err, state.ErrNotEnrolled):
			return err
		}
	}
	// El directorio se prepara ANTES de gastar el enroll token: si el disco no sirve, no se quema.
	if err := st.Prepare(); err != nil {
		return err
	}

	client, err := transport.NewClient(cfg.CAFile, httpTimeout)
	if err != nil {
		return fmt.Errorf("cliente http: %w", err)
	}
	res, err := enroll.Do(ctx, client, serverURL, enrollToken, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return fmt.Errorf("enroll: %w", err)
	}
	creds := state.Credentials{HostID: res.HostID, AgentToken: res.AgentToken, ServerURL: serverURL}
	if err := st.Save(creds); err != nil {
		return fmt.Errorf("el servidor enrolo el host %s pero no se pudo guardar el token (revoca ese host en el dashboard y enrola de nuevo): %w", res.HostID, err)
	}
	// Solo host_id y ruta: el token nunca sale por stdout ni por logs.
	next := "argos-agent run"
	if filepath.Clean(cfg.StateDir) == filepath.Clean(a.svc.StateDir()) {
		next = "argos-agent start (si el servicio ya estaba en ejecucion, retoma solo en ~30 s)"
	}
	if _, err := fmt.Fprintf(a.stdout, "enrolado: host_id=%s\ncredenciales guardadas en %s\nsiguiente paso: %s\n", res.HostID, st.Path(), next); err != nil {
		return fmt.Errorf("escribiendo salida: %w", err)
	}
	return nil
}

// readTokenLine lee la primera linea de r (acotada) como token.
func readTokenLine(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("no hay entrada estandar de donde leer el token")
	}
	line, err := bufio.NewReader(io.LimitReader(r, maxTokenBytes)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("leyendo el token de la entrada estandar: %w", err)
	}
	tok := strings.TrimSpace(line)
	if tok == "" {
		return "", errors.New("no llego ningun token por la entrada estandar (ej.: echo <token> | argos-agent enroll --server <URL> --token-stdin)")
	}
	return tok, nil
}

func (a *app) runCommand(ctx context.Context, args []string) error {
	fs := a.newFlagSet("run")
	svcFlag := fs.Bool("service", false, "modo servicio (lo usan systemd y launchd): ante un token rechazado queda inactivo en vez de salir")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("argumentos de run: %w", err)
	}
	if err := noArgs("run", fs.Args()); err != nil {
		return err
	}
	if *svcFlag {
		a.serviceMode = true
	}
	if a.runAsService != nil {
		return a.runAsService(a.run)
	}
	return a.run(ctx)
}

// run ejecuta el agente. En primer plano, un 401 (token rechazado) termina con un
// error que main traduce a salida 2. Como servicio, un 401 (o la falta de enroll)
// NO termina el proceso: un supervisor lo reiniciaria en bucle. El agente queda
// inactivo, con un log periodico claro, y retoma solo cuando aparecen credenciales
// distintas (tras `enroll --force`) o al detener el servicio.
func (a *app) run(ctx context.Context) error {
	for {
		rejected, err := a.runOnce(ctx)
		if err == nil {
			return nil
		}
		var reason string
		switch {
		case errors.Is(err, sender.ErrTokenRejected):
			reason = "token rechazado: re-enrola el agente (argos-agent enroll --force ...); el host pudo ser revocado"
		case errors.Is(err, state.ErrNotEnrolled):
			reason = "el agente no esta enrolado: ejecuta argos-agent enroll --server <URL> --token-stdin"
		default:
			return err
		}
		if !a.serviceMode {
			if errors.Is(err, sender.ErrTokenRejected) {
				a.logger.Error(reason)
			}
			return err
		}
		if !a.idle(ctx, reason, rejected) {
			return nil
		}
	}
}

// idle espera sin reportar: loguea reason de inmediato y luego cada idleLogEvery, y
// revisa cada idleRecheck si hay credenciales distintas de la rechazada. Devuelve
// true si conviene reintentar y false si ctx se cancelo (apagado).
func (a *app) idle(ctx context.Context, reason, rejectedToken string) bool {
	logEvery := durationOr(a.idleLogEvery, defaultIdleLogEvery)
	recheck := durationOr(a.idleRecheck, defaultIdleRecheck)
	a.logger.ErrorContext(ctx, reason)
	lastLog := time.Now()
	t := time.NewTicker(recheck)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			if a.credentialsChanged(rejectedToken) {
				a.logger.InfoContext(ctx, "credenciales nuevas detectadas, se reanuda el envio de metricas")
				return true
			}
			if time.Since(lastLog) >= logEvery {
				a.logger.ErrorContext(ctx, reason)
				lastLog = time.Now()
			}
		}
	}
}

// credentialsChanged dice si ahora hay un agent token valido y distinto del rechazado.
func (a *app) credentialsChanged(rejectedToken string) bool {
	cfg, err := a.loadConfig()
	if err != nil {
		return false
	}
	creds, err := state.Resolve(cfg.Token, a.newStore(cfg))
	return err == nil && creds.AgentToken != rejectedToken
}

func durationOr(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// runOnce resuelve credenciales y corre el loop del agente hasta que ctx se
// cancele o el agente falle. Devuelve el token usado (pa detectar cambios en modo inactivo).
func (a *app) runOnce(ctx context.Context) (token string, err error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return "", err
	}
	st := a.newStore(cfg)
	creds, err := state.Resolve(cfg.Token, st)
	if err != nil {
		return "", err
	}
	serverURL := firstNonEmpty(cfg.ServerURL, creds.ServerURL)
	if serverURL == "" {
		return creds.AgentToken, errors.New("falta el servidor: define ARGOS_SERVER_URL")
	}
	if err := a.checkTransport(serverURL, cfg.AllowInsecure); err != nil {
		return creds.AgentToken, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return creds.AgentToken, fmt.Errorf("obteniendo hostname: %w", err)
	}

	client, err := transport.NewClient(cfg.CAFile, httpTimeout)
	if err != nil {
		return creds.AgentToken, fmt.Errorf("cliente http: %w", err)
	}
	runner := checks.NewRunner(checks.Options{Policy: cfg.CheckAllow, Logger: a.logger})
	defer runner.Close()
	snd, err := sender.New(sender.Options{
		Endpoint:   serverURL,
		Token:      creds.AgentToken,
		Logger:     a.logger,
		HTTPClient: client,
		Interval:   cfg.Interval,
		OnChecks:   runner.Apply,
	})
	if err != nil {
		return creds.AgentToken, fmt.Errorf("creando cliente: %w", err)
	}
	col := a.collector
	if col == nil {
		col = collector.NewSystem(a.logger)
	}
	disks, _ := col.(agent.DiskCollector)        // ok=false: este colector no mide discos
	hostInfo, _ := col.(agent.HostInfoCollector) // ok=false: este colector no lee el host

	origin := "archivo"
	if cfg.Token != "" {
		origin = "variable de entorno" // solo el origen, nunca el valor
	}
	a.logger.Info("argos agent iniciado", "version", a.version, "hostname", hostname,
		"server", serverURL, "interval", cfg.Interval.String(), "disk_interval", cfg.DiskInterval.String(),
		"host_id", creds.HostID, "token_origin", origin, "service", a.serviceMode)
	// Inventario: solo si el operador activo modulos; muere junto con el loop de metricas.
	invCtx, stopInventory := context.WithCancel(ctx)
	var invWG sync.WaitGroup
	if len(cfg.InventoryModules) > 0 {
		a.logger.Info("inventario activado", "modules", cfg.InventoryModules)
		invWG.Add(1)
		go func() {
			defer invWG.Done()
			inventory.Run(invCtx, inventory.Options{
				Endpoint:   serverURL,
				Token:      creds.AgentToken,
				HTTPClient: client,
				Modules:    cfg.InventoryModules,
				Logger:     a.logger,
			})
		}()
	}
	err = agent.Run(ctx, snd, agent.Options{
		Hostname:     hostname,
		Version:      a.version,
		Interval:     cfg.Interval,
		Collector:    col,
		Disks:        disks,
		HostInfo:     hostInfo,
		Checks:       runner,
		DiskInterval: cfg.DiskInterval,
		Now:          time.Now,
		Logger:       a.logger,
	})
	stopInventory()
	invWG.Wait()
	if err != nil {
		return creds.AgentToken, err
	}
	a.logger.Info("agente detenido")
	return creds.AgentToken, nil
}

func (a *app) install(ctx context.Context, args []string) error {
	fs := a.newFlagSet("install")
	caFile := fs.String("ca-file", "", "CA privada (PEM) del servidor; se copia al estado como ca.pem (o ARGOS_CA_FILE)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("argumentos de install: %w", err)
	}
	if err := noArgs("install", fs.Args()); err != nil {
		return err
	}
	err := a.svc.Install(ctx, service.InstallOptions{
		CAFile: firstNonEmpty(*caFile, a.getenv("ARGOS_CA_FILE")),
		Out:    a.stdout,
		Logger: a.logger,
	})
	if err != nil {
		return fmt.Errorf("install: %w", err)
	}
	_, err = fmt.Fprintf(a.stdout, "siguiente paso: enrolar el agente y arrancar el servicio:\n  echo <enroll-token> | argos-agent enroll --server <URL> --token-stdin\n  argos-agent start\n")
	if err != nil {
		return fmt.Errorf("escribiendo salida: %w", err)
	}
	return nil
}

func (a *app) uninstall(ctx context.Context, args []string) error {
	fs := a.newFlagSet("uninstall")
	keep := fs.Bool("keep-state", false, "conserva las credenciales y la CA (por defecto se borran)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("argumentos de uninstall: %w", err)
	}
	if err := noArgs("uninstall", fs.Args()); err != nil {
		return err
	}
	if err := a.svc.Uninstall(ctx, *keep, a.stdout); err != nil {
		return fmt.Errorf("uninstall: %w", err)
	}
	return nil
}

func (a *app) start(ctx context.Context, args []string) error {
	if err := noArgs("start", args); err != nil {
		return err
	}
	// El servicio usa el estado del servicio; sin enroll solo quedaria inactivo.
	st := state.New(a.svc.StateDir(), a.logger)
	if _, err := st.Load(); errors.Is(err, state.ErrNotEnrolled) {
		return fmt.Errorf("el agente aun no esta enrolado (%s): ejecuta antes `argos-agent enroll --server <URL> --token-stdin`", st.Path())
	}
	if err := a.svc.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	_, err := fmt.Fprintln(a.stdout, "servicio iniciado")
	return err //nolint:wrapcheck // error de escritura a stdout, sin contexto extra
}

func (a *app) stop(ctx context.Context, args []string) error {
	if err := noArgs("stop", args); err != nil {
		return err
	}
	if err := a.svc.Stop(ctx); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	_, err := fmt.Fprintln(a.stdout, "servicio detenido")
	return err //nolint:wrapcheck // error de escritura a stdout, sin contexto extra
}

func (a *app) status(ctx context.Context, args []string) error {
	if err := noArgs("status", args); err != nil {
		return err
	}
	line, err := a.svc.Status(ctx)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	enrolled := "no enrolado"
	st := state.New(a.svc.StateDir(), a.logger)
	switch creds, err := st.Load(); {
	case err == nil:
		enrolled = "enrolado (host_id " + creds.HostID + ")"
	case errors.Is(err, state.ErrNotEnrolled):
	default:
		enrolled = "no se pudo leer el estado (¿faltan privilegios?)"
	}
	_, err = fmt.Fprintf(a.stdout, "servicio: %s\nestado del agente: %s\n", line, enrolled)
	return err //nolint:wrapcheck // error de escritura a stdout, sin contexto extra
}

// checkTransport aplica la politica https-o-loopback y advierte fuerte si se
// la salto con ARGOS_ALLOW_INSECURE.
func (a *app) checkTransport(serverURL string, allowInsecure bool) error {
	insecureRemote, err := config.ValidateServerURL(serverURL, allowInsecure)
	if err != nil {
		return fmt.Errorf("servidor %q: %w", serverURL, err)
	}
	if insecureRemote {
		a.logger.Warn("ARGOS_ALLOW_INSECURE activo: se usa http hacia un host remoto, el token y las metricas viajan SIN CIFRAR y cualquiera en la red puede leerlos o suplantar al servidor; usa https")
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
