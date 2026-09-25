// Package inventory reune el inventario del host (puertos en escucha, servicios del SO y
// procesos agrupados) y lo envia al servidor. Todos los modulos estan APAGADOS por defecto:
// solo corren los que el operador lista en la config local (ARGOS_INVENTORY_MODULES); el
// servidor no puede activarlos.
//
// Solo se recolectan campos de una lista blanca (ver los tipos Port, Service y Process).
// Nunca se leen ni se envian la linea de comandos, el entorno, la ruta del ejecutable, el
// directorio de trabajo ni el usuario de un proceso.
package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Nombres de modulo (van tal cual en el protocolo y en la config).
const (
	ModulePorts     = "ports"
	ModuleServices  = "services"
	ModuleProcesses = "processes"
)

// Topes de cantidad por modulo: si se exceden se corta (tras ordenar, es determinista) y
// se marca truncated.
const (
	maxPorts     = 512
	maxServices  = 1024
	maxProcesses = 100

	maxNameLen    = 64
	maxDisplayLen = 128

	// maxBodyBytes es el limite del cuerpo del POST (el servidor responde 413 si se excede).
	maxBodyBytes = 256 << 10
	// itemsBudget deja margen pa el resto del sobre JSON (modulo, rev, fechas).
	itemsBudget = maxBodyBytes - 1<<10

	fieldSep = "\x1f" // los nombres no pueden traer caracteres de control: el separador es inequivoco
)

// Port es un puerto en escucha (TCP LISTEN o UDP sin conexion).
type Port struct {
	Proto      string `json:"proto"` // tcp | udp
	Bind       string `json:"bind"`  // IP de enlace
	Port       int    `json:"port"`
	PID        int32  `json:"pid,omitempty"`
	Proc       string `json:"proc"` // nombre base del proceso dueno; vacio si owner_known=false
	OwnerKnown bool   `json:"owner_known"`
}

// Service es un servicio del SO (Windows y Linux con systemd).
type Service struct {
	Name    string `json:"name"`
	Display string `json:"display"`
	State   string `json:"state"` // running | stopped | paused | other
	Start   string `json:"start"` // auto | manual | disabled | other
}

// Process agrupa los procesos que comparten nombre.
type Process struct {
	Name     string `json:"name"`
	Count    int    `json:"count"`
	RSSBytes uint64 `json:"rss_bytes"`
}

// snapshot es la foto de un modulo lista pa enviarse: items ya ordenados y acotados, y su rev.
type snapshot struct {
	rev       string
	items     any // slice de Port, Service o Process
	truncated bool
}

// ParseModules valida la lista de modulos de la config (separada por comas). Vacia = ninguno.
func ParseModules(raw string) ([]string, error) {
	var out []string
	for _, m := range strings.Split(raw, ",") {
		m = strings.ToLower(strings.TrimSpace(m))
		switch m {
		case "":
		case ModulePorts, ModuleServices, ModuleProcesses, ModuleUpdates:
			if !slices.Contains(out, m) {
				out = append(out, m)
			}
		default:
			return nil, fmt.Errorf("modulo desconocido %q (validos: %s, %s, %s, %s)", m, ModulePorts, ModuleServices, ModuleProcesses, ModuleUpdates)
		}
	}
	return out, nil
}

// clean deja un nombre en UTF-8 valido, sin caracteres de control, recortado y con maximo max runas.
func clean(s string, limit int) string {
	s = strings.ToValidUTF8(s, "?")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > limit {
		s = string([]rune(s)[:limit])
	}
	return s
}

// finish ordena items por su linea canonica, aplica el tope de cantidad y de bytes, y calcula el
// rev: SHA-256 del listado canonico ordenado (modulo + una linea por item). line define los
// campos que entran al hash (p. ej. sin pid ni RSS: cambian sin que cambie el inventario).
func finish[T any](module string, items []T, truncated bool, maxItems int, line func(T) string) (snapshot, error) {
	slices.SortFunc(items, func(a, b T) int { return strings.Compare(line(a), line(b)) })
	if len(items) > maxItems {
		items, truncated = items[:maxItems], true
	}
	for {
		b, err := json.Marshal(items)
		if err != nil {
			return snapshot{}, fmt.Errorf("serializando %s: %w", module, err)
		}
		if len(b) <= itemsBudget || len(items) == 0 {
			break
		}
		items, truncated = items[:len(items)*9/10], true
	}
	h := sha256.New()
	h.Write([]byte(module))
	for _, it := range items {
		h.Write([]byte("\n" + line(it)))
	}
	return snapshot{rev: hex.EncodeToString(h.Sum(nil)), items: items, truncated: truncated}, nil
}

func portLine(p Port) string {
	return strings.Join([]string{p.Proto, p.Bind, fmt.Sprintf("%05d", p.Port), p.Proc, fmt.Sprint(p.OwnerKnown)}, fieldSep)
}

func serviceLine(s Service) string {
	return strings.Join([]string{s.Name, s.Display, s.State, s.Start}, fieldSep)
}

func processLine(p Process) string {
	return strings.Join([]string{p.Name, fmt.Sprint(p.Count)}, fieldSep)
}
