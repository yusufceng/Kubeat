"use client";

import { useEffect, useId, useRef } from "react";

/** The tip's width at most, fixed so that it can be placed before it shows. */
const TIP_WIDTH = 320;

/**
 * How to read or use something, kept out of the way: a small "?" button
 * that opens the words in a popover under it. Escape, a click elsewhere
 * or the page moving under it closes it.
 */
export function HelpTip({ label, children }: { label: string; children: React.ReactNode }) {
  const id = useId();
  const button = useRef<HTMLButtonElement>(null);
  const tip = useRef<HTMLDivElement>(null);

  // Under the button, starting at its left edge, inside the window.
  useEffect(() => {
    const el = tip.current;
    if (!el) return;
    const place = (e: Event) => {
      if ((e as ToggleEvent).newState !== "open" || !button.current) return;
      const r = button.current.getBoundingClientRect();
      const width = Math.min(TIP_WIDTH, window.innerWidth - 16);
      el.style.width = `${width}px`;
      el.style.top = `${r.bottom + 6}px`;
      el.style.left = `${Math.max(8, Math.min(r.left - 10, window.innerWidth - width - 8))}px`;
    };
    const close = (e: Event) => {
      if (e.type === "scroll" && el.contains(e.target as Node)) return;
      if (el.matches(":popover-open")) el.hidePopover();
    };
    el.addEventListener("beforetoggle", place);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      el.removeEventListener("beforetoggle", place);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, []);

  return (
    <>
      <button ref={button} type="button" popoverTarget={id} className="help-btn" aria-label={label} title={label}>
        ?
      </button>
      <div id={id} ref={tip} popover="auto" role="note" className="menu inset-auto help-tip">
        {children}
      </div>
    </>
  );
}
