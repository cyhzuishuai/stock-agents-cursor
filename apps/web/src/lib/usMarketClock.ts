/** US equity regular session clock (America/New_York, Mon–Fri 09:30–16:00). Holidays not modeled. */

export type UsMarketSnapshot = {
  etLabel: string;
  phase: "open" | "pre_open" | "after_close" | "weekend";
  /** Milliseconds since 09:30 ET when phase === "open"; otherwise 0. */
  openForMs: number;
};

const NY = "America/New_York";
const OPEN_MINUTES = 9 * 60 + 30;
const CLOSE_MINUTES = 16 * 60;

function nyParts(now: Date) {
  const fmt = new Intl.DateTimeFormat("en-US", {
    timeZone: NY,
    weekday: "short",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
  const map: Record<string, string> = {};
  for (const p of fmt.formatToParts(now)) {
    if (p.type !== "literal") map[p.type] = p.value;
  }
  return map;
}

function isWeekend(weekday: string): boolean {
  return weekday === "Sat" || weekday === "Sun";
}

export function getUsMarketSnapshot(now: Date = new Date()): UsMarketSnapshot {
  const parts = nyParts(now);
  const weekday = parts.weekday ?? "";
  const hour = Number(parts.hour);
  const minute = Number(parts.minute);
  const second = Number(parts.second);
  const minutes = hour * 60 + minute + second / 60;

  const etLabel = new Intl.DateTimeFormat("en-US", {
    timeZone: NY,
    weekday: "short",
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: true,
  }).format(now);

  if (isWeekend(weekday)) {
    return { etLabel, phase: "weekend", openForMs: 0 };
  }
  if (minutes < OPEN_MINUTES) {
    return { etLabel, phase: "pre_open", openForMs: 0 };
  }
  if (minutes >= CLOSE_MINUTES) {
    return { etLabel, phase: "after_close", openForMs: 0 };
  }

  const openForMs = Math.max(0, Math.round((minutes - OPEN_MINUTES) * 60_000));
  return { etLabel, phase: "open", openForMs };
}

export function formatOpenDuration(ms: number): string {
  const totalSec = Math.floor(ms / 1000);
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  if (h <= 0) return `${m}m`;
  return `${h}h ${m}m`;
}

export function formatUsMarketStatus(snap: UsMarketSnapshot): string {
  switch (snap.phase) {
    case "open":
      return `Open for ${formatOpenDuration(snap.openForMs)}`;
    case "pre_open":
      return "Closed — pre-market";
    case "after_close":
      return "Closed — after hours";
    case "weekend":
      return "Closed — weekend";
  }
}
