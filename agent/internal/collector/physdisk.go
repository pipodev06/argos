package collector

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"argos/agent/internal/sender"
)

// Topes del contrato host_info.physical_disks (los valida el servidor).
const (
	maxPhysDisks    = 16
	maxPhysMounts   = 32
	maxPhysIDLen    = 32
	maxPhysModelLen = 64
	maxPhysMountLen = 64
	maxPhysSizeMB   = 1 << 40
)

// diskMeta son los datos opcionales de un disco fisico; vacio/cero = no se pudo leer.
type diskMeta struct {
	Model  string
	Bus    string // nvme|sata|scsi|usb|virtual|other o vacio
	SizeMB int64
}

// groupPhysicalDisks agrupa los montajes por disco fisico. diskOf devuelve el id del
// disco de un montaje (false = no asociable: queda fuera de todo disco); meta lee los
// datos del disco. Salida determinista y acotada segun el contrato.
func groupPhysicalDisks(mounts []string, diskOf func(mount string) (string, bool), meta func(id string) diskMeta) []sender.PhysicalDisk {
	byID := map[string][]string{}
	var ids []string
	for _, m := range mounts {
		if m == "" || len(m) > maxPhysMountLen || !utf8.ValidString(m) {
			continue
		}
		id, ok := diskOf(m)
		if !ok || id == "" || len(id) > maxPhysIDLen || !utf8.ValidString(id) {
			continue
		}
		if _, seen := byID[id]; !seen {
			ids = append(ids, id)
		}
		if len(byID[id]) < maxPhysMounts && !slices.Contains(byID[id], m) {
			byID[id] = append(byID[id], m)
		}
	}
	// "2" antes que "10": primero el largo, luego alfabetico.
	slices.SortFunc(ids, func(a, b string) int {
		if len(a) != len(b) {
			return len(a) - len(b)
		}
		return strings.Compare(a, b)
	})
	if len(ids) > maxPhysDisks {
		ids = ids[:maxPhysDisks]
	}
	out := make([]sender.PhysicalDisk, 0, len(ids))
	for _, id := range ids {
		md := meta(id)
		d := sender.PhysicalDisk{ID: id, Model: cleanModel(md.Model), Mounts: byID[id]}
		switch md.Bus {
		case "nvme", "sata", "scsi", "usb", "virtual", "other":
			d.Bus = md.Bus
		}
		if md.SizeMB >= 1 && md.SizeMB <= maxPhysSizeMB {
			d.SizeMB = md.SizeMB
		}
		out = append(out, d)
	}
	return out
}

// cleanModel quita caracteres de control, junta espacios y trunca a maxPhysModelLen bytes
// sin partir un caracter.
func cleanModel(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxPhysModelLen {
		cut := maxPhysModelLen
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = strings.TrimSpace(s[:cut])
	}
	return s
}

// physCache guarda la topologia de discos: se relee cada staticInfoTTL o cuando cambia
// el conjunto de montajes/dispositivos.
type physCache struct {
	disks []sender.PhysicalDisk
	key   string
	at    time.Time
	done  bool
}

// physicalDisks devuelve los discos fisicos de los montajes que reporta Disks (nil =
// desconocido). Cachea por hora; un cambio de montajes fuerza la relectura.
func (s *System) physicalDisks(ctx context.Context) []sender.PhysicalDisk {
	parts, err := s.partitions(ctx)
	if err != nil && len(parts) == 0 {
		return s.phys.disks // Disks ya avisa del fallo; se conserva lo ultimo conocido
	}
	sel := selectMounts(parts, s.keepMount)
	var key strings.Builder
	for _, p := range sel {
		key.WriteString(p.Mountpoint + "\x00" + p.Device + "\x00")
	}
	now := s.now()
	if s.phys.done && s.phys.key == key.String() && now.Sub(s.phys.at) < staticInfoTTL {
		return s.phys.disks
	}
	s.phys = physCache{disks: s.physDisks(ctx, sel), key: key.String(), at: now, done: true}
	return s.phys.disks
}
