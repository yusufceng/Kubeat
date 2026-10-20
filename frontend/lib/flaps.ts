/*
 * A monitor that flaps fails and recovers over and over, each outage short.
 * Told one by one, its outages bury everything else: a page of incidents,
 * a day of activity. A run is MIN_RUN or more outages of one monitor, each
 * over and shorter than the window, each beginning within the window of
 * the end of the one before; the activity feed tells a run as one line and
 * the incident list as one row.
 *
 * No imports, so that the tests run it as it is.
 */

/** Fewer short outages than this in a row are told one by one. */
export const MIN_RUN = 3;

/** What an incident needs to be judged here. */
export type Spell = { monitor_id: number; kind: "down" | "cert"; started_at: string; resolved_at: string | null };

/** A short outage that is over: shorter than the window. */
export function isBlip(i: Spell, windowMs: number): boolean {
  return i.kind === "down" && i.resolved_at !== null && Date.parse(i.resolved_at) - Date.parse(i.started_at) < windowMs;
}

/** Whether two short outages of one monitor follow each other closely enough to be one run. */
function near(a: Spell, b: Spell, windowMs: number): boolean {
  const [first, second] = Date.parse(a.started_at) <= Date.parse(b.started_at) ? [a, b] : [b, a];
  return a.monitor_id === b.monitor_id && Date.parse(second.started_at) - Date.parse(first.resolved_at!) <= windowMs;
}

/**
 * The list, in its order, with each run of neighbours gathered into an
 * array: a run is made of incidents next to each other in the list.
 */
export function gatherRuns<T extends Spell>(list: T[], windowMs: number): (T | T[])[] {
  const out: (T | T[])[] = [];
  let run: T[] = [];
  const flush = () => {
    if (run.length >= MIN_RUN) out.push(run);
    else out.push(...run);
    run = [];
  };
  for (const i of list) {
    const last = run.at(-1);
    if (last && isBlip(i, windowMs) && near(last, i, windowMs)) {
      run.push(i);
      continue;
    }
    flush();
    if (isBlip(i, windowMs)) run = [i];
    else out.push(i);
  }
  flush();
  return out;
}

/** A run's extent: how many, from the first start to the last end, and the shortest and longest outage, in milliseconds. */
export function runExtent(run: Spell[]): { count: number; from: string; to: string; shortest: number; longest: number } {
  const lengths = run.map((i) => Date.parse(i.resolved_at!) - Date.parse(i.started_at));
  const starts = run.map((i) => i.started_at).sort();
  const ends = run.map((i) => i.resolved_at!).sort();
  return {
    count: run.length,
    from: starts[0],
    to: ends[ends.length - 1],
    shortest: Math.min(...lengths),
    longest: Math.max(...lengths),
  };
}
