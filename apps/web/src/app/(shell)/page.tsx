"use client";

import { useEffect, useState } from "react";
import { OverviewDashboard } from "@/components/OverviewDashboard";
import { api } from "@/lib/api";
import type { OverviewResponse } from "@/lib/types";

export default function OverviewPage() {
  const [data, setData] = useState<OverviewResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const response = await api.get<OverviewResponse>("/api/v1/overview");
        if (!cancelled) {
          setData(response);
        }
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
  }, []);

  if (loading) {
    return <p>Loading overview…</p>;
  }

  if (error) {
    return <p role="alert">{error}</p>;
  }

  if (!data) {
    return <p role="alert">Overview data unavailable</p>;
  }

  return <OverviewDashboard data={data} />;
}
