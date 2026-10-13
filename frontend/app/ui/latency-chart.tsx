"use client";

import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import type { Series, SeriesPoint } from "@/lib/api";
import { duration } from "@/lib/format";
import { useT } from "@/lib/i18n/client";

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
  ok: { color: "var(--viz-good)", height: 4, label: "monitorPage.chart.state.ok" },
  partial: { color: "var(--viz-warning)", height: STRIP_HEIGHT, label: "monitorPage.chart.state.partial" },
  down: { color: "var(--viz-critical)", height: STRIP_HEIGHT, label: "monitorPage.chart.state.down" },
};

/** A round upper bound and tick step for an axis that starts at zero. */
function axis(max: number): { top: number; step: number } {
  const rough = Math.max(max, 1) / 4;
  const magnitude = 10 ** Math.floor(Math.log10(rough));
  const step = [1, 2, 5, 10].map((m) => m * magnitude).find((s) => s >= rough)!;
  return { top: Math.ceil(Math.max(max, 1) / step) * step, step };
}

const ms = (n: number, intl: string) => `${n.toLocaleString(intl, { maximumFractionDigits: n < 10 ? 1 : 0 })} ms`;

/** A span of time in milliseconds since the epoch; `to` is null while it is still going on. */
export type Span = { from: number; to: number | null };

// A press that moves less than this is a click, not a drag; a finger
// wobbles more than a mouse.
const CLICK_SLOP = 4;
const TOUCH_SLOP = 12;
// How close to an edge of the marked span a press grabs it, and half the
// room its toolbar needs, in pixels.
const EDGE_GRIP = 7;
const TOOLBAR_HALF = 170;

