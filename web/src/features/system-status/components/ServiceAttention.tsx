import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { useSystemStatus } from '../hooks/useSystemStatus'

/** Línea `xs` bajo el título del Resumen (solo admin): visible únicamente con el servicio en warn/crit. */
export function ServiceAttention() {
  const { data } = useSystemStatus()
  if (!data || data.status === 'ok') return null
  return (
    <p className="-mt-1 mb-3 flex items-center gap-2 text-xs">
      Servicio: atención requerida.
      <Button asChild variant="outline" size="sm">
        <Link to="/sistema">Ver estado</Link>
      </Button>
    </p>
  )
}
