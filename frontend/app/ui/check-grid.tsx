"use client";

import Link from "next/link";
import { useMemo, useRef, useState } from "react";
import type { CheckGrid as Grid, GridCell } from "@/lib/api";
import { useT } from "@/lib/i18n/client";
import { useMounted } from "@/lib/use-mounted";

// Layout, in CSS pixels.
const CELL = 14;
const GAP = 3;
const TOOLTIP_WIDTH = 260;

type State = "ok" | "partial" | "down" | "none";

function stateOf(cell: GridCell | undefined): State {
  if (!cell || cell.checks === 0) return "none";
  if (cell.failed === 0) return "ok";
  return cell.failed === cell.checks ? "down" : "partial";
}

// The states are told apart by lightness as well as hue: a calm, dim green
// for what went well, full amber and red for what did not, an outline for
// a slot without checks.
const stateStyle: Record<State, { style: React.CSSProperties; label: string }> = {
  ok: { style: { background: "var(--viz-good)", opacity: 0.42 }, label: "monitorPage.chart.state.ok" },
  partial: { style: { background: "var(--viz-warning)" }, label: "monitorPage.chart.state.partial" },
  down: { style: { background: "var(--viz-critical)" }, label: "monitorPage.chart.state.down" },
  none: { style: { boxShadow: "inset 0 0 0 1px var(--line)" }, label: "monitorPage.history.state.none" },
};

type Slot = { start: number; col: number; row: number };

/**
 * The check history as a calendar-like grid: every cell is a slot of time,
 * coloured by how its checks went. Over a day a column is an hour and a
 * row a part of it; over a week a row is a day and a column an hour. A
 * cell shows its checks and the error they gave on hover, and keeps
 * showing them below the grid once clicked.
 */
