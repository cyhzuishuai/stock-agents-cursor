import type { NavHistoryPoint, NavHistoryRange } from "@/lib/mockNavHistory";

const WIDTH = 640;
const HEIGHT = 220;
const PAD_L = 48;
const PAD_R = 12;
const PAD_T = 12;
const PAD_B = 28;

function compactUsd(n: number): string {
  const abs = Math.abs(n);
  if (abs >= 1_000_000) {
    return `$${(n / 1_000_000).toFixed(abs >= 10_000_000 ? 0 : 1)}M`;
  }
  if (abs >= 1000) {
    return `$${Math.round(n / 1000)}k`;
  }
  return `$${Math.round(n)}`;
}

function formatXLabel(ts: string, range: NavHistoryRange): string {
  const d = new Date(ts);
  if (range === "1H" || range === "1D") {
    return new Intl.DateTimeFormat("en-US", {
      hour: "numeric",
      minute: range === "1H" ? "2-digit" : undefined,
      hour12: true,
      timeZone: "UTC",
    }).format(d);
  }
  if (range === "1W") {
    return new Intl.DateTimeFormat("en-US", {
      weekday: "short",
      hour: "numeric",
      hour12: true,
      timeZone: "UTC",
    }).format(d);
  }
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  }).format(d);
}

function layout(series: NavHistoryPoint[]) {
  const values = series.map((p) => p.nav);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const pad = (max - min) * 0.08 || Math.abs(max) * 0.01 || 1;
  const yMin = min - pad;
  const yMax = max + pad;
  const span = yMax - yMin || 1;
  const innerW = WIDTH - PAD_L - PAD_R;
  const innerH = HEIGHT - PAD_T - PAD_B;

  const coords = series.map((point, index) => {
    const x =
      series.length === 1
        ? PAD_L + innerW / 2
        : PAD_L + (index / (series.length - 1)) * innerW;
    const y = PAD_T + innerH - ((point.nav - yMin) / span) * innerH;
    return { x, y, point };
  });

  return { coords, yMin, yMax, span, innerW, innerH };
}

export function NavHistoryChart({
  series,
  range,
}: {
  series: NavHistoryPoint[];
  range: NavHistoryRange;
}) {
  if (series.length === 0) {
    return (
      <div className="nav-chart nav-chart--empty" role="img" aria-label="NAV history chart">
        <p className="empty-state">No data</p>
      </div>
    );
  }

  const { coords, yMin, yMax } = layout(series);
  const linePoints = coords.map((c) => `${c.x},${c.y}`).join(" ");
  const first = coords[0]!;
  const last = coords[coords.length - 1]!;
  const areaPoints = [
    `${first.x},${HEIGHT - PAD_B}`,
    ...coords.map((c) => `${c.x},${c.y}`),
    `${last.x},${HEIGHT - PAD_B}`,
  ].join(" ");

  const yTicks = [yMax, (yMin + yMax) / 2, yMin];
  const xIdx =
    series.length === 1
      ? [0]
      : [0, Math.floor((series.length - 1) / 2), series.length - 1];

  return (
    <svg
      className="nav-chart"
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      role="img"
      aria-label="NAV history chart"
    >
      <defs>
        <linearGradient id="nav-chart-fill" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" className="nav-chart__fill-stop--top" />
          <stop offset="100%" className="nav-chart__fill-stop--bottom" />
        </linearGradient>
      </defs>

      {yTicks.map((v, i) => {
        const y =
          PAD_T +
          ((HEIGHT - PAD_T - PAD_B) * i) / Math.max(yTicks.length - 1, 1);
        return (
          <g key={`y-${i}`}>
            <line
              className="nav-chart__grid"
              x1={PAD_L}
              x2={WIDTH - PAD_R}
              y1={y}
              y2={y}
            />
            <text
              className="nav-chart__ylabel"
              x={PAD_L - 8}
              y={y}
              textAnchor="end"
              dominantBaseline="middle"
            >
              {compactUsd(v)}
            </text>
          </g>
        );
      })}

      <polygon
        className="nav-chart__area"
        data-testid="nav-chart-area"
        points={areaPoints}
        fill="url(#nav-chart-fill)"
      />
      <polyline
        className="nav-chart__line"
        data-testid="nav-chart-line"
        fill="none"
        points={linePoints}
      />

      {xIdx.map((idx) => {
        const c = coords[idx]!;
        return (
          <text
            key={`x-${idx}`}
            className="nav-chart__xlabel"
            x={c.x}
            y={HEIGHT - 8}
            textAnchor={
              idx === 0 ? "start" : idx === series.length - 1 ? "end" : "middle"
            }
          >
            {formatXLabel(c.point.ts, range)}
          </text>
        );
      })}
    </svg>
  );
}
