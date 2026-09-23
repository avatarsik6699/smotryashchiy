// Live callouts for the guide: the operator's own hosts, targets and events, judged by the same
// rules as the dashboard (domain/health.ts), so each lesson is tied to what is on screen right now.

import { containerInfos, diskInfos } from "../data/detail";
import { eventSource } from "../data/events";
import { fleetSummary, hostHealth, targetHealth } from "../data/health";
import {
  errorsLastHour,
  hostView,
  WINDOW_MS,
  type DashboardState,
} from "../data/model";
import {
  formatAge,
  formatBytes,
  formatMeta,
  formatRate,
  formatUptime,
} from "../domain/format";
import { HOST_STATE_LABEL, hostState } from "../domain/freshness";
import {
  assessCheck,
  assessErrors,
  assessPercent,
  type Level,
} from "../domain/health";
import { tlsDaysLeft, tlsLabel } from "../domain/uptime";
import type { LessonId } from "./lessons";

export interface Callout {
  /** Host, target or topic the value belongs to. */
  subject: string;
  text: string;
  /** Absent for values that are explained but never assessed (network, analytics). */
  level?: Level;
}

type Live = (state: DashboardState, now: number) => Callout[];

const perHost = (
  state: DashboardState,
  fn: (rec: DashboardState["hosts"][number]) => Callout | Callout[],
): Callout[] => state.hosts.flatMap(fn);

const LIVE: Partial<Record<LessonId, Live>> = {
  "how-it-works": (state, now) =>
    perHost(state, (rec) => {
      const h = hostHealth(rec, now);
      const seen =
        rec.lastSeenMs === null
          ? "never reported"
          : `last data ${formatAge(now - rec.lastSeenMs)}`;
      return {
        subject: rec.host.name,
        text: `${HOST_STATE_LABEL[hostState(rec.lastSeenMs, now)]}, ${seen}`,
        level: h.freshness.level,
      };
    }),
  "daily-check": (state, now) => {
    const s = fleetSummary(state, now);
    const detail = [...s.issues.map((i) => i.text), ...s.facts].join(" · ");
    return [
      {
        subject: "summary",
        text: detail ? `${s.headline}: ${detail}` : s.headline,
        level: s.level,
      },
    ];
  },
  status: (state, now) =>
    perHost(state, (rec) => {
      const v = hostView(rec, now);
      const up =
        v.uptimeSeconds === null
          ? ""
          : `, running for ${formatUptime(v.uptimeSeconds)}`;
      return {
        subject: rec.host.name,
        text: `${HOST_STATE_LABEL[hostState(rec.lastSeenMs, now)]}${up}`,
        level: hostHealth(rec, now).freshness.level,
      };
    }),
  cpu: (state, now) =>
    perHost(state, (rec) => {
      const a = hostHealth(rec, now).cpu;
      return { subject: rec.host.name, text: a.reason, level: a.level };
    }),
  load: (state, now) =>
    perHost(state, (rec) => {
      const a = hostHealth(rec, now).load;
      return { subject: rec.host.name, text: a.reason, level: a.level };
    }),
  memory: (state, now) =>
    perHost(state, (rec) => {
      const h = hostHealth(rec, now);
      return [
        {
          subject: rec.host.name,
          text: h.memory.reason,
          level: h.memory.level,
        },
        { subject: rec.host.name, text: h.swap.reason, level: h.swap.level },
      ];
    }),
  disk: (state, now) =>
    perHost(state, (rec) =>
      diskInfos(rec, now).map((d) => {
        const a = assessPercent("disk", d.usedPercent, `disk ${d.mount}`);
        const size =
          d.usedBytes === null || d.totalBytes === null
            ? ""
            : ` — ${formatBytes(d.usedBytes)} of ${formatBytes(d.totalBytes)}`;
        return {
          subject: rec.host.name,
          text: a.reason + size,
          level: a.level,
        };
      }),
    ),
  network: (state, now) =>
    perHost(state, (rec) => {
      const rate = hostView(rec, now).network.rate;
      return {
        subject: rec.host.name,
        text:
          rate === null
            ? "no recent traffic data"
            : `${formatRate(rate)} in + out right now`,
      };
    }),
  containers: (state, now) =>
    perHost(state, (rec) => {
      const list = containerInfos(rec, now);
      if (list.length === 0)
        return {
          subject: rec.host.name,
          text: "no Docker containers reported",
        };
      const busiest = [...list].sort(
        (a, b) => (b.memUsedPercent ?? -1) - (a.memUsedPercent ?? -1),
      )[0]!;
      const a = assessPercent(
        "containerMemory",
        busiest.memUsedPercent,
        `highest container memory: ${busiest.name}`,
      );
      return [
        {
          subject: rec.host.name,
          text: `${list.length} running: ${list.map((c) => c.name).join(", ")}`,
        },
        { subject: rec.host.name, text: a.reason, level: a.level },
      ];
    }),
  checks: (state) =>
    perHost(state, (rec) =>
      rec.checks.length === 0
        ? { subject: rec.host.name, text: "no checks reported" }
        : rec.checks.map((c) => {
            const meta = formatMeta(c.meta);
            return {
              subject: rec.host.name,
              text: `${c.name}: ${c.status}${meta ? ` · ${meta}` : ""}`,
              level: assessCheck(c.status).level,
            };
          }),
    ),
  events: (state, now) => {
    const errors = assessErrors(errorsLastHour(state, now));
    const recent = state.events.filter(
      (e) => now - Date.parse(e.ts) <= WINDOW_MS,
    );
    const warn = recent.filter((e) => e.level === "warn");
    const sources = [
      ...new Set(warn.map((e) => eventSource(e.labels)).filter(Boolean)),
    ].slice(0, 4);
    const out: Callout[] = [
      { subject: "errors", text: errors.reason, level: errors.level },
    ];
    if (warn.length > 0)
      out.push({
        subject: "warnings",
        text: `${warn.length} of the last ${recent.length} events are WARN${sources.length ? `, from ${sources.join(", ")}` : ""}`,
      });
    return out;
  },
  uptime: (state, now) =>
    state.targets.map((rec) => {
      const a = targetHealth(rec, now);
      const tls = tlsLabel(tlsDaysLeft(rec.last, now));
      return {
        subject: rec.target.name,
        text: `${a.reason}${tls ? ` · ${tls}` : ""}`,
        level: a.level,
      };
    }),
  triage: (state, now) => {
    const s = fleetSummary(state, now);
    if (s.issues.length === 0)
      return [
        {
          subject: "now",
          text:
            s.level === "normal" ? "nothing to triage — all good" : s.headline,
          level: s.level,
        },
      ];
    return s.issues.map((i) => ({ subject: "now", text: i.text, level: i.level }));
  },
};

/** Callouts for one lesson; empty when the lesson has none or there is no data yet. */
export function liveCallouts(
  id: LessonId,
  state: DashboardState,
  now: number,
): Callout[] {
  if (state.status !== "ready") return [];
  return LIVE[id]?.(state, now) ?? [];
}
