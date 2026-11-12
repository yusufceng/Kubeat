// A list row's recent checks as one character each. The list goes to the
// browser with every refresh and a fleet has hundreds of rows, so a check
// is a symbol here, not an object.

/** What a check came to: it failed, it passed, or it passed slower than the monitor's threshold. */
export type BeatState = "down" | "ok" | "slow";

const SYMBOLS: Record<BeatState, string> = { down: "0", ok: "1", slow: "2" };

/**
 * The checks as a string, oldest first: "1" for a success, "0" for a
 * failure, "2" for a success that took longer than slowAboveMs. That is
 * the backend's rule for a slow check, as near as whole milliseconds tell;
 * 0 means the monitor has no threshold. history is newest first, the way
 * the backend lists it.
 */
export function encodeBeats(history: { up: boolean; ms: number }[], slowAboveMs: number): string {
  return history
    .toReversed()
    .map((h) => SYMBOLS[!h.up ? "down" : slowAboveMs > 0 && h.ms > slowAboveMs ? "slow" : "ok"])
    .join("");
}

/** The beats back as what each check came to, oldest first. */
export function decodeBeats(beats: string): BeatState[] {
  return [...beats].map((symbol) => (symbol === SYMBOLS.down ? "down" : symbol === SYMBOLS.slow ? "slow" : "ok"));
}
