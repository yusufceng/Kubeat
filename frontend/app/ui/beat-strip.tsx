"use client";

import { useId, useLayoutEffect, useRef, useState } from "react";
import { useT } from "@/lib/i18n/client";
import { type GlyphKind, StatusGlyph } from "@/app/ui/status-glyph";

/**
 * What a slot shows: healthy, failed, partly failed (a bucket or a day with
 * some failures), in maintenance, a check of a monitor since paused (idle,
 * whatever it came to), or nothing known.
 */
export type SlotState = "ok" | "down" | "partial" | "maint" | "idle" | "none";

/** One slot of a strip: a check, a bucket of checks, or a day. */
export type Beat = {
  state: SlotState;
  /**
   * How much of the slot's height its state fills, from 0 to 1, the rest
   * being healthy: the share of a fleet that was down in it, say. 1 when
   * left out.
   */
  level?: number;
  /** When it ran or began, Unix milliseconds; shown in the tip in the viewer's zone. */
  at?: number;
  /** The slot is a whole (UTC) day, so the tip shows its date alone. */
  day?: boolean;
  /** The state in a word, at the head of the tip beside its glyph: "Succeeded", "Failed". */
  word?: string;
  /** What it came to, in words: "38 ms", "99.20% uptime". */
  text?: string;
  /** An error, shown in mono under the text. */
  detail?: string;
};

/** The glyph of a slot's state: the language of the rows, the filter and the hero. */
const glyphOf: Record<Exclude<SlotState, "none">, GlyphKind> = {
  ok: "up",
  down: "down",
  partial: "warn",
  maint: "maint",
  idle: "paused",
};

/** Whether a slot has anything to tell beyond its color. */
const tells = (beat: Beat | null): beat is Beat =>
  beat !== null && (beat.at !== undefined || !!beat.word || !!beat.text || !!beat.detail);

/** How far from the window's edge a tip keeps, in pixels. */
const EDGE = 8;

/**
 * The one way Kubeat draws time as slots, newest on the right: the
 * heartbeat of a monitor, the beats of a list row, the days of a status
 * page. Healthy slots are a calm green, failures red, partial ones amber,
 * maintenance hatched; where nothing is known there is only a baseline, so
 * a young monitor does not look like it has a long healthy past.
 *
 * beats are oldest first; when there are fewer than slots, the strip is
 * padded on the left with slots of no data. Without slotWidth the slots
 * share the width; narrowSlots keeps only the newest that many on small
 * screens. label is read in place of the picture.
 *
 * A strip whose slots have something to tell (when, a word, a figure, an
 * error) tells it in a tip: under the pointer, through one listener on the
 * strip, and from the keyboard, where the strip is one stop and the arrows
 * walk its slots (Home and End to the oldest and the newest, Escape closes).
 * The tip is a live region, so the keyboard hears what it shows. A strip
 * whose slots say nothing beyond their color (a list row) stays a picture.
 */
