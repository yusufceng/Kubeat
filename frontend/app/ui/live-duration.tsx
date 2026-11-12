"use client";

import { useSyncExternalStore } from "react";
import { span, spanShort } from "@/lib/format";
import { useT } from "@/lib/i18n/client";

// One clock for every duration on the page. A list with thirty outages
// holds one timer and not thirty, and its durations turn over at the same
// instant, not each a moment after the other.
const listeners = new Set<() => void>();
let timer: ReturnType<typeof setInterval> | undefined;
let now = 0;

function subscribe(listener: () => void) {
  listeners.add(listener);
  if (timer === undefined) {
    now = Date.now();
    timer = setInterval(() => {
      now = Date.now();
      for (const tick of listeners) tick();
    }, 1000);
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) {
      clearInterval(timer);
      timer = undefined;
    }
  };
}

/** The clock's time; with nothing listening yet, the time it is. */
function time() {
  if (timer === undefined) now = Date.now();
  return now;
}

/**
 * Time elapsed since a moment, counting up while the page is open. short
 * keeps its largest unit alone ("3 dk"), for where a row has no room for
 * two. What is read from the clock is the text, so a duration is drawn
 * again only when its words change: once a minute for "3 dk", not once a
 * second.
 */
export function LiveDuration({ since, short = false }: { since: string; short?: boolean }) {
  const t = useT();
  const text = useSyncExternalStore(
    subscribe,
    () => (short ? spanShort : span)(time() - new Date(since).getTime(), t),
    // The server does not know the viewer's clock.
    () => "…",
  );
  return <span>{text}</span>;
}
