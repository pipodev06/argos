package inventory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"
)

// Rutas y comandos de lectura. Todo es constante: ruta absoluta, argv fijo, sin shell y sin
// datos externos en los argumentos. Jamas se refresca la cache del gestor ni se instala nada.
const (
	aptGetPath     = "/usr/bin/apt-get"
	dnfPath        = "/usr/bin/dnf"
	yumPath        = "/usr/bin/yum"
	zypperPath     = "/usr/bin/zypper"
	rpmPath        = "/usr/bin/rpm"
	needsRestart   = "/usr/bin/needs-restarting"
	softwareUpdate = "/usr/sbin/softwareupdate"
	sudoPath       = "/usr/bin/sudo"

	rebootRequiredFile = "/var/run/reboot-required"
	aptHistoryFile     = "/var/log/apt/history.log"
	aptStampFile       = "/var/lib/apt/periodic/update-success-stamp"
	aptListsDir        = "/var/lib/apt/lists"

	linuxPath = "/usr/sbin:/usr/bin:/sbin:/bin"

	linuxTimeout   = 30 * time.Second
	windowsTimeout = 90 * time.Second
	macTimeout     = 90 * time.Second
	maxCmdOut      = 1 << 20
	maxHistoryRead = 4 << 20
)

// updEnv son las lecturas del SO del modulo (inyectables en tests).
type updEnv struct {
	goos          string
	windowsOnline string
	run           runFunc
	sudo          bool // modo privilegiado: dnf/yum/zypper via `sudo -n --` (solo PrivilegedCommands)
	getenv        func(string) string
	exists        func(path string) bool
	modTime       func(path string) (time.Time, bool)
	readTail      func(path string, limit int64) ([]byte, bool) // ultimos limit bytes
	now           func() time.Time
}

func realUpdEnv(cfg UpdatesConfig) updEnv {
	online := cfg.WindowsOnline
	if online == "" {
		online = WindowsOnlineAuto
	}
	return updEnv{
		goos:          runtime.GOOS,
		windowsOnline: online,
		sudo:          cfg.UseSudo && runtime.GOOS == "linux",
		run:           runCmd,
		getenv:        os.Getenv,
		exists:        func(p string) bool { _, err := os.Stat(p); return err == nil },
		modTime: func(p string) (time.Time, bool) {
			fi, err := os.Stat(p)
			if err != nil {
				return time.Time{}, false
			}
			return fi.ModTime(), true
		},
		readTail: readTail,
		now:      time.Now,
	}
}

func readTail(path string, limit int64) ([]byte, bool) {
	f, err := os.Open(path) //nolint:gosec // G304: solo se llama con las rutas constantes de este archivo
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }() // archivo de solo lectura
	fi, err := f.Stat()
	if err != nil {
		return nil, false
	}
	if fi.Size() > limit {
		if _, err := f.Seek(fi.Size()-limit, io.SeekStart); err != nil {
			return nil, false
		}
	}
	b, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, false
	}
	return b, true
}

// systemUpdates devuelve la lectura real de parches del SO; nil si este SO no esta soportado.
func systemUpdates(cfg UpdatesConfig) func(context.Context) (Updates, error) {
	e := realUpdEnv(cfg)
	switch e.goos {
	case "windows", "linux", "darwin":
		return e.collect
	}
	return nil
}

func (e updEnv) collect(ctx context.Context) (Updates, error) {
	switch e.goos {
	case "windows":
		return e.windows(ctx)
	case "linux":
		return e.linux(ctx)
	case "darwin":
		return e.darwin(ctx)
	}
	return Updates{}, fmt.Errorf("sistema %s sin soporte de parches", e.goos)
}

func ptr[T any](v T) *T { return &v }

func linuxEnv() []string { return []string{"PATH=" + linuxPath, "LC_ALL=C", "LANG=C"} }

func (e updEnv) age(paths ...string) *int64 {
	for _, p := range paths {
		if t, ok := e.modTime(p); ok {
			if s := int64(e.now().Sub(t).Seconds()); s >= 0 {
				return &s
			}
		}
	}
	return nil
}

