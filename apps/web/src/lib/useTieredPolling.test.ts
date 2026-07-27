// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { pollIntervalMs, useTieredPolling } from "./useTieredPolling";

describe("pollIntervalMs", () => {
  it("returns account-tier intervals by market phase", () => {
    expect(pollIntervalMs("account", "open")).toBe(20_000);
    expect(pollIntervalMs("account", "weekend")).toBe(180_000);
    expect(pollIntervalMs("account", "pre_open")).toBe(180_000);
    expect(pollIntervalMs("account", "after_close")).toBe(180_000);
  });

  it("returns orders-tier interval regardless of phase", () => {
    expect(pollIntervalMs("orders", "open")).toBe(8_000);
    expect(pollIntervalMs("orders", "weekend")).toBe(8_000);
  });
});

describe("useTieredPolling", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    Object.defineProperty(document, "hidden", {
      configurable: true,
      get: () => false,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("fires tick on interval when enabled", () => {
    const tick = vi.fn();
    renderHook(() => useTieredPolling(true, "orders", tick));

    act(() => {
      vi.advanceTimersByTime(8_000);
    });
    expect(tick).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(8_000);
    });
    expect(tick).toHaveBeenCalledTimes(2);
  });

  it("does not poll when disabled", () => {
    const tick = vi.fn();
    renderHook(() => useTieredPolling(false, "orders", tick));

    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(tick).not.toHaveBeenCalled();
  });

  it("pauses while document is hidden and resumes on visible", () => {
    let hidden = false;
    Object.defineProperty(document, "hidden", {
      configurable: true,
      get: () => hidden,
    });

    const tick = vi.fn();
    renderHook(() => useTieredPolling(true, "orders", tick));

    act(() => {
      vi.advanceTimersByTime(8_000);
    });
    expect(tick).toHaveBeenCalledTimes(1);

    hidden = true;
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
      vi.advanceTimersByTime(24_000);
    });
    expect(tick).toHaveBeenCalledTimes(1);

    hidden = false;
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
      vi.advanceTimersByTime(8_000);
    });
    expect(tick).toHaveBeenCalledTimes(2);
  });
});
