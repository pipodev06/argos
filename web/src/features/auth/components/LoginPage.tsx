import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { FormField } from '@/components/ui/form-field'
import { InlineError } from '@/components/ui/inline-error'
import { useLogin } from '../hooks/useLogin'
import { AuthCard } from './AuthCard'

export function LoginPage() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const { error, submitting, submit } = useLogin()

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const ok = await submit({ username: username.trim(), password })
    if (!ok) setPassword('')
  }

  return (
    <AuthCard title="Iniciar sesión">
      <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-3">
        <FormField
          id="username"
          label="Usuario"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoComplete="username"
          autoCapitalize="none"
          spellCheck={false}
          autoFocus
          required
        />
        <FormField
          id="password"
          label="Contraseña"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          required
        />
        {error && <InlineError>{error}</InlineError>}
        <Button type="submit" disabled={submitting || !username.trim() || !password}>
          {submitting ? 'Ingresando...' : 'Iniciar sesión'}
        </Button>
      </form>
    </AuthCard>
  )
}
