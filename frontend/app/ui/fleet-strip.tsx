"use client";

import { useMemo } from "react";
import { duration } from "@/lib/format";
import { type FleetSlot, fleetSlots, type StripOutage, type StripWindow } from "@/lib/fleet-strip";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { type Beat, BeatStrip } from "@/app/ui/beat-strip";

/** Names of a slot's monitors or windows: the first few, then how many more. */
function names(list: string[], t: T): string {
  const shown = list.slice(0, 4).join(", ");
  return list.length > 4 ? `${shown} ${t("monitorsList.hero.strip.more", { count: list.length - 4 })}` : shown;
}

function slotBeat(slot: FleetSlot, t: T): Beat {
  if (slot.state === "none") return { state: "none" };
  const time = (ms: number) =>
    new Date(ms).toLocaleTimeString(t.intl, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  const range = `${time(slot.start)}–${time(slot.end)}`;
  switch (slot.state) {
    case "down":
      return {
        state: "down",
        level: slot.level,
        text: `${range} · ${t.n("monitorsList.hero.strip.down", slot.down.length)}`,
        detail: names(slot.down, t),
      };
    case "partial":
      return {
        state: "partial",
        level: slot.level,
        text: `${range} · ${t.n("monitorsList.hero.strip.partial", slot.down.length)}`,
        detail: names(slot.down, t),
      };
    case "maint":
      return { state: "maint", text: `${range} · ${t("monitorsList.hero.strip.maint")}`, detail: names(slot.maint, t) };
    default:
      return { state: "ok", text: `${range} · ${t("monitorsList.hero.strip.ok")}` };
  }
}

/**
 * The last hours of a fleet in slots of a quarter of an hour, newest on
 * the right: when monitors were down, briefly down or in maintenance. The
 * red (or amber) of a slot is as tall as the share of the fleet that was
 * down in it, so one monitor down all day is a low band, not a red strip.
 * It is worked out here from the outages and windows the page read, as of
 * the moment the server answered, so that both sides draw the same strip;
 * the times in a slot's hover are the viewer's own.
 */
export function FleetStrip({
  outages,
  windows,
  now,
  hours,
  slots,
  knownFrom,
  fleet,
}: {
  outages: StripOutage[];
  windows: StripWindow[];
  now: number;
  hours: number;
  slots: number;
  knownFrom: number;
  /** How many monitors the strip is about. */
  fleet: number;
}) {
  const t = useT();
  const strip = useMemo(
    () => fleetSlots({ outages, windows, now, hours, slots, knownFrom, fleet }),
    [outages, windows, now, hours, slots, knownFrom, fleet],
  );
  const beats = useMemo(() => strip.map((s) => slotBeat(s, t)), [strip, t]);
  const window = duration(hours * 3600, t);
  const slot = duration(Math.round((hours * 3600) / slots), t);
  const failing = strip.filter((s) => s.down.length > 0);
  const most = Math.max(0, ...failing.map((s) => s.down.length));
  return (
    <div className="fl-strip">
      <BeatStrip
        beats={beats}
        slots={slots}
        height={32}
        gap={1}
        label={
          failing.length === 0
            ? t("monitorsList.hero.strip.labelNone", { window })
            : t("monitorsList.hero.strip.label", { window, slots: failing.length, all: slots, most, total: fleet })
        }
      />
      <p className="fl-strip-legend" aria-hidden>
        <span>{t("format.ago", { duration: window })}</span>
        <span className="fl-strip-what">{t("monitorsList.hero.strip.what", { slot })}</span>
        <span>{t("monitorsList.hero.strip.now")}</span>
      </p>
    </div>
  );
}
