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
      return { state: "down", text: `${range} · ${t.n("monitorsList.hero.strip.down", slot.down.length)}`, detail: names(slot.down, t) };
    case "partial":
      return {
        state: "partial",
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
 * the right: when monitors were down, briefly down or in maintenance. It
 * is worked out here from the outages and windows the page read, as of the
 * moment the server answered, so that both sides draw the same strip; the
 * times in a slot's hover are the viewer's own.
 */
export function FleetStrip({
  outages,
  windows,
  now,
  hours,
  slots,
  knownFrom,
}: {
  outages: StripOutage[];
  windows: StripWindow[];
  now: number;
  hours: number;
  slots: number;
  knownFrom: number;
}) {
  const t = useT();
  const fleet = useMemo(
    () => fleetSlots({ outages, windows, now, hours, slots, knownFrom }),
    [outages, windows, now, hours, slots, knownFrom],
  );
  const beats = useMemo(() => fleet.map((s) => slotBeat(s, t)), [fleet, t]);
  const window = duration(hours * 3600, t);
  const slot = duration(Math.round((hours * 3600) / slots), t);
  return (
    <div className="fl-strip">
      <BeatStrip
        beats={beats}
        slots={slots}
        height={32}
        gap={1}
        label={t("monitorsList.hero.strip.label", {
          window,
          down: fleet.filter((s) => s.state === "down").length,
          partial: fleet.filter((s) => s.state === "partial").length,
        })}
      />
      <p className="fl-strip-legend" aria-hidden>
        <span>{t("format.ago", { duration: window })}</span>
        <span className="fl-strip-what">{t("monitorsList.hero.strip.what", { slot })}</span>
        <span>{t("monitorsList.hero.strip.now")}</span>
      </p>
    </div>
  );
}
