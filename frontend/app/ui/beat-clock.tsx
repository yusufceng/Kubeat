"use client";

import { useEffect } from "react";

/** The animations that beat: the keyframes of these names in globals.css. */
const BEATS = new Set(["glyph-beat", "glyph-pulse"]);

/**
 * Keeps every heartbeat on the page in phase. A CSS animation starts when
 * its element is first drawn, and the parts of a page are not drawn at
 * once: the list streams in apart from the overview, a monitor goes down
 * between refreshes, a namespace is scrolled into view. Each outage would
 * then beat to its own clock. Here each beat is started at the timeline's
 * zero instead, whenever it begins, so a screen has one pulse.
 *
 * Draws nothing. For viewers who ask for less motion there are no beats to
 * keep (globals.css), and so nothing to do.
 */
export function BeatClock() {
  useEffect(() => {
    const align = (animations: Animation[]) => {
      for (const animation of animations) {
        if (animation instanceof CSSAnimation && BEATS.has(animation.animationName)) animation.startTime = 0;
      }
    };
    align(document.getAnimations());
    const onStart = (e: AnimationEvent) => {
      if (BEATS.has(e.animationName) && e.target instanceof Element) align(e.target.getAnimations({ subtree: true }));
    };
    document.addEventListener("animationstart", onStart);
    return () => document.removeEventListener("animationstart", onStart);
  }, []);
  return null;
}
