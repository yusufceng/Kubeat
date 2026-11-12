"use client";

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { milliseconds } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { type GlyphKind, StatusGlyph } from "@/app/ui/status-glyph";

/**
 * What a slot shows: healthy, slow (a check that passed, but took longer
 * than its monitor's threshold), failed, partly failed (a bucket or a day
 * with some failures), in maintenance, a check of a monitor since paused
 * (idle, whatever it came to), or nothing known.
 */
export type SlotState = "ok" | "slow" | "down" | "partial" | "maint" | "idle" | "none";

/**
 * How much of a graded strip's height a slot stands to: what went well
 * stays two steps below the top, what was slow or partly failed one, and
 * a failure fills the height. The steps are small on purpose: a calm
 * stretch keeps most of its body and the outline still breaks where
 * something went wrong. They are shares of the height, not pixels,
 * because a page may set the strip's height itself; in eighths, so that a
 * strip whose height is a multiple of eight draws whole pixels (a row's
 * 16px strip: 12, 14, 16; a monitor page's 40px: 30, 35, 40).
 */
const GRADES: Partial<Record<SlotState, number>> = { ok: 6 / 8, idle: 6 / 8, maint: 6 / 8, slow: 7 / 8, partial: 7 / 8 };

/** The state sign a slot's hover carries next to its words. */
const GLYPHS: Partial<Record<SlotState, GlyphKind>> = {
  ok: "up",
  slow: "warn",
  partial: "warn",
  down: "down",
  maint: "maint",
  idle: "paused",
};

/**
 * How long the pointer rests on a strip it may only be passing over (a
 * list row's) before the first hover opens, and for how long after one
 * closes the next strip answers at once: reading down a column of strips
 * then takes one wait, not one a row. Plain numbers, not the motion
 * tokens: they are about intent, and stay as they are for a viewer who
 * asks for less motion. The clock is the events' own (timeStamp).
 */
const REST_MS = 120;
const WARM_MS = 300;
let warmUntil = 0;

/** How far the hover stands off the strip, and off the window's edges. */
const TIP_GAP = 6;
const TIP_MARGIN = 8;

/**
 * Whether the browser can open a popover by hand (Chrome 114, Safari 17,
 * Firefox 125). The older ones Next still serves get the same hover as a
 * fixed box at the end of the page: the same words in the same place,
 * only not in the top layer.
 */
const POPOVER = typeof HTMLElement !== "undefined" && typeof HTMLElement.prototype.showPopover === "function";

/** An error can be a page long: a screen reader is told its first line, to a sentence's length. */
const HEARD_CHARS = 160;

/** One slot of a strip: a check, a bucket of checks, or a day. */
export type Beat = {
  state: SlotState;
  /**
   * How much of the slot's height its state fills, from 0 to 1, the rest
   * being healthy: the share of a fleet that was down in it, say. 1 when
   * left out.
   */
  level?: number;
  /** When it ran or began, Unix milliseconds; shown on hover in the viewer's zone. */
  at?: number;
  /** When the span it covers ended, Unix milliseconds; the hover then writes both ends. */
  until?: number;
  /** The slot is a whole (UTC) day, so the hover shows its date alone. */
  day?: boolean;
  /** How long a check took, in milliseconds; the hover writes it, and how it stands to the threshold. */
  ms?: number;
  /** What a bucket or a day came to, in words: "99.20% uptime", "3 monitors down". */
  text?: string;
  /** An error, or the names behind a count, shown in mono under the words. */
  detail?: string;
};

/** Whether a slot has anything to say on hover. */
function tells(beat: Beat | null): beat is Beat {
  return beat !== null && (beat.at !== undefined || beat.ms !== undefined || !!beat.text || !!beat.detail);
}

/** The slots of a strip that are on screen: a narrow screen hides the oldest (see narrowSlots). */
function visibleSlots(strip: HTMLElement, narrowSlots: number | undefined): { slots: HTMLElement[]; first: number } {
  const slots = [...strip.querySelectorAll<HTMLElement>(":scope > .slot")];
  const hidden = narrowSlots !== undefined && slots.length > 0 && slots[0].getClientRects().length === 0;
  return { slots, first: hidden ? Math.max(0, slots.length - narrowSlots) : 0 };
}

