//go:build windows

package collector

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
	"golang.org/x/sys/windows"

	"argos/agent/internal/sender"
)

const (
	ioctlStorageGetDeviceNumber = 0x2D1080
	ioctlStorageQueryProperty   = 0x2D1400
	ioctlDiskGetDriveGeometryEx = 0x700A0

	fileDeviceDisk = 7
	descBufSize    = 1024
)

// readPhysicalDisks asocia cada unidad fija con su disco fisico via DeviceIoControl
// (acceso 0: no requiere administrador). Un dato que falla queda ausente.
func readPhysicalDisks(ctx context.Context, parts []disk.PartitionStat) []sender.PhysicalDisk {
	mounts := make([]string, 0, len(parts))
	for _, p := range parts {
		mounts = append(mounts, p.Mountpoint)
	}
	return groupPhysicalDisks(mounts, windowsDiskOf, windowsDiskMeta)
}

// openDevice abre un dispositivo; access 0 no exige administrador.
func openDevice(path string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("ruta invalida: %w", err)
	}
	h, err := windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("abriendo %s: %w", path, err)
	}
	return h, nil
}

func ioctl(h windows.Handle, code uint32, in, out []byte) (uint32, error) {
	var inP, outP *byte
	if len(in) > 0 {
		inP = &in[0]
	}
	if len(out) > 0 {
		outP = &out[0]
	}
	var n uint32
	if err := windows.DeviceIoControl(h, code, inP, uint32(len(in)), outP, uint32(len(out)), &n, nil); err != nil { //nolint:gosec // buffers de tamano fijo pequeno
		return 0, fmt.Errorf("deviceiocontrol %#x: %w", code, err)
	}
	return n, nil
}

// windowsDiskOf: mount "C:" -> numero de disco ("0", "1"...).
func windowsDiskOf(mount string) (string, bool) {
	letter := strings.TrimSuffix(mount, `\`)
	if len(letter) != 2 || letter[1] != ':' {
		return "", false
	}
	h, err := openDevice(`\\.\`+letter, 0)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h) //nolint:errcheck // solo lectura, nada que hacer si falla
	out := make([]byte, 12)      // STORAGE_DEVICE_NUMBER
	if n, err := ioctl(h, ioctlStorageGetDeviceNumber, nil, out); err != nil || n < 12 {
		return "", false
	}
	if binary.LittleEndian.Uint32(out[0:4]) != fileDeviceDisk {
		return "", false
	}
	return fmt.Sprint(binary.LittleEndian.Uint32(out[4:8])), true
}

// windowsDiskMeta lee modelo, bus y tamano de \\.\PhysicalDriveN; cada dato es independiente.
func windowsDiskMeta(id string) diskMeta {
	var md diskMeta
	h, err := openDevice(`\\.\PhysicalDrive`+id, 0)
	if err != nil {
		return md
	}
	defer windows.CloseHandle(h) //nolint:errcheck // solo lectura

	// STORAGE_PROPERTY_QUERY{PropertyId=StorageDeviceProperty(0), QueryType=PropertyStandardQuery(0)}
	desc := make([]byte, descBufSize)
	if n, err := ioctl(h, ioctlStorageQueryProperty, make([]byte, 12), desc); err == nil && n >= 32 {
		md.Model = strings.TrimSpace(descString(desc[:n], 12) + " " + descString(desc[:n], 16))
		md.Bus = windowsBus(binary.LittleEndian.Uint32(desc[28:32]))
	}
	// DISK_GEOMETRY_EX: DISK_GEOMETRY (24 bytes) y luego DiskSize int64 (FILE_ANY_ACCESS).
	geo := make([]byte, 128)
	if n, err := ioctl(h, ioctlDiskGetDriveGeometryEx, nil, geo); err == nil && n >= 32 {
		if b := binary.LittleEndian.Uint64(geo[24:32]); b>>20 <= maxPhysSizeMB {
			md.SizeMB = int64(b >> 20) //nolint:gosec // acotado arriba
		}
	}
	return md
}

// descString lee la cadena ASCII terminada en NUL cuyo offset esta en desc[at:at+4].
func descString(desc []byte, at int) string {
	off := int(binary.LittleEndian.Uint32(desc[at : at+4]))
	if off <= 0 || off >= len(desc) {
		return ""
	}
	end := off
	for end < len(desc) && desc[end] != 0 {
		end++
	}
	return string(desc[off:end])
}

func windowsBus(t uint32) string {
	switch t {
	case 17:
		return "nvme"
	case 3, 11: // ATA, SATA
		return "sata"
	case 1, 6, 8, 9, 10: // SCSI, FC, RAID, iSCSI, SAS
		return "scsi"
	case 7:
		return "usb"
	case 14, 15: // virtual, file-backed virtual
		return "virtual"
	}
	return "other"
}