// ---- Windows (Windows Update Agent via PowerShell con script FIJO) ----

const (
	psRegAU  = `HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update`
	psRegCBS = `HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`
	// GUID de las categorias Security Updates y Critical Updates (no dependen del idioma).
	psCatSecurity = `0FA1201D-4330-4FA8-8AE9-B877473B6441`
	psCatCritical = `E6CF1350-C01B-414D-A61F-263D14D133B4`
)

// windowsScript es el script constante; solo cambia el modo, que sale de un enum cerrado.
// Sin comillas dobles (van en un argumento de linea de comandos). Solo lee: busca (sin
// descargar ni instalar), consulta historial, registro y SystemInfo.
func windowsScript(mode string) string {
	if mode != WindowsOnlineNever && mode != WindowsOnlineAlways {
		mode = WindowsOnlineAuto
	}
	return strings.Join([]string{
		`$ErrorActionPreference='Stop'`,
		`$mode='` + mode + `'`,
		`$o=[ordered]@{total=$null;security=$null;kbs=$null;reboot=$null;last=$null;cache_age_s=$null}`,
		`$online=($mode -eq 'always')`,
		// LastSearchSuccessDate viene en UTC aunque PowerShell lo marque como hora local (medido).
		`try{$t=[DateTime]::SpecifyKind((New-Object -ComObject Microsoft.Update.AutoUpdate).Results.LastSearchSuccessDate,'Utc');` +
			`$age=[int64]([DateTime]::UtcNow-$t).TotalSeconds;if($age -ge 0 -and $age -lt 315360000){$o.cache_age_s=$age;` +
			`if($mode -eq 'auto' -and $age -gt 86400){$online=$true}}}catch{}`,
		`try{$q=(New-Object -ComObject Microsoft.Update.Session).CreateUpdateSearcher()}catch{$q=$null}`,
		`$f='IsInstalled=0 and IsHidden=0 and Type=''Software'''`,
		`$r=$null`,
		`if($q){try{$q.Online=$online;$r=$q.Search($f)}catch{if($online){try{$q.Online=$false;$r=$q.Search($f)}catch{$r=$null}}}}`,
		`if($r){try{$n=0;$s=0;$kb=@();foreach($u in $r.Updates){$n++;$sec=$false;` +
			`foreach($c in $u.Categories){if($c.CategoryID -eq '` + psCatSecurity + `' -or $c.CategoryID -eq '` + psCatCritical + `'){$sec=$true}};` +
			`if($u.MsrcSeverity){$sec=$true};if($sec){$s++};foreach($id in $u.KBArticleIDs){$kb+=('KB'+$id)}};` +
			`$o.total=$n;$o.security=$s;$o.kbs=(($kb|Select-Object -First 200) -join ',')}catch{}}`,
		`if($q){try{$m=$null;foreach($e in $q.QueryHistory(0,200)){if($e.Operation -eq 1 -and $e.ResultCode -eq 2){$d=$e.Date.ToUniversalTime();if($m -eq $null -or $d -gt $m){$m=$d}}};` +
			`if($m -ne $null){$o.last=$m.ToString('yyyy-MM-dd',[Globalization.CultureInfo]::InvariantCulture)}}catch{}}`,
		`try{$rb=[bool](New-Object -ComObject Microsoft.Update.SystemInfo).RebootRequired;` +
			`if(Test-Path '` + psRegAU + `\RebootRequired'){$rb=$true};if(Test-Path '` + psRegCBS + `'){$rb=$true};$o.reboot=$rb}catch{}`,
		`$o|ConvertTo-Json -Compress`,
	}, ";")
}

// windowsEnv es el entorno minimo del hijo. Sin ProgramData, Windows Update crea un directorio
// literal "%ProgramData%" en el directorio actual (medido); por eso se pasa cuando existe.
func windowsEnv(root, programData string) []string {
	env := []string{"SystemRoot=" + root, "windir=" + root}
	if programData != "" {
		env = append(env, "ProgramData="+programData)
	}
	return env
}