/**
 * The slot under a pointer, from where the first and the last one stand;
 * the gaps between slots belong to the nearer one, so that the hover does
 * not flicker while the pointer crosses the strip.
 */
function slotAt(strip: HTMLElement, clientX: number, narrowSlots: number | undefined): number | null {
  const { slots, first } = visibleSlots(strip, narrowSlots);
  const count = slots.length - first;
  if (count <= 0) return null;
  const a = slots[first].getBoundingClientRect();
  const b = slots[slots.length - 1].getBoundingClientRect();
  const pitch = count > 1 ? (b.left - a.left) / (count - 1) : a.width;
  if (pitch <= 0) return null;
  const index = Math.floor((clientX - a.left + (pitch - a.width) / 2) / pitch);
  return first + Math.min(Math.max(index, 0), count - 1);
}

/**
 * How far the keyboard's ring reaches out of a strip it is drawn around,
 * nothing without one: the hover stands off the ring, not on it.
 */
function ringReach(strip: HTMLElement): number {
  if (!strip.matches(":focus-visible")) return 0;
  const style = getComputedStyle(strip);
  if (style.outlineStyle === "none") return 0;
  return Math.max(0, (parseFloat(style.outlineOffset) || 0) + (parseFloat(style.outlineWidth) || 0));
}

/** What a slot was, in the parts its hover shows and a screen reader is told. */
function describe(beat: Beat, slowAboveMs: number, t: T) {
  const clock = (at: number, seconds: boolean) =>
    new Date(at).toLocaleTimeString(t.intl, {
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
      ...(seconds && { second: "2-digit" }),
    });
  const date = (at: number) => new Date(at).toLocaleDateString(t.intl, { day: "numeric", month: "short" });
  const when =
    beat.at === undefined
      ? undefined
      : beat.day
        ? new Date(beat.at).toLocaleDateString(t.intl, {
            timeZone: "UTC",
            weekday: "short",
            day: "numeric",
            month: "short",
            year: "numeric",
          })
        : beat.until !== undefined
          ? `${date(beat.at)} ${clock(beat.at, false)} – ${clock(beat.until, false)}`
          : `${date(beat.at)} ${clock(beat.at, true)}`;
  const glyph = GLYPHS[beat.state];
  const base = { when, glyph, detail: beat.detail, tone: "text-ink", word: undefined, note: undefined };
  // A bucket or a day says what it came to in its own words.
  if (beat.text) return { ...base, reading: beat.text };
  if (beat.state === "down") {
    return {
      ...base,
      reading: t("beat.failed"),
      tone: "text-down-ink",
      // A failure took its time too: a timeout reads differently from a refusal.
      note: beat.ms !== undefined && beat.ms >= 1 ? milliseconds(beat.ms, t) : undefined,
    };
  }
  if (beat.ms === undefined) return { ...base, reading: undefined };
  const slow = beat.state === "slow";
  return {
    ...base,
    reading: milliseconds(beat.ms, t),
    tone: slow ? "text-warn-ink" : "text-ink",
    // The figure alone does not name the outcome; the glyph does for the eye, this for the ear.
    word: slow ? t("beat.slow") : t("beat.ok"),
    note: slow && slowAboveMs > 0 ? t("beat.threshold", { value: milliseconds(slowAboveMs, t) }) : undefined,
  };
}