export function CheckGrid({
  grid,
  inspectBase,
}: {
  grid: Grid;
  /** The page's address with its query, to which a slot's from and to are added. */
  inspectBase: string;
}) {
  const t = useT();
  // The grid is laid out in the viewer's time zone, which the server does
  // not know; until mounted a placeholder of about the same size stands in.
  const mounted = useMounted();
  const [active, setActive] = useState<number | null>(null);
  const [pinned, setPinned] = useState<number | null>(null);
  // How far the grid is scrolled sideways on a narrow screen, so that the
  // tooltip, which sits outside the scrolling box, stays under its cell.
  const scroller = useRef<HTMLDivElement>(null);
  const [scrolled, setScrolled] = useState(0);

  const from = new Date(grid.from).getTime();
  const to = new Date(grid.to).getTime();
  const bucket = grid.bucket_seconds * 1000;
  const week = grid.bucket_seconds >= 3600;

  const layout = useMemo(() => {
    if (!mounted) return null;
    const hour = 3_600_000;
    const slots: Slot[] = [];
    const columns: string[] = [];
    const rows: string[] = [];
    const two = (n: number) => String(n).padStart(2, "0");

    if (week) {
      // Rows are local days, columns the hours of the day.
      for (let h = 0; h < 24; h++) columns.push(h % 3 === 0 ? two(h) : "");
      const day = new Date(from);
      day.setHours(0, 0, 0, 0);
      for (let row = 0; day.getTime() < to; row++) {
        rows.push(day.toLocaleDateString(t.intl, { weekday: "short", day: "numeric" }));
        for (let h = 0; h < 24; h++) {
          const start = new Date(day.getFullYear(), day.getMonth(), day.getDate(), h).getTime();
          if (start + bucket > from && start < to) slots.push({ start, col: h, row });
        }
        day.setDate(day.getDate() + 1);
      }
    } else {
      // Columns are local hours, rows the parts of an hour.
      const perHour = Math.max(Math.round(hour / bucket), 1);
      for (let r = 0; r < perHour; r++) {
        const minute = (r * bucket) / 60_000;
        rows.push(perHour <= 4 || minute % 15 === 0 ? `:${two(minute)}` : "");
      }
      const first = new Date(from);
      first.setMinutes(0, 0, 0);
      for (let col = 0, at = first.getTime(); at < to; col++, at += hour) {
        const h = new Date(at).getHours();
        columns.push(h % 3 === 0 ? two(h) : "");
        for (let r = 0; r < perHour; r++) {
          const start = at + r * bucket;
          if (start >= from && start < to) slots.push({ start, col, row: r });
        }
      }
    }
    return { slots, columns, rows };
  }, [mounted, week, from, to, bucket, t.intl]);

  const byStart = useMemo(() => new Map(grid.cells.map((c) => [new Date(c.start).getTime(), c])), [grid.cells]);

  if (!layout) {
    return <div className="h-52 rounded-lg bg-sunken/60" aria-hidden />;
  }
  const { slots, columns, rows } = layout;
  const width = columns.length * (CELL + GAP);
  const height = rows.length * (CELL + GAP);

  const time = (at: number) => new Date(at).toLocaleTimeString(t.intl, { hour: "2-digit", minute: "2-digit" });
  const span = (slot: Slot) => {
    const end = Math.min(slot.start + bucket, to);
    const day = new Date(slot.start).toLocaleDateString(t.intl, { day: "numeric", month: "short" });
    return `${day} ${time(slot.start)} – ${time(end)}`;
  };
  const describe = (slot: Slot) => {
    const cell = byStart.get(slot.start);
    const state = stateOf(cell);
    return { cell, state, label: t(stateStyle[state].label) };
  };

  // Arrow keys move through the grid by position, skipping what is not there.
  const move = (dc: number, dr: number) => {
    const current = active !== null ? slots[active] : null;
    if (!current) {
      setActive(slots.length - 1);
      return;
    }
    let { col, row } = current;
    for (let step = 0; step < Math.max(columns.length, rows.length); step++) {
      col += dc;
      row += dr;
      const next = slots.findIndex((s) => s.col === col && s.row === row);
      if (next >= 0) {
        setActive(next);
        return;
      }
    }
  };

  const shown = active ?? pinned;
  const tip = active !== null ? slots[active] : null;
  const detail = pinned !== null ? slots[pinned] : null;

  return (
    <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:gap-8">
      <div className="relative min-w-0 lg:w-[31rem] lg:shrink-0">
        <div ref={scroller} className="overflow-x-auto pb-1" onScroll={(e) => setScrolled(e.currentTarget.scrollLeft)}>
          <div
            className="relative inline-block select-none outline-none focus-visible:ring-2 focus-visible:ring-[var(--viz-series)]"
            tabIndex={0}
            role="group"
            aria-label={t("monitorPage.history.aria")}
            onPointerLeave={() => setActive(null)}
            onBlur={() => setActive(null)}
            onKeyDown={(e) => {
              const arrows: Record<string, [number, number]> = {
                ArrowLeft: [-1, 0],
                ArrowRight: [1, 0],
                ArrowUp: [0, -1],
                ArrowDown: [0, 1],
              };
              if (e.key in arrows) {
                e.preventDefault();
                move(...arrows[e.key]);
              } else if ((e.key === "Enter" || e.key === " ") && active !== null) {
                e.preventDefault();
                setPinned(pinned === active ? null : active);
              } else if (e.key === "Escape") {
                setActive(null);
                setPinned(null);
              }
            }}
          >
            {/* The hours (or, over a week, the hours of the day) above the grid. */}
            <div className="flex text-[10px] leading-none text-muted" style={{ marginLeft: 44, height: 14 }} aria-hidden>
              {columns.map((label, i) => (
                <span key={i} style={{ width: CELL + GAP }} className="tabular-nums">
                  {label}
                </span>
              ))}
            </div>
            <div className="flex">
              <div className="text-[10px] leading-none text-muted" style={{ width: 44 }} aria-hidden>
                {rows.map((label, i) => (
                  <div key={i} style={{ height: CELL + GAP, lineHeight: `${CELL}px` }} className="truncate pr-2 text-right tabular-nums">
                    {label}
                  </div>
                ))}
              </div>
              <div className="relative" style={{ width, height }}>
                {slots.map((slot, i) => {
                  const { state, cell } = describe(slot);
                  return (
                    <span
                      key={slot.start}
                      onPointerEnter={() => setActive(i)}
                      onClick={() => setPinned(pinned === i ? null : i)}
                      className="absolute rounded-[3px]"
                      style={{
                        left: slot.col * (CELL + GAP),
                        top: slot.row * (CELL + GAP),
                        width: CELL,
                        height: CELL,
                        cursor: "pointer",
                        ...stateStyle[state].style,
                        // One failed check in a long slot is not an outage:
                        // the more of a slot failed, the stronger its amber.
                        ...(state === "partial" && cell ? { opacity: 0.4 + 0.6 * (cell.failed / cell.checks) } : null),
                        // The cell in hand is lifted with a ring in the page's ink.
                        ...(shown === i || pinned === i
                          ? { outline: "2px solid var(--ink)", outlineOffset: 1, opacity: 1 }
                          : null),
                      }}
                    />
                  );
                })}
              </div>
            </div>
          </div>
        </div>

        {/* Outside the scrolling box, which would clip it. */}
        {tip && (
          <div
            role="status"
            className="pointer-events-none absolute z-10 rounded-lg border border-line bg-panel px-3 py-2 text-xs shadow-lg"
            style={{
              width: TOOLTIP_WIDTH,
              top: 14 + tip.row * (CELL + GAP) + CELL + 8,
              left: Math.max(Math.min(44 + tip.col * (CELL + GAP) - scrolled - TOOLTIP_WIDTH / 2, 44 + width - TOOLTIP_WIDTH), 0),
            }}
          >
            {cellText(tip)}
          </div>
        )}

        <p className="mt-2 flex select-none flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted">
          {(Object.keys(stateStyle) as State[]).map((s) => (
            <span key={s} className="inline-flex items-center gap-1.5">
              <span className="h-2.5 w-2.5 rounded-[2px]" style={stateStyle[s].style} aria-hidden />
              {t(stateStyle[s].label)}
            </span>
          ))}
        </p>
        <p className="mt-1 text-xs text-muted">
          {t(week ? "monitorPage.history.caption.week" : "monitorPage.history.caption.day", {
            minutes: grid.bucket_seconds / 60,
          })}{" "}
          {t("monitorPage.history.hint")}
        </p>

        {detail && (
          <div className="mt-3 rounded-lg border border-line bg-sunken/60 px-3 py-2 text-xs">
            {cellText(detail)}
            <Link
              href={`${inspectBase}&from=${encodeURIComponent(new Date(detail.start).toISOString())}&to=${encodeURIComponent(
                new Date(Math.min(detail.start + bucket, to)).toISOString(),
              )}#selected-range`}
              className="mt-2 inline-block font-medium underline hover:text-ink"
            >
              {t("monitorPage.history.inspect")}
            </Link>
          </div>
        )}
      </div>

      <div className="min-w-0 flex-1 lg:min-w-[16rem]">
        <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">{t("monitorPage.history.errors.title")}</h3>
        {grid.errors.length === 0 ? (
          <p className="text-sm text-muted">{t("monitorPage.history.errors.none")}</p>
        ) : (
          <ul className="divide-y divide-line rounded-lg border border-line text-xs">
            {grid.errors.map((e) => (
              <li key={e.error} className="px-3 py-2">
                <p className="break-words font-mono text-ink">{e.error || t("monitorPage.history.errors.unnamed")}</p>
                <p className="mt-1 text-muted">
                  {t.n("monitorPage.history.errors.count", e.count)} · {fullTime(e.first_at)} – {fullTime(e.last_at)}
                </p>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );

  function fullTime(iso: string) {
    return new Date(iso).toLocaleString(t.intl, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
  }

  /** What a slot holds: when, how it went, and the error its failures gave. */
  function cellText(slot: Slot) {
    const { cell, state, label } = describe(slot);
    return (
      <>
        <div className="text-muted">{span(slot)}</div>
        <div className="mt-1 flex items-center gap-2">
          <span className="h-2.5 w-2.5 shrink-0 rounded-[2px]" style={stateStyle[state].style} aria-hidden />
          <span>
            <span className="font-semibold text-ink">{label}</span>
            {cell && (
              <span className="text-muted">
                {" · "}
                {t.n("monitorPage.chart.checks", cell.checks)}
                {cell.failed > 0 && `, ${t("monitorPage.chart.failed", { count: cell.failed })}`}
                {cell.avg_latency_ms !== null && ` · ${Math.round(cell.avg_latency_ms).toLocaleString(t.intl)} ms`}
              </span>
            )}
          </span>
        </div>
        {cell?.error && <div className="mt-1 break-words font-mono text-down">{cell.error}</div>}
      </>
    );
  }
}
