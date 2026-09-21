import { Button } from '@base-ui/react/button'
import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import styles from './Shell.module.css'

interface ShellProps {
  onLogout: () => Promise<void>
}

interface HostsResponse {
  hosts: { id: string; name: string }[]
}

/**
 * The authenticated frame: a command bar and an empty body. The dashboard sections arrive in the
 * next change; for now the body proves the API round trip by counting registered hosts.
 */
export function Shell({ onLogout }: ShellProps) {
  const [hostCount, setHostCount] = useState<number | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    api<HostsResponse>('/api/hosts', { signal: controller.signal }).then(
      (data) => setHostCount(data.hosts.length),
      () => setHostCount(null),
    )
    return () => controller.abort()
  }, [])

  return (
    <div className={styles.shell}>
      <header className={styles.bar}>
        <span className={styles.brand}>
          <span className={styles.prompt} aria-hidden="true">
            ${' '}
          </span>
          smotryashchiy
        </span>
        <Button className={styles.action} onClick={() => void onLogout()}>
          logout
        </Button>
      </header>
      <main className={styles.body}>
        <p className={styles.muted}>
          {hostCount === null ? 'loading hosts…' : `${hostCount} ${hostCount === 1 ? 'host' : 'hosts'} registered`}
        </p>
      </main>
    </div>
  )
}
