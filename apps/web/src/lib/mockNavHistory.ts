export type NavHistoryRange = "1H" | "1D" | "1W" | "1M";

export interface NavHistoryPoint {
  ts: string;
  nav: number;
}

const HOUR_MS = 60 * 60 * 1000;
const MONTH_HOURS = 30 * 24;

const RANGE_HOURS: Record<NavHistoryRange, number> = {
  "1H": 1,
  "1D": 24,
  "1W": 7 * 24,
  "1M": MONTH_HOURS,
};

/** Deterministic LCG — same seed → same walk. */
function nextUnit(seed: number): { value: number; seed: number } {
  const next = (seed * 1664525 + 1013904223) >>> 0;
  return { value: next / 0x100000000, seed: next };
}

function floorToHour(date: Date): Date {
  const d = new Date(date.getTime());
  d.setUTCMinutes(0, 0, 0);
  return d;
}

/**
 * Hourly mock NAV series covering 1M, ending at `now` (floored to hour).
 * Last point equals `anchorNav`.
 */
export function buildMockNavHistory(
  anchorNav: number,
  now: Date = new Date(),
): NavHistoryPoint[] {
  const end = floorToHour(now);
  const points: NavHistoryPoint[] = [];
  let seed = 0xC0FFEE ^ Math.floor(anchorNav);

  // Walk forward from oldest, then rescale so the last point is anchorNav.
  const raw: number[] = [];
  let level = 1;
  for (let i = 0; i <= MONTH_HOURS; i++) {
    const step = nextUnit(seed);
    seed = step.seed;
    level *= 1 + (step.value - 0.5) * 0.004;
    raw.push(level);
  }

  const last = raw[raw.length - 1] || 1;
  for (let i = 0; i <= MONTH_HOURS; i++) {
    const ts = new Date(end.getTime() - (MONTH_HOURS - i) * HOUR_MS);
    points.push({
      ts: ts.toISOString(),
      nav: (raw[i]! / last) * anchorNav,
    });
  }

  points[points.length - 1] = {
    ts: end.toISOString(),
    nav: anchorNav,
  };

  return points;
}

export function sliceNavHistoryByRange(
  series: NavHistoryPoint[],
  range: NavHistoryRange,
): NavHistoryPoint[] {
  if (series.length === 0) return [];
  const hours = RANGE_HOURS[range];
  const count = hours + 1;
  if (series.length <= count) return series.slice();
  return series.slice(series.length - count);
}
