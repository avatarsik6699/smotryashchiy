// Health assessment (docs/SPEC.md §5, Change 22): each signal maps to normal / watch / problem,
// or unknown when there is no data. It is an assessment shown next to the value, never alerting,
// and always written as text as well as color. Every threshold lives here, so the dashboard
// colors, the "?" help, the summary line and the guide all say the same thing.

import { formatLatency, formatPercent } from "./format";
import type { HostState } from "./freshness";
import { isFresh } from "./freshness";
import type { Point } from "./types";
import type { UptimeState } from "./uptime";

export type Level = "normal" | "watch" | "problem" | "unknown";

export interface Assessment {
  level: Level;
  /** One short sentence for tooltips, the summary line and the guide, e.g. "disk 85% (watch ≥ 80%)". */
  reason: string;
}

export const LEVEL_LABEL: Record<Level, string> = {
  normal: "normal",
  watch: "watch",
  problem: "problem",
  unknown: "unknown",
};

interface Band {
  watch: number;
  problem: number;
}

export const THRESHOLDS = {
  cpu: { watch: 80, problem: 95 },
  memory: { watch: 85, problem: 95 },
  swap: { watch: 25, problem: 60 },
  disk: { watch: 80, problem: 90 },
  containerMemory: { watch: 85, problem: 95 },
  loadPerCpu: { watch: 1, problem: 2 },
  latencyMs: { watch: 1500 },
  tlsDays: { watch: 14, problem: 7 },
  errorsPerHour: { watch: 1, problem: 10 },
} as const;

/** CPU is judged on the mean of the last five minutes, so a single busy tick does not color it. */
export const CPU_WINDOW_MS = 5 * 60_000;

const RANK: Record<Level, number> = {
  unknown: 0,
  normal: 1,
  watch: 2,
  problem: 3,
};

/** The most severe level; unknown only when nothing is known. */
export function worst(levels: Level[]): Level {
  return levels.reduce<Level>((a, b) => (RANK[b] > RANK[a] ? b : a), "unknown");
}

function band(value: number, b: Band): Level {
  if (value >= b.problem) return "problem";
  if (value >= b.watch) return "watch";
  return "normal";
}

function describe(
  level: Level,
  subject: string,
  value: string,
  b: Band,
  unit = "",
): Assessment {
  const rule =
    level === "problem"
      ? ` (problem ≥ ${b.problem}${unit})`
      : level === "watch"
        ? ` (watch ≥ ${b.watch}${unit})`
        : "";
  return { level, reason: `${subject} ${value}${rule}` };
}

const unknown = (subject: string): Assessment => ({
  level: "unknown",
  reason: `${subject}: no recent data`,
});

/** Mean of the samples in the last five minutes (only fresh ones count). */
export function recentMean(points: Point[], nowMs: number): number | null {
  const recent = points.filter(
    (p) => nowMs - p.t <= CPU_WINDOW_MS && isFresh(p.t, nowMs),
  );
  if (recent.length === 0) return null;
  return recent.reduce((sum, p) => sum + p.v, 0) / recent.length;
}

export function assessCpu(points: Point[], nowMs: number): Assessment {
  const mean = recentMean(points, nowMs);
  if (mean === null) return unknown("CPU");
  const level = band(mean, THRESHOLDS.cpu);
  return describe(
    level,
    "CPU",
    `${formatPercent(mean)} over 5 min`,
    THRESHOLDS.cpu,
    "%",
  );
}

type PercentSignal = "memory" | "swap" | "disk" | "containerMemory";
const PERCENT_SUBJECT: Record<PercentSignal, string> = {
  memory: "memory",
  swap: "swap",
  disk: "disk",
  containerMemory: "container memory",
};

/** Memory, swap, disk or container memory in percent; `subject` overrides the default wording. */
export function assessPercent(
  signal: PercentSignal,
  value: number | null,
  subject = PERCENT_SUBJECT[signal],
): Assessment {
  if (value === null) return unknown(subject);
  const b = THRESHOLDS[signal];
  return describe(band(value, b), subject, formatPercent(value), b, "%");
}

/** The 5-minute load average divided by the CPU count; without the count it cannot be judged. */
export function assessLoad(
  load5: number | null,
  cpus: number | null,
): Assessment {
  if (load5 === null) return unknown("load");
  if (cpus === null || cpus <= 0)
    return {
      level: "unknown",
      reason: `load ${load5.toFixed(2)}: CPU count unknown (agent older than v0.2.6)`,
    };
  const perCpu = load5 / cpus;
  const level = band(perCpu, THRESHOLDS.loadPerCpu);
  return describe(
    level,
    "load",
    `${load5.toFixed(2)} on ${cpus} CPU${cpus === 1 ? "" : "s"} = ${perCpu.toFixed(2)} per CPU`,
    THRESHOLDS.loadPerCpu,
  );
}

export function assessHostState(state: HostState): Assessment {
  switch (state) {
    case "ok":
      return { level: "normal", reason: "reporting" };
    case "stale":
      return { level: "watch", reason: "no data for over 45 s" };
    case "offline":
      return { level: "problem", reason: "no data for over 5 min" };
    case "new":
      return { level: "unknown", reason: "never reported" };
  }
}

/** A check's own status: ok is normal, warn is watch, critical/fail is a problem. */
export function assessCheck(status: string): Assessment {
  const s = status.toLowerCase();
  if (s === "ok") return { level: "normal", reason: "check ok" };
  if (s === "warn" || s === "warning")
    return { level: "watch", reason: "check warns" };
  if (s === "critical" || s === "fail" || s === "failed" || s === "error")
    return { level: "problem", reason: `check ${s}` };
  return { level: "unknown", reason: `check status "${status}"` };
}

export function assessUptime(
  state: UptimeState,
  latencyMs: number | null,
  tlsDays: number | null,
): Assessment {
  if (state === "new") return { level: "unknown", reason: "no result yet" };
  if (state === "stale")
    return { level: "watch", reason: "no recent probe result" };
  if (state === "down") return { level: "problem", reason: "down" };
  if (tlsDays !== null && tlsDays <= THRESHOLDS.tlsDays.problem) {
    return {
      level: "problem",
      reason:
        tlsDays < 0
          ? "TLS certificate expired"
          : `TLS expires in ${tlsDays}d (problem ≤ ${THRESHOLDS.tlsDays.problem}d)`,
    };
  }
  if (tlsDays !== null && tlsDays <= THRESHOLDS.tlsDays.watch)
    return {
      level: "watch",
      reason: `TLS expires in ${tlsDays}d (watch ≤ ${THRESHOLDS.tlsDays.watch}d)`,
    };
  if (latencyMs !== null && latencyMs >= THRESHOLDS.latencyMs.watch) {
    return {
      level: "watch",
      reason: `slow: ${formatLatency(latencyMs)} (watch ≥ ${formatLatency(THRESHOLDS.latencyMs.watch)})`,
    };
  }
  return {
    level: "normal",
    reason: latencyMs === null ? "up" : `up, ${formatLatency(latencyMs)}`,
  };
}

/** Error events in the last hour; null when they could not be loaded. */
export function assessErrors(count: number | null): Assessment {
  if (count === null) return unknown("errors");
  if (count === 0)
    return { level: "normal", reason: "no errors in the last hour" };
  const level = band(count, THRESHOLDS.errorsPerHour);
  return describe(
    level,
    "errors in the last hour:",
    String(count),
    THRESHOLDS.errorsPerHour,
  );
}
