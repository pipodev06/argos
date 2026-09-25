import { describe, expect, it } from 'vitest'
import type { HostDisk, PhysicalDisk } from '../types/host.types'
import { groupDisks, physicalDiskDetail, physicalDiskLabel } from './detail'

const vol = (mount: string, used = 10, total = 100): HostDisk => ({ mount, used_mb: used, total_mb: total }) as HostDisk
const disk = (id: string, mounts: string[], model: string | null = null, bus: PhysicalDisk['bus'] = null): PhysicalDisk => ({ id, model, bus, size_mb: null, mounts })

describe('discos fisicos', () => {
  const volumes = [vol('C:'), vol('D:'), vol('E:'), vol('Z:')]
  it('agrupa por disco y deja sueltos los volumenes sin disco', () => {
    const { groups, loose } = groupDisks(volumes, [disk('0', ['C:']), disk('1', ['D:', 'E:'])])
    expect(groups.map((g) => g.volumes.map((v) => v.mount))).toEqual([['C:'], ['D:', 'E:']])
    expect(loose.map((v) => v.mount)).toEqual(['Z:'])
  })
  it('sin datos de discos fisicos (null o vacio) todo queda suelto', () => {
    expect(groupDisks(volumes, null).groups).toEqual([])
    expect(groupDisks(volumes, []).loose).toHaveLength(4)
  })
  it('un disco cuyos volumenes no llegaron sigue apareciendo', () => {
    expect(groupDisks([vol('C:')], [disk('1', ['D:'])]).groups[0].volumes).toEqual([])
  })
  it('textos: "Disco N" para ids numericos y modelo · bus', () => {
    expect(physicalDiskLabel({ id: '1' })).toBe('Disco 1')
    expect(physicalDiskLabel({ id: 'nvme0n1' })).toBe('nvme0n1')
    expect(physicalDiskDetail({ model: 'SAMSUNG X', bus: 'nvme' })).toBe('SAMSUNG X · NVMe')
    expect(physicalDiskDetail({ model: null, bus: null })).toBe('')
  })
})
