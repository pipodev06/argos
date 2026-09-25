import type { FilterField, FiltersOf } from '@/components/filters/types'
import type { User } from '@/features/auth/types/auth.types'
import type { ChangeKind, ModuleStatus, ServiceStart, ServiceState } from '../types/inventory.types'

export const SECTIONS = ['ports', 'services', 'processes', 'changes'] as const
export type Section = (typeof SECTIONS)[number]
export const SECTION_LABELS: Record<Section, string> = {
  ports: 'Puertos',
  services: 'Servicios',
  processes: 'Procesos',
  changes: 'Cambios',
}

const MODULE_FILTERS = ['all', 'ports', 'services'] as const
const KIND_FILTERS = ['all', 'added', 'removed', 'changed', 'overflow'] as const
const STATE_FILTERS = ['all', 'running', 'stopped', 'paused', 'other'] as const

/** Estado en la URL: sub-seccion y filtros. Los valores por defecto se omiten de la URL. */
export const INVENTORY_FIELDS = [
  { kind: 'choice', key: 'section', param: 'inv', label: 'Sección', values: SECTIONS, default: 'ports' },
  { kind: 'choice', key: 'module', param: 'inv_module', label: 'Módulo', values: MODULE_FILTERS, default: 'all' },
  { kind: 'choice', key: 'kind', param: 'inv_kind', label: 'Tipo', values: KIND_FILTERS, default: 'all' },
  { kind: 'choice', key: 'state', param: 'inv_state', label: 'Estado', values: STATE_FILTERS, default: 'all' },
] as const satisfies readonly FilterField[]

export type InventoryFilters = FiltersOf<typeof INVENTORY_FIELDS>

export const MODULE_LABELS: Record<string, string> = { all: 'Todos', ports: 'Puertos', services: 'Servicios' }
export const KIND_LABELS: Record<string, string> = {
  all: 'Todos',
  added: 'Agregado',
  removed: 'Eliminado',
  changed: 'Modificado',
  overflow: 'Desbordamiento',
}
export const STATE_LABELS: Record<ServiceState | 'all', string> = {
  all: 'Todos',
  running: 'En ejecución',
  stopped: 'Detenido',
  paused: 'Pausado',
  other: 'Otro',
}
export const START_LABELS: Record<ServiceStart, string> = {
  auto: 'Automático',
  manual: 'Manual',
  disabled: 'Deshabilitado',
  other: 'Otro',
}

/** Acceso a inventario: admin o permiso explicito (el servidor lo vuelve a validar). */
export function canViewInventory(user: Pick<User, 'role' | 'can_inventory'> | null | undefined): boolean {
  return !!user && (user.role === 'admin' || user.can_inventory)
}

/** Expuesto = escucha en algo distinto de loopback (127.0.0.0/8, ::1). Un bind vacio o ilegible cuenta como expuesto. */
export function isExposed(bind: string): boolean {
  const b = bind.trim().toLowerCase().replace(/^\[|\]$/g, '')
  if (b === '::1' || b === 'localhost') return false
  const v4 = b.startsWith('::ffff:') ? b.slice(7) : b
  return !/^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(v4)
}

export type EmptyKind = 'disabled' | 'no_data' | 'empty' | null

/** Que vacio mostrar: `disabled`/`no_data` del servidor, o lista `ok` sin elementos. */
export function emptyKind(status: ModuleStatus, count: number): EmptyKind {
  if (status === 'disabled') return 'disabled'
  if (status === 'no_data') return 'no_data'
  return count === 0 ? 'empty' : null
}

export const EMPTY_TEXT: Record<Exclude<EmptyKind, null>, { title: string; description?: string }> = {
  disabled: {
    title: 'Módulo desactivado en este agente',
    description: 'Se activa con ARGOS_INVENTORY_MODULES en el agente.',
  },
  no_data: { title: 'Sin datos aún' },
  empty: { title: 'Sin elementos' },
}

const FIELD_LABELS: Record<string, string> = {
  proc: 'proceso',
  owner_known: 'dueño conocido',
  display: 'nombre',
  state: 'estado',
  start: 'inicio',
}

function valueText(field: string, value: unknown): string {
  if (typeof value === 'boolean') return value ? 'sí' : 'no'
  if (field === 'state' && typeof value === 'string') return (STATE_LABELS[value as ServiceState] ?? value).toLowerCase()
  if (field === 'start' && typeof value === 'string') return (START_LABELS[value as ServiceStart] ?? value).toLowerCase()
  if (value === null || value === undefined) return '—'
  return String(value)
}

/** `before`/`after` (JSON) como texto legible: "estado: en ejecución, inicio: automático". null -> "—". */
export function describeValue(value: unknown): string {
  if (value === null || value === undefined) return '—'
  if (typeof value !== 'object' || Array.isArray(value)) return String(value)
  const entries = Object.entries(value as Record<string, unknown>)
  if (entries.length === 0) return '—'
  return entries.map(([k, v]) => `${FIELD_LABELS[k] ?? k}: ${valueText(k, v)}`).join(', ')
}

/** Detalle de un cambio: en `overflow`, cuantos se omitieron; si no, antes -> despues. */
export function describeChange(kind: ChangeKind, before: unknown, after: unknown): string {
  if (kind === 'overflow') {
    const dropped = (after as { dropped?: unknown } | null)?.dropped
    return typeof dropped === 'number' ? `Se omitieron ${dropped} cambios` : 'Se omitieron cambios'
  }
  if (kind === 'added') return describeValue(after)
  if (kind === 'removed') return describeValue(before)
  return `${describeValue(before)} → ${describeValue(after)}`
}
