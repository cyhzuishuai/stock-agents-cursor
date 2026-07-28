import { describe, expect, it } from "vitest";
import { formatStartedAt } from "./datetime";

describe("formatStartedAt", () => {
  it("formats RFC3339 to local YYYY-MM-DD HH:mm", () => {
    const out = formatStartedAt("2026-07-28T09:05:00Z");
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/);
  });

  it("returns em dash for empty", () => {
    expect(formatStartedAt("")).toBe("—");
    expect(formatStartedAt(undefined)).toBe("—");
  });
});
