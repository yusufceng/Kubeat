"use client";

import { useRouter } from "next/navigation";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import type { Series, SeriesPoint } from "@/lib/api";
import { duration } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
import { ChevronIcon } from "@/app/ui/icons";
import { CloseIcon, ZoomInIcon } from "@/app/ui/monitor-icons";
import { type GlyphKind, StatusGlyph } from "@/app/ui/status-glyph";

// Layout, in CSS pixels: the plot, then the availability strip under it,
// then the time axis.
const LEFT = 44;
const RIGHT = 8;
const TOP = 18;
const PLOT_HEIGHT = 200;
const STRIP_TOP = TOP + PLOT_HEIGHT + 8;
const STRIP_HEIGHT = 8;
const HEIGHT = STRIP_TOP + STRIP_HEIGHT + 24;
const TOOLTIP_WIDTH = 224;
/** How long the line takes to draw itself in, the first time the chart appears. */
const INTRO_MS = 450;

type State = "ok" | "partial" | "down";

function stateOf(p: SeriesPoint): State {
  if (p.up_checks === p.checks) return "ok";
  return p.up_checks === 0 ? "down" : "partial";
}

const stateStyle: Record<State, { color: string; height: number; glyph: GlyphKind; label: string }> = {
  // Problems are taller as well as differently colored, so the strip does
  // not rely on hue alone.
  ok: { color: "var(--ok-bar)", height: 4, glyph: "up", label: "monitorPage.chart.state.ok" },
  partial: { color: "var(--warn)", height: STRIP_HEIGHT, glyph: "warn", label: "monitorPage.chart.state.partial" },
  down: { color: "var(--down)", height: STRIP_HEIGHT, glyph: "down", label: "monitorPage.chart.state.down" },
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
  maintenance = [],
}: {
  series: Series;
  /** The page's address with its query; a timeframe's from and to are added to it. */
  baseHref: string;
  /** The timeframe the chart is zoomed into, carried in the page's URL. */
  selection: Span | null;
  /** The monitor's outages: drawn as bands, and clicking one selects its whole span. */
  incidents: Span[];
  /** The maintenance that covered the monitor, drawn hatched. */
  maintenance?: { from: number; to: number }[];
}) {
  const t = useT();
  const router = useRouter();
  // The ids of the gradient and the hatching, safe inside url(#…).
  const ids = `chart${useId().replace(/[^\w-]/g, "")}`;
  const wrapper = useRef<HTMLDivElement>(null);
  // Measured on the client; until then a placeholder of the same height is
  // rendered, which also keeps times in the viewer's zone, not the server's.
  const [width, setWidth] = useState<number | null>(null);
  // The line draws itself in once, when the chart first gets its width.
  const [intro, setIntro] = useState(true);
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

  const measured = width !== null;
  useEffect(() => {
    if (!measured) return;
    const id = setTimeout(() => setIntro(false), INTRO_MS + 100);
    return () => clearTimeout(id);
  }, [measured]);

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

  const clock = (at: number, seconds = false) =>
    new Date(at).toLocaleTimeString(t.intl, {
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
      ...(seconds && { second: "2-digit" }),
    });
  const day = (at: number) => new Date(at).toLocaleDateString(t.intl, { day: "numeric", month: "short" });
  const timeLabel = (at: number) => (longRange ? day(at) : clock(at));
  const bucketLabel = (p: SeriesPoint) => {
    const start = new Date(p.start).getTime();
    const end = Math.min(start + bucket, to);
    return `${day(start)} ${clock(start)} – ${clock(end)}`;
  };

  if (points.length === 0) {
    return (
      <p className="rounded-md border border-dashed border-line p-8 text-center text-[13px] text-ink-3">
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

  const spanLabel = ([a, b]: [number, number]) => {
    const seconds = bucket < 60_000;
    return `${day(a)} ${clock(a, seconds)} – ${day(a) === day(b) ? "" : day(b) + " "}${clock(b, seconds)} · ${duration(Math.round((b - a) / 1000), t)}`;
  };

  // The outages and maintenance that show within the chart's span.
  const within = (s: { from: number; to: number | null }) => s.from < to && (s.to ?? to) > from;
  const outageBands = incidents.filter(within);
  const maintenanceBands = maintenance.filter(within);
  const pattern = `${ids}-maint`;
  const gradient = `${ids}-area`;

  return (
    <figure>
      <div
        ref={wrapper}
        className={`relative select-none rounded-sm outline-offset-4 ${intro ? "mon-intro" : ""}`}
        // Horizontal drags select; vertical ones still scroll the page.
        style={{ height: HEIGHT, touchAction: "pan-y", cursor: overEdge || gesture?.kind === "edge" ? "ew-resize" : "crosshair" }}
        tabIndex={0}
        role="group"
        aria-label={t("monitorPage.chart.aria")}
        onPointerDown={(e) => {
          if (e.button !== 0 || !geometry || gesture) return;
          // The toolbar's buttons are not part of the plot.
          if ((e.target as HTMLElement).closest("[data-selection-toolbar]")) return;
          // No native text selection while dragging; keep keyboard focus,
          // without the ring that a pointer does not need.
          e.preventDefault();
          e.currentTarget.focus({ preventScroll: true, focusVisible: false } as FocusOptions);
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
          <svg width={width!} height={HEIGHT} className="block overflow-visible" aria-hidden>
            <defs>
              <linearGradient id={gradient} x1="0" x2="0" y1="0" y2="1">
                <stop offset="0" stopColor="var(--series)" stopOpacity={0.22} />
                <stop offset="1" stopColor="var(--series)" stopOpacity={0} />
              </linearGradient>
              <pattern id={pattern} width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
                <rect width="6" height="6" fill="var(--maint)" fillOpacity={0.07} />
                <line x1="0" y1="0" x2="0" y2="6" stroke="var(--maint)" strokeOpacity={0.45} strokeWidth={1.5} />
              </pattern>
            </defs>

            <text x={LEFT - 8} y={TOP - 8} textAnchor="end" fontSize={10} fill="var(--ink-3)">
              {t("monitorPage.chart.unit")}
            </text>
            {geometry.yTicks.map((v) => (
              <g key={v}>
                <line
                  x1={LEFT}
                  x2={LEFT + geometry.plotWidth}
                  y1={geometry.y(v)}
                  y2={geometry.y(v)}
                  stroke={v === 0 ? "var(--line-strong)" : "var(--line)"}
                  strokeDasharray={v === 0 ? undefined : "2 4"}
                  strokeWidth={1}
                />
                <text
                  x={LEFT - 8}
                  y={geometry.y(v)}
                  dy="0.32em"
                  textAnchor="end"
                  fontSize={11}
                  fill="var(--ink-3)"
                  style={{ fontVariantNumeric: "tabular-nums" }}
                >
                  {v.toLocaleString(t.intl)}
                </text>
              </g>
            ))}

            {/* Maintenance, hatched, under everything else. */}
            {maintenanceBands.map((m) => {
              const a = geometry.x(Math.max(m.from, from));
              const b = geometry.x(Math.min(m.to, to));
              return (
                <rect
                  key={`m${m.from}`}
                  x={a}
                  y={TOP}
                  width={Math.max(b - a, 2)}
                  height={PLOT_HEIGHT}
                  fill={`url(#${pattern})`}
                />
              );
            })}

            {/* Outages: a red wash with a cap along the top. */}
            {outageBands.map((o) => {
              const a = geometry.x(Math.max(o.from, from));
              const b = geometry.x(Math.min(o.to ?? to, to));
              const w = Math.max(b - a, 2);
              return (
                <g key={`o${o.from}`}>
                  <rect x={a} y={TOP} width={w} height={PLOT_HEIGHT} fill="var(--down)" fillOpacity={0.1} />
                  <rect x={a} y={TOP} width={w} height={2} fill="var(--down)" />
                </g>
              );
            })}

            {applied && applied[1] > applied[0] && (
              <g>
                <rect
                  x={geometry.x(applied[0])}
                  y={TOP}
                  width={Math.max(geometry.x(applied[1]) - geometry.x(applied[0]), 1)}
                  height={STRIP_TOP + STRIP_HEIGHT - TOP}
                  fill="var(--accent)"
                  fillOpacity={0.06}
                />
                {applied.map((edge) => (
                  <line
                    key={edge}
                    x1={geometry.x(edge)}
                    x2={geometry.x(edge)}
                    y1={TOP}
                    y2={STRIP_TOP + STRIP_HEIGHT}
                    stroke="var(--accent)"
                    strokeOpacity={0.7}
                    strokeWidth={1}
                    strokeDasharray="3 3"
                  />
                ))}
              </g>
            )}

            {geometry.segments.map((segment) => {
              const coords = segment.map((i) => `${geometry.centers[i]},${geometry.y(points[i].avg_latency_ms!)}`);
              const first = geometry.centers[segment[0]];
              const last = geometry.centers[segment[segment.length - 1]];
              const base = geometry.y(0);
              return (
                <g key={segment[0]}>
                  {segment.length > 1 && (
                    <path
                      className="mon-area"
                      d={`M${first},${base} L${coords.join(" L")} L${last},${base} Z`}
                      fill={`url(#${gradient})`}
                    />
                  )}
                  {segment.length > 1 ? (
                    <path
                      className="mon-line"
                      pathLength={1}
                      d={`M${coords.join(" L")}`}
                      fill="none"
                      stroke="var(--series)"
                      strokeWidth={1.6}
                      strokeLinejoin="round"
                      strokeLinecap="round"
                    />
                  ) : (
                    // A lone measurement between gaps would otherwise be invisible.
                    <circle cx={first} cy={geometry.y(points[segment[0]].avg_latency_ms!)} r={2} fill="var(--series)" />
                  )}
                </g>
              );
            })}

            {/* The availability strip: no data is the baseline alone. */}
            <rect x={LEFT} y={STRIP_TOP + STRIP_HEIGHT - 2} width={geometry.plotWidth} height={2} fill="var(--line)" />
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

            {marked && marked[1] > marked[0] && (
              <g>
                <rect
                  x={geometry.x(Math.max(marked[0], from))}
                  y={TOP}
                  width={Math.max(geometry.x(Math.min(marked[1], to)) - geometry.x(Math.max(marked[0], from)), 1)}
                  height={STRIP_TOP + STRIP_HEIGHT - TOP}
                  fill="var(--accent)"
                  fillOpacity={0.1}
                />
                {[Math.max(marked[0], from), Math.min(marked[1], to)].map((edge) => (
                  <g key={edge}>
                    <line
                      x1={geometry.x(edge)}
                      x2={geometry.x(edge)}
                      y1={TOP}
                      y2={STRIP_TOP + STRIP_HEIGHT}
                      stroke="var(--accent)"
                      strokeWidth={1}
                    />
                    {/* The grip an edge is dragged by. */}
                    <rect
                      x={geometry.x(edge) - 4}
                      y={TOP + PLOT_HEIGHT / 2 - 14}
                      width={8}
                      height={28}
                      rx={4}
                      fill="var(--accent)"
                      stroke="var(--panel)"
                      strokeWidth={1.5}
                    />
                    <line
                      x1={geometry.x(edge)}
                      x2={geometry.x(edge)}
                      y1={TOP + PLOT_HEIGHT / 2 - 6}
                      y2={TOP + PLOT_HEIGHT / 2 + 6}
                      stroke="var(--accent-ink)"
                      strokeWidth={1}
                      strokeLinecap="round"
                    />
                  </g>
                ))}
              </g>
            )}

            {geometry.xTicks.map((at, i) => (
              <text
                key={at}
                x={geometry.x(at)}
                y={HEIGHT - 4}
                textAnchor={i === 0 ? "start" : i === geometry.xTicks.length - 1 ? "end" : "middle"}
                fontSize={11}
                fill="var(--ink-3)"
                style={{ fontVariantNumeric: "tabular-nums" }}
              >
                {timeLabel(at)}
              </text>
            ))}

            {active !== null && (
              <g>
                <line
                  x1={geometry.centers[active]}
                  x2={geometry.centers[active]}
                  y1={TOP}
                  y2={STRIP_TOP + STRIP_HEIGHT}
                  stroke="var(--ink-3)"
                  strokeDasharray="2 3"
                  strokeWidth={1}
                />
                {points[active].avg_latency_ms !== null && (
                  <circle
                    cx={geometry.centers[active]}
                    cy={geometry.y(points[active].avg_latency_ms!)}
                    r={4}
                    fill="var(--series)"
                    stroke="var(--panel)"
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
            className="pop absolute z-20 flex -translate-x-1/2 items-center gap-2 py-1 pr-1 pl-3 text-xs whitespace-nowrap"
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
            <span className="tabular-nums text-ink-2">{spanLabel(marked)}</span>
            {draft && !gesture && (
              <>
                <button type="button" className="btn btn-primary btn-sm" onClick={() => zoom(draft)}>
                  <ZoomInIcon width={14} height={14} />
                  {t("monitorPage.chart.zoom")}
                </button>
                <button
                  type="button"
                  className="btn btn-ghost btn-sm px-1.5"
                  aria-label={t("monitorPage.chart.dropSelection")}
                  title={t("monitorPage.chart.dropSelection")}
                  onClick={() => setDraft(null)}
                >
                  <CloseIcon width={14} height={14} />
                </button>
              </>
            )}
          </div>
        )}

        {geometry && point && active !== null && !gesture && (
          <div
            role="status"
            className="pop pointer-events-none absolute z-10 px-3 py-2.5 text-xs"
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
            <div className="text-ink-3 tabular-nums">{bucketLabel(point)}</div>
            <div className="mt-1.5 flex items-baseline gap-1.5 whitespace-nowrap">
              <span className="text-[13px] font-semibold tabular-nums text-ink">
                {point.avg_latency_ms !== null ? ms(point.avg_latency_ms, t.intl) : t("monitorPage.chart.noResponse")}
              </span>
              {point.avg_latency_ms !== null && <span className="text-ink-3">{t("monitorPage.chart.average")}</span>}
            </div>
            {point.max_latency_ms !== null && (
              <div className="mt-0.5 whitespace-nowrap text-ink-3">
                {t("monitorPage.chart.max")} <span className="tabular-nums text-ink-2">{ms(point.max_latency_ms, t.intl)}</span>
              </div>
            )}
            <div className="mt-2 flex items-center gap-2 border-t border-line pt-2">
              <StatusGlyph kind={stateStyle[stateOf(point)].glyph} />
              <span>
                <span className="font-semibold text-ink">{t(stateStyle[stateOf(point)].label)}</span>
                <span className="text-ink-3">
                  {" · "}
                  {t.n("monitorPage.chart.checks", point.checks)}
                  {point.checks > point.up_checks && `, ${t("monitorPage.chart.failed", { count: point.checks - point.up_checks })}`}
                </span>
              </span>
            </div>
          </div>
        )}
      </div>

      <figcaption className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1.5 pl-11 text-xs text-ink-3 select-none">
        <span className="font-medium text-ink-2">{t("monitorPage.chart.availability")}</span>
        {(Object.keys(stateStyle) as State[]).map((s) => (
          <span key={s} className="inline-flex items-center gap-1.5">
            <span
              className="w-2.5 rounded-[1px]"
              style={{ background: stateStyle[s].color, height: stateStyle[s].height }}
              aria-hidden
            />
            {t(stateStyle[s].label)}
          </span>
        ))}
        {outageBands.length > 0 && (
          <span className="inline-flex items-center gap-1.5">
            <span className="h-2.5 w-2.5 border-t-2 border-down bg-down/10" aria-hidden />
            {t("monitorPage.chart.band.outage")}
          </span>
        )}
        {maintenanceBands.length > 0 && (
          <span className="inline-flex items-center gap-1.5">
            <span className="slot-maint h-2.5 w-2.5 rounded-[1px]" aria-hidden />
            {t("monitorPage.chart.band.maintenance")}
          </span>
        )}
        <span className="basis-full">{t("monitorPage.chart.selectHint")}</span>
      </figcaption>

      <details className="mon-more mt-3 px-0 pb-0">
        <summary>
          <ChevronIcon width={14} height={14} />
          {t("monitorPage.chart.showTable")}
        </summary>
        <div className="mt-2 max-h-72 overflow-auto rounded-md border border-line">
          <table className="mon-table text-xs">
            <thead className="sticky top-0 bg-panel">
              <tr>
                <th>{t("monitorPage.chart.table.bucket")}</th>
                <th className="num">{t("monitorPage.chart.table.avg")}</th>
                <th className="num">{t("monitorPage.chart.table.max")}</th>
                <th className="num">{t("monitorPage.chart.table.checks")}</th>
                <th className="num">{t("monitorPage.chart.table.failed")}</th>
              </tr>
            </thead>
            <tbody>
              {width !== null &&
                points.map((p) => (
                  <tr key={p.start}>
                    <td className="tabular-nums">{bucketLabel(p)}</td>
                    <td className="num">{p.avg_latency_ms !== null ? ms(p.avg_latency_ms, t.intl) : "–"}</td>
                    <td className="num">{p.max_latency_ms !== null ? ms(p.max_latency_ms, t.intl) : "–"}</td>
                    <td className="num">{p.checks}</td>
                    <td className={`num ${p.checks > p.up_checks ? "text-down-ink" : ""}`}>{p.checks - p.up_checks}</td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      </details>
    </figure>
  );
}
