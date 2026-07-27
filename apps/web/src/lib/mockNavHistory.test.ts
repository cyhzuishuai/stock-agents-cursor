import { describe, expect, it } from "vitest";
import {
  buildMockNavHistory,
  sliceNavHistoryByRange,
  type NavHistoryPoint,
} from "./mockNavHistory";

describe("mockNavHistory", () => {
  const now = new Date("2026-07-27T15:00:00.000Z");

  it("builds hourly series covering 1M ending at now with anchor nav", () => {
    const series = buildMockNavHistory(200000, now);

    expect(series.length).toBe(30 * 24 + 1);
    expect(series[series.length - 1]).toEqual({
      ts: "2026-07-27T15:00:00.000Z",
      nav: 200000,
    });
    expect(series[0]?.ts).toBe("2026-06-27T15:00:00.000Z");
    // Hourly spacing
    const a = Date.parse(series[0]!.ts);
    const b = Date.parse(series[1]!.ts);
    expect(b - a).toBe(60 * 60 * 1000);
  });

  it("slices by range with strict hourly points", () => {
    const series = buildMockNavHistory(200000, now);

    expect(sliceNavHistoryByRange(series, "1H")).toHaveLength(2);
    expect(sliceNavHistoryByRange(series, "1D")).toHaveLength(24 + 1);
    expect(sliceNavHistoryByRange(series, "1W")).toHaveLength(7 * 24 + 1);
    expect(sliceNavHistoryByRange(series, "1M")).toHaveLength(30 * 24 + 1);

    const oneHour = sliceNavHistoryByRange(series, "1H");
    expect(oneHour[0]?.ts).toBe("2026-07-27T14:00:00.000Z");
    expect(oneHour[1]?.nav).toBe(200000);
  });

  it("is deterministic for the same now and anchor", () => {
    const a = buildMockNavHistory(150000, now);
    const b = buildMockNavHistory(150000, now);
    expect(a).toEqual(b);
  });

  it("returns empty when series is empty", () => {
    const empty: NavHistoryPoint[] = [];
    expect(sliceNavHistoryByRange(empty, "1D")).toEqual([]);
  });
});
