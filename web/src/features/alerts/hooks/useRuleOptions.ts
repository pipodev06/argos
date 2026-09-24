import type { FilterOption } from '@/components/filters/types'
import { usePolledFetch } from '@/hooks/usePolledFetch'
import { listRules } from '../services/alerts.service'

const options = { intervalMs: 300_000 }

/** Opciones del filtro "Regla". Montar solo donde se usa. */
export function useRuleOptions(): readonly FilterOption[] {
  const { data } = usePolledFetch(listRules, options)
  return (data ?? []).map((r) => ({ id: r.id, name: r.name }))
}
