import { Button } from '@base-ui/react/button'
import type { ConnectionState } from '../../data/model'
import styles from './CommandBar.module.css'
import shared from './Dashboard.module.css'

const CONNECTION_TEXT: Record<ConnectionState, string> = {
  connecting: 'connecting',
  live: 'live',
  reconnecting: 'reconnecting',
  offline: 'offline',
}

interface CommandBarProps {
  connection: ConnectionState
  onAddHost: () => void
  onLogout: () => void
}

/** Sticky top bar: where you are, whether data is live, and the two actions. No navigation. */
export function CommandBar({ connection, onAddHost, onLogout }: CommandBarProps) {
  return (
    <header className={styles.bar}>
      <span className={styles.brand}>
        <span className={styles.prompt} aria-hidden="true">
          ${' '}
        </span>
        smotryashchiy
      </span>
      <span className={styles.connection} role="status" data-connection={connection}>
        <span className={styles.pulse} aria-hidden="true" />
        {CONNECTION_TEXT[connection]}
      </span>
      <span className={styles.actions}>
        <Button className={shared.button} onClick={onAddHost}>
          + add host
        </Button>
        <Button className={shared.button} onClick={onLogout}>
          logout
        </Button>
      </span>
    </header>
  )
}