export function BeatStrip({
  beats,
  slots,
  height = 30,
  slotWidth,
  gap = 2,
  narrowSlots,
  label,
  className = "",
}: {
  beats: Beat[];
  slots: number;
  height?: number;
  slotWidth?: number;
  gap?: number;
  narrowSlots?: number;
  label: string;
  className?: string;
}) {
  const t = useT();
  const hintId = useId();
  const strip = useRef<HTMLDivElement>(null);
  const shown = beats.slice(-slots);
  const padding = Math.max(0, slots - shown.length);
  const all: (Beat | null)[] = [...Array.from({ length: padding }, () => null), ...shown];
  // The slot in hand, from the pointer or the keyboard, and where its middle is.
  const [active, setActive] = useState<{ index: number; x: number } | null>(null);
  const tips = shown.some(tells);
  // A no-data slot is a baseline, a little heavier on the tall strips.
  const baseline = height >= 24 ? 3 : 2;

  // The slots are the strip's first children, in order.
  const show = (index: number) => {
    const slot = strip.current?.children[index] as HTMLElement | undefined;
    if (!slot) return;
    setActive({ index, x: slot.offsetLeft + slot.offsetWidth / 2 });
  };
  // The arrows walk the slots that have something to tell; from nothing,
  // left goes to the newest and right to the oldest.
  const step = (from: number | null, delta: 1 | -1) => {
    let i = from ?? (delta > 0 ? -1 : all.length);
    for (i += delta; i >= 0 && i < all.length; i += delta) {
      if (tells(all[i])) {
        show(i);
        return;
      }
    }
  };

  return (
    <div
      ref={strip}
      role={tips ? "group" : "img"}
      aria-label={label}
      aria-describedby={tips ? hintId : undefined}
      tabIndex={tips ? 0 : undefined}
      className={`beat-strip ${className}`}
      style={{ height, gap, width: slotWidth ? "fit-content" : undefined }}
      onPointerOver={
        tips
          ? (e) => {
              const index = (e.target as HTMLElement).dataset.slot;
              if (index !== undefined) show(Number(index));
            }
          : undefined
      }
      onPointerLeave={tips ? () => setActive(null) : undefined}
      onBlur={tips ? () => setActive(null) : undefined}
      onKeyDown={
        tips
          ? (e) => {
              if (e.altKey || e.ctrlKey || e.metaKey) return;
              const at = active?.index ?? null;
              if (e.key === "ArrowRight") step(at, 1);
              else if (e.key === "ArrowLeft") step(at, -1);
              else if (e.key === "Home") step(null, 1);
              else if (e.key === "End") step(null, -1);
              else if (e.key === "Escape" && active) setActive(null);
              else return;
              e.preventDefault();
            }
          : undefined
      }
    >
      {all.map((beat, i) => {
        const state = beat?.state ?? "none";
        // A part of a slot: its state from the foot up, healthy above.
        const part = beat?.level !== undefined && beat.level < 1 && (state === "down" || state === "partial");
        return (
          <span
            key={i}
            data-slot={tips && tells(beat) ? i : undefined}
            className={`slot slot-${state} ${narrowSlots !== undefined && i < slots - narrowSlots ? "max-sm:hidden" : ""}`}
            style={{
              width: slotWidth,
              flex: slotWidth ? undefined : "1 1 0",
              minWidth: slotWidth ? undefined : 2,
              height: state === "none" ? baseline : undefined,
              background: part
                ? `linear-gradient(to top, var(--${state === "down" ? "down" : "warn"}) ${Math.round(beat!.level! * 100)}%, var(--ok-bar) 0)`
                : undefined,
            }}
          />
        );
      })}
      {tips && (
        <span id={hintId} className="sr-only">
          {t("beatStrip.keys")}
        </span>
      )}
      {active && tells(all[active.index]) && <SlotTip beat={all[active.index]!} x={active.x} />}
    </div>
  );
}

/**
 * What the slot in hand was: when, its state as a glyph and a word with
 * what it came to, and its error. Centred on the slot, then kept inside
 * the window.
 */
function SlotTip({ beat, x }: { beat: Beat; x: number }) {
  const t = useT();
  const ref = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    const tip = ref.current;
    if (!tip) return;
    tip.style.translate = "0 0";
    const r = tip.getBoundingClientRect();
    const dx = Math.max(EDGE - r.left, Math.min(0, window.innerWidth - EDGE - r.right));
    tip.style.translate = `${dx}px 0`;
  }, [x, beat]);

  const when =
    beat.at === undefined
      ? null
      : beat.day
        ? new Date(beat.at).toLocaleDateString(t.intl, {
            timeZone: "UTC",
            weekday: "short",
            day: "numeric",
            month: "short",
            year: "numeric",
          })
        : new Date(beat.at).toLocaleString(t.intl, {
            day: "numeric",
            month: "short",
            hour: "2-digit",
            minute: "2-digit",
            second: "2-digit",
            hourCycle: "h23",
          });
  const glyph = beat.state === "none" ? null : glyphOf[beat.state];
  const words = [beat.word, beat.text].filter(Boolean).join(" · ");
  return (
    <span ref={ref} role="status" className="pop beat-tip" style={{ left: x, transform: "translateX(-50%)" }}>
      {when && <span className="text-ink-3 tabular-nums">{when}</span>}
      {(glyph || words) && (
        <span className="beat-tip-state">
          {glyph && <StatusGlyph kind={glyph} />}
          {words && <span className="text-ink">{words}</span>}
        </span>
      )}
      {beat.detail && <span className="type-mono-s break-words text-ink-2">{beat.detail}</span>}
    </span>
  );
}
