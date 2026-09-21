import { Button } from '@base-ui/react/button'
import { Login } from './components/Login/Login'
import { Shell } from './components/Shell/Shell'
import { useSession } from './session/useSession'

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
      return <Shell onLogout={session.logout} />
  }
}
