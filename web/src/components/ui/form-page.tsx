import type { ReactNode } from 'react'
import { BackLink } from '@/components/ui/back-link'
import { Card } from '@/components/ui/card'
import { PageHeader } from '@/components/ui/page-header'

interface FormPageProps {
  title: string
  /** Lista a la que se vuelve (enlace arriba y destino de Cancelar/Guardar). */
  back: { to: string; label: string }
  children: ReactNode
}

/** Pagina unica de formulario largo (antes modales): volver + titulo + formulario en una `Card` de ancho acotado. */
export function FormPage({ title, back, children }: FormPageProps) {
  return (
    <div className="flex flex-col gap-2">
      <BackLink to={back.to}>{back.label}</BackLink>
      <PageHeader title={title} />
      <Card className="max-w-3xl px-4">{children}</Card>
    </div>
  )
}
