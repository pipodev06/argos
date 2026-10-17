import { ArrowLeftIcon } from 'lucide-react'
import { Link } from 'react-router'

/** Enlace unico de vuelta (a una lista o al detalle): flecha + texto muted. */
export function BackLink({ children, to }: { children: string; to: string }) {
  return (
    <Link
      to={to}
      className="inline-flex w-fit items-center gap-1 rounded-md text-sm text-text-muted outline-none hover:text-text hover:underline focus-visible:ring-2 focus-visible:ring-ring"
    >
      <ArrowLeftIcon className="size-4" strokeWidth={1.5} aria-hidden />
      {children}
    </Link>
  )
}
