// What the monitor list and the pages beside it show, read once per request.
import "server-only";

import { connection } from "next/server";
import { cache } from "react";
import { listMonitors, type Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import { monitorLabel } from "@/app/ui/monitor-name";
import { type DisplayState, displayState } from "@/app/ui/status";

/**
 * Every monitor with its most recent checks. The list in the sidebar and
 * the page next to it both need them; cache() makes that one request to the
 * backend per render instead of two.
 */
export const workspaceMonitors = cache(async () => {
  // How much history to ask for is a setting of the running server.
  await connection();
  return listMonitors({ history: config.listBeats });
});

/** A monitor as the list shows it: only what a row needs. */
export type ListItem = {
  id: number;
  name: string;
  /** "Service", "Route", ... for discovered monitors. */
  kind: string | null;
  type: Monitor["type"];
  namespace: string;
  tags: string[];
  target: string;
  state: DisplayState;
  uptime: number | null;
  /** The recent checks, oldest first: "1" for a success, "0" for a failure. */
  beats: string;
  /** Kept in step with an annotated cluster object; it cannot be deleted here. */
  managed: boolean;
};

export function toListItem(m: Monitor): ListItem {
  const label = monitorLabel(m);
  return {
    id: m.id,
    name: label.name,
    kind: label.kind,
    type: m.type,
    namespace: m.namespace,
    tags: m.tags,
    target: m.target,
    state: displayState(m),
    uptime: m.uptime_percent,
    managed: m.managed,
    beats: m.history
      .toReversed()
      .map((h) => (h.up ? "1" : "0"))
      .join(""),
  };
}

/** How many monitors are in each state. */
export type StateCounts = Record<DisplayState, number> & { total: number };

export function countStates(monitors: Pick<Monitor, "status" | "open_incident" | "maintenance_until">[]): StateCounts {
  const counts: StateCounts = {
    up: 0, down: 0, verifying: 0, recovering: 0, maintenance: 0, pending: 0, paused: 0, total: monitors.length,
  };
  for (const m of monitors) counts[displayState(m)]++;
  return counts;
}

/** A monitor whose latest check failed or that has an open outage. */
export function isProblem(state: DisplayState): boolean {
  return state === "down" || state === "recovering" || state === "verifying";
}

/** The groups monitors entered by hand are in, for the form to suggest. */
export function manualGroups(monitors: Monitor[]): string[] {
  return [...new Set(monitors.filter((m) => m.source === "" && m.namespace !== "").map((m) => m.namespace))].sort();
}
