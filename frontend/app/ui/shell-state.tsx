"use client";

import { useLayoutEffect, useSyncExternalStore } from "react";

/**
 * State that parts of the frame share in the browser: what the monitor
 * list is filtered to, and the breadcrumb the page asks for. Each is a small
 * store kept for as long as the tab is open, so it outlives navigation
 * between pages; the server always renders the starting value.
 */
function store<T>(initial: T) {
  let value = initial;
  const listeners = new Set<() => void>();
  return {
    get: () => value,
    set(next: T) {
      value = next;
      listeners.forEach((notify) => notify());
    },
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
}

/**
 * Which monitors the list shows, by state: up, down (an ongoing outage),
 * degraded (verifying or recovering), idle (paused, pending or in
 * maintenance). The list's own control sets it, and so does the fleet pulse
 * in the context bar.
 */
export type ListFilter = "all" | "up" | "down" | "degraded" | "idle";

const listFilter = store<ListFilter>("all");

export function useListFilter(): [ListFilter, (filter: ListFilter) => void] {
  return [useSyncExternalStore(listFilter.subscribe, listFilter.get, () => "all"), listFilter.set];
}

/** One step of a breadcrumb; the last one is the page itself and has no link. mono sets cluster names. */
export type Crumb = { label: string; href?: string; mono?: boolean };

const crumbs = store<Crumb[] | null>(null);

/** The page's breadcrumb, or null when the page has not given one. */
export function usePageCrumbs(): Crumb[] | null {
  return useSyncExternalStore(crumbs.subscribe, crumbs.get, () => null);
}

/**
 * Puts the page's breadcrumb into the context bar while the page is shown:
 * render it anywhere in the page. Until it runs, the bar names the section.
 */
export function Crumbs({ items }: { items: Crumb[] }) {
  // Compared by value, so that a refresh of the page does not reset it.
  const key = JSON.stringify(items);
  useLayoutEffect(() => {
    crumbs.set(JSON.parse(key) as Crumb[]);
    return () => crumbs.set(null);
  }, [key]);
  return null;
}
