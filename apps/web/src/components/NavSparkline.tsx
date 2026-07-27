import type { NavHistoryPoint } from "@/lib/mockNavHistory";

const WIDTH = 240;
const HEIGHT = 64;
const PAD = 4;

function buildPoints(series: { nav: number }[]): string {
  if (series.length === 0) return "";

  const values = series.map((p) => p.nav);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  const innerW = WIDTH - PAD * 2;
  const innerH = HEIGHT - PAD * 2;

  return series
    .map((point, index) => {
      const x =
        series.length === 1
          ? WIDTH / 2
          : PAD + (index / (series.length - 1)) * innerW;
      const y = PAD + innerH - ((point.nav - min) / span) * innerH;
      return `${x},${y}`;
    })
    .join(" ");
}

export function NavSparkline({
  series,
}: {
  series: NavHistoryPoint[] | { nav: number }[];
}) {
  const points = buildPoints(series);

  return (
    <svg
      className="nav-sparkline"
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      width={WIDTH}
      height={HEIGHT}
      role="img"
      aria-label="NAV history sparkline"
    >
      {points ? (
        <polyline
          className="nav-sparkline__line"
          fill="none"
          points={points}
        />
      ) : (
        <text
          className="nav-sparkline__empty"
          x={WIDTH / 2}
          y={HEIGHT / 2}
          textAnchor="middle"
          dominantBaseline="middle"
        >
          No data
        </text>
      )}
    </svg>
  );
}
