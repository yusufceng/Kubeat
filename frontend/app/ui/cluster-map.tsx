"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import { uptime as formatUptime } from "@/lib/format";
import type { FleetItem } from "@/lib/fleet";
import { useT } from "@/lib/i18n/client";
import { MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { DownFor, groupCount } from "@/app/ui/monitor-row";
import { stateStyles } from "@/app/ui/status";
import { StatusGlyph } from "@/app/ui/status-glyph";

/** What a square needs: a monitor as the list has it, without its history. */
export type MapItem = Pick<FleetItem, "id" | "name" | "kindShort" | "namespace" | "state" | "uptime" | "downSince" | "error">;

export type MapGroup = { namespace: string; items: MapItem[] };

/** The square straight above or below another, the nearest across. */
function vertical(squares: HTMLElement[], from: HTMLElement, down: boolean): HTMLElement | undefined {
  const a = from.getBoundingClientRect();
  const x = a.left + a.width / 2;
  let best: HTMLElement | undefined;
  let bestDy = Infinity;
  let bestDx = Infinity;
  for (const s of squares) {
    const b = s.getBoundingClientRect();
    const dy = down ? b.top - a.top : a.top - b.top;
    if (dy <= 1) continue;
    const dx = Math.abs(b.left + b.width / 2 - x);
    if (dy < bestDy - 1 || (Math.abs(dy - bestDy) <= 1 && dx < bestDx)) {
      best = s;
      bestDy = dy;
      bestDx = dx;
    }
  }
  return best;
}

/**
 * The fleet as a map: a cell per namespace, a square per monitor in it,
 * colored and shaped by its state as the glyphs are (an outage wears a
 * halo, degraded is half filled, maintenance hatched, paused and pending
 * hollow). A square names its monitor on hover and opens it on a click.
 * Some three hundred monitors fit on a screen.
 *
 * The squares are one stop for the keyboard: the arrows move between them
 * (up and down to the nearest square above or below), Home and End to the
 * ends, Enter opens.
 */
export function ClusterMap({ groups }: { groups: MapGroup[] }) {
  const t = useT();
  const root = useRef<HTMLElement>(null);
  const [current, setCurrent] = useState<number | null>(null);
  const [tip, setTip] = useState<{ id: number; x: number; y: number; width: number } | null>(null);
  const all = groups.flatMap((g) => g.items);
  // The square the keyboard comes back to: the last one it was on, else the first.
  const tabbable = current !== null && all.some((i) => i.id === current) ? current : all[0]?.id;
  const tipItem = tip ? all.find((i) => i.id === tip.id) : undefined;

  function show(square: HTMLElement, id: number) {
    const box = root.current?.getBoundingClientRect();
    if (!box) return;
    const r = square.getBoundingClientRect();
    setTip({ id, x: r.left + r.width / 2 - box.left, y: r.top - box.top, width: box.width });
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLElement>) {
    const from = (e.target as HTMLElement).closest<HTMLElement>("a[data-square]");
    if (!from || e.altKey || e.ctrlKey || e.metaKey) return;
    const squares = [...(root.current?.querySelectorAll<HTMLElement>("a[data-square]") ?? [])];
    const at = squares.indexOf(from);
    let next: HTMLElement | undefined;
    if (e.key === "ArrowRight") next = squares[at + 1];
    else if (e.key === "ArrowLeft") next = squares[at - 1];
    else if (e.key === "ArrowDown" || e.key === "ArrowUp") next = vertical(squares, from, e.key === "ArrowDown");
    else if (e.key === "Home") next = squares[0];
    else if (e.key === "End") next = squares[squares.length - 1];
    else return;
    e.preventDefault();
    next?.focus();
  }

  return (
    <section ref={root} className="panel fl-map-panel" aria-labelledby="fl-map-title" onKeyDown={onKeyDown}>
      <header className="fl-panel-head">
        <h2 id="fl-map-title" className="type-heading">
          {t("monitorsList.map.title")}
        </h2>
        <span className="text-xs text-ink-3">{t("monitorsList.map.caption")}</span>
      </header>
      <p className="sr-only">{t("monitorsList.map.keys")}</p>
      <div className="fl-map">
        {groups.map((g) => {
          const label = g.namespace || t(MANUAL_LABEL);
          const count = groupCount(g.items, t);
          return (
            <div key={g.namespace} className="fl-cell" role="group" aria-label={`${label}: ${count.text}`}>
              <div className="fl-cell-head">
                <Link href={namespaceHref(g.namespace)} className={g.namespace ? "fl-cell-name" : "fl-cell-name fl-ns-manual"}>
                  {label}
                </Link>
                <span className={`fl-cell-count ${count.tone}`} aria-hidden>
                  {count.text}
                </span>
              </div>
              <ul className="fl-squares">
                {g.items.map((i) => {
                  const style = stateStyles[i.state];
                  return (
                    <li key={i.id}>
                      <Link
                        href={`/monitors/${i.id}`}
                        data-square={i.id}
                        data-state={style.glyph}
                        className="fl-sq"
                        tabIndex={i.id === tabbable ? 0 : -1}
                        aria-label={`${i.name}, ${t(style.label)}`}
                        onPointerEnter={(e) => show(e.currentTarget, i.id)}
                        onPointerLeave={() => setTip(null)}
                        onFocus={(e) => {
                          setCurrent(i.id);
                          show(e.currentTarget, i.id);
                        }}
                        onBlur={() => setTip(null)}
                      />
                    </li>
                  );
                })}
              </ul>
            </div>
          );
        })}
      </div>
      {tip && tipItem && <SquareTip item={tipItem} x={tip.x} y={tip.y} width={tip.width} />}
    </section>
  );
}

/** What a square stands for, over it: the monitor, its state and its trouble. */
function SquareTip({ item, x, y, width }: { item: MapItem; x: number; y: number; width: number }) {
  const t = useT();
  const style = stateStyles[item.state];
  // Near an edge the tip is anchored on that side, so it stays inside the panel.
  const shift = x < 130 ? "-18px" : x > width - 130 ? "calc(-100% + 18px)" : "-50%";
  return (
    <div className="fl-tip pop" style={{ left: x, top: y, translate: `${shift} calc(-100% - 8px)` }} aria-hidden>
      <p className="flex items-center gap-2 font-medium">
        <StatusGlyph kind={style.glyph} />
        <span className="truncate">{item.name}</span>
      </p>
      <p className="type-mono mt-0.5 truncate text-ink-3">
        {item.kindShort}
        {item.namespace && ` · ${item.namespace}`}
      </p>
      <p className={`mt-1.5 text-xs font-medium ${style.text}`}>
        {item.state === "down" && item.downSince ? <DownFor since={item.downSince} /> : t(style.label)}
      </p>
      {item.uptime !== null && item.state !== "paused" && (
        <p className="text-xs text-ink-3">{t("monitorsList.map.uptime", { uptime: formatUptime(item.uptime, t) })}</p>
      )}
      {item.error && <p className="type-mono mt-1 line-clamp-2 break-words text-ink-2">{item.error}</p>}
    </div>
  );
}
