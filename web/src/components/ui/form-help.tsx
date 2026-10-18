import { useState } from 'react'
import { CircleHelpIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'

/** Un campo y cómo usarlo. */
export type HelpItem = readonly [field: string, text: string]

interface FormHelpProps {
  /** Nombre del formulario ("Regla", "Agregar host"). */
  title: string
  items: readonly HelpItem[]
}

/**
 * Ayuda de un formulario: icono junto al título que abre un modal con cada campo y su uso.
 * Reemplaza los textos de ayuda bajo los campos (ruido visual): el formulario muestra solo etiquetas.
 */
export function FormHelp({ title, items }: FormHelpProps) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button type="button" variant="ghost" size="icon-sm" aria-label={`Ayuda: ${title}`} title="Ayuda" onClick={() => setOpen(true)}>
        <CircleHelpIcon className="text-text-muted" strokeWidth={1.5} aria-hidden />
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Ayuda: {title}</DialogTitle>
          </DialogHeader>
          <dl className="grid gap-2">
            {items.map(([field, text]) => (
              <div key={field}>
                <dt className="font-medium">{field}</dt>
                <dd className="text-text-muted">{text}</dd>
              </div>
            ))}
          </dl>
        </DialogContent>
      </Dialog>
    </>
  )
}

