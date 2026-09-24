import { Button } from '@/components/ui/button'
import { InlineError } from '@/components/ui/inline-error'
import { useAuth } from '../hooks/useAuth'
import { useLogout } from '../hooks/useLogout'
import { AuthCard } from './AuthCard'
import { ChangePasswordForm } from './ChangePasswordForm'

// must_change_password: el servidor responde 403 al resto de rutas, asi que solo se ofrece esto.
export function ForcedPasswordPage() {
  const { user } = useAuth()
  const { error, logout } = useLogout()

  return (
    <AuthCard
      title="Cambia tu contraseña"
      description={`Hola, ${user?.username ?? ''}. Debes definir una contraseña nueva antes de continuar.`}
    >
      <ChangePasswordForm
        actions={
          <Button type="button" variant="ghost" onClick={() => void logout()}>
            Cerrar sesión
          </Button>
        }
      />
      {error && <InlineError>{error}</InlineError>}
    </AuthCard>
  )
}
