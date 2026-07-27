"use client";

import { useCallback, useEffect, useState } from "react";
import { OverviewDashboard } from "@/components/OverviewDashboard";
import { api } from "@/lib/api";
import type { OverviewResponse } from "@/lib/types";
import {
  applyQuoteToOverview,
  useMarketStream,
  type QuoteUpdate,
} from "@/lib/useMarketStream";
import { useTieredPolling } from "@/lib/useTieredPolling";

export default function OverviewPage() {
  const [data, setData] = useState<OverviewResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const loadOverview = useCallback(async () => {
    const response = await api.get<OverviewResponse>("/api/v1/overview");
    setData(response);
    setError(null);
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        await loadOverview();
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to load overview");
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    void load();
    return () => {
      cancelled = true;
    };
  }, [loadOverview]);

  // REST remains reconciliation; SSE quotes merge when the stream is up.
  useTieredPolling(true, "account", loadOverview);

  const onQuote = useCallback((quote: QuoteUpdate) => {
    setData((prev) => (prev ? applyQuoteToOverview(prev, quote) : prev));
  }, []);
  useMarketStream(true, onQuote);

  async function handleRefresh() {
    setError(null);
    setRefreshing(true);
    try {
      await loadOverview();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load overview");
    } finally {
      setRefreshing(false);
    }
  }

  if (loading) {
    return <p>Loading overview…</p>;
  }

  if (error && !data) {
    return <p role="alert">{error}</p>;
  }

  if (!data) {
    return <p role="alert">Overview data unavailable</p>;
  }

  return (
    <>
      {error ? <p className="alert" role="alert">{error}</p> : null}
      <OverviewDashboard
        data={data}
        onRefresh={() => void handleRefresh()}
        refreshing={refreshing}
      />
    </>
  );
}
