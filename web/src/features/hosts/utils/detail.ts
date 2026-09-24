import type { HostDisk } from '../types/host.types'

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
