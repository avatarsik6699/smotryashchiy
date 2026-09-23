// The guide (docs/SPEC.md §5, Change 22): a step-by-step course on reading this dashboard. Lessons
// are plain data so the "?" help popovers can quote the same summary and thresholds; the live
// callouts next to each lesson come from guide/live.ts.

import { THRESHOLDS } from "../domain/health";

export type LessonId =
  | "how-it-works"
  | "daily-check"
  | "status"
  | "cpu"
  | "load"
  | "memory"
  | "disk"
  | "network"
  | "containers"
  | "checks"
  | "events"
  | "uptime"
  | "analytics"
  | "triage";

export interface Lesson {
  id: LessonId;
  title: string;
  /** One or two sentences; also the body of the matching "?" popover. */
  summary: string;
  /** The explanation, one paragraph per entry. */
  body: string[];
  /** What healthy looks like. */
  normal: string[];
  /** Signs worth a closer look. */
  worry: string[];
  /** Concrete next steps. */
  act: string[];
  /** The assessment rule, when the dashboard colors this signal. */
  thresholds?: string;
}

const t = THRESHOLDS;
const pct = (b: { watch: number; problem: number }) =>
  `watch ≥ ${b.watch}%, problem ≥ ${b.problem}%`;

export const LESSONS: Lesson[] = [
  {
    id: "how-it-works",
    title: "How the data gets here",
    summary:
      "Each monitored server runs a small agent that measures the machine every 10 seconds and sends the numbers here through an encrypted tunnel.",
    body: [
      "smotryashchiy has two parts. The server is this dashboard. The agent is the same program, run in agent mode on every server you want to watch. The agent never listens on a port: it dials out to this server over a private WireGuard tunnel, so a monitored server gains no new way in.",
      "Every 10 seconds the agent reads the operating system (the /proc files on Linux), the disks, Docker, the system journal and fail2ban, and sends one batch. If the tunnel is down it keeps the batches in a local queue on disk and delivers them later, so short outages do not leave holes in the history.",
      'The dashboard keeps the last hour of detail on screen and updates live: new samples arrive over a WebSocket, and "live" in the top bar says that stream is connected. Raw data is kept for 30 days and hourly averages for 13 months.',
      "Uptime checks are different: the server itself calls your sites from the outside, the way a visitor would. Analytics is different again: the tracking snippet in your site's pages sends one small beacon per page view.",
    ],
    normal: [
      '"live" in the top bar.',
      "Every host row says OK and was seen a few seconds ago.",
    ],
    worry: [
      '"reconnecting" or "offline" in the top bar: this page lost its live stream (the data on screen stops moving).',
      "A host that is STALE or OFFLINE: its agent stopped reporting.",
    ],
    act: [
      "For the top bar: reload the page; if it persists, check that sre.infraege.ru itself is up.",
      "For a silent host: see the triage lesson (last one).",
    ],
  },
  {
    id: "daily-check",
    title: "The one-minute daily check",
    summary:
      'The line at the top of Monitoring sums everything up: the worst finding first, then the facts it is based on. If it says "All good", you are done.',
    body: [
      "You do not need to read every number every day. The summary line at the top of the Monitoring view does that for you: it assesses every host, every uptime target and the error log with the same rules the rest of the dashboard uses, and names the worst finding first.",
      'The words matter more than the colors. "All good" means nothing crossed a threshold. "Watch" means something is heading the wrong way and deserves a look today. "Problem" means something needs attention now. "No data yet" means there is nothing to judge (no hosts, or the page is still loading).',
      "A daily routine that takes a minute: read the summary line; glance at the HOSTS rows (all OK, values not colored); look at UPTIME (UP, certificate not close to expiry); scroll EVENTS for anything red. Once a week, open each host and look at the disk trend and the containers list.",
    ],
    normal: [
      '"All good: 2/2 hosts reporting, infraege.ru up (…), max disk …, no errors in the last hour".',
    ],
    worry: [
      '"Watch: …" — read what it names, and open that host or target.',
      '"Problem: …" — act on it now; the lesson for that signal says how.',
    ],
    act: [
      'Click the "?" next to any block to see what it means and jump to its lesson.',
    ],
  },
  {
    id: "status",
    title: "STATUS, HOSTS and host states",
    summary:
      "A host is OK while its agent reports (last data within 45 s), STALE after 45 s of silence and OFFLINE after 5 minutes. The state only says whether data arrives, not whether the values are good.",
    body: [
      'The STATUS strip counts your hosts by state and averages CPU and memory across them. "probes up/down" counts uptime targets.',
      'A host row in HOSTS shows, left to right: the state, the name and how long the machine has been running ("up 3d 04h"), then four current values with a one-hour sparkline — CPU, memory (MEM), the fullest disk and network traffic (NET) — and when data last arrived.',
      'State is about freshness only. OK: data within the last 45 s. STALE: 45 s to 5 min of silence — often a network hiccup or an agent restart. OFFLINE: over 5 min — the agent or the whole machine is down, or the tunnel is blocked. NEW: the host was added but has never reported. A value that is not fresh is shown as "—" rather than an old number.',
      'Click a row to open the host: large one-hour charts and the per-disk, per-container, per-interface, check and event lists. Colored values and the words "watch"/"problem" next to them come from the thresholds described in the following lessons.',
    ],
    normal: [
      'All hosts OK, "seen" a few seconds ago; uptime counts up steadily.',
    ],
    worry: [
      "STALE that does not return to OK within a minute or two.",
      "OFFLINE.",
      "Uptime that suddenly reset to minutes: the machine rebooted (planned or not).",
    ],
    act: [
      "OFFLINE: can you reach the site it serves? Log in over SSH and run `systemctl status smotryashchiy-agent` and `journalctl -u smotryashchiy-agent -n 50`.",
      "An unexpected reboot: check `last -x | head` and the provider's console.",
    ],
    thresholds:
      "watch: STALE (no data > 45 s), problem: OFFLINE (no data > 5 min)",
  },
  {
    id: "cpu",
    title: "CPU",
    summary:
      "CPU is the share of time the processor was busy, across all its cores. It is judged on the average of the last 5 minutes, so short bursts are normal and not colored.",
    body: [
      "CPU % is how much of the last interval the processors spent working instead of idling, summed over all cores and scaled to 0–100 %. 50 % on a 2-core machine means one core's worth of work.",
      "Short spikes are normal: a deploy, a backup, a page build, a burst of visitors. That is why the assessment uses the mean of the last five minutes, not the latest sample. What matters is sustained load: a line that stays high means the machine is saturated and requests will queue up.",
      "The row shows the current value; the host detail shows the last hour. Compare it with the load chart (next lesson) — together they tell you whether the machine is busy or overloaded.",
    ],
    normal: [
      "A web server with modest traffic idles at a few to ~20 %, with spikes during deploys and nightly backups.",
    ],
    worry: [
      "A flat line near the top for many minutes.",
      "CPU climbing steadily day after day without more traffic (a runaway process).",
    ],
    act: [
      "Open the host and look at CONTAINERS — which one has the high CPU?",
      "On the server: `top` or `docker stats` to find the process.",
      "If it is expected load, the machine may need more cores.",
    ],
    thresholds: `mean of the last 5 min — ${pct(t.cpu)}`,
  },
  {
    id: "load",
    title: "Load average and cores",
    summary:
      "Load is the number of tasks running or waiting for a CPU, averaged over 1, 5 and 15 minutes. Divided by the number of CPUs it tells you whether the machine keeps up: below 1 per CPU it does.",
    body: [
      "The load average counts processes that are running or waiting to run (plus some waiting for the disk), smoothed over 1, 5 and 15 minutes. It is the length of the queue in front of the processors.",
      "A raw load number means nothing on its own: load 2 is a busy but fine 4-core machine and an overloaded 1-core one. So the dashboard divides the 5-minute load by the CPU count the agent reports. Below 1.0 per CPU there is spare capacity; around 1.0 every core is busy; above 1.0 work is waiting.",
      "Read the three lines together: 1m rising above 15m means load is increasing right now; 1m below 15m means it is calming down.",
    ],
    normal: ["Well below 1.0 per CPU, e.g. 0.2 on 2 CPUs."],
    worry: [
      "Above 1.0 per CPU for a long time.",
      "High load while CPU is low: processes are waiting for the disk (slow or failing disk, heavy I/O).",
    ],
    act: [
      "Same as for CPU: find the busy container or process.",
      "High load with low CPU: check disk activity with `iostat -x 5` or `iotop`.",
    ],
    thresholds: `5-minute load per CPU — watch ≥ ${t.loadPerCpu.watch.toFixed(1)}, problem ≥ ${t.loadPerCpu.problem.toFixed(1)} (unknown for agents older than v0.2.6, which do not report the CPU count)`,
  },
  {
    id: "memory",
    title: "Memory and swap",
    summary:
      "Memory is the RAM in use (what the system cannot hand out right away). Swap is disk space used as overflow when RAM runs short; some swap is fine, growing swap means memory pressure.",
    body: [
      'MEM is used memory as a share of total RAM, where "used" means what is not available to new programs. Linux uses spare RAM as a disk cache and gives it back on demand, so that cache is not counted as used.',
      "Swap is a file or partition on disk that the system uses when RAM is short. Disk is thousands of times slower than RAM, so a machine that swaps a lot becomes slow. A little swap in use is normal: the system moves rarely touched memory out of the way. A host without swap reports 0 %.",
      "If memory runs out completely, the kernel's OOM killer ends a process — often a database or the application — which shows up in EVENTS as a kernel message.",
    ],
    normal: [
      "Memory stable over hours (up and down with traffic, but no steady climb).",
      "Swap near 0 % and not growing.",
    ],
    worry: [
      "Memory creeping up for days: a memory leak.",
      "Swap growing while memory is high.",
      '"Out of memory: Killed process" in EVENTS.',
    ],
    act: [
      "Open CONTAINERS: which container uses the most memory, and does it keep growing?",
      "Restart a leaking service as a stopgap; fix or limit it for good.",
      "If usage is legitimately high, the machine needs more RAM.",
    ],
    thresholds: `memory — ${pct(t.memory)}; swap — ${pct(t.swap)}`,
  },
  {
    id: "disk",
    title: "Disk space",
    summary:
      "Disk is the used share of each real filesystem. It usually grows slowly (logs, backups, images, data); a full disk breaks databases and deploys, so act well before 100 %.",
    body: [
      "The row shows the fullest mount; the host detail lists every real filesystem with used and total size. Virtual filesystems (tmpfs, overlay) are not listed.",
      "Disk usage mostly moves in one direction. Typical sources of growth on a web server: container images from old releases, backups, logs, database files and uploaded files. The trend matters more than today's number: 60 % that grew 10 points in a week is more urgent than a flat 75 %.",
      "When a disk is full, the database cannot write, deploys fail to unpack, and logs stop. Some filesystems also slow down badly when nearly full.",
    ],
    normal: ["Well below 80 %, growing slowly and predictably."],
    worry: [
      "Above 80 %.",
      "A sudden jump (a runaway log or a dump left behind).",
    ],
    act: [
      "On the server: `df -h` and `du -xh / --max-depth=2 | sort -h | tail` to find what grew.",
      "Docker: `docker system df`; old images can go (`docker image prune`). infraege.ru deploys already keep only the three newest releases.",
      "Logs: journald is capped by its own config; a runaway application log belongs in its logging settings.",
    ],
    thresholds: `fullest mount — ${pct(t.disk)}`,
  },
  {
    id: "network",
    title: "Network traffic",
    summary:
      "NET is bytes per second received plus sent, over all interfaces. It is not assessed: what is normal depends entirely on your traffic, so learn your own pattern.",
    body: [
      "The agent reads byte counters for every network interface and the dashboard turns them into rates: rx is received, tx is sent. The row shows the sum; the detail splits rx and tx and lists the busiest interfaces.",
      "On a Docker host you will see many interfaces: the public one (e.g. ens3 or eth0) carries real traffic, docker0/br-… and veth… are internal bridges between containers.",
      "There is no universal threshold, so network is never colored. Learn your normal shape instead: daily visitor peaks, nightly backup transfers, a spike on every deploy while images are pulled.",
    ],
    normal: [
      "A repeating daily pattern; spikes that match deploys and backups.",
    ],
    worry: [
      "Sustained high tx with no visitors: the server may be sending something it should not (misconfigured service, compromise).",
      "Big rx bursts together with fail2ban bans: a scan or an attack.",
    ],
    act: [
      "Match spikes with EVENTS at the same time.",
      "On the server: `ss -tunap` shows who is connected; `iftop` shows live traffic per peer.",
    ],
  },
  {
    id: "containers",
    title: "Containers",
    summary:
      "Each running Docker container with its image, CPU and memory. Only containers with fresh data are listed, so one that stopped disappears.",
    body: [
      "On a Docker host the agent reads each running container's CPU and memory. The image column tells you which version runs; on infraege.ru it ends with the release SHA, so after a deploy you can check the new release is really running.",
      "Container CPU is a share of the whole machine. Container memory percent is relative to the container's memory limit, or to the host's RAM when there is no limit.",
      "A container that exits stops reporting and drops out of the list within five minutes. A container that is missing when it should be there is itself the signal.",
    ],
    normal: [
      "The expected set (for infraege.ru: api, web, nginx, postgres), all with the current release image.",
    ],
    worry: [
      "A container missing from the list.",
      "Memory of one container climbing steadily.",
      "An old image after a deploy.",
    ],
    act: [
      "On the server: `docker ps -a` (is it restarting or exited?) and `docker logs --tail 100 <name>`.",
      "Its lines also appear in EVENTS under [container-name].",
    ],
    thresholds: `container memory — ${pct(t.containerMemory)}`,
  },
  {
    id: "checks",
    title: "Checks and fail2ban",
    summary:
      "Checks are yes/no health signals reported by the agent. Today they are fail2ban jails: OK plus how many addresses are currently banned for attacking the server.",
    body: [
      "A check has a status (ok, warn or critical) and optional details shown next to it. The agent reports one check per fail2ban jail.",
      "fail2ban watches logs for repeated failed logins or rate-limit hits and bans the source address in the firewall for a while; repeat offenders get longer bans. `currently_banned` is how many addresses are banned right now.",
      "Any server on the public internet is scanned and attacked constantly, mostly by bots guessing SSH passwords. A few to a few dozen bans is normal background noise and means fail2ban is doing its job — it is not a sign of trouble by itself.",
    ],
    normal: [
      "Every check OK; `currently_banned` somewhere between 0 and a few dozen.",
    ],
    worry: [
      "A check that is not OK.",
      "Bans jumping to hundreds: a larger attack (still usually handled).",
      "A jail missing: fail2ban stopped.",
    ],
    act: [
      "On the server: `fail2ban-client status sshd`.",
      "Check that SSH login still works for you (you might be banned yourself after mistyping a password).",
    ],
    thresholds: "watch: status warn, problem: critical or fail",
  },
  {
    id: "events",
    title: "Events and log levels",
    summary:
      'EVENTS is the combined log of your hosts: the system journal, container output and fail2ban. Errors are what to look for; "warn" often only means a program wrote to its error stream.',
    body: [
      "Every line has a time, a level, the host and its source in brackets: a systemd unit, a container or a fail2ban jail. The filter at the top narrows it to one source. Routine health-check requests are dropped so they do not bury real lines.",
      'Levels come from where the line came from. Journal lines carry a priority (error, warning, info). Container lines carry no priority at all: anything a container writes to its standard output is "info", anything written to its error stream is "warn". Many programs write routine messages to the error stream (Postgres checkpoints, web servers shutting down cleanly), so a container "warn" is often harmless.',
      "The dashboard counts error and critical events of the last hour — that is the number to care about. Firewall packet logging is off on your hosts, so blocked port scans no longer flood this list.",
      "One kind of error line is deliberately not counted: sshd saying `maximum authentication attempts exceeded … [preauth]` and similar lines ending in `[preauth]`. They mean a connection was refused before anyone logged in. With password login enabled, bots try to guess the root password around the clock; `MaxAuthTries` cuts each attempt short and fail2ban bans the address, which the CHECKS block reports as `currently_banned`. These lines stay in EVENTS as ERROR, and the lesson\'s live values show how many were set aside. An sshd error without `[preauth]` still counts.",
    ],
    normal: [
      "Mostly INFO; a few WARN from containers during deploys or restarts; no ERROR.",
    ],
    worry: [
      "Any ERROR or CRITICAL, especially repeated.",
      "The same WARN repeating many times per minute.",
      'Kernel lines about the disk, memory ("Out of memory") or file systems.',
    ],
    act: [
      "Filter by the source and read the lines around it.",
      'On the server: `journalctl -u <unit> --since "1 hour ago"` or `docker logs --since 1h <container>`.',
    ],
    thresholds: `error or critical events in the last hour — watch ≥ ${t.errorsPerHour.watch}, problem ≥ ${t.errorsPerHour.problem}`,
  },
  {
    id: "uptime",
    title: "Uptime: is the site reachable?",
    summary:
      "The server calls each target from outside every interval, like a visitor would: UP or DOWN, how long the answer took, and how many days the TLS certificate has left.",
    body: [
      "Host metrics tell you how a machine feels; uptime tells you what visitors see. Each target is probed on its interval (60 s by default): HTTP targets must answer with a success status, TCP targets must accept a connection, TLS targets must present a valid certificate.",
      "LATENCY is the time until the response headers arrive, measured from the monitoring server, so it includes the network between the two machines. The sparkline shows the last hour; gaps are failed probes.",
      '"TLS 46d" is how long the certificate stays valid. Certificates from Let\'s Encrypt last 90 days and renew automatically around 30 days before expiry, so the number should never go much below 30. If it keeps falling, renewal is broken.',
    ],
    normal: ["UP, latency a few hundred ms or less, TLS above 30 days."],
    worry: [
      "DOWN, even briefly (check EVENTS and the host at that time).",
      "Latency rising over days.",
      "TLS going below 30 days.",
    ],
    act: [
      "DOWN: open the site yourself; check the host row and its CONTAINERS (is nginx or the app missing?).",
      "TLS low: on the server, `certbot certificates` and `systemctl list-timers | grep certbot`.",
    ],
    thresholds: `problem: DOWN or TLS ≤ ${t.tlsDays.problem} days; watch: latency ≥ ${t.latencyMs.watch / 1000} s, TLS ≤ ${t.tlsDays.watch} days, or no recent result`,
  },
  {
    id: "analytics",
    title: "Analytics: visitors and pages",
    summary:
      "Pageviews count page loads; visitors count distinct anonymous visitors per day; top pages and referrers show where people go and where they came from. Days are UTC days.",
    body: [
      "The tracking snippet sends one small beacon per page view with the page path and where the visitor came from. No cookies are set and nothing identifies a person.",
      "A visitor is estimated from a hash of the address and browser with a secret that changes every day. So one person is counted once per day, but the same person tomorrow is a new visitor, and several people behind one office or mobile network may count as one. Treat visitors as an estimate, pageviews as exact.",
      "Top referrers list the domains visitors came from (search engines, links on other sites); visits typed directly or from apps have no referrer. Known bots and crawlers are not counted.",
      '"today" is a UTC day: in Moscow (UTC+3) it starts at 03:00 local time.',
    ],
    normal: [
      "A steady pattern that follows your audience (school days vs weekends for an exam-prep site).",
    ],
    worry: [
      "Zero pageviews on a day with real traffic: the snippet stopped loading (a site change or Content-Security-Policy).",
      "A single page or referrer with an absurd count: spam or a misbehaving client.",
    ],
    act: [
      "Open the site in a browser with developer tools: is `track.js` loaded and does the beacon get 204?",
    ],
  },
  {
    id: "triage",
    title: "When something is wrong",
    summary:
      "A short checklist from symptom to cause: start at the summary line, check reachability, then the host, then its containers and events.",
    body: [
      "1. Read the summary line: it names the worst finding.",
      "2. Is the site reachable? Look at UPTIME. If DOWN, visitors are affected — that comes first.",
      "3. Is the host reporting? OFFLINE means the machine, its network or the agent is down; everything else on the page is then stale for that host.",
      "4. Which resource is short? Colored CPU, memory, disk or load point at the cause; open the host and look at the charts for when it started.",
      "5. Which service? CONTAINERS shows who uses the resource or who is missing.",
      "6. What did it say? EVENTS filtered to that source around the time it started.",
      "7. Fix, then watch the same numbers return to normal. If it was a real incident, note what happened and what you changed.",
    ],
    normal: ['Most days: step 1 says "All good" and you are done.'],
    worry: ["Anything the summary names as a problem."],
    act: [
      "Server access: `ssh root@<host>`.",
      'Useful commands: `systemctl --failed`, `docker ps -a`, `df -h`, `free -h`, `journalctl -p err --since "1 hour ago"`.',
      "The infraege.ru production runbook (in its repository) covers deploys, rollbacks and backups.",
    ],
  },
];

export const LESSON_BY_ID: Record<LessonId, Lesson> = Object.fromEntries(
  LESSONS.map((l) => [l.id, l]),
) as Record<LessonId, Lesson>;
