import { Accordion } from '@base-ui/react/accordion'
import { Button } from '@base-ui/react/button'
import { formatAge, formatBytes, formatPercent, UNKNOWN } from '../../domain/format'
import { primaryJob, type HostBackup } from '../../domain/backup'
import type { Level } from '../../domain/health'
import { HelpButton } from '../Help/HelpButton'
import { LevelMark, toneClass } from '../Health/LevelMark'
import shared from './Dashboard.module.css'
import styles from './BackupsSection.module.css'

const STATE_LABEL: Record<Level, string> = { normal: 'OK', watch: 'WATCH', problem: 'PROBLEM', unknown: 'UNKNOWN' }

interface BackupsSectionProps {
  status: 'loading' | 'ready' | 'error'
  error: string | null
  hosts: HostBackup[]
  now: number
  onRetry: () => void
}

/**
 * BACKUPS (docs/SPEC.md §5, Change 29): one row per host that reports a backup summary, assessed
 * with the same normal / watch / problem rules as the rest of the view; a row expands in place to
 * every job, repository, hold and failed unit. An assessment only: nothing is sent anywhere.
 */
export function BackupsSection({ status, error, hosts, now, onRetry }: BackupsSectionProps) {
  return (
    <section className={shared.section} aria-labelledby="backups-title">
      <div className={shared.titleRow}>
        <h2 id="backups-title" className={shared.sectionTitle}>
          Backups
        </h2>
        <HelpButton lesson="backups" topic="a backup row" />
      </div>
      {status === 'loading' && hosts.length === 0 ? (
        <p className={shared.notice} role="status">
          loading backup summaries…
        </p>
      ) : status === 'error' && hosts.length === 0 ? (
        <p className={shared.notice} role="alert">
          <span className={shared.noticeStrong}>Could not load backup summaries.</span> {error}{' '}
          <Button className={shared.button} onClick={onRetry}>
            retry
          </Button>
        </p>
      ) : hosts.length === 0 ? (
        <p className={shared.notice}>
          No host reports its backups yet. A host that runs backups writes a <code>smotryashchiy.backup/1</code> summary,
          and its agent reports it when started with <code>--document backup=PATH</code>.
        </p>
      ) : (
        <Accordion.Root className={styles.ledger}>
          {hosts.map((h) => (
            <BackupRow key={h.hostId} host={h} now={now} />
          ))}
        </Accordion.Root>
      )}
    </section>
  )
}

function BackupRow({ host, now }: { host: HostBackup; now: number }) {
  const s = host.summary
  const primary = primaryJob(host)
  const headline = s === null ? host.summaryAge.reason : host.issues[0]?.reason ?? `${s.jobs.length} job${s.jobs.length === 1 ? '' : 's'} ok`
  const freePct = s?.disk ? (s.disk.free_bytes / s.disk.total_bytes) * 100 : null
  return (
    <Accordion.Item value={host.hostId} className={styles.item} data-level={host.overall}>
      <Accordion.Header className={styles.header}>
        <Accordion.Trigger className={styles.trigger}>
          <span className={styles.state}>
            <svg className={styles.chevron} width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="square" aria-hidden="true">
              <path d="M3 1.5 6.5 5 3 8.5" />
            </svg>
            <span className={styles.pulse} aria-hidden="true" />
            {STATE_LABEL[host.overall]}
          </span>
          <span className={styles.identity}>
            <span className={styles.name}>{host.hostName}</span>
            <span className={`${styles.reason} ${toneClass}`} data-level={host.issues[0]?.level}>
              {headline}
              {host.issues.length > 1 && <span className={styles.more}> +{host.issues.length - 1} more</span>}
            </span>
          </span>
          <Measure
            area={styles.last}
            label={primary ? primary.job.label.toUpperCase() : 'BACKUP'}
            value={primary === null ? UNKNOWN : primary.job.completed_at === null ? 'never' : `${primary.job.status === 'failed' ? 'failed ' : ''}${formatAge(now - Date.parse(primary.job.completed_at))}`}
            assessment={primary?.assessment}
          />
          <Measure area={styles.snapshots} label="SNAPSHOTS" value={s && s.repositories.length > 0 ? s.repositories.map((r) => `${r.name} ${r.snapshots}`).join(' · ') : UNKNOWN} />
          <Measure area={styles.disk} label="DISK FREE" value={freePct === null ? UNKNOWN : formatPercent(freePct)} assessment={host.disk} />
          <span className={styles.age}>{s ? `written ${formatAge(now - Date.parse(s.generated_at))}` : UNKNOWN}</span>
        </Accordion.Trigger>
      </Accordion.Header>
      <Accordion.Panel className={styles.panel}>{s ? <BackupDetails host={host} now={now} /> : <p className={shared.notice}>{host.summaryAge.reason}.</p>}</Accordion.Panel>
    </Accordion.Item>
  )
}

