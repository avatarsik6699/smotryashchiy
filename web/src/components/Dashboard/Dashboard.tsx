import { Button } from '@base-ui/react/button'
import { useMemo, useState } from 'react'
import { DashboardProvider, useDashboard, useStore } from '../../data/DashboardContext'
import type { DashboardStore } from '../../data/store'
import { hostView } from '../../data/model'
import { hostState } from '../../domain/freshness'
import { useNow } from '../../hooks/useNow'
import { AddHostDialog } from './AddHostDialog'
import { CommandBar } from './CommandBar'
import { EventsSection } from './EventsSection'
import { HostsSection } from './HostsSection'
import { StatusStrip, type HostEntry } from './StatusStrip'
import styles from './Dashboard.module.css'

interface DashboardProps {
  onLogout: () => Promise<void>
  /** Tests inject a store; production creates its own and connects the live stream. */
  store?: DashboardStore
}

/** The whole product on one page: status, hosts, events. No navigation, no filters. */
export function Dashboard({ onLogout, store }: DashboardProps) {
  return (
    <DashboardProvider store={store}>
      <DashboardView onLogout={onLogout} />
    </DashboardProvider>
  )
}

function DashboardView({ onLogout }: { onLogout: () => Promise<void> }) {
  const state = useDashboard()
  const store = useStore()
  const now = useNow(5000)
  const [adding, setAdding] = useState(false)

  const entries: HostEntry[] = useMemo(() => state.hosts.map((rec) => ({ view: hostView(rec, now), state: hostState(rec.lastSeenMs, now) })), [state.hosts, now])
  const hostNames = useMemo(() => new Map(state.hosts.map((h) => [h.host.id, h.host.name])), [state.hosts])

  return (
    <div className={styles.page}>
      <CommandBar connection={state.connection} onAddHost={() => setAdding(true)} onLogout={() => void onLogout()} />
      <main className={styles.main} aria-busy={state.status === 'loading'}>
        {state.status === 'loading' && (
          <div className={styles.skeleton} role="status">
            <span className="visually-hidden">Loading hosts and events…</span>
            <div className={styles.skeletonRow} aria-hidden="true" />
            <div className={styles.skeletonRow} aria-hidden="true" />
            <div className={styles.skeletonRow} aria-hidden="true" />
          </div>
        )}
        {state.status === 'error' && (
          <section className={styles.section} role="alert">
            <p className={styles.notice}>
              <span className={styles.noticeStrong}>Could not load the dashboard.</span> {state.error}
            </p>
            <p>
              <Button className={styles.button} onClick={() => void store.load()}>
                retry
              </Button>
            </p>
          </section>
        )}
        {state.status === 'ready' && (
          <>
            {state.error && (
              <p className={styles.notice} role="alert">
                <span className={styles.noticeStrong}>Data may be out of date.</span> The last refresh failed ({state.error}).{' '}
                <Button className={styles.button} onClick={() => void store.load()}>
                  retry
                </Button>
              </p>
            )}
            {state.connection === 'offline' && (
              <p className={styles.notice} role="status">
                <span className={styles.noticeStrong}>Live updates are offline.</span> Showing the last refresh; reconnecting in the background.
              </p>
            )}
            <StatusStrip entries={entries} />
            <HostsSection entries={entries} records={state.hosts} now={now} onAddHost={() => setAdding(true)} />
            <EventsSection events={state.events} hostNames={hostNames} />
          </>
        )}
      </main>
      <AddHostDialog open={adding} onOpenChange={setAdding} />
    </div>
  )
}
