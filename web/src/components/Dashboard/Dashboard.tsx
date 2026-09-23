import { Button } from '@base-ui/react/button'
import { lazy, Suspense, useCallback, useMemo, useState } from 'react'
import { DashboardProvider, useDashboard, useStore } from '../../data/DashboardContext'
import type { DashboardStore } from '../../data/store'
import { fleetSummary } from '../../data/health'
import { LESSON_BY_ID, LESSONS, type LessonId } from '../../guide/lessons'
import { useSites } from '../../data/useSites'
import { hostView } from '../../data/model'
import { uptimeState } from '../../domain/uptime'
import { hostState } from '../../domain/freshness'
import { useNow } from '../../hooks/useNow'
import { AddSiteDialog } from '../Analytics/AddSiteDialog'
import { AnalyticsView } from '../Analytics/AnalyticsView'
import { AddHostDialog } from './AddHostDialog'
import { AddTargetDialog } from './AddTargetDialog'
import { CommandBar, type View } from './CommandBar'
import { EventsSection } from './EventsSection'
import { HostsSection } from './HostsSection'
import { UptimeSection } from './UptimeSection'
import { StatusStrip, type HostEntry } from './StatusStrip'
import { SummaryLine } from './SummaryLine'
import { GuideNavContext } from '../Guide/GuideNav'
import styles from './Dashboard.module.css'

// The guide is text the Monitoring view does not need: loaded on first use, outside the main bundle.
const GuideView = lazy(() => import('../Guide/GuideView'))

const LESSON_KEY = 'smotryashchiy.guide.lesson'

/** The last opened lesson is a per-browser convenience; storage may be unavailable (private mode). */
function storedLesson(): LessonId {
  try {
    const v = window.localStorage.getItem(LESSON_KEY)
    if (v && v in LESSON_BY_ID) return v as LessonId
  } catch {
    // ignore: start at the first lesson
  }
  return LESSONS[0]!.id
}

function storeLesson(id: LessonId) {
  try {
    window.localStorage.setItem(LESSON_KEY, id)
  } catch {
    // ignore: the guide still works, it just will not remember
  }
}

interface DashboardProps {
  onLogout: () => Promise<void>
  /** Tests inject a store; production creates its own and connects the live stream. */
  store?: DashboardStore
}

/** The whole product on three views: Monitoring (default), Analytics and the Guide (docs/SPEC.md §5). */
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
  const [view, setView] = useState<View>('monitoring')
  const [lesson, setLessonState] = useState<LessonId>(storedLesson)
  const setLesson = useCallback((id: LessonId) => {
    setLessonState(id)
    storeLesson(id)
  }, [])
  const openLesson = useCallback(
    (id: LessonId) => {
      setLesson(id)
      setView('guide')
      window.scrollTo?.({ top: 0 })
    },
    [setLesson],
  )
  const [adding, setAdding] = useState(false)
  const [addingTarget, setAddingTarget] = useState(false)
  const [addingSite, setAddingSite] = useState(false)
  const sites = useSites()

  const entries: HostEntry[] = useMemo(
    () =>
      state.hosts.map((rec) => ({
        view: hostView(rec, now),
        state: hostState(rec.lastSeenMs, now),
      })),
    [state.hosts, now],
  )
  const probes = useMemo(() => {
    if (state.targets.length === 0) return null
    const states = state.targets.map((t) => uptimeState(t.last, t.target.interval_seconds, now))
    return {
      up: states.filter((s) => s === 'up').length,
      down: states.filter((s) => s === 'down').length,
    }
  }, [state.targets, now])
  const hostNames = useMemo(() => new Map(state.hosts.map((h) => [h.host.id, h.host.name])), [state.hosts])
  const summary = useMemo(() => fleetSummary(state, now), [state, now])

  return (
    <GuideNavContext.Provider value={openLesson}>
    <div className={styles.page}>
      <CommandBar connection={state.connection} view={view} onViewChange={setView} onAddHost={() => setAdding(true)} onAddSite={() => setAddingSite(true)} onLogout={() => void onLogout()} />
      <main className={styles.main} aria-busy={state.status === 'loading'}>
        {view === 'monitoring' ? (
          <>
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
                <SummaryLine summary={summary} />
                <StatusStrip entries={entries} probes={probes} />
                <HostsSection entries={entries} records={state.hosts} now={now} onAddHost={() => setAdding(true)} />
                <UptimeSection targets={state.targets} now={now} onAdd={() => setAddingTarget(true)} />
                <EventsSection events={state.events} hostNames={hostNames} />
              </>
            )}
          </>
        ) : view === 'analytics' ? (
          <AnalyticsView status={sites.status} error={sites.error} sites={sites.sites} onAddSite={() => setAddingSite(true)} />
        ) : (
          <Suspense
            fallback={
              <p className={styles.notice} role="status">
                loading the guide…
              </p>
            }
          >
            <GuideView lesson={lesson} onLessonChange={setLesson} sites={sites.sites} />
          </Suspense>
        )}
      </main>
      <AddHostDialog open={adding} onOpenChange={setAdding} />
      <AddTargetDialog open={addingTarget} onOpenChange={setAddingTarget} />
      <AddSiteDialog open={addingSite} onOpenChange={setAddingSite} onCreated={sites.reload} />
    </div>
    </GuideNavContext.Provider>
  )
}
