import { describe, expect, it } from "vitest";
import {
  formatOpenDuration,
  formatUsMarketStatus,
  getUsMarketSnapshot,
} from "./usMarketClock";

/** Construct an Instant that is this wall clock in America/New_York. */
function atNy(
  year: number,
  month: number,
  day: number,
  hour: number,
  minute: number,
  second = 0,
): Date {
  const utcGuess = Date.UTC(year, month - 1, day, hour + 4, minute, second);
  const fmt = new Intl.DateTimeFormat("en-US", {
    timeZone: "America/New_York",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
  for (let delta = -5; delta <= 5; delta++) {
    const cand = new Date(utcGuess + delta * 3600_000);
    const parts: Record<string, string> = {};
    for (const p of fmt.formatToParts(cand)) {
      if (p.type !== "literal") parts[p.type] = p.value;
    }
    if (
      Number(parts.year) === year &&
      Number(parts.month) === month &&
      Number(parts.day) === day &&
      Number(parts.hour) === hour &&
      Number(parts.minute) === minute &&
      Number(parts.second) === second
    ) {
      return cand;
    }
  }
  throw new Error("failed to resolve NY wall time");
}

describe("getUsMarketSnapshot", () => {
  it("marks weekend as closed", () => {
    // 2026-07-25 is Saturday
    const snap = getUsMarketSnapshot(atNy(2026, 7, 25, 12, 0));
    expect(snap.phase).toBe("weekend");
    expect(formatUsMarketStatus(snap)).toContain("weekend");
  });

  it("marks pre-open weekday", () => {
    // 2026-07-27 Monday 09:00 ET
    const snap = getUsMarketSnapshot(atNy(2026, 7, 27, 9, 0));
    expect(snap.phase).toBe("pre_open");
    expect(formatUsMarketStatus(snap)).toContain("pre-market");
  });

  it("marks open and elapsed duration", () => {
    // 2026-07-27 Monday 11:00 ET → open 1h 30m
    const snap = getUsMarketSnapshot(atNy(2026, 7, 27, 11, 0));
    expect(snap.phase).toBe("open");
    expect(snap.openForMs).toBe(90 * 60_000);
    expect(formatUsMarketStatus(snap)).toBe("Open for 1h 30m");
  });

  it("marks after close", () => {
    const snap = getUsMarketSnapshot(atNy(2026, 7, 27, 16, 0));
    expect(snap.phase).toBe("after_close");
    expect(formatUsMarketStatus(snap)).toContain("after hours");
  });
});

describe("formatOpenDuration", () => {
  it("formats minutes only under one hour", () => {
    expect(formatOpenDuration(25 * 60_000)).toBe("25m");
  });
});
