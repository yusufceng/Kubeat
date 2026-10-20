"use client";

import { useEffect, useId, useRef, useState } from "react";

/**
 * A menu that drops from its button: a popover, so that Escape and a click
 * elsewhere close it, placed under the button (above it when the window
 * has no room below) and closed when the page scrolls under it. The arrow
 * keys move between its items, which carry role="menuitem"; choosing one
 * closes the menu.
 */
export function Menu({
  label,
  button,
  buttonLabel,
  buttonClassName = "btn btn-ghost",
  align = "end",
  className = "",
  describedBy,
  children,
}: {
  /** The menu's name, for screen readers. */
  label: string;
  /** The id of what explains the menu, read after its name. */
  describedBy?: string;
  /** What the button shows. */
  button: React.ReactNode;
  /** The button's name, when what it shows is an icon alone. */
  buttonLabel?: string;
  buttonClassName?: string;
  /** Which edge of the button the menu lines up with. */
  align?: "start" | "end";
  className?: string;
  children: React.ReactNode;
}) {
  const id = useId();
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  // Opened with a pointer, the first item takes the focus without a ring;
  // opened from the keyboard, with one.
  const byPointer = useRef(false);

  useEffect(() => {
    const el = menu.current;
    if (!el) return;
    const GAP = 4;
    const EDGE = 8;
    const place = () => {
      const t = trigger.current?.getBoundingClientRect();
      if (!t) return;
      el.style.top = `${t.bottom + GAP}px`;
      if (align === "end") {
        el.style.left = "auto";
        el.style.right = `${Math.max(EDGE, document.documentElement.clientWidth - t.right)}px`;
      } else {
        el.style.right = "auto";
        el.style.left = `${Math.max(EDGE, t.left)}px`;
      }
    };
    const flip = () => {
      const t = trigger.current?.getBoundingClientRect();
      if (!t) return;
      const height = el.offsetHeight;
      if (t.bottom + GAP + height > window.innerHeight - EDGE && t.top - GAP - height > EDGE) {
        el.style.top = `${t.top - GAP - height}px`;
      }
    };
    const close = () => el.hidePopover();
    const before = (e: Event) => {
      if ((e as ToggleEvent).newState === "open") place();
    };
    const toggled = (e: Event) => {
      const isOpen = (e as ToggleEvent).newState === "open";
      setOpen(isOpen);
      if (isOpen) {
        flip();
        el.querySelector<HTMLElement>('[role="menuitem"]:not(:disabled)')?.focus({
          focusVisible: !byPointer.current,
        } as FocusOptions);
        // The menu stays where it was put; the page moving under it closes it.
        window.addEventListener("scroll", close, { capture: true, once: true });
        window.addEventListener("resize", close, { once: true });
      } else {
        window.removeEventListener("scroll", close, { capture: true });
        window.removeEventListener("resize", close);
      }
    };
    el.addEventListener("beforetoggle", before);
    el.addEventListener("toggle", toggled);
    return () => {
      el.removeEventListener("beforetoggle", before);
      el.removeEventListener("toggle", toggled);
      window.removeEventListener("scroll", close, { capture: true });
      window.removeEventListener("resize", close);
    };
  }, [align]);

  function onKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    const items = [...e.currentTarget.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)')];
    if (items.length === 0) return;
    const at = items.indexOf(document.activeElement as HTMLElement);
    const go = (i: number) => {
      e.preventDefault();
      items[(i + items.length) % items.length].focus();
    };
    if (e.key === "ArrowDown") go(at + 1);
    else if (e.key === "ArrowUp") go(at < 0 ? -1 : at - 1);
    else if (e.key === "Home") go(0);
    else if (e.key === "End") go(-1);
    else if (e.key === "Tab") menu.current?.hidePopover();
  }

  return (
    <>
      <button
        ref={trigger}
        type="button"
        popoverTarget={id}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={id}
        aria-label={buttonLabel}
        title={buttonLabel}
        className={buttonClassName}
        onPointerDown={() => (byPointer.current = true)}
        onKeyDown={() => (byPointer.current = false)}
      >
        {button}
      </button>
      <div
        id={id}
        ref={menu}
        popover="auto"
        role="menu"
        aria-label={label}
        aria-describedby={describedBy}
        onKeyDown={onKeyDown}
        onClick={(e) => {
          // An item was chosen: the menu has done its part. A submit button
          // still submits its form; hiding the menu does not stop it.
          const item = (e.target as HTMLElement).closest<HTMLElement>('[role="menuitem"]');
          if (item && !item.matches(":disabled")) menu.current?.hidePopover();
        }}
        className={`menu mon-menu inset-auto ${className}`}
      >
        {children}
      </div>
    </>
  );
}
