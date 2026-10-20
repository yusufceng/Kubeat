"use client";

import { useState } from "react";
import { useT } from "@/lib/i18n/client";

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
  /** When it ran or began, Unix milliseconds; shown on hover in the viewer's zone. */
  at?: number;
  /** The slot is a whole (UTC) day, so the hover shows its date alone. */
  day?: boolean;
  /** What it came to, in words: "38 ms", "99.20% uptime", "Failed". */
  text?: string;
  /** An error, shown in mono under the text. */
  detail?: string;
};

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
  const shown = beats.slice(-slots);
  const padding = Math.max(0, slots - shown.length);
  const all: (Beat | null)[] = [...Array.from({ length: padding }, () => null), ...shown];
  const [hover, setHover] = useState<{ index: number; x: number; width: number } | null>(null);
  // Strips whose slots say nothing beyond their color (the list rows) skip
  // the hover altogether.
  const tips = shown.some((b) => b.at !== undefined || b.text || b.detail);
  // A no-data slot is a baseline, a little heavier on the tall strips.
  const baseline = height >= 24 ? 3 : 2;

  return (
    <div
      role="img"
      aria-label={label}
      className={`beat-strip ${className}`}
      style={{ height, gap, width: slotWidth ? "fit-content" : undefined }}
      onPointerLeave={tips ? () => setHover(null) : undefined}
    >
      {all.map((beat, i) => {
        const state = beat?.state ?? "none";
        // A part of a slot: its state from the foot up, healthy above.
        const part = beat?.level !== undefined && beat.level < 1 && (state === "down" || state === "partial");
        return (
          <span
            key={i}
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
            onPointerEnter={
              tips && beat && (beat.at !== undefined || beat.text || beat.detail)
                ? (e) => {
                    const slot = e.currentTarget;
                    const strip = slot.parentElement!;
                    setHover({ index: i, x: slot.offsetLeft + slot.offsetWidth / 2, width: strip.offsetWidth });
                  }
                : undefined
            }
          />
        );
      })}
      {hover && all[hover.index] && <SlotTip beat={all[hover.index]!} x={hover.x} width={hover.width} />}
    </div>
  );
}

/** What a hovered slot was: when, what it came to, and its error. */
function SlotTip({ beat, x, width }: { beat: Beat; x: number; width: number }) {
  const t = useT();
  // Near an edge the tip is anchored on that side, so it stays over the strip.
  const align = x < 90 ? "translateX(-14px)" : x > width - 90 ? "translateX(calc(-100% + 14px))" : "translateX(-50%)";
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
  return (
    <span
      className="pop pointer-events-none absolute bottom-[calc(100%+6px)] z-20 flex w-max max-w-72 flex-col gap-0.5 px-2.5 py-2 text-xs"
      style={{ left: x, transform: align }}
    >
      {when && <span className="text-ink-3">{when}</span>}
      {beat.text && <span className="font-medium text-ink">{beat.text}</span>}
      {beat.detail && <span className="type-mono-s break-words text-ink-2">{beat.detail}</span>}
    </span>
  );
}
