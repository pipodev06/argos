import { IconArrowLeft } from '@tabler/icons-react'
import { Link } from 'react-router'

// Enlace de vuelta a la lista; lo comparten el detalle, el id invalido y la pagina 404.
export function BackToHostsLink({ children = 'Hosts' }: { children?: string }) {
  return (
    <Link
      to="/hosts"
      className="inline-flex w-fit items-center gap-1 rounded-md text-sm text-text-muted outline-none hover:text-text hover:underline focus-visible:ring-2 focus-visible:ring-ring"
    >
      <IconArrowLeft className="size-4" stroke={1.5} aria-hidden />
      {children}
    </Link>
  )
}