/**
 * The one way Kubeat draws time as slots, newest on the right: the
 * heartbeat of a monitor, the beats of a list row, the days of a status
 * page. Healthy slots are a calm green, failures red, slow and partial ones
 * amber, maintenance hatched; where nothing is known there is only a
 * baseline, so a young monitor does not look like it has a long healthy
 * past.
 *
 * beats are oldest first; when there are fewer than slots, the strip is
 * padded on the left with slots of no data. Without slotWidth the slots
 * share the width; narrowSlots keeps only the newest that many on small
 * screens. graded draws a slot's state as its height as well (see GRADES),
 * so the strip reads by its outline where color does not: a calm stretch
 * is a low even line, and trouble stands out of it. idle draws every slot
 * idle grey, for a monitor since paused: its checks are history, not how
 * it stands, and their hover still tells what each came to. label is read
 * in place of the picture.
 *
 * A slot that knows its time or its outcome tells it on hover: when, what
 * it came to (against slowAboveMs, the monitor's threshold, when it was
 * slow) and its error. The pointer picks the slot by where it is along
 * the strip, a finger by a touch or a sideways drag. passing is for a
 * strip among many that the pointer crosses on its way elsewhere (a list
 * row's): the first hover waits for the pointer to rest. Escape puts the
 * hover away.
 *
 * keys makes the strip a stop for the keyboard: a slider over its slots
 * that stands at the newest one, which the arrow keys, Home and End move
 * and a screen reader reads as it moves. The keyboard holds a check, not
 * a place: when a newer one lands, what is being read moves along with
 * its slot. arrivals lets the newest slot rise once when a check lands.
 * onScrub tells the surroundings that the pointer is reading the strip,
 * so that they can put their own hover away.
 */