export function LatencyChart({
  series,
  baseHref,
  selection,
  incidents,
}: {
  series: Series;
  /** The page's address with its query; a timeframe's from and to are added to it. */
  baseHref: string;
  /** The timeframe the chart is zoomed into, carried in the page's URL. */
  selection: Span | null;
  /** The monitor's incidents, so that clicking an outage selects its whole span. */
  incidents: Span[];
}) {
  const t = useT();
  const router = useRouter();
  const wrapper = useRef<HTMLDivElement>(null);
  // Measured on the client; until then a placeholder of the same height is
  // rendered, which also keeps times in the viewer's zone, not the server's.
  const [width, setWidth] = useState<number | null>(null);
  const [hovered, setActive] = useState<number | null>(null);
  // What the pointer is doing: drawing a new span (pixels from the
  // wrapper's left edge) or dragging one edge of the drawn one.
  const [gesture, setGesture] = useState<
    { kind: "new"; start: number; current: number; slop: number } | { kind: "edge"; edge: "from" | "to" } | null
  >(null);
  // A span marked on the chart and not yet applied: it can still be
  // adjusted by its edges, zoomed into, or dropped.
  const [draft, setDraft] = useState<{ from: number; to: number } | null>(null);
  const [overEdge, setOverEdge] = useState(false);

  useEffect(() => {
    const el = wrapper.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const { points } = series;
  // The point in hand; a series that changed under it (a zoom) lets go.
  const active = hovered !== null && hovered < points.length ? hovered : null;
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

  const timeLabel = (at: number) =>
    new Date(at).toLocaleString(
      t.intl,
      longRange
        ? { day: "numeric", month: "short" }
        : { hour: "2-digit", minute: "2-digit" },
    );
  const bucketLabel = (p: SeriesPoint) => {
    const start = new Date(p.start);
    const end = new Date(Math.min(start.getTime() + bucket, to));
    const day = start.toLocaleDateString(t.intl, { day: "numeric", month: "short" });
    const time = (d: Date) => d.toLocaleTimeString(t.intl, { hour: "2-digit", minute: "2-digit" });
    return `${day} ${time(start)} – ${time(end)}`;
  };

  if (points.length === 0) {
    return (
      <p className="rounded-lg border border-dashed border-line p-8 text-center text-sm text-muted">
        {t("monitorPage.chart.empty")}
      </p>
    );
  }

  const pixel = (clientX: number) => (wrapper.current ? clientX - wrapper.current.getBoundingClientRect().left : 0);
  const nearestAt = (px: number) => {
    if (!geometry) return null;
    let best = 0;
    geometry.centers.forEach((c, i) => {
      if (Math.abs(c - px) < Math.abs(geometry.centers[best] - px)) best = i;
    });
    return best;
  };
  // The moment under a pixel, kept inside the chart's span.
  const timeAt = (px: number) => {
    if (!geometry) return from;
    const f = Math.min(Math.max((px - LEFT) / geometry.plotWidth, 0), 1);
    return from + f * (to - from);
  };
  // Spans snap to bucket edges, so a drag picks whole buckets.
  const snapDown = (at: number) => Math.max(from + Math.floor((at - from) / bucket) * bucket, from);
  const snapUp = (at: number) => Math.min(from + Math.ceil((at - from) / bucket) * bucket, to);
  const snap = (at: number) => Math.min(Math.max(from + Math.round((at - from) / bucket) * bucket, from), to);

  // Which edge of the drawn span a pixel is on, if any.
  const edgeAt = (px: number): "from" | "to" | null => {
    if (!geometry || !draft) return null;
    if (Math.abs(px - geometry.x(Math.max(draft.from, from))) <= EDGE_GRIP) return "from";
    if (Math.abs(px - geometry.x(Math.min(draft.to, to))) <= EDGE_GRIP) return "to";
    return null;
  };

  // Zooming puts the timeframe in the URL: the page then shows the chart
  // and what happened for it, and the address can be shared. It is pushed,
  // so the browser's back button zooms out again.
  const zoom = (span: { from: number; to: number }) => {
    setDraft(null);
    router.push(
      `${baseHref}&from=${encodeURIComponent(new Date(span.from).toISOString())}&to=${encodeURIComponent(new Date(span.to).toISOString())}`,
      { scroll: false },
    );
  };
  // A click marks the bucket under it; on a bucket with failures, the
  // incident that covers it, so one click frames the whole outage. A click
  // on what is already marked lets go of it.
  const clickAt = (px: number) => {
    const i = nearestAt(px);
    if (i === null) return;
    const p = points[i];
    const start = new Date(p.start).getTime();
    const end = Math.min(start + bucket, to);
    if (draft && start >= draft.from && end <= draft.to) {
      setDraft(null);
      return;
    }
    const incident = stateOf(p) !== "ok" ? incidents.find((inc) => inc.from < end && (inc.to ?? Infinity) > start) : undefined;
    setDraft(incident ? { from: incident.from, to: incident.to ?? to } : { from: start, to: end });
  };

  const point = active !== null ? points[active] : null;
  // The band being drawn right now, else the one drawn and waiting.
  const marked: [number, number] | null =
    gesture?.kind === "new" && Math.abs(gesture.current - gesture.start) >= gesture.slop
      ? [snapDown(timeAt(Math.min(gesture.start, gesture.current))), snapUp(timeAt(Math.max(gesture.start, gesture.current)))]
      : draft
        ? [draft.from, draft.to]
        : null;
  // The timeframe the page is zoomed into, as a quiet band inside its margins.
  const applied: [number, number] | null = selection ? [Math.max(selection.from, from), Math.min(selection.to ?? to, to)] : null;

  const clock = (at: number) => new Date(at).toLocaleTimeString(t.intl, { hour: "2-digit", minute: "2-digit", ...(bucket < 60_000 && { second: "2-digit" }) });
  const day = (at: number) => new Date(at).toLocaleDateString(t.intl, { day: "numeric", month: "short" });
  const spanLabel = ([a, b]: [number, number]) =>
    `${day(a)} ${clock(a)} – ${day(a) === day(b) ? "" : day(b) + " "}${clock(b)} · ${duration(Math.round((b - a) / 1000), t)}`;

  return (
    <figure>
      <div
        ref={wrapper}
        className="relative select-none outline-none focus-visible:ring-2 focus-visible:ring-[var(--viz-series)]"
        // Horizontal drags select; vertical ones still scroll the page.
        style={{ height: HEIGHT, touchAction: "pan-y", cursor: overEdge || gesture?.kind === "edge" ? "ew-resize" : "crosshair" }}
        tabIndex={0}
        role="group"
        aria-label={t("monitorPage.chart.aria")}
        onPointerDown={(e) => {
          if (e.button !== 0 || !geometry || gesture) return;
          // The toolbar's buttons are not part of the plot.
          if ((e.target as HTMLElement).closest("[data-selection-toolbar]")) return;
          // No native text selection while dragging; keep keyboard focus.
          e.preventDefault();
          e.currentTarget.focus();
          const px = pixel(e.clientX);
          const edge = edgeAt(px);
          setGesture(
            edge
              ? { kind: "edge", edge }
              : { kind: "new", start: px, current: px, slop: e.pointerType === "touch" ? TOUCH_SLOP : CLICK_SLOP },
          );
          e.currentTarget.setPointerCapture(e.pointerId);
        }}
        onPointerMove={(e) => {
          const px = pixel(e.clientX);
          setActive(nearestAt(px));
          setOverEdge(edgeAt(px) !== null);
          if (gesture?.kind === "new") {
            setGesture({ ...gesture, current: px });
          } else if (gesture?.kind === "edge" && draft) {
            // An edge stops one bucket short of the other.
            const at = snap(timeAt(px));
            setDraft(
              gesture.edge === "from"
                ? { from: Math.min(at, draft.to - bucket), to: draft.to }
                : { from: draft.from, to: Math.max(at, draft.from + bucket) },
            );
          }
        }}
        onPointerUp={(e) => {
          if (!gesture) return;
          setGesture(null);
          e.currentTarget.releasePointerCapture(e.pointerId);
          if (gesture.kind !== "new") return;
          const { start, current, slop } = gesture;
          if (Math.abs(current - start) < slop) {
            clickAt(start);
          } else {
            const span = { from: snapDown(timeAt(Math.min(start, current))), to: snapUp(timeAt(Math.max(start, current))) };
            if (span.to > span.from) setDraft(span);
          }
        }}
        onPointerCancel={() => setGesture(null)}
        onPointerLeave={() => {
          if (!gesture) setActive(null);
        }}
        onBlur={(e) => {
          // Focus moving to the toolbar's buttons stays inside.
          if (!e.currentTarget.contains(e.relatedTarget)) setActive(null);
        }}
        onKeyDown={(e) => {
          if ((e.target as HTMLElement).closest("[data-selection-toolbar]")) return;
          if (e.key === "ArrowRight" || e.key === "ArrowLeft") {
            e.preventDefault();
            const delta = e.key === "ArrowRight" ? 1 : -1;
            setActive((i) =>
              Math.min(Math.max((i ?? (delta > 0 ? -1 : points.length)) + delta, 0), points.length - 1),
            );
          } else if ((e.key === "Enter" || e.key === " ") && geometry && (draft || active !== null)) {
            e.preventDefault();
            // Enter marks the point in hand; once something is marked it zooms in.
            if (draft) zoom(draft);
            else clickAt(geometry.centers[active!]);
          } else if (e.key === "Escape") {
            if (draft) setDraft(null);
            else setActive(null);
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
                  {v.toLocaleString(t.intl)}
                </text>
              </g>
            ))}

            {applied && applied[1] > applied[0] && (
              <g>
                <rect
                  x={geometry.x(applied[0])}
                  y={TOP}
                  width={Math.max(geometry.x(applied[1]) - geometry.x(applied[0]), 1)}
                  height={STRIP_TOP + STRIP_HEIGHT - TOP}
                  fill="var(--viz-series)"
                  opacity={0.08}
                />
                {applied.map((edge) => (
                  <line
                    key={edge}
                    x1={geometry.x(edge)}
                    x2={geometry.x(edge)}
                    y1={TOP}
                    y2={STRIP_TOP + STRIP_HEIGHT}
                    stroke="var(--viz-series)"
                    strokeWidth={1}
                    strokeDasharray="3 3"
                  />
                ))}
              </g>
            )}

            {marked && marked[1] > marked[0] && (
              <g>
                <rect
                  x={geometry.x(Math.max(marked[0], from))}
                  y={TOP}
                  width={Math.max(geometry.x(Math.min(marked[1], to)) - geometry.x(Math.max(marked[0], from)), 1)}
                  height={STRIP_TOP + STRIP_HEIGHT - TOP}
                  fill="var(--viz-series)"
                  opacity={0.2}
                />
                {[Math.max(marked[0], from), Math.min(marked[1], to)].map((edge) => (
                  <g key={edge}>
                    <line
                      x1={geometry.x(edge)}
                      x2={geometry.x(edge)}
                      y1={TOP}
                      y2={STRIP_TOP + STRIP_HEIGHT}
                      stroke="var(--viz-series)"
                      strokeWidth={2}
                    />
                    {/* The grip an edge is dragged by. */}
                    <rect
                      x={geometry.x(edge) - 3}
                      y={TOP + PLOT_HEIGHT / 2 - 14}
                      width={6}
                      height={28}
                      rx={3}
                      fill="var(--viz-series)"
                      stroke="var(--background)"
                      strokeWidth={1}
                    />
                  </g>
                ))}
              </g>
            )}

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

        {geometry && marked && marked[1] > marked[0] && (
          <div
            data-selection-toolbar
            className="absolute z-20 flex -translate-x-1/2 items-center gap-2 whitespace-nowrap rounded-lg border border-line bg-panel py-1 pl-3 pr-1 text-xs shadow-lg"
            style={{
              top: 2,
              // Centred on the span, kept inside the chart.
              left: Math.min(
                Math.max((geometry.x(Math.max(marked[0], from)) + geometry.x(Math.min(marked[1], to))) / 2, TOOLBAR_HALF),
                Math.max(width! - TOOLBAR_HALF, TOOLBAR_HALF),
              ),
              cursor: "default",
            }}
          >
            <span className="tabular-nums">{spanLabel(marked)}</span>
            {draft && !gesture && (
              <>
                <button type="button" className="btn btn-primary px-2.5 py-1 text-xs" onClick={() => zoom(draft)}>
                  {t("monitorPage.chart.zoom")}
                </button>
                <button
                  type="button"
                  className="rounded-md px-1.5 py-1 text-muted hover:bg-sunken hover:text-ink"
                  aria-label={t("monitorPage.chart.dropSelection")}
                  title={t("monitorPage.chart.dropSelection")}
                  onClick={() => setDraft(null)}
                >
                  ✕
                </button>
              </>
            )}
          </div>
        )}

        {geometry && point && active !== null && !gesture && (
          <div
            role="status"
            className="pointer-events-none absolute z-10 rounded-lg border border-line bg-panel px-3 py-2 text-xs shadow-md"
            style={{
              top: marked ? 44 : 8,
              width: TOOLTIP_WIDTH,
              // Beside the crosshair, on whichever side has room.
              left:
                geometry.centers[active] + 12 + TOOLTIP_WIDTH > width!
                  ? geometry.centers[active] - 12 - TOOLTIP_WIDTH
                  : geometry.centers[active] + 12,
            }}
          >
            <div className="text-muted">{bucketLabel(point)}</div>
            <div className="mt-1 flex items-center gap-2">
              <span className="h-0.5 w-3 rounded bg-[var(--viz-series)]" aria-hidden />
              <span className="text-sm font-semibold">
                {point.avg_latency_ms !== null ? ms(point.avg_latency_ms, t.intl) : t("monitorPage.chart.noResponse")}
              </span>
              <span className="text-muted">{t("monitorPage.chart.average")}</span>
            </div>
            {point.max_latency_ms !== null && (
              <div className="mt-0.5 text-muted">
                {t("monitorPage.chart.max")} <span className="text-foreground">{ms(point.max_latency_ms, t.intl)}</span>
              </div>
            )}
            <div className="mt-1 flex items-center gap-2 border-t border-line pt-1">
              <span
                className="h-2 w-2 rounded-sm"
                style={{ background: stateStyle[stateOf(point)].color }}
                aria-hidden
              />
              <span>
                <span className="font-semibold">{t(stateStyle[stateOf(point)].label)}</span>
                <span className="text-muted">
                  {" · "}
                  {t.n("monitorPage.chart.checks", point.checks)}
                  {point.checks > point.up_checks && `, ${t("monitorPage.chart.failed", { count: point.checks - point.up_checks })}`}
                </span>
              </span>
            </div>
          </div>
        )}
      </div>

      <figcaption className="mt-2 flex select-none flex-wrap items-center gap-x-4 gap-y-1 pl-12 text-xs text-muted">
        <span>{t("monitorPage.chart.availability")}</span>
        {(Object.keys(stateStyle) as State[]).map((s) => (
          <span key={s} className="inline-flex items-center gap-1.5">
            <span
              className="w-2 rounded-sm"
              style={{ background: stateStyle[s].color, height: stateStyle[s].height }}
              aria-hidden
            />
            {t(stateStyle[s].label)}
          </span>
        ))}
        <span className="basis-full">{t("monitorPage.chart.selectHint")}</span>
      </figcaption>

      <details className="mt-3 text-sm">
        <summary className="cursor-pointer text-xs text-muted">{t("monitorPage.chart.showTable")}</summary>
        <div className="mt-2 max-h-72 overflow-auto rounded-lg border border-line">
          <table className="w-full text-left text-xs">
            <thead className="sticky top-0 border-b border-line bg-background text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">{t("monitorPage.chart.table.bucket")}</th>
                <th className="px-3 py-2 text-right font-medium">{t("monitorPage.chart.table.avg")}</th>
                <th className="px-3 py-2 text-right font-medium">{t("monitorPage.chart.table.max")}</th>
                <th className="px-3 py-2 text-right font-medium">{t("monitorPage.chart.table.checks")}</th>
                <th className="px-3 py-2 text-right font-medium">{t("monitorPage.chart.table.failed")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line tabular-nums">
              {width !== null &&
                points.map((p) => (
                  <tr key={p.start}>
                    <td className="px-3 py-1.5">{bucketLabel(p)}</td>
                    <td className="px-3 py-1.5 text-right">
                      {p.avg_latency_ms !== null ? ms(p.avg_latency_ms, t.intl) : "–"}
                    </td>
                    <td className="px-3 py-1.5 text-right">
                      {p.max_latency_ms !== null ? ms(p.max_latency_ms, t.intl) : "–"}
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
