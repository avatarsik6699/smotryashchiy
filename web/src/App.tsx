import { Button } from '@base-ui/react/button'
import { lazy, Suspense } from 'react'
import { Login } from './components/Login/Login'
import { useSession } from './session/useSession'

// Change 38: the sign-in screen does not download the dashboard.
const Dashboard = lazy(() => import('./components/Dashboard/Dashboard').then((m) => ({ default: m.Dashboard })))

export function App() {
  const session = useSession()

  switch (session.state) {
    case 'checking':
      return (
        <main style={{ padding: 'var(--space-4) var(--page-pad)' }}>
          <p role="status">checking session…</p>
        </main>
      )
    case 'unreachable':
      return (
        <main style={{ padding: 'var(--space-4) var(--page-pad)' }}>
          <p role="alert">Cannot reach the server.</p>
          <Button onClick={session.retry}>retry</Button>
        </main>
      )
    case 'anonymous':
      return <Login onLogin={session.login} />
    case 'authenticated':
      return (
        <Suspense
          fallback={
            <main style={{ padding: 'var(--space-4) var(--page-pad)' }}>
              <p role="status">loading the dashboard…</p>
            </main>
          }
        >
          <Dashboard onLogout={session.logout} />
        </Suspense>
      )
  }
}