export function BeatStrip({
  beats,
  slots,
  height = 30,
  slotWidth,
  gap = 2,
  narrowSlots,
  graded = false,
  idle = false,
  slowAboveMs = 0,
  passing = false,
  keys = false,
  arrivals = false,
  onScrub,
  label,
  className = "",
}: {
  beats: Beat[];
  slots: number;
  height?: number;
  slotWidth?: number;
  gap?: number;
  narrowSlots?: number;
  graded?: boolean;
  idle?: boolean;
  slowAboveMs?: number;
  passing?: boolean;
  keys?: boolean;
  arrivals?: boolean;
  onScrub?: (scrubbing: boolean) => void;
  label: string;
  className?: string;
}) {
  const t = useT();
  const shown = beats.slice(-slots);
  const padding = Math.max(0, slots - shown.length);
  const all: (Beat | null)[] = [...Array.from({ length: padding }, () => null), ...shown];
  const root = useRef<HTMLDivElement>(null);
  const tip = useRef<HTMLDivElement>(null);
  // The slot being read: a pointer reads a place along the strip, the
  // keyboard holds a check by its time (kept) and follows it from there.
  const [hovered, setHovered] = useState<number | null>(null);
  const [kept, setKept] = useState<number | null>(null);
  // The oldest slot on screen when the keyboard last came by (a narrow screen hides the oldest).
  const [first, setFirst] = useState(0);
  const open = useRef(false);
  const dismissed = useRef(false);
  const rest = useRef<{ timer: ReturnType<typeof setTimeout>; index: number | null } | null>(null);
  // The frame in which a strip the keyboard has just come to shows its newest slot.
  const arrival = useRef(0);
  // The check the newest slot stood for when it last rose, or when the strip first showed.
  const newest = shown.length > 0 ? shown[shown.length - 1].at : undefined;
  const [seen, setSeen] = useState(newest);
  const tips = shown.some(tells);
  // The slot in hand; a strip that changed under it (fewer slots, a slot
  // gone quiet, the held check fallen off the old end) lets go.
  const place = kept === null ? hovered : all.findIndex((beat) => beat?.at === kept);
  const active =
    place !== null && place >= (kept === null ? 0 : first) && place < all.length && tells(all[place]) ? place : null;
  const walkable = keys && tips;
  // A no-data slot is a baseline, a little heavier on the tall strips.
  const baseline = height >= 24 ? 3 : 2;

  const show = (index: number | null, byKey = false) => {
    open.current = index !== null;
    setHovered(index);
    setKept(byKey && index !== null ? (all[index]?.at ?? null) : null);
  };
  const unrest = () => {
    if (rest.current) clearTimeout(rest.current.timer);
    rest.current = null;
  };

  // The hover is a popover, so that it is drawn over everything and no
  // scrolling list can cut it off; here it is opened and set over the
  // slot in hand (under it where there is no room above), inside the
  // window. Run after every render, as its words change its size.
  useLayoutEffect(() => {
    const el = tip.current;
    const strip = root.current;
    if (!el || !strip || active === null) return;
    if (POPOVER && !el.matches(":popover-open")) el.showPopover();
    const slot = strip.querySelectorAll<HTMLElement>(":scope > .slot")[active];
    if (!slot) return;
    const s = slot.getBoundingClientRect();
    const r = strip.getBoundingClientRect();
    const gap = TIP_GAP + ringReach(strip);
    const left = s.left + s.width / 2 - el.offsetWidth / 2;
    const above = r.top - el.offsetHeight - gap;
    el.style.left = `${Math.max(TIP_MARGIN, Math.min(left, window.innerWidth - el.offsetWidth - TIP_MARGIN))}px`;
    el.style.top = `${above >= TIP_MARGIN ? above : r.bottom + gap}px`;
  });

  // The page moving under the hover, a press anywhere else or Escape closes it.
  const reading = active !== null;
  useEffect(() => {
    if (!reading) return;
    const close = () => {
      open.current = false;
      setHovered(null);
      setKept(null);
    };
    const away = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) close();
    };
    // Put away with Escape, a pointer's hover stays away until the pointer leaves the strip or presses it.
    const escape = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      dismissed.current = true;
      close();
    };
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    document.addEventListener("pointerdown", away);
    document.addEventListener("keydown", escape);
    return () => {
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
      document.removeEventListener("pointerdown", away);
      document.removeEventListener("keydown", escape);
    };
  }, [reading]);

  // A wait still running when the strip goes away opens nothing.
  useEffect(
    () => () => {
      if (rest.current) clearTimeout(rest.current.timer);
      cancelAnimationFrame(arrival.current);
    },
    [],
  );

  const point = (e: React.PointerEvent<HTMLDivElement>) => {
    const mouse = e.pointerType === "mouse";
    // A strip inside a link leaves a touch to the link.
    if (!mouse && e.currentTarget.closest("a")) return;
    if (e.type !== "pointermove") dismissed.current = false;
    else if (dismissed.current) return;
    const index = slotAt(e.currentTarget, e.clientX, narrowSlots);
    const next = index !== null && tells(all[index]) ? index : null;
    if (open.current || !passing || !mouse || e.timeStamp < warmUntil) {
      unrest();
      show(next);
    } else if (rest.current) {
      rest.current.index = next;
    } else {
      const timer = setTimeout(() => {
        const index = rest.current?.index ?? null;
        rest.current = null;
        show(index);
      }, REST_MS);
      rest.current = { timer, index: next };
    }
  };

  /** The slots the keyboard stops at: those on screen that have something to say. */
  const stopsFrom = (from: number) => all.flatMap((beat, i) => (i >= from && tells(beat) ? [i] : []));

  const walk = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      show(null);
      return;
    }
    // A slider's keys: right and up are newer, left and down older.
    const step =
      e.key === "ArrowRight" || e.key === "ArrowUp" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowDown" ? -1 : 0;
    if (step === 0 && e.key !== "Home" && e.key !== "End") return;
    e.preventDefault();
    const from = visibleSlots(e.currentTarget, narrowSlots).first;
    setFirst(from);
    const stops = stopsFrom(from);
    if (stops.length === 0) return;
    // With nothing in hand the strip stands at its newest slot, as it tells a screen reader.
    const at = active === null ? stops.length - 1 : stops.indexOf(active);
    const next =
      e.key === "Home"
        ? stops[0]
        : e.key === "End"
          ? stops[stops.length - 1]
          : stops[Math.min(Math.max(at + step, 0), stops.length - 1)];
    show(next, true);
  };

  const arrived = arrivals && newest !== seen;
  const told = active !== null ? describe(all[active]!, slowAboveMs, t) : null;

  // The slider's value, for a screen reader: the slot in hand, the newest one otherwise, in words.
  const stops = walkable ? stopsFrom(first) : [];
  const value = active !== null && stops.includes(active) ? active : (stops[stops.length - 1] ?? null);
  const heard = (beat: Beat) => {
    const d = describe(beat, slowAboveMs, t);
    const line = d.detail?.split("\n", 1)[0].trim();
    const detail = line && line.length > HEARD_CHARS ? `${line.slice(0, HEARD_CHARS)}…` : line;
    return [d.when, d.word, d.reading, d.note, detail].filter(Boolean).join(", ");
  };

  const hover = told && (
    <div ref={tip} popover={POPOVER ? "manual" : undefined} aria-hidden className="beat-tip pop text-xs">
      {told.when && <span className="text-ink-3 tabular-nums">{told.when}</span>}
      {told.reading && (
        <span className="beat-tip-read">
          {told.glyph && <StatusGlyph kind={told.glyph} />}
          <strong className={told.tone}>{told.reading}</strong>
          {told.note && <span className="text-ink-3 tabular-nums">{told.note}</span>}
        </span>
      )}
      {told.detail && <span className="beat-tip-detail type-mono-s text-ink-2">{told.detail}</span>}
    </div>
  );

  return (
    <div
      ref={root}
      role={walkable ? "slider" : "img"}
      aria-label={label}
      aria-valuemin={walkable ? 1 : undefined}
      aria-valuemax={walkable ? Math.max(stops.length, 1) : undefined}
      aria-valuenow={walkable ? Math.max(stops.indexOf(value ?? -1) + 1, 1) : undefined}
      aria-valuetext={walkable && value !== null ? heard(all[value]!) : undefined}
      tabIndex={walkable ? 0 : undefined}
      className={`beat-strip ${keys ? "rounded-sm" : ""} ${className}`}
      data-scrub={active !== null || undefined}
      // A finger reads along the strip; up and down still scroll the page, and two fingers still zoom it.
      style={{
        height,
        gap,
        width: slotWidth ? "fit-content" : undefined,
        touchAction: tips && !passing ? "pan-y pinch-zoom" : undefined,
      }}
      onPointerEnter={
        tips
          ? (e) => {
              if (e.pointerType === "mouse") onScrub?.(true);
              point(e);
            }
          : undefined
      }
      onPointerMove={tips ? point : undefined}
      onPointerDown={tips ? point : undefined}
      onPointerLeave={
        tips
          ? (e) => {
              unrest();
              // A finger's hover stays until a touch elsewhere: it left with the tap that opened it.
              if (e.pointerType !== "mouse") return;
              dismissed.current = false;
              onScrub?.(false);
              if (open.current) warmUntil = e.timeStamp + WARM_MS;
              show(null);
            }
          : undefined
      }
      onKeyDown={walkable ? walk : undefined}
      onFocus={
        walkable
          ? (e) => {
              const el = e.currentTarget;
              const from = visibleSlots(el, narrowSlots).first;
              setFirst(from);
              // Arriving by keyboard shows the newest slot: what a screen reader is told is on screen
              // too. A frame later, since the browser may be bringing the strip into view, and that
              // scroll (told before the frame's callbacks run) would close a hover opened here.
              cancelAnimationFrame(arrival.current);
              arrival.current = requestAnimationFrame(() => {
                if (document.activeElement !== el || !el.matches(":focus-visible")) return;
                const stops = stopsFrom(from);
                if (stops.length > 0) show(stops[stops.length - 1], true);
              });
            }
          : undefined
      }
      onBlur={
        walkable
          ? () => {
              cancelAnimationFrame(arrival.current);
              show(null);
            }
          : undefined
      }
    >
      {all.map((beat, i) => {
        const state = beat === null ? "none" : idle && beat.state !== "none" ? "idle" : beat.state;
        // A part of a slot: its state from the foot up, healthy above.
        const part = beat?.level !== undefined && beat.level < 1 && (state === "down" || state === "partial");
        const grade = graded ? GRADES[state] : undefined;
        const rises = arrived && i === all.length - 1;
        return (
          <span
            key={i}
            className={`slot slot-${state}${rises ? " slot-new" : ""} ${narrowSlots !== undefined && i < slots - narrowSlots ? "max-sm:hidden" : ""}`}
            data-on={active === i || undefined}
            style={{
              width: slotWidth,
              flex: slotWidth ? undefined : "1 1 0",
              minWidth: slotWidth ? undefined : 2,
              height: state === "none" ? baseline : grade !== undefined ? `${grade * 100}%` : undefined,
              background: part
                ? `linear-gradient(to top, var(--${state === "down" ? "down" : "warn"}) ${Math.round(beat!.level! * 100)}%, var(--ok-bar) 0)`
                : undefined,
            }}
            onAnimationEnd={rises ? () => setSeen(newest) : undefined}
          />
        );
      })}
      {POPOVER ? hover : hover && createPortal(hover, document.body)}
    </div>
  );
}
