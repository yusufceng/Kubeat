"use client";

import Link from "next/link";
import { memo, useEffect, useLayoutEffect, useRef, useState } from "react";
import { duration, uptime as formatUptime } from "@/lib/format";
import type { FleetItem } from "@/lib/fleet";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { splitName } from "@/lib/kinds";
import { uptimeTone, type UptimeTones } from "@/lib/uptime-tone";
import { BeatStrip } from "@/app/ui/beat-strip";
import { CertIcon } from "@/app/ui/fleet-icons";
import { ChevronIcon } from "@/app/ui/icons";
import { LiveDuration } from "@/app/ui/live-duration";
import { type DisplayState, stateStyles } from "@/app/ui/status";
import { StatusGlyph } from "@/app/ui/status-glyph";

/** A row's uptime as a bare number ("99.86", "100") or a dash; floored, so that 99.996 is not a perfect 100. */
function uptimeFigure(item: FleetItem, t: T): string {
  if (item.state === "paused" || item.uptime === null) return t("common.none");
  const floored = Math.floor(item.uptime * 100) / 100;
  return floored === 100 ? "100" : floored.toLocaleString(t.intl, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

/** How wide each beat of a row is: as many as fit in about 72px, 2 to 4px each. */
function beatWidth(slots: number): number {
  return Math.min(4, Math.max(2, Math.floor(72 / slots) - 1));
}

/** Plays the row's flash once when its state changes between refreshes; never on the first render. */
function useStateFlash(ref: React.RefObject<HTMLElement | null>, state: DisplayState) {
  const previous = useRef(state);
  useEffect(() => {
    if (previous.current === state) return;
    previous.current = state;
    const el = ref.current;
    if (!el || matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    el.classList.remove("fl-flash");
    void el.offsetWidth; // so that the animation starts again
    el.classList.add("fl-flash");
  }, [ref, state]);
}

/** The certificate's days left, as a small amber chip (red once it expired). */
export function CertChip({ days }: { days: number }) {
  const t = useT();
  const expired = days < 0;
  const label = expired ? t("monitorsList.list.cert.expired") : t.n("monitorsList.list.cert.left", days);
  return (
    <span className={`fl-cert ${expired ? "fl-cert-expired" : ""}`} role="img" aria-label={label} title={label}>
      <CertIcon width={11} height={11} />
      <span aria-hidden>{expired ? t("monitorsList.list.cert.expiredShort") : t("monitorsList.list.cert.days", { count: days })}</span>
    </span>
  );
}

/**
 * One monitor of the list, 34px tall: its state, its name with the kind,
 * its recent checks and its uptime. The glyph's place holds a checkbox for
 * editors, shown on hover and while monitors are being selected.
 */
export const Row = memo(function Row({
  item,
  describedBy,
  beats,
  active,
  picked,
  canPick,
  selecting,
  onPick,
  tones,
  onHover,
}: {
  item: FleetItem;
  /** The id of what describes the list's keys, on the first row. */
  describedBy?: string;
  beats: number;
  active: boolean;
  picked: boolean;
  /** Whether the viewer may select monitors (and act on them). */
  canPick: boolean;
  /** Whether monitors are being selected: every checkbox shows. */
  selecting: boolean;
  onPick: (id: number, on: boolean) => void;
  tones: UptimeTones;
  /** The row the pointer or the keyboard rests on, for the hover card; null when it leaves. */
  onHover: (id: number | null, row: HTMLElement | null) => void;
}) {
  const t = useT();
  const ref = useRef<HTMLLIElement>(null);
  useStateFlash(ref, item.state);
  const style = stateStyles[item.state];
  const [name, rest] = splitName(item.name, item.discovered);
  const figure = uptimeFigure(item, t);
  const shown = item.beats.slice(-beats);
  const failed = shown.split("0").length - 1;

  return (
    <li
      ref={ref}
      className="fl-row"
      data-state={style.glyph}
      data-active={active || undefined}
      data-picked={picked || undefined}
      onPointerEnter={(e) => e.pointerType === "mouse" && onHover(item.id, e.currentTarget)}
      onPointerLeave={(e) => e.pointerType === "mouse" && onHover(null, null)}
    >
      {canPick ? (
        // The whole place of the glyph is the checkbox's target.
        <label className="fl-lead">
          <StatusGlyph kind={style.glyph} live={item.state === "down"} className="fl-glyph" />
          <input
            type="checkbox"
            className="fl-pick"
            checked={picked}
            onChange={(e) => onPick(item.id, e.target.checked)}
            // Outside selecting, x on the row ticks it: one stop per row for the keyboard.
            tabIndex={selecting ? 0 : -1}
            aria-label={t("monitorsList.list.row.select", { name: item.name })}
          />
        </label>
      ) : (
        <span className="fl-lead">
          <StatusGlyph kind={style.glyph} live={item.state === "down"} className="fl-glyph" />
        </span>
      )}
      <Link
        href={`/monitors/${item.id}`}
        data-row={item.id}
        aria-current={active ? "page" : undefined}
        aria-keyshortcuts={canPick ? "x" : undefined}
        aria-describedby={describedBy}
        className="fl-row-link"
        onFocus={(e) => e.currentTarget.matches(":focus-visible") && onHover(item.id, ref.current)}
        onBlur={() => onHover(null, null)}
      >
        <span className="fl-name">
          <span className="fl-name-text">
            {name}
            {rest && <span className="text-ink-3">{rest}</span>}
          </span>
          {/* A certificate near its end matters more than the kind, which the card still shows. */}
          {item.certDays !== null ? <CertChip days={item.certDays} /> : <span className="fl-kind">{item.kindShort}</span>}
          <span className="sr-only">, {t(style.label)}</span>
        </span>
        <BeatStrip
          beats={[...shown].map((beat) => ({ state: beat === "1" ? "ok" : "down" }))}
          slots={beats}
          height={14}
          slotWidth={beatWidth(beats)}
          gap={1}
          label={t("monitorsList.list.beats.label", { count: shown.length, failed })}
          className="shrink-0"
        />
        <span className={`fl-uptime ${item.state === "paused" ? "text-ink-3" : uptimeTone(item.uptime, tones)}`}>
          <span aria-hidden>{figure}</span>
          <span className="sr-only">
            {item.state === "paused" || item.uptime === null
              ? t("monitorsList.list.row.noUptime")
              : t("monitorsList.list.row.uptime", { uptime: formatUptime(item.uptime, t) })}
          </span>
        </span>
      </Link>
    </li>
  );
});

/** A namespace's states as one 44px bar: down, degraded, maintenance, up, idle. */
export function HealthBar({ items }: { items: FleetItem[] }) {
  const t = useT();
  const count = (states: DisplayState[]) => items.filter((i) => states.includes(i.state)).length;
  const parts: [string, number][] = [
    ["var(--down)", count(["down"])],
    ["var(--warn)", count(["verifying", "recovering"])],
    ["var(--maint)", count(["maintenance"])],
    ["var(--ok-bar)", count(["up"])],
    ["var(--line-strong)", count(["paused", "pending"])],
  ];
  return (
    <span
      className="fl-health"
      role="img"
      aria-label={t("monitorsList.list.group.health", {
        up: parts[3][1],
        down: parts[0][1],
        degraded: parts[1][1],
        idle: parts[2][1] + parts[4][1],
      })}
    >
      {parts.map(([color, n]) => n > 0 && <i key={color} style={{ flexGrow: n, background: color }} />)}
    </span>
  );
}

/** What a namespace's header says on its right: the trouble in it, or how many are up. */
export function groupCount(items: { state: DisplayState }[], t: T): { text: string; tone: string } {
  const down = items.filter((i) => i.state === "down").length;
  if (down > 0) return { text: t("monitorsList.list.group.down", { count: down }), tone: "text-down-ink" };
  const degraded = items.filter((i) => i.state === "verifying" || i.state === "recovering").length;
  if (degraded > 0) return { text: t("monitorsList.list.group.degraded", { count: degraded }), tone: "text-warn-ink" };
  return { text: `${items.filter((i) => i.state === "up").length}/${items.length}`, tone: "text-ink-3" };
}

/** "Down for 4 d 2 h", counting up; the sentence is split around its duration, which ticks. */
export function DownFor({ since, className = "" }: { since: string; className?: string }) {
  const t = useT();
  const [before, after] = t("monitorsList.list.card.downFor").split("{duration}");
  return (
    <span className={className}>
      {before}
      <LiveDuration since={since} />
      {after}
    </span>
  );
}

/** The chevron of a namespace that opens and closes. */
export function Chevron({ open }: { open: boolean }) {
  return <ChevronIcon width={14} height={14} className={`fl-chevron ${open ? "rotate-90" : ""}`} />;
}

/**
 * What a row is about beyond its line, after the pointer has rested on it
 * a moment: the object and the address, the namespace, how often it is
 * checked, its tags and what its latest check failed with. It floats beside
 * the list and takes no pointer, so the row stays clickable.
 */
export function HoverCard({ item, anchor }: { item: FleetItem; anchor: HTMLElement }) {
  const t = useT();
  const ref = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<{ left: number; top: number } | null>(null);

  // Beside the list, level with the row, and kept inside the window.
  useLayoutEffect(() => {
    const card = ref.current;
    if (!card) return;
    const row = anchor.getBoundingClientRect();
    const list = anchor.closest("[data-list]")?.getBoundingClientRect() ?? row;
    const height = card.offsetHeight;
    const top = Math.min(Math.max(8, row.top - 6), window.innerHeight - height - 8);
    setPosition({ left: list.right + 8, top });
  }, [anchor, item]);

  const style = stateStyles[item.state];
  return (
    <div
      ref={ref}
      aria-hidden
      className="fl-card pop"
      style={position ? { left: position.left, top: position.top } : { visibility: "hidden" }}
    >
      <div className="flex items-center gap-2">
        <StatusGlyph kind={style.glyph} live={item.state === "down"} />
        <span className="min-w-0 flex-1 truncate font-semibold">{item.name}</span>
        <span className={`text-xs font-medium ${style.text}`}>{t(style.label)}</span>
      </div>
      <p className="type-mono mt-1.5 truncate text-ink-2">{item.object ?? item.kindShort}</p>
      {item.target && <p className="type-mono mt-1 line-clamp-2 break-all text-ink-3">{item.target}</p>}
      <p className="mt-2 flex flex-wrap items-center gap-x-1.5 text-xs text-ink-3">
        {item.namespace && <span className="type-mono text-ink-2">{item.namespace}</span>}
        {item.namespace && <span aria-hidden>·</span>}
        <span>{t("monitorsList.list.card.every", { interval: duration(item.interval, t) })}</span>
        {item.tags.length > 0 && <span aria-hidden>·</span>}
        {item.tags.length > 0 && <span className="truncate">{item.tags.join(", ")}</span>}
      </p>
      {item.state === "down" && item.downSince && (
        <DownFor since={item.downSince} className="mt-2 block text-xs font-medium tabular-nums text-down-ink" />
      )}
      {item.error && <p className="type-mono mt-1.5 line-clamp-3 break-words text-down-ink">{item.error}</p>}
      {item.certDays !== null && (
        <p className={`mt-2 text-xs font-medium ${item.certDays < 0 ? "text-down-ink" : "text-warn-ink"}`}>
          {item.certDays < 0 ? t("monitorsList.list.cert.expired") : t.n("monitorsList.list.cert.left", item.certDays)}
        </p>
      )}
    </div>
  );
}
