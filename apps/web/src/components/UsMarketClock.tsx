"use client";

import { useEffect, useState } from "react";
import {
  formatUsMarketStatus,
  getUsMarketSnapshot,
  type UsMarketSnapshot,
} from "@/lib/usMarketClock";

export function UsMarketClock() {
  const [snap, setSnap] = useState<UsMarketSnapshot | null>(null);

  useEffect(() => {
    const tick = () => setSnap(getUsMarketSnapshot(new Date()));
    tick();
    const id = window.setInterval(tick, 1000);
    return () => window.clearInterval(id);
  }, []);

  if (!snap) {
    return (
      <section className="market-clock" aria-label="US market clock">
        <p className="market-clock__line empty-state">Loading US Eastern time…</p>
      </section>
    );
  }

  const open = snap.phase === "open";

  return (
    <section
      className={open ? "market-clock market-clock--open" : "market-clock"}
      aria-label="US market clock"
    >
      <p className="market-clock__line">
        <span className="market-clock__label">US Eastern</span>
        <span className="market-clock__time">{snap.etLabel} ET</span>
      </p>
      <p className="market-clock__status">{formatUsMarketStatus(snap)}</p>
    </section>
  );
}
