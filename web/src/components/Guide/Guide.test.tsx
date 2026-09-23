import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { DashboardStore } from "../../data/store";
import type { EventDTO, HostDTO, MetricDTO } from "../../domain/types";
import { LESSONS } from "../../guide/lessons";
import { Dashboard } from "../Dashboard/Dashboard";

const NOW = Date.now();
const iso = (offsetMs: number) => new Date(NOW + offsetMs).toISOString();
const fetchMock = vi.fn<typeof fetch>();

const host = (id: string, name: string, lastSeenOffsetMs: number): HostDTO => ({
  id,
  name,
  created_at: iso(-86_400_000),
  last_seen_at: iso(lastSeenOffsetMs),
});
const metric = (
  h: string,
  name: string,
  value: number,
  labels: Record<string, string> = {},
): MetricDTO => ({ host: h, name, ts: iso(-5_000), value, labels });

const HOSTS = [host("a", "prod", -5_000), host("b", "monitor", -5_000)];
const LATEST = [
  metric("a", "cpu.usage_percent", 11),
  metric("a", "memory.used_percent", 40),
  metric("a", "swap.used_percent", 0),
  metric("a", "disk.used_percent", 85, { mount: "/", device: "/dev/vda2" }),
  metric("a", "load.avg_5m", 0.2),
  metric("a", "cpu.count", 2),
  metric("a", "docker.container.cpu_percent", 0.3, {
    container: "api-1",
    image: "api:new",
  }),
  metric("a", "docker.container.memory_used_percent", 23, {
    container: "api-1",
    image: "api:new",
  }),
  metric("b", "cpu.usage_percent", 3),
  metric("b", "memory.used_percent", 30),
  metric("b", "disk.used_percent", 31, { mount: "/", device: "/dev/sda1" }),
];
const EVENTS: EventDTO[] = [
  {
    host: "a",
    ts: iso(-60_000),
    level: "warn",
    message: "checkpoint starting",
    labels: { container: "postgres-1" },
  },
];

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200 });
}

