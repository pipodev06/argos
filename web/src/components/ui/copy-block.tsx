import { useState } from "react"
import { IconCheck, IconCopy } from "@tabler/icons-react"
import { Button } from "@/components/ui/button"
import { InlineError } from "@/components/ui/inline-error"
import { useCopyToClipboard } from "@/hooks/useCopyToClipboard"

interface CopyBlockProps {
  /** Texto a mostrar y copiar (token, comando). */
  value: string
  /** Nombre accesible de la region ("Token de enrolamiento"). */
  label: string
}

/**
 * Bloque de texto copiable (`<pre>` enfocable + boton "Copiar"). El boton cambia a "Copiado" 2 s y un
 * `role="status"` lo anuncia; sin toast. Si el navegador niega el portapapeles se avisa y el texto sigue seleccionable.
 */
function CopyBlock({ value, label }: CopyBlockProps) {
  const { copied, copy } = useCopyToClipboard()
  const [failed, setFailed] = useState(false)

  async function handleCopy() {
    setFailed(!(await copy(value)))
  }

  return (
    <div className="flex flex-col gap-2">
      {/* tabIndex=0: sin el, el scroll horizontal no se alcanza con teclado */}
      <pre
        role="region"
        aria-label={label}
        tabIndex={0}
        className="overflow-x-auto rounded-md border border-border bg-bg px-3 py-2 font-mono text-xs text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <code>{value}</code>
      </pre>
      <div className="flex items-center justify-end gap-2">
        <span role="status" className="sr-only">
          {copied ? "Copiado" : ""}
        </span>
        <Button type="button" variant="outline" size="sm" onClick={() => void handleCopy()}>
          {copied ? (
            <>
              <IconCheck stroke={1.5} aria-hidden />
              Copiado
            </>
          ) : (
            <>
              <IconCopy stroke={1.5} aria-hidden />
              Copiar
            </>
          )}
        </Button>
      </div>
      {failed && !copied && <InlineError>No se pudo copiar; selecciona el texto y cópialo a mano.</InlineError>}
    </div>
  )
}

export { CopyBlock }
