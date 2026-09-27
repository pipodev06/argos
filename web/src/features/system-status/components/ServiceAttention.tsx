import { Link } from 'react-router'
import { useSystemStatus } from '../hooks/useSystemStatus'

/** Línea `xs` bajo el título del Resumen (solo admin): visible únicamente con el servicio en warn/crit. */
export function ServiceAttention() {
  const { data } = useSystemStatus()
  if (!data || data.status === 'ok') return null
  return (
    <p className="-mt-2 mb-4 text-xs">
      Servicio: atención requerida.{' '}
      <Link to="/configuracion" className="text-brand underline">Ver estado del servicio</Link>
    </p>
  )
}
