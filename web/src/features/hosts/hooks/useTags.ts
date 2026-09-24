import { usePolledFetch } from '@/hooks/usePolledFetch'
import { listTags } from '../services/hosts.service'
import type { TagCount } from '../types/host.types'

const options = { intervalMs: 300_000 }
const EMPTY: TagCount[] = []

/** Etiquetas existentes con su conteo (sugerencias y filtros). Montar solo donde se usa. */
export function useTags(): TagCount[] {
  const { data } = usePolledFetch(listTags, options)
  return data ?? EMPTY
}
