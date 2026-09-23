import { describe, expect, it } from "vitest";
import {
  assessCheck,
  assessCpu,
  assessErrors,
  assessHostState,
  assessLoad,
  assessPercent,
  assessUptime,
  worst,
} from "./health";

const NOW = Date.UTC(2026, 8, 23, 12, 0, 0);
const at = (secondsAgo: number, v: number) => ({
  t: NOW - secondsAgo * 1000,
  v,
});

describe("assessPercent", () => {
  it.each([
    ["memory", 84.9, "normal"],
    ["memory", 85, "watch"],
    ["memory", 95, "problem"],
    ["swap", 24, "normal"],
    ["swap", 25, "watch"],
    ["swap", 60, "problem"],
    ["disk", 79, "normal"],
    ["disk", 80, "watch"],
    ["disk", 90, "problem"],
    ["containerMemory", 85, "watch"],
    ["containerMemory", 95, "problem"],
  ] as const)("%s %d%% is %s", (signal, value, level) => {
    expect(assessPercent(signal, value).level).toBe(level);
  });

  it("states the rule that was crossed and treats missing data as unknown, never normal", () => {
    expect(assessPercent("disk", 85).reason).toBe("disk 85% (watch ≥ 80%)");
    expect(assessPercent("disk", 24).reason).toBe("disk 24%");
    expect(assessPercent("memory", null)).toEqual({
      level: "unknown",
      reason: "memory: no recent data",
    });
  });
});

describe("assessCpu", () => {
  it("judges the mean of the last five minutes, so one spike does not color it", () => {
    expect(assessCpu([at(250, 10), at(120, 10), at(10, 100)], NOW).level).toBe(
      "normal",
    ); // mean 40
    expect(assessCpu([at(250, 90), at(120, 80), at(10, 85)], NOW).level).toBe(
      "watch",
    );
    expect(assessCpu([at(60, 96), at(10, 98)], NOW).level).toBe("problem");
  });

  it("ignores samples older than five minutes and is unknown without recent ones", () => {
    expect(assessCpu([at(3600, 99), at(10, 5)], NOW).level).toBe("normal");
    expect(assessCpu([at(600, 99)], NOW).level).toBe("unknown");
    expect(assessCpu([], NOW).level).toBe("unknown");
  });
});

describe("assessLoad", () => {
  it("divides the 5-minute load by the CPU count", () => {
    expect(assessLoad(0.9, 1).level).toBe("normal");
    expect(assessLoad(2, 2).level).toBe("watch");
    expect(assessLoad(4.1, 2).level).toBe("problem");
    expect(assessLoad(0.2, 2).reason).toBe(
      "load 0.20 on 2 CPUs = 0.10 per CPU",
    );
  });

  it("cannot judge load without the CPU count (agents older than v0.2.6)", () => {
    expect(assessLoad(3, null).level).toBe("unknown");
    expect(assessLoad(null, 4).level).toBe("unknown");
  });
});

describe("assessHostState and assessCheck", () => {
  it("maps freshness and check status", () => {
    expect(
      [
        assessHostState("ok"),
        assessHostState("stale"),
        assessHostState("offline"),
        assessHostState("new"),
      ].map((a) => a.level),
    ).toEqual(["normal", "watch", "problem", "unknown"]);
    expect(
      ["ok", "warn", "critical", "FAIL", "weird"].map(
        (s) => assessCheck(s).level,
      ),
    ).toEqual(["normal", "watch", "problem", "problem", "unknown"]);
  });
});

describe("assessUptime", () => {
  it("orders down, TLS and latency by severity", () => {
    expect(assessUptime("down", 100, 60).level).toBe("problem");
    expect(assessUptime("up", 100, -1).reason).toBe("TLS certificate expired");
    expect(assessUptime("up", 100, 7).level).toBe("problem");
    expect(assessUptime("up", 100, 14).level).toBe("watch");
    expect(assessUptime("up", 1500, 60).level).toBe("watch");
    expect(assessUptime("up", 180, 46)).toEqual({
      level: "normal",
      reason: "up, 180 ms",
    });
    expect(assessUptime("stale", null, null).level).toBe("watch");
    expect(assessUptime("new", null, null).level).toBe("unknown");
  });
});

describe("assessErrors and worst", () => {
  it("counts errors in the last hour", () => {
    expect(assessErrors(0)).toEqual({
      level: "normal",
      reason: "no errors in the last hour",
    });
    expect(assessErrors(1).level).toBe("watch");
    expect(assessErrors(10).level).toBe("problem");
    expect(assessErrors(null).level).toBe("unknown");
  });

  it("picks the most severe level, unknown only when nothing is known", () => {
    expect(worst(["normal", "watch", "unknown"])).toBe("watch");
    expect(worst(["normal", "problem", "watch"])).toBe("problem");
    expect(worst(["unknown", "normal"])).toBe("normal");
    expect(worst([])).toBe("unknown");
  });
});