function Measure({ area, label, value, assessment }: { area?: string; label: string; value: string; assessment?: HostBackup['disk'] }) {
  return (
    <span className={`${styles.measure} ${area ?? ''}`}>
      <span className={styles.label}>{label}</span>
      <span className={`${styles.value} ${toneClass}`} data-level={assessment?.level}>
        {value}
        {assessment && <LevelMark assessment={assessment} />}
      </span>
    </span>
  )
}

function BackupDetails({ host, now }: { host: HostBackup; now: number }) {
  const s = host.summary!
  return (
    <div className={styles.details}>
      <div className={styles.scroll}>
        <table className={styles.table}>
          <caption className="visually-hidden">Backup jobs on {host.hostName}</caption>
          <thead>
            <tr>
              <th scope="col">Job</th>
              <th scope="col">State</th>
              <th scope="col">Last run</th>
              <th scope="col" className={styles.num}>
                Limit
              </th>
              <th scope="col" className={styles.num}>
                Took
              </th>
              <th scope="col">Source</th>
            </tr>
          </thead>
          <tbody>
            {host.jobs.map(({ job, assessment }) => (
              <tr key={job.name}>
                <th scope="row">{job.label}</th>
                <td className={toneClass} data-level={assessment.level}>
                  {job.status}
                  <LevelMark assessment={assessment} />
                </td>
                <td>{job.completed_at ? formatAge(now - Date.parse(job.completed_at)) : 'never'}</td>
                <td className={styles.num}>{job.max_age_hours}h</td>
                <td className={styles.num}>{job.duration_seconds == null ? UNKNOWN : `${job.duration_seconds}s`}</td>
                <td>{job.source ?? UNKNOWN}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <dl className={styles.facts}>
        {s.repositories.map((r) => (
          <div key={r.name} className={styles.fact}>
            <dt className={styles.label}>REPOSITORY {r.name.toUpperCase()}</dt>
            <dd>
              {r.snapshots} snapshot{r.snapshots === 1 ? '' : 's'}, {formatBytes(r.size_bytes)}
            </dd>
          </div>
        ))}
        <div className={styles.fact}>
          <dt className={styles.label}>BACKUP DISK</dt>
          <dd className={toneClass} data-level={host.disk.level}>
            {s.disk ? `${s.disk.path}: ${formatBytes(s.disk.free_bytes)} free of ${formatBytes(s.disk.total_bytes)}` : UNKNOWN}
            <LevelMark assessment={host.disk} />
          </dd>
        </div>
        <div className={styles.fact}>
          <dt className={styles.label}>HOLDS</dt>
          <dd>
            {host.holds.length === 0
              ? 'none'
              : host.holds.map(({ hold, assessment }) => (
                  <span key={hold.name} className={`${styles.hold} ${toneClass}`} data-level={assessment.level}>
                    {hold.name}, {formatAge(now - Date.parse(hold.created_at))}
                    <LevelMark assessment={assessment} />
                  </span>
                ))}
          </dd>
        </div>
        <div className={styles.fact}>
          <dt className={styles.label}>FAILED UNITS</dt>
          <dd className={toneClass} data-level={host.units.level}>
            {s.failed_units.length === 0 ? 'none' : s.failed_units.join(', ')}
            <LevelMark assessment={host.units} />
          </dd>
        </div>
      </dl>
    </div>
  )
}
