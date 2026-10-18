import type { ReactNode } from 'react'
import { BackLink } from '@/components/ui/back-link'
import { Card } from '@/components/ui/card'
import { FormHelp, type HelpItem } from '@/components/ui/form-help'
import { PageHeader } from '@/components/ui/page-header'

interface FormPageProps {
  title: string
  /** Lista a la que se vuelve (enlace arriba y destino de Cancelar/Guardar). */
  back: { to: string; label: string }
  /** Ayuda del formulario (icono junto al titulo); reemplaza los textos bajo los campos. */
  help?: readonly HelpItem[]
  children: ReactNode
}

/** Pagina unica de formulario largo (antes modales): volver + titulo + formulario a todo el ancho (grilla de 2-4 columnas en pantallas anchas). */
export function FormPage({ title, back, help, children }: FormPageProps) {
  return (
    <div className="flex flex-col gap-2">
      <BackLink to={back.to}>{back.label}</BackLink>
      <PageHeader title={title} help={help && <FormHelp title={title} items={help} />} />
      <Card className="px-3">{children}</Card>
    </div>
  )
}