async function mount() {
  fetchMock.mockImplementation(async (input) => {
    const url = String(input);
    if (url.startsWith("/api/hosts")) return json({ hosts: HOSTS });
    if (url.startsWith("/api/metrics"))
      return json({ metrics: url.includes("latest=true") ? LATEST : [] });
    if (url.startsWith("/api/events"))
      return json({ events: url.includes("level=") ? [] : EVENTS });
    if (url.startsWith("/api/checks"))
      return json({
        checks: [
          {
            host: "a",
            name: "fail2ban.jail.sshd",
            ts: iso(-5_000),
            status: "ok",
            meta: { currently_banned: 3 },
          },
        ],
      });
    if (url.startsWith("/api/uptime")) return json({ targets: [] });
    if (url.startsWith("/api/sites")) return json({ sites: [] });
    throw new Error(`unexpected ${url}`);
  });
  const store = new DashboardStore();
  await store.load();
  const view = render(
    <Dashboard store={store} onLogout={vi.fn().mockResolvedValue(undefined)} />,
  );
  return { store, ...view };
}

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
  window.localStorage.clear();
});
afterEach(() => {
  fetchMock.mockReset();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("summary line and assessment", () => {
  it("names the worst finding first, then the facts", async () => {
    await mount();
    const summary = screen.getByRole("status", { name: "Summary" });
    expect(summary).toHaveTextContent("Watch:");
    expect(
      within(summary).getByRole("list", { name: "Findings" }),
    ).toHaveTextContent("disk / 85% (watch ≥ 80%) on prod");
    const facts = within(within(summary).getByRole("list", { name: "Based on" }))
      .getAllByRole("listitem")
      .map((li) => li.textContent);
    expect(facts).toEqual(["2/2 hosts reporting", "max disk 85%", "no errors in the last hour"]);
  });

  it("writes the level next to a colored value in the host row", async () => {
    await mount();
    const prodRow = screen.getByRole("button", { name: /prod.*CPU/ });
    expect(within(prodRow).getByText("watch")).toHaveAttribute(
      "data-level",
      "watch",
    );
    expect(
      within(screen.getByRole("button", { name: /monitor.*CPU/ })).queryByText(
        /watch|problem/,
      ),
    ).not.toBeInTheDocument();
  });
});

describe('"?" help', () => {
  it("opens with the keyboard, explains the block and leads to its lesson", async () => {
    const user = userEvent.setup();
    await mount();
    const help = screen.getByRole("button", { name: "What is uptime?" });
    help.focus();
    await user.keyboard("{Enter}");
    const dialog = await screen.findByRole("dialog", {
      name: "Uptime: is the site reachable?",
    });
    expect(dialog).toHaveTextContent(
      "The server calls each target from outside",
    );
    expect(dialog).toHaveTextContent(
      "Assessment: problem: DOWN or TLS ≤ 7 days",
    );
    await user.keyboard("{Escape}");
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(help).toHaveFocus();

    await user.click(help);
    await user.click(
      await screen.findByRole("button", { name: "read the lesson →" }),
    );
    expect(
      await screen.findByRole("heading", {
        level: 2,
        name: "Uptime: is the site reachable?",
      }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "guide" })).toHaveAttribute(
      "data-active",
      "true",
    );
  });

  it("has no axe violations with a popover open", async () => {
    const user = userEvent.setup();
    const { container } = await mount();
    await user.click(
      screen.getByRole("button", { name: "What is a host row?" }),
    );
    await screen.findByRole("dialog");
    expect(await axe(container)).toHaveNoViolations();
  });
});

describe("guide", () => {
  it("walks through every lesson from the table of contents and the pager", async () => {
    const user = userEvent.setup();
    await mount();
    await user.click(screen.getByRole("button", { name: "guide" }));
    const toc = await screen.findByRole("navigation", {
      name: "Guide lessons",
    });
    const items = within(toc).getAllByRole("button");
    expect(items).toHaveLength(LESSONS.length);
    for (const [i, lesson] of LESSONS.entries()) {
      await user.click(items[i]!);
      const heading = screen.getByRole("heading", { level: 2, name: lesson.title });
      // The first lesson is already open, so clicking it moves nothing; every other one takes focus.
      if (i > 0) expect(heading).toHaveFocus();
      expect(items[i]).toHaveAttribute("aria-current", "step");
    }
    await user.click(
      screen.getByRole("button", {
        name: `← ${LESSONS[LESSONS.length - 2]!.title}`,
      }),
    );
    expect(
      screen.getByRole("heading", {
        level: 2,
        name: LESSONS[LESSONS.length - 2]!.title,
      }),
    ).toBeInTheDocument();
  });

  it("shows live values of the operator's hosts, judged like the dashboard", async () => {
    const user = userEvent.setup();
    await mount();
    await user.click(screen.getByRole("button", { name: "guide" }));
    const toc = await screen.findByRole("navigation", {
      name: "Guide lessons",
    });
    await user.click(
      within(toc).getByRole("button", { name: /Load average and cores/ }),
    );
    const live = screen.getByRole("region", {
      name: "Right now on your servers",
    });
    expect(live).toHaveTextContent("prodload 0.20 on 2 CPUs = 0.10 per CPU");
    expect(live).toHaveTextContent("monitorload: no recent data");
    await user.click(within(toc).getByRole("button", { name: /Disk space/ }));
    expect(
      screen.getByRole("region", { name: "Right now on your servers" }),
    ).toHaveTextContent("disk / 85% (watch ≥ 80%)watch");
    await user.click(
      within(toc).getByRole("button", { name: /Checks and fail2ban/ }),
    );
    expect(
      screen.getByRole("region", { name: "Right now on your servers" }),
    ).toHaveTextContent("fail2ban.jail.sshd: ok · currently_banned=3");
  });

  it("remembers the last lesson, and still works when storage is unavailable", async () => {
    const user = userEvent.setup();
    const first = await mount();
    await user.click(screen.getByRole("button", { name: "guide" }));
    await user.click(
      within(
        await screen.findByRole("navigation", { name: "Guide lessons" }),
      ).getByRole("button", { name: /Memory and swap/ }),
    );
    first.unmount();

    await mount();
    await user.click(screen.getByRole("button", { name: "guide" }));
    expect(
      await screen.findByRole("heading", { level: 2, name: "Memory and swap" }),
    ).toBeInTheDocument();

    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied");
    });
    const third = await mount();
    await user.click(
      within(third.container).getByRole("button", { name: "guide" }),
    );
    expect(
      await within(third.container).findByRole("heading", {
        level: 2,
        name: LESSONS[0]!.title,
      }),
    ).toBeInTheDocument();
  });

  it("has no axe violations", async () => {
    const user = userEvent.setup();
    const { container } = await mount();
    await user.click(screen.getByRole("button", { name: "guide" }));
    await screen.findByRole("navigation", { name: "Guide lessons" });
    expect(await axe(container)).toHaveNoViolations();
  });
});
