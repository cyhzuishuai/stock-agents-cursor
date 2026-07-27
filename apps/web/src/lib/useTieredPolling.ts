"use client";

import { useEffect, useRef } from "react";
import {
  getUsMarketSnapshot,
  type UsMarketSnapshot,
} from "./usMarketClock";

export type Tier = "account" | "orders";

export function pollIntervalMs(
  tier: Tier,
  phase: UsMarketSnapshot["phase"],
): number | null {
  if (tier === "orders") {
    return 8_000;
  }
  if (phase === "open") {
    return 20_000;
  }
  return 180_000;
}

export function useTieredPolling(
  enabled: boolean,
  tier: Tier,
  tick: () => void | Promise<void>,
): void {
  const tickRef = useRef(tick);
  tickRef.current = tick;

  useEffect(() => {
    if (!enabled) {
      return;
    }

    let intervalId: ReturnType<typeof setInterval> | null = null;
    let phaseCheckId: ReturnType<typeof setInterval> | null = null;
    let currentMs: number | null = null;

    const clearPoll = () => {
      if (intervalId != null) {
        clearInterval(intervalId);
        intervalId = null;
      }
      currentMs = null;
    };

    const startPoll = () => {
      if (document.hidden) {
        return;
      }

      const { phase } = getUsMarketSnapshot();
      const ms = pollIntervalMs(tier, phase);
      if (ms == null) {
        clearPoll();
        return;
      }

      if (intervalId != null && currentMs === ms) {
        return;
      }

      clearPoll();
      currentMs = ms;
      intervalId = setInterval(() => {
        if (!document.hidden) {
          void tickRef.current();
        }
      }, ms);
    };

    const onVisibilityChange = () => {
      if (document.hidden) {
        clearPoll();
      } else {
        startPoll();
      }
    };

    startPoll();
    phaseCheckId = setInterval(startPoll, 60_000);
    document.addEventListener("visibilitychange", onVisibilityChange);

    return () => {
      clearPoll();
      if (phaseCheckId != null) {
        clearInterval(phaseCheckId);
      }
      document.removeEventListener("visibilitychange", onVisibilityChange);
    };
  }, [enabled, tier]);
}
