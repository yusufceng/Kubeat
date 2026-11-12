// A list row's recent checks, written small. The list goes to the browser
// with every refresh and a fleet has hundreds of rows, so a check is a
// symbol and a few characters here, not an object.

/** What a check came to: it failed, it passed, or it passed slower than the monitor's threshold. */
export type BeatState = "down" | "ok" | "slow";

const SYMBOLS: Record<BeatState, string> = { down: "0", ok: "1", slow: "2" };

/**
 * What one check came to. A success that took longer than slowAboveMs is
 * slow: that is the backend's rule for a slow check, as near as the
 * milliseconds at hand tell; 0 means the monitor has no threshold. A
 * failure is never slow, however long it took.
 */
export function beatState(up: boolean, ms: number, slowAboveMs: number): BeatState {
  if (!up) return "down";
  return slowAboveMs > 0 && ms > slowAboveMs ? "slow" : "ok";
}

/**
 * The checks as a string, oldest first: "1" for a success, "0" for a
 * failure, "2" for a slow success (see beatState). history is newest
 * first, the way the backend lists it.
 */
export function encodeBeats(history: { up: boolean; ms: number }[], slowAboveMs: number): string {
  return history
    .toReversed()
    .map((h) => SYMBOLS[beatState(h.up, h.ms, slowAboveMs)])
    .join("");
}

/** The beats back as what each check came to, oldest first. */
export function decodeBeats(beats: string): BeatState[] {
  return [...beats].map((symbol) => (symbol === SYMBOLS.down ? "down" : symbol === SYMBOLS.slow ? "slow" : "ok"));
}

/**
 * When the checks ran, oldest first, for a row's hover: the first as Unix
 * seconds and each later one as the seconds since the one before it, in
 * base 36 and joined by dots ("teqz4w.u.u.1o"). A hover shows a time to the
 * second, so the milliseconds are dropped. history is newest first, its t
 * in Unix milliseconds.
 */
export function encodeBeatTimes(history: { t: number }[]): string {
  let previous = 0;
  return history
    .toReversed()
    .map((h) => {
      const seconds = Math.floor(h.t / 1000);
      const step = seconds - previous;
      previous = seconds;
      return step.toString(36);
    })
    .join(".");
}

/** The times back in Unix milliseconds, oldest first. */
export function decodeBeatTimes(times: string): number[] {
  if (times === "") return [];
  let seconds = 0;
  return times.split(".").map((step) => {
    seconds += parseInt(step, 36);
    return seconds * 1000;
  });
}

/** How long each check took, oldest first: whole milliseconds in base 36, joined by dots. */
export function encodeBeatLatencies(history: { ms: number }[]): string {
  return history
    .toReversed()
    .map((h) => Math.max(0, Math.round(h.ms)).toString(36))
    .join(".");
}

/** The latencies back in milliseconds, oldest first. */
export function decodeBeatLatencies(latencies: string): number[] {
  return latencies === "" ? [] : latencies.split(".").map((ms) => parseInt(ms, 36));
}
