"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { Series, SeriesPoint } from "@/lib/api";

// Layout, in CSS pixels.
const HEIGHT = 236;
const LEFT = 48;
const RIGHT = 12;
const TOP = 12;
const PLOT_HEIGHT = 160;
const STRIP_TOP = TOP + PLOT_HEIGHT + 10;
const STRIP_HEIGHT = 10;
const TOOLTIP_WIDTH = 220;

type State = "ok" | "partial" | "down";

function stateOf(p: SeriesPoint): State {
  if (p.up_checks === p.checks) return "ok";
  return p.up_checks === 0 ? "down" : "partial";
}

const stateStyle: Record<State, { color: string; height: number; label: string }> = {
  // Problems are taller as well as differently colored, so the strip does
  // not rely on hue alone.
  ok: { color: "var(--viz-good)", height: 4, label: "Sorunsuz" },
  partial: { color: "var(--viz-warning)", height: STRIP_HEIGHT, label: "Kısmi hata" },
  down: { color: "var(--viz-critical)", height: STRIP_HEIGHT, label: "Kesinti" },
};

/** A round upper bound and tick step for an axis that starts at zero. */
function axis(max: number): { top: number; step: number } {
  const rough = Math.max(max, 1) / 4;
  const magnitude = 10 ** Math.floor(Math.log10(rough));
  const step = [1, 2, 5, 10].map((m) => m * magnitude).find((s) => s >= rough)!;
  return { top: Math.ceil(Math.max(max, 1) / step) * step, step };
}

const ms = (n: number) => `${n.toLocaleString("tr-TR", { maximumFractionDigits: n < 10 ? 1 : 0 })} ms`;

