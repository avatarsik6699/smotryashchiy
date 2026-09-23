// Health of hosts, uptime targets and the whole fleet, composed from the dashboard state with the
// pure rules of domain/health.ts (docs/SPEC.md §5, Change 22).

import { formatLatency, formatPercent } from "../domain/format";
import { hostState } from "../domain/freshness";
import {
  assessCpu,
  assessErrors,
  assessHostState,
  assessLoad,
  assessPercent,
  assessUptime,
  worst,
  type Assessment,
  type Level,
} from "../domain/health";
import { tlsDaysLeft, uptimeState } from "../domain/uptime";
import {
  currentValue,
  errorsLastHour,
  hostView,
  type DashboardState,
  type HostRecord,
  type UptimeRecord,
} from "./model";

export interface HostHealth {
  freshness: Assessment;
  cpu: Assessment;
  memory: Assessment;
  swap: Assessment;
  disk: Assessment;
  load: Assessment;
  overall: Level;
}

export function hostHealth(rec: HostRecord, nowMs: number): HostHealth {
  const view = hostView(rec, nowMs);
  const freshness = assessHostState(hostState(rec.lastSeenMs, nowMs));
  const cpu = assessCpu(view.cpu.points, nowMs);
  const memory = assessPercent("memory", view.memory.value);
  const swap = assessPercent(
    "swap",
    currentValue(rec, "swap.used_percent", nowMs),
  );
  const disk = assessPercent(
    "disk",
    view.disk.value,
    view.disk.mount === null ? "disk" : `disk ${view.disk.mount}`,
  );
  const load = assessLoad(
    currentValue(rec, "load.avg_5m", nowMs),
    currentValue(rec, "cpu.count", nowMs),
  );
  // An offline host's last values say nothing about now: freshness alone decides then.
  const overall =
    freshness.level === "problem"
      ? "problem"
      : worst([freshness, cpu, memory, swap, disk, load].map((a) => a.level));
  return { freshness, cpu, memory, swap, disk, load, overall };
}

export function targetHealth(rec: UptimeRecord, nowMs: number): Assessment {
  const state = uptimeState(rec.last, rec.target.interval_seconds, nowMs);
  return assessUptime(
    state,
    rec.last?.latency_ms ?? null,
    tlsDaysLeft(rec.last, nowMs),
  );
}

export interface Issue {
  level: Level;
  text: string;
}

export interface Summary {
  level: Level;
  /** "All good", "Watch", "Problem" or "No data yet". */
  headline: string;
  /** What is wrong, worst first; empty when all is good. */
  issues: Issue[];
  /** The facts behind the verdict, always shown. */
  facts: string[];
}

const HEADLINE: Record<Level, string> = {
  normal: "All good",
  watch: "Watch",
  problem: "Problem",
  unknown: "No data yet",
};

/** One line for the top of Monitoring: the worst level first, then what it is based on. */
export function fleetSummary(
  state: Pick<DashboardState, "status" | "hosts" | "targets" | "errors">,
  nowMs: number,
): Summary {
  const hosts = state.hosts.map((rec) => ({
    name: rec.host.name,
    health: hostHealth(rec, nowMs),
    rec,
  }));
  const targets = state.targets.map((rec) => ({
    name: rec.target.name,
    health: targetHealth(rec, nowMs),
    rec,
  }));
  const errors = assessErrors(errorsLastHour(state, nowMs));

  const found: { level: Level; text: string }[] = [];
  for (const h of hosts) {
    if (
      h.health.freshness.level === "problem" ||
      h.health.freshness.level === "watch"
    ) {
      found.push({
        level: h.health.freshness.level,
        text: `${h.name}: ${h.health.freshness.reason}`,
      });
      // Offline: the last values say nothing about now. Stale values are still under 5 min old.
      if (h.health.freshness.level === "problem") continue;
    }
    for (const a of [
      h.health.cpu,
      h.health.memory,
      h.health.swap,
      h.health.disk,
      h.health.load,
    ]) {
      if (a.level === "watch" || a.level === "problem")
        found.push({ level: a.level, text: `${a.reason} on ${h.name}` });
    }
  }
  for (const t of targets) {
    if (t.health.level === "watch" || t.health.level === "problem")
      found.push({
        level: t.health.level,
        text: `${t.name} ${t.health.reason}`,
      });
  }
  if (errors.level === "watch" || errors.level === "problem")
    found.push({ level: errors.level, text: errors.reason });

  const reporting = hosts.filter(
    (h) => h.health.freshness.level === "normal",
  ).length;
  const facts: string[] = [];
  if (hosts.length > 0)
    facts.push(`${reporting}/${hosts.length} hosts reporting`);
  for (const t of targets) {
    const latency = t.rec.last?.latency_ms;
    const upState = uptimeState(
      t.rec.last,
      t.rec.target.interval_seconds,
      nowMs,
    );
    facts.push(
      `${t.name} ${upState === "up" ? "up" : upState}${upState === "up" && latency != null ? ` (${formatLatency(latency)})` : ""}`,
    );
  }
  const disks = hosts
    .map((h) => hostView(h.rec, nowMs).disk.value)
    .filter((v): v is number => v !== null);
  if (disks.length > 0)
    facts.push(`max disk ${formatPercent(Math.max(...disks))}`);
  // A plain count: the rule it breaks, if any, is already named among the issues.
  const errorCount = errorsLastHour(state, nowMs);
  if (errorCount !== null)
    facts.push(
      errorCount === 0
        ? "no errors in the last hour"
        : `${errorCount} error${errorCount === 1 ? "" : "s"} in the last hour`,
    );

  const known = [
    ...hosts.map((h) => h.health.overall),
    ...targets.map((t) => t.health.level),
    errors.level,
  ];
  const level =
    state.status !== "ready" || hosts.length + targets.length === 0
      ? "unknown"
      : worst(known);
  const order: Record<Level, number> = {
    problem: 0,
    watch: 1,
    normal: 2,
    unknown: 3,
  };
  const issues = found.sort((a, b) => order[a.level] - order[b.level]);
  return { level, headline: HEADLINE[level], issues, facts };
}
