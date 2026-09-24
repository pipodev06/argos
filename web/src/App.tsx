import { AuthGate } from '@/features/auth/components/AuthGate'
import { AuthProvider } from '@/features/auth/components/AuthProvider'
import { DashboardPage } from '@/pages/DashboardPage'

function App() {
  return (
    <AuthProvider>
      <AuthGate>
        <DashboardPage />
      </AuthGate>
    </AuthProvider>
  )
}

export default App
