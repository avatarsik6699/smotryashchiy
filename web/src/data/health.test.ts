import { describe, expect, it } from "vitest";
import type {
  EventDTO,
  HostDTO,
  MetricDTO,
  UptimeTargetDTO,
} from "../domain/types";
import { fleetSummary, hostHealth } from "./health";
import {
  buildRecords,
  buildTargets,
  initialState,
  type DashboardState,
} from "./model";

const NOW = Date.UTC(2026, 8, 23, 12, 0, 0);
const iso = (offsetSeconds: number) =>
  new Date(NOW + offsetSeconds * 1000).toISOString();
const host = (
  id: string,
  name: string,
  lastSeenOffset: number | null = -5,
): HostDTO => ({
  id,
  name,
  created_at: iso(-99999),
  last_seen_at: lastSeenOffset === null ? null : iso(lastSeenOffset),
});
const m = (
  h: string,
  name: string,
  value: number,
  labels: Record<string, string> = {},
  offset = -5,
): MetricDTO => ({ host: h, name, ts: iso(offset), value, labels });

/** A healthy host: CPU 11 %, memory 40 %, no swap, disk 24 %, load 0.2 on 2 CPUs. */
function healthy(
  id: string,
  over: Partial<Record<string, number>> = {},
): MetricDTO[] {
  const v = {
    cpu: 11,
    mem: 40,
    swap: 0,
    disk: 24,
    load5: 0.2,
    cpus: 2,
    ...over,
  };
  return [
    m(id, "cpu.usage_percent", v.cpu),
    m(id, "memory.used_percent", v.mem),
    m(id, "swap.used_percent", v.swap),
    m(id, "disk.used_percent", v.disk, { mount: "/", device: "/dev/vda2" }),
    m(id, "load.avg_5m", v.load5),
    m(id, "cpu.count", v.cpus),
  ];
}

const target = (
  name: string,
  ok: boolean,
  latency: number,
  certDays = 46,
): UptimeTargetDTO => ({
  id: name,
  name,
  kind: "http",
  target: `https://${name}/`,
  interval_seconds: 60,
  created_at: iso(-99999),
  last: {
    target_id: name,
    ts: iso(-20),
    ok,
    status_code: ok ? 200 : 502,
    latency_ms: latency,
    error: ok ? "" : "bad gateway",
    cert_expires_at: new Date(NOW + certDays * 86_400_000).toISOString(),
  },
  latency: [],
});

function state(
  hosts: HostDTO[],
  metrics: MetricDTO[],
  targets: UptimeTargetDTO[] = [],
  errors: EventDTO[] = [],
): DashboardState {
  return {
    ...initialState,
    status: "ready",
    hosts: buildRecords({
      hosts,
      latest: metrics,
      history: [],
      events: [],
      checks: [],
    }),
    targets: buildTargets(targets),
    errors,
  };
}

describe("hostHealth", () => {
  it("is normal for a healthy host and names each signal", () => {
    const s = state([host("a", "prod")], healthy("a"));
    const h = hostHealth(s.hosts[0]!, NOW);
    expect(h.overall).toBe("normal");
    expect(h.load.reason).toBe("load 0.20 on 2 CPUs = 0.10 per CPU");
    expect(h.disk.reason).toBe("disk / 24%");
  });

  it("takes the worst signal, and lets an offline host be judged by freshness alone", () => {
    expect(
      hostHealth(
        state([host("a", "prod")], healthy("a", { disk: 85 })).hosts[0]!,
        NOW,
      ).overall,
    ).toBe("watch");
    expect(
      hostHealth(
        state([host("a", "prod")], healthy("a", { mem: 97 })).hosts[0]!,
        NOW,
      ).overall,
    ).toBe("problem");
    const offline = state(
      [host("a", "prod", -900)],
      healthy("a").map((x) => ({ ...x, ts: iso(-900) })),
    );
    expect(hostHealth(offline.hosts[0]!, NOW).overall).toBe("problem");
  });
});

describe("fleetSummary", () => {
  it("says all good with the facts behind it", () => {
    const s = state(
      [host("a", "prod"), host("b", "monitor")],
      [...healthy("a"), ...healthy("b", { disk: 31 })],
      [target("infraege.ru", true, 180)],
    );
    expect(fleetSummary(s, NOW)).toEqual({
      level: "normal",
      headline: "All good",
      issues: [],
      facts: [
        "2/2 hosts reporting",
        "infraege.ru up (180 ms)",
        "max disk 31%",
        "no errors in the last hour",
      ],
    });
  });

  it("puts problems before watch items", () => {
    const errors: EventDTO[] = [
      { host: "a", ts: iso(-60), level: "error", message: "boom", labels: {} },
    ];
    const s = state(
      [host("a", "prod")],
      healthy("a", { disk: 85 }),
      [target("infraege.ru", false, 30)],
      errors,
    );
    const sum = fleetSummary(s, NOW);
    expect(sum.level).toBe("problem");
    expect(sum.headline).toBe("Problem");
    expect(sum.issues).toEqual([
      { level: "problem", text: "infraege.ru down" },
      { level: "watch", text: "disk / 85% (watch ≥ 80%) on prod" },
      { level: "watch", text: "errors in the last hour: 1 (watch ≥ 1)" },
    ]);
    expect(sum.facts).toContain("infraege.ru down");
    // The count is a plain fact; the rule it breaks is named once, among the issues.
    expect(sum.facts).toContain("1 error in the last hour");
  });

  it("reports a stale host with its recent values, and an offline host by freshness alone", () => {
    const stale = state([host("a", "prod", -120)], healthy("a", { disk: 95 }));
    expect(fleetSummary(stale, NOW)).toMatchObject({
      level: "problem",
      issues: [
        { level: "problem", text: "disk / 95% (problem ≥ 90%) on prod" },
        { level: "watch", text: "prod: no data for over 45 s" },
      ],
    });
    const offline = state([host("a", "prod", -900)], healthy("a", { disk: 95 }));
    expect(fleetSummary(offline, NOW).issues).toEqual([{ level: "problem", text: "prod: no data for over 5 min" }]);
  });

  it("has no verdict before data arrives", () => {
    expect(fleetSummary({ ...initialState }, NOW)).toMatchObject({
      level: "unknown",
      headline: "No data yet",
    });
    expect(fleetSummary(state([], []), NOW).level).toBe("unknown");
  });
});