export function LatencyChart({ series }: { series: Series }) {
  const wrapper = useRef<HTMLDivElement>(null);
  // Measured on the client; until then a placeholder of the same height is
  // rendered, which also keeps times in the viewer's zone, not the server's.
  const [width, setWidth] = useState<number | null>(null);
  const [active, setActive] = useState<number | null>(null);

  useEffect(() => {
    const el = wrapper.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const { points } = series;
  const from = new Date(series.from).getTime();
  const to = new Date(series.to).getTime();
  const bucket = series.bucket_seconds * 1000;
  const longRange = series.range_seconds > 2 * 86400;

  const geometry = useMemo(() => {
    if (width === null) return null;
    const plotWidth = Math.max(width - LEFT - RIGHT, 10);
    const x = (t: number) => LEFT + ((t - from) / (to - from)) * plotWidth;
    const { top, step } = axis(Math.max(0, ...points.map((p) => p.avg_latency_ms ?? 0)));
    const y = (v: number) => TOP + PLOT_HEIGHT - (v / top) * PLOT_HEIGHT;

    // A point sits at the middle of its bucket (the newest bucket is cut
    // short by "now").
    const centers = points.map((p) => {
      const start = new Date(p.start).getTime();
      return x(Math.min(start + bucket / 2, (start + to) / 2));
    });

    // The line breaks where there is no response time: an empty bucket or
    // one in which every check failed.
    const segments: number[][] = [];
    let current: number[] = [];
    points.forEach((p, i) => {
      const gap =
        i > 0 &&
        new Date(p.start).getTime() - new Date(points[i - 1].start).getTime() > bucket * 1.5;
      if ((gap || p.avg_latency_ms === null) && current.length > 0) {
        segments.push(current);
        current = [];
      }
      if (p.avg_latency_ms !== null) current.push(i);
    });
    if (current.length > 0) segments.push(current);

    const yTicks = [];
    for (let v = 0; v <= top + step / 2; v += step) yTicks.push(v);
    const xTicks = [0, 0.25, 0.5, 0.75, 1].map((f) => from + f * (to - from));
    const barWidth = Math.max((plotWidth * bucket) / (to - from), 1);

    return { plotWidth, x, y, centers, segments, yTicks, xTicks, barWidth };
  }, [width, points, from, to, bucket]);

  const timeLabel = (t: number) =>
    new Date(t).toLocaleString(
      "tr-TR",
      longRange
        ? { day: "numeric", month: "short" }
        : { hour: "2-digit", minute: "2-digit" },
    );
  const bucketLabel = (p: SeriesPoint) => {
    const start = new Date(p.start);
    const end = new Date(Math.min(start.getTime() + bucket, to));
    const day = start.toLocaleDateString("tr-TR", { day: "numeric", month: "short" });
    const time = (d: Date) => d.toLocaleTimeString("tr-TR", { hour: "2-digit", minute: "2-digit" });
    return `${day} ${time(start)} – ${time(end)}`;
  };

  if (points.length === 0) {
    return (
      <p className="rounded-lg border border-dashed border-black/15 p-8 text-center text-sm text-zinc-500 dark:border-white/20">
        Bu aralıkta kontrol sonucu yok.
      </p>
    );
  }

  const nearest = (clientX: number) => {
    if (!geometry || !wrapper.current) return null;
    const px = clientX - wrapper.current.getBoundingClientRect().left;
    let best = 0;
    geometry.centers.forEach((c, i) => {
      if (Math.abs(c - px) < Math.abs(geometry.centers[best] - px)) best = i;
    });
    return best;
  };

  const point = active !== null ? points[active] : null;

  return (
    <figure>
      <div
        ref={wrapper}
        className="relative outline-none focus-visible:ring-2 focus-visible:ring-[var(--viz-series)]"
        style={{ height: HEIGHT }}
        tabIndex={0}
        role="group"
        aria-label="Yanıt süresi grafiği. Sol ve sağ ok tuşlarıyla noktalar arasında gezinin."
        onPointerMove={(e) => setActive(nearest(e.clientX))}
        onPointerLeave={() => setActive(null)}
        onBlur={() => setActive(null)}
        onKeyDown={(e) => {
          if (e.key === "ArrowRight" || e.key === "ArrowLeft") {
            e.preventDefault();
            const delta = e.key === "ArrowRight" ? 1 : -1;
            setActive((i) =>
              Math.min(Math.max((i ?? (delta > 0 ? -1 : points.length)) + delta, 0), points.length - 1),
            );
          } else if (e.key === "Escape") {
            setActive(null);
          }
        }}
      >
        {geometry && (
          <svg width={width!} height={HEIGHT} className="block" aria-hidden>
            {geometry.yTicks.map((v) => (
              <g key={v}>
                <line
                  x1={LEFT}
                  x2={LEFT + geometry.plotWidth}
                  y1={geometry.y(v)}
                  y2={geometry.y(v)}
                  stroke={v === 0 ? "var(--viz-axis)" : "var(--viz-grid)"}
                  strokeWidth={1}
                />
                <text
                  x={LEFT - 8}
                  y={geometry.y(v)}
                  dy="0.32em"
                  textAnchor="end"
                  fontSize={11}
                  fill="var(--viz-muted)"
                  style={{ fontVariantNumeric: "tabular-nums" }}
                >
                  {v.toLocaleString("tr-TR")}
                </text>
              </g>
            ))}

            {geometry.segments.map((segment) => {
              const coords = segment.map(
                (i) => `${geometry.centers[i]},${geometry.y(points[i].avg_latency_ms!)}`,
              );
              const first = geometry.centers[segment[0]];
              const last = geometry.centers[segment[segment.length - 1]];
              const base = geometry.y(0);
              return (
                <g key={segment[0]}>
                  {segment.length > 1 && (
                    <path
                      d={`M${first},${base} L${coords.join(" L")} L${last},${base} Z`}
                      fill="var(--viz-series)"
                      opacity={0.1}
                    />
                  )}
                  {segment.length > 1 ? (
                    <path
                      d={`M${coords.join(" L")}`}
                      fill="none"
                      stroke="var(--viz-series)"
                      strokeWidth={2}
                      strokeLinejoin="round"
                      strokeLinecap="round"
                    />
                  ) : (
                    // A lone measurement between gaps would otherwise be invisible.
                    <circle
                      cx={first}
                      cy={geometry.y(points[segment[0]].avg_latency_ms!)}
                      r={2}
                      fill="var(--viz-series)"
                    />
                  )}
                </g>
              );
            })}

            {points.map((p, i) => {
              const style = stateStyle[stateOf(p)];
              const gap = geometry.barWidth > 5 ? 2 : geometry.barWidth > 2 ? 1 : 0;
              const start = Math.max(geometry.x(new Date(p.start).getTime()), LEFT);
              const end = Math.min(start + geometry.barWidth, LEFT + geometry.plotWidth);
              return (
                <rect
                  key={p.start}
                  x={start}
                  y={STRIP_TOP + STRIP_HEIGHT - style.height}
                  width={Math.max(end - start - gap, 1)}
                  height={style.height}
                  rx={1}
                  fill={style.color}
                  opacity={active === null || active === i ? 1 : 0.55}
                />
              );
            })}

            {geometry.xTicks.map((t, i) => (
              <text
                key={t}
                x={geometry.x(t)}
                y={HEIGHT - 6}
                textAnchor={i === 0 ? "start" : i === geometry.xTicks.length - 1 ? "end" : "middle"}
                fontSize={11}
                fill="var(--viz-muted)"
                style={{ fontVariantNumeric: "tabular-nums" }}
              >
                {timeLabel(t)}
              </text>
            ))}

            {active !== null && (
              <g>
                <line
                  x1={geometry.centers[active]}
                  x2={geometry.centers[active]}
                  y1={TOP}
                  y2={STRIP_TOP + STRIP_HEIGHT}
                  stroke="var(--viz-muted)"
                  strokeWidth={1}
                />
                {points[active].avg_latency_ms !== null && (
                  <circle
                    cx={geometry.centers[active]}
                    cy={geometry.y(points[active].avg_latency_ms!)}
                    r={5}
                    fill="var(--viz-series)"
                    stroke="var(--background)"
                    strokeWidth={2}
                  />
                )}
              </g>
            )}
          </svg>
        )}

        {geometry && point && active !== null && (
          <div
            role="status"
            className="pointer-events-none absolute top-2 z-10 rounded-md border border-black/10 bg-background px-3 py-2 text-xs shadow-md dark:border-white/15"
            style={{
              width: TOOLTIP_WIDTH,
              // Beside the crosshair, on whichever side has room.
              left:
                geometry.centers[active] + 12 + TOOLTIP_WIDTH > width!
                  ? geometry.centers[active] - 12 - TOOLTIP_WIDTH
                  : geometry.centers[active] + 12,
            }}
          >
            <div className="text-zinc-500">{bucketLabel(point)}</div>
            <div className="mt-1 flex items-center gap-2">
              <span className="h-0.5 w-3 rounded bg-[var(--viz-series)]" aria-hidden />
              <span className="text-sm font-semibold">
                {point.avg_latency_ms !== null ? ms(point.avg_latency_ms) : "Yanıt yok"}
              </span>
              <span className="text-zinc-500">ortalama</span>
            </div>
            {point.max_latency_ms !== null && (
              <div className="mt-0.5 text-zinc-500">
                en yüksek <span className="text-foreground">{ms(point.max_latency_ms)}</span>
              </div>
            )}
            <div className="mt-1 flex items-center gap-2 border-t border-black/10 pt-1 dark:border-white/15">
              <span
                className="h-2 w-2 rounded-sm"
                style={{ background: stateStyle[stateOf(point)].color }}
                aria-hidden
              />
              <span>
                <span className="font-semibold">{stateStyle[stateOf(point)].label}</span>
                <span className="text-zinc-500">
                  {" · "}
                  {point.checks} kontrol
                  {point.checks > point.up_checks && `, ${point.checks - point.up_checks} başarısız`}
                </span>
              </span>
            </div>
          </div>
        )}
      </div>

      <figcaption className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 pl-12 text-xs text-zinc-500">
        <span>Erişilebilirlik:</span>
        {(Object.keys(stateStyle) as State[]).map((s) => (
          <span key={s} className="inline-flex items-center gap-1.5">
            <span
              className="w-2 rounded-sm"
              style={{ background: stateStyle[s].color, height: stateStyle[s].height }}
              aria-hidden
            />
            {stateStyle[s].label}
          </span>
        ))}
      </figcaption>

      <details className="mt-3 text-sm">
        <summary className="cursor-pointer text-xs text-zinc-500">Tablo olarak göster</summary>
        <div className="mt-2 max-h-72 overflow-auto rounded-lg border border-black/10 dark:border-white/15">
          <table className="w-full text-left text-xs">
            <thead className="sticky top-0 border-b border-black/10 bg-background text-zinc-500 dark:border-white/15">
              <tr>
                <th className="px-3 py-2 font-medium">Aralık</th>
                <th className="px-3 py-2 text-right font-medium">Ort. yanıt</th>
                <th className="px-3 py-2 text-right font-medium">En yüksek</th>
                <th className="px-3 py-2 text-right font-medium">Kontrol</th>
                <th className="px-3 py-2 text-right font-medium">Başarısız</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-black/5 tabular-nums dark:divide-white/10">
              {width !== null &&
                points.map((p) => (
                  <tr key={p.start}>
                    <td className="px-3 py-1.5">{bucketLabel(p)}</td>
                    <td className="px-3 py-1.5 text-right">
                      {p.avg_latency_ms !== null ? ms(p.avg_latency_ms) : "–"}
                    </td>
                    <td className="px-3 py-1.5 text-right">
                      {p.max_latency_ms !== null ? ms(p.max_latency_ms) : "–"}
                    </td>
                    <td className="px-3 py-1.5 text-right">{p.checks}</td>
                    <td className="px-3 py-1.5 text-right">{p.checks - p.up_checks}</td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      </details>
    </figure>
  );
}
