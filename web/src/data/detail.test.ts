import { describe, expect, it } from "vitest";
import type { HostDTO, MetricDTO } from "../domain/types";
import { containerInfos } from "./detail";
import { buildRecords } from "./model";

const NOW = Date.UTC(2026, 8, 22, 12, 0, 0);
const iso = (offsetSeconds: number) =>
  new Date(NOW + offsetSeconds * 1000).toISOString();
const host = (id: string, name: string): HostDTO => ({
  id,
  name,
  created_at: iso(-9999),
  last_seen_at: iso(0),
});
const metric = (
  name: string,
  offset: number,
  value: number,
  labels: Record<string, string>,
): MetricDTO => ({ host: "a", name, ts: iso(offset), value, labels });

describe("containerInfos", () => {
  it("groups docker.container.* metrics by the container label", () => {
    const record = buildRecords({
      hosts: [host("a", "alpha")],
      latest: [
        metric("docker.container.cpu_percent", 0, 3.5, {
          container: "web",
          image: "nginx:latest",
        }),
        metric("docker.container.memory_used_bytes", 0, 1024, {
          container: "web",
          image: "nginx:latest",
        }),
        metric("docker.container.memory_used_percent", 0, 12.3, {
          container: "web",
          image: "nginx:latest",
        }),
        metric("docker.container.cpu_percent", 0, 0.1, {
          container: "db",
          image: "postgres:16",
        }),
      ],
      history: [],
      events: [],
      checks: [],
    })[0]!;

    const infos = containerInfos(record, NOW);
    expect(infos).toHaveLength(2);
    // busiest (highest CPU) first
    expect(infos[0]).toEqual({
      name: "web",
      image: "nginx:latest",
      cpuPercent: 3.5,
      memUsedBytes: 1024,
      memUsedPercent: 12.3,
    });
    expect(infos[1]).toEqual({
      name: "db",
      image: "postgres:16",
      cpuPercent: 0.1,
      memUsedBytes: null,
      memUsedPercent: null,
    });
  });

  it("returns an empty list for a host with no Docker metrics", () => {
    const record = buildRecords({
      hosts: [host("a", "alpha")],
      latest: [metric("cpu.usage_percent", 0, 10, {})],
      history: [],
      events: [],
      checks: [],
    })[0]!;
    expect(containerInfos(record, NOW)).toEqual([]);
  });

  it("shows the freshest series of a redeployed container and drops containers that are gone", () => {
    // Production shape (2026-09-23): latest=true returns every series still inside the raw TTL, so a
    // redeployed container has one series per image, with the current one between two stale ones.
    const api = (image: string, offset: number, cpu: number) => [
      metric("docker.container.cpu_percent", offset, cpu, {
        container: "api-1",
        image,
      }),
      metric("docker.container.memory_used_bytes", offset, cpu * 100, {
        container: "api-1",
        image,
      }),
    ];
    const record = buildRecords({
      hosts: [host("a", "alpha")],
      latest: [
        ...api("api:old", -3 * 3600, 9),
        ...api("api:new", -5, 2.5),
        ...api("api:older", -8 * 3600, 7),
        metric("docker.container.cpu_percent", -3600, 1, {
          container: "ops-umami-1",
          image: "umami:3",
        }),
        metric("docker.container.cpu_percent", -6 * 60, 4, {
          container: "db-migrate-run-1",
          image: "api:new",
        }),
        metric("docker.container.cpu_percent", -2, 0.4, {
          container: "postgres-1",
          image: "postgres:18",
        }),
      ],
      history: [],
      events: [],
      checks: [],
    })[0]!;

    expect(containerInfos(record, NOW)).toEqual([
      {
        name: "api-1",
        image: "api:new",
        cpuPercent: 2.5,
        memUsedBytes: 250,
        memUsedPercent: null,
      },
      {
        name: "postgres-1",
        image: "postgres:18",
        cpuPercent: 0.4,
        memUsedBytes: null,
        memUsedPercent: null,
      },
    ]);
  });

  it("reports null for a stale (>5 min old) metric instead of a fabricated value", () => {
    const record = buildRecords({
      hosts: [host("a", "alpha")],
      latest: [
        metric("docker.container.cpu_percent", -5, 2, { container: "web" }),
        metric("docker.container.memory_used_bytes", -600, 9, {
          container: "web",
        }),
      ],
      history: [],
      events: [],
      checks: [],
    })[0]!;
    expect(containerInfos(record, NOW)).toEqual([
      {
        name: "web",
        image: "",
        cpuPercent: 2,
        memUsedBytes: null,
        memUsedPercent: null,
      },
    ]);
  });

  it("omits a container whose every sample is stale", () => {
    const record = buildRecords({
      hosts: [host("a", "alpha")],
      latest: [
        metric("docker.container.cpu_percent", -600, 9, { container: "web" }),
      ],
      history: [],
      events: [],
      checks: [],
    })[0]!;
    expect(containerInfos(record, NOW)).toEqual([]);
  });
});
