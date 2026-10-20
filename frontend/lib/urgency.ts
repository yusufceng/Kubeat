import type { DisplayState } from "@/app/ui/status";

/** Down first, then degraded (verifying, recovering), then the rest. */
export function urgencyRank(state: DisplayState): number {
  return state === "down" ? 0 : state === "verifying" || state === "recovering" ? 1 : 2;
}

/** Orders monitors problems first, then by name, in the viewer's language. */
export function byUrgency<T extends { state: DisplayState; name: string }>(locale: string) {
  return (a: T, b: T) => urgencyRank(a.state) - urgencyRank(b.state) || a.name.localeCompare(b.name, locale);
}