func (e updEnv) windows(ctx context.Context) (Updates, error) {
	root := e.getenv("SystemRoot")
	if root == "" {
		return Updates{}, errors.New("SystemRoot no esta definida: no se localiza PowerShell")
	}
	res, err := e.run(ctx, cmdSpec{
		path: root + `\System32\WindowsPowerShell\v1.0\powershell`,
		args: []string{"-NoProfile", "-NonInteractive", "-NoLogo", "-Command", windowsScript(e.windowsOnline)},
		env:  windowsEnv(root, e.getenv("ProgramData")),
		// El proceso auxiliar mide ~4-9 s (medido); 90 s es el tope duro.
		timeout: windowsTimeout,
		maxOut:  256 << 10,
	})
	if err != nil {
		return Updates{}, err
	}
	if res.code != 0 || res.truncated {
		return Updates{}, fmt.Errorf("el script de Windows Update salio con codigo %d (salida truncada: %t)", res.code, res.truncated)
	}
	return parseWUA(res.out)
}

// ---- Linux ----

func (e updEnv) linux(ctx context.Context) (Updates, error) {
	switch {
	case e.exists(aptGetPath):
		return e.apt(ctx)
	case e.exists(dnfPath):
		return e.rpmFamily(ctx, dnfPath, "dnf", dnfSecArgs)
	case e.exists(yumPath):
		return e.rpmFamily(ctx, yumPath, "yum", yumSecArgs)
	case e.exists(zypperPath):
		return e.zypper(ctx)
	}
	return Updates{Source: "none"}, nil // sin gestor conocido: todo desconocido
}

// Argumentos fijos de los gestores RPM. Son la fuente unica: el modulo los ejecuta y
// PrivilegedCommands genera de ellos la lista del sudoers (deben coincidir exactamente).
var (
	dnfSecArgs  = []string{"updateinfo", "list", "--security"}
	yumSecArgs  = []string{"updateinfo", "list", "security"}
	rpmBaseArgs = []string{"-q", "--cacheonly"}
	zypperBase  = []string{"--non-interactive", "--no-refresh", "-x"}
)

func rpmCheckArgs() []string { return slices.Concat(rpmBaseArgs, []string{"check-update"}) }
func rpmSecurityArgs(secArgs []string) []string {
	return slices.Concat(rpmBaseArgs, secArgs)
}
func zypperUpdatesArgs() []string { return slices.Concat(zypperBase, []string{"list-updates"}) }
func zypperPatchesArgs() []string {
	return slices.Concat(zypperBase, []string{"list-patches", "--category", "security"})
}

// PrivilegedCommands son los unicos comandos (ruta absoluta + argv exacto) que el modulo
// ejecuta con `sudo -n --` en modo privilegiado. El instalador los vuelca al sudoers.
func PrivilegedCommands() [][]string {
	return [][]string{
		slices.Concat([]string{dnfPath}, rpmCheckArgs()),
		slices.Concat([]string{dnfPath}, rpmSecurityArgs(dnfSecArgs)),
		slices.Concat([]string{yumPath}, rpmCheckArgs()),
		slices.Concat([]string{yumPath}, rpmSecurityArgs(yumSecArgs)),
		slices.Concat([]string{zypperPath}, zypperUpdatesArgs()),
		slices.Concat([]string{zypperPath}, zypperPatchesArgs()),
	}
}

func isPrivilegedCommand(path string, args []string) bool {
	argv := slices.Concat([]string{path}, args)
	return slices.ContainsFunc(PrivilegedCommands(), func(c []string) bool { return slices.Equal(c, argv) })
}

func (e updEnv) runLinux(ctx context.Context, path string, args ...string) (cmdResult, error) {
	if e.sudo && isPrivilegedCommand(path, args) {
		args = slices.Concat([]string{"-n", "--", path}, args)
		path = sudoPath
	}
	return e.run(ctx, cmdSpec{path: path, args: args, env: linuxEnv(), timeout: linuxTimeout, maxOut: maxCmdOut})
}

