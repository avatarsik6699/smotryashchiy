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

export type View = 'monitoring' | 'analytics' | 'guide'

interface CommandBarProps {
  connection: ConnectionState
  view: View
  onViewChange: (view: View) => void
  onAddHost: () => void
  onAddSite: () => void
  onLogout: () => void
}

/**
 * Sticky top bar: where you are, whether data is live, the Monitoring/Analytics/Guide tabs (the only
 * navigation the UI has, docs/SPEC.md §5, Changes 14-15 and 22), and one add-action per data view.
 */
export function CommandBar({ connection, view, onViewChange, onAddHost, onAddSite, onLogout }: CommandBarProps) {
  return (
    <header className={styles.bar}>
      <span className={styles.brand}>
        <span className={styles.prompt} aria-hidden="true">
          ${' '}
        </span>
        smotryashchiy
      </span>
      <nav className={styles.tabs} aria-label="View">
        <button type="button" className={styles.tab} data-active={view === 'monitoring'} onClick={() => onViewChange('monitoring')}>
          monitoring
        </button>
        <button type="button" className={styles.tab} data-active={view === 'analytics'} onClick={() => onViewChange('analytics')}>
          analytics
        </button>
        <button type="button" className={styles.tab} data-active={view === 'guide'} onClick={() => onViewChange('guide')}>
          guide
        </button>
      </nav>
      <span className={styles.connection} role="status" data-connection={connection}>
        <span className={styles.pulse} aria-hidden="true" />
        {CONNECTION_TEXT[connection]}
      </span>
      <span className={styles.actions}>
        {view === 'monitoring' && (
          <Button className={shared.button} onClick={onAddHost}>
            + add host
          </Button>
        )}
        {view === 'analytics' && (
          <Button className={shared.button} onClick={onAddSite}>
            + add site
          </Button>
        )}
        <Button className={shared.button} onClick={onLogout}>
          logout
        </Button>
      </span>
    </header>
  )
}
