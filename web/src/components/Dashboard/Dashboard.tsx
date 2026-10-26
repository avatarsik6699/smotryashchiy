import { Button } from '@base-ui/react/button'
import { lazy, Suspense, useCallback, useMemo, useState } from 'react'
import { DashboardProvider, useDashboard, useStore } from '../../data/DashboardContext'
import type { DashboardStore } from '../../data/store'
import { fleetSummary } from '../../data/health'
import { LESSON_IDS, type LessonId } from '../../guide/lessonIds'
import { useSites } from '../../data/useSites'
import { hostView } from '../../data/model'
import { uptimeState } from '../../domain/uptime'
import { hostState } from '../../domain/freshness'
import { useNow } from '../../hooks/useNow'
import { useBackups } from '../../data/useBackups'
import { hostBackup } from '../../domain/backup'
import { worst } from '../../domain/health'
import { BackupsSection } from './BackupsSection'
import { CommandBar, type View } from './CommandBar'
import { EventsSection } from './EventsSection'
import { HostsSection } from './HostsSection'
import { UptimeSection } from './UptimeSection'
import { StatusStrip, type BackupCount, type HostEntry } from './StatusStrip'
import { SummaryLine } from './SummaryLine'
import { GuideNavContext } from '../Guide/GuideNav'
import styles from './Dashboard.module.css'

// The guide is text the Monitoring view does not need: loaded on first use, outside the main bundle.
const GuideView = lazy(() => import('../Guide/GuideView'))
// Change 38: the other views and the dialogs load on first use too.
const AnalyticsView = lazy(() => import('../Analytics/AnalyticsView').then((m) => ({ default: m.AnalyticsView })))
const SalesSection = lazy(() => import('../Sales/SalesSection'))
const OperatorSection = lazy(() => import('../Operator/OperatorSection'))
const AddHostDialog = lazy(() => import('./AddHostDialog').then((m) => ({ default: m.AddHostDialog })))
const AddTargetDialog = lazy(() => import('./AddTargetDialog').then((m) => ({ default: m.AddTargetDialog })))
const AddSiteDialog = lazy(() => import('../Analytics/AddSiteDialog').then((m) => ({ default: m.AddSiteDialog })))

const LESSON_KEY = 'smotryashchiy.guide.lesson'

/** The last opened lesson is a per-browser convenience; storage may be unavailable (private mode). */
function storedLesson(): LessonId {
  try {
    const v = window.localStorage.getItem(LESSON_KEY)
    if (v && (LESSON_IDS as readonly string[]).includes(v)) return v as LessonId
  } catch {
    // ignore: start at the first lesson
  }
  return LESSON_IDS[0]
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

/** The whole product on five views: Monitoring (default), Analytics, Sales, Operator and the Guide (docs/SPEC.md §5). */
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
  const [view, setView] = useState<View>('monitoring')
  // Ages on Monitoring and in the guide tick every 5 s; the other views show minutes at most
  // (Change 38: the 5 s tick re-rendered every view).
  const now = useNow(view === 'monitoring' || view === 'guide' ? 5000 : 60_000)
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
  const [adding, setAddingState] = useState(false)
  const [addingTarget, setAddingTargetState] = useState(false)
  const [addingSite, setAddingSiteState] = useState(false)
  const [opened, setOpened] = useState<ReadonlySet<'host' | 'target' | 'site'>>(new Set())
  const opener = (kind: 'host' | 'target' | 'site', set: (open: boolean) => void) => (open: boolean) => {
    if (open) setOpened((o) => (o.has(kind) ? o : new Set([...o, kind])))
    set(open)
  }
  const setAdding = opener('host', setAddingState)
  const setAddingTarget = opener('target', setAddingTargetState)
  const setAddingSite = opener('site', setAddingSiteState)
  const sites = useSites()
  const backupDocs = useBackups()

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
  const backups = useMemo(() => backupDocs.documents.map((d) => hostBackup(d, now)), [backupDocs.documents, now])
  const backupCount: BackupCount | null = useMemo(
    () =>
      backups.length === 0
        ? null
        : { ok: backups.filter((b) => b.overall === 'normal').length, total: backups.length, level: worst(backups.map((b) => b.overall)) },
    [backups],
  )
  const summary = useMemo(() => fleetSummary(state, now, backups), [state, now, backups])

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
                <StatusStrip entries={entries} probes={probes} backups={backupCount} />
                <HostsSection entries={entries} records={state.hosts} now={now} onAddHost={() => setAdding(true)} />
                <UptimeSection targets={state.targets} now={now} onAdd={() => setAddingTarget(true)} />
                <BackupsSection status={backupDocs.status} error={backupDocs.error} hosts={backups} now={now} onRetry={backupDocs.reload} />
                <EventsSection events={state.events} hostNames={hostNames} />
              </>
            )}
          </>
        ) : (
          <Suspense
            fallback={
              <p className={styles.notice} role="status">
                loading {view}…
              </p>
            }
          >
            {view === 'analytics' ? (
              <AnalyticsView status={sites.status} error={sites.error} sites={sites.sites} onAddSite={() => setAddingSite(true)} />
            ) : view === 'sales' ? (
              <SalesSection now={now} />
            ) : view === 'operator' ? (
              <OperatorSection now={now} />
            ) : (
              <GuideView lesson={lesson} onLessonChange={setLesson} sites={sites.sites} backups={backups} />
            )}
          </Suspense>
        )}
      </main>
      {/* A dialog's code loads the first time it opens and then stays, so it can animate closed. */}
      <Suspense fallback={null}>
        {opened.has('host') && <AddHostDialog open={adding} onOpenChange={setAdding} />}
        {opened.has('target') && <AddTargetDialog open={addingTarget} onOpenChange={setAddingTarget} />}
        {opened.has('site') && <AddSiteDialog open={addingSite} onOpenChange={setAddingSite} onCreated={sites.reload} />}
      </Suspense>
    </div>
    </GuideNavContext.Provider>
  )
}