func (e updEnv) apt(ctx context.Context) (Updates, error) {
	u := Updates{Source: "apt", RebootRequired: ptr(e.exists(rebootRequiredFile)), CacheAgeS: e.age(aptStampFile, aptListsDir)}
	// -s = simulacion: no escribe ni exige root. NoLocking evita el error de lock sin root.
	res, err := e.runLinux(ctx, aptGetPath, "-s", "-q", "-o", "Debug::NoLocking=1", "upgrade")
	if err != nil && ctx.Err() != nil {
		return Updates{}, err
	}
	if err == nil && res.code == 0 && !res.truncated {
		total, sec := parseAptSim(res.out)
		u.PendingTotal, u.PendingSecurity = &total, &sec
	}
	if b, ok := e.readTail(aptHistoryFile, maxHistoryRead); ok {
		if d := parseAptHistory(b); d != "" {
			u.LastInstalled = &d
		}
	}
	return u, nil
}

func (e updEnv) rpmFamily(ctx context.Context, mgr, source string, secArgs []string) (Updates, error) {
	u := Updates{Source: source}
	res, err := e.runLinux(ctx, mgr, rpmCheckArgs()...)
	if err != nil && ctx.Err() != nil {
		return Updates{}, err
	}
	// check-update: 100 = hay actualizaciones, 0 = ninguna, otro = error (p. ej. sin cache).
	if err == nil && !res.truncated && (res.code == 0 || res.code == 100) {
		n := 0
		if res.code == 100 {
			n = parseDnfCheck(res.out)
		}
		u.PendingTotal = &n
		sres, serr := e.runLinux(ctx, mgr, rpmSecurityArgs(secArgs)...)
		if serr == nil && sres.code == 0 && !sres.truncated {
			pk, ids := parseDnfSecurity(sres.out)
			u.PendingSecurity, u.Advisories = &pk, ids
		}
	}
	if e.exists(needsRestart) {
		if r, err := e.runLinux(ctx, needsRestart, "-r"); err == nil && !r.truncated && (r.code == 0 || r.code == 1) {
			u.RebootRequired = ptr(r.code == 1)
		}
	}
	e.rpmLast(ctx, &u)
	return u, nil
}

func (e updEnv) rpmLast(ctx context.Context, u *Updates) {
	if !e.exists(rpmPath) {
		return
	}
	r, err := e.runLinux(ctx, rpmPath, "-qa", "--qf", `%{INSTALLTIME}\n`)
	if err != nil || r.code != 0 || r.truncated {
		return
	}
	if d := parseRPMInstallTimes(r.out); d != "" {
		u.LastInstalled = &d
	}
}

func (e updEnv) zypper(ctx context.Context) (Updates, error) {
	u := Updates{Source: "zypper"}
	if res, err := e.runLinux(ctx, zypperPath, zypperUpdatesArgs()...); err == nil && res.code == 0 && !res.truncated {
		if n, _, perr := parseZypperXML(res.out, "package"); perr == nil {
			u.PendingTotal = &n
		}
	}
	if res, err := e.runLinux(ctx, zypperPath, zypperPatchesArgs()...); err == nil && res.code == 0 && !res.truncated {
		if n, names, perr := parseZypperXML(res.out, "patch"); perr == nil {
			u.PendingSecurity, u.Advisories = &n, names
		}
	}
	if ctx.Err() != nil {
		return Updates{}, fmt.Errorf("zypper cancelado: %w", ctx.Err())
	}
	e.rpmLast(ctx, &u)
	return u, nil
}

// ---- macOS ----

func (e updEnv) darwin(ctx context.Context) (Updates, error) {
	res, err := e.run(ctx, cmdSpec{
		path: softwareUpdate, args: []string{"-l"},
		env:     []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "LANG=C"},
		timeout: macTimeout, maxOut: 256 << 10,
	})
	if err != nil {
		return Updates{}, err
	}
	total, restart, known := parseSoftwareUpdate(res.out)
	if res.code != 0 || res.truncated || !known {
		return Updates{}, fmt.Errorf("softwareupdate no dio un listado reconocible (codigo %d)", res.code)
	}
	// macOS no distingue seguridad, ids ni ultima instalacion sin heuristicas: quedan desconocidos.
	return Updates{Source: "softwareupdate", PendingTotal: &total, RebootRequired: &restart}, nil
}
