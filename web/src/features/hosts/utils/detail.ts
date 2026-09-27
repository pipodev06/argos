import type { HostDisk, PhysicalDisk } from '../types/host.types'
import { formatCapacity } from './format'

function isSystemMount(mount: string): boolean {
  return mount === '/' || /^c:/i.test(mount)
}

/** Disco del sistema primero, luego alfabetico. */
export function sortDisks(disks: readonly HostDisk[]): HostDisk[] {
  return [...disks].sort((a, b) => {
    const sys = Number(isSystemMount(b.mount)) - Number(isSystemMount(a.mount))
    return sys !== 0 ? sys : a.mount.localeCompare(b.mount, 'es')
  })
}

export function diskPct(disk: HostDisk): number {
  return disk.total_mb > 0 ? (disk.used_mb / disk.total_mb) * 100 : 0
}

const BUS_LABELS: Record<string, string> = { nvme: 'NVMe', sata: 'SATA', scsi: 'SCSI', usb: 'USB', virtual: 'Virtual', other: 'Otro' }

/** "Disco 1" para ids numericos (Windows) y el nombre del dispositivo tal cual (Linux: nvme0n1). */
export function physicalDiskLabel(disk: Pick<PhysicalDisk, 'id'>): string {
  return /^\d+$/.test(disk.id) ? `Disco ${disk.id}` : disk.id
}

/** Detalle corto de un disco fisico: modelo y bus (lo que se sepa). */
export function physicalDiskDetail(disk: Pick<PhysicalDisk, 'model' | 'bus'>): string {
  return [disk.model, disk.bus ? BUS_LABELS[disk.bus] : null].filter(Boolean).join(' · ')
}

/** Resumen para la columna "Disco" de la tabla: "N discos · X TB" (capacidad conocida) o solo la cuenta. */
export function physicalDisksSummary(physical: readonly PhysicalDisk[] | null): string {
  if (!physical || physical.length === 0) return '—'
  const known = physical.filter((d) => d.size_mb !== null)
  const totalMb = known.reduce((sum, d) => sum + (d.size_mb ?? 0), 0)
  const count = `${physical.length} ${physical.length === 1 ? 'disco' : 'discos'}`
  return known.length === physical.length ? `${count} · ${formatCapacity(totalMb)}` : count
}

export interface DiskGroup {
  disk: PhysicalDisk
  volumes: HostDisk[]
}

/**
 * Agrupa los volumenes por disco fisico (cruce exacto por `mount`). Los que ningun disco reclama quedan en
 * `loose` (red, LVM sin resolver...). Con `physical` nulo o vacio no hay grupos: todo es `loose`.
 */
export function groupDisks(disks: readonly HostDisk[], physical: readonly PhysicalDisk[] | null): { groups: DiskGroup[]; loose: HostDisk[] } {
  if (!physical || physical.length === 0) return { groups: [], loose: sortDisks(disks) }
  const claimed = new Set<string>()
  const groups = physical.map((disk) => {
    const volumes = sortDisks(disks.filter((d) => disk.mounts.includes(d.mount)))
    for (const v of volumes) claimed.add(v.mount)
    return { disk, volumes }
  })
  return { groups, loose: sortDisks(disks.filter((d) => !claimed.has(d.mount))) }
}
