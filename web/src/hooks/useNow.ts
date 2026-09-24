import { useEffect, useState } from 'react'

/** Instante actual (ms) que se refresca cada `intervalMs`: para textos relativos ("hace 12 s"). */
export function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(id)
  }, [intervalMs])
  return now
}
