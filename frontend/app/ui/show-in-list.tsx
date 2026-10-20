"use client";

import { DRAWER_MEDIA, type ListFilter, useListDrawer, useListFilter } from "@/app/ui/shell-state";

/**
 * A button that filters the monitor list (to those down, say) and brings
 * it into view: beside the page on a wide screen it is already there;
 * between 1024 and 1199px it opens as a drawer; on a narrow screen it is
 * under the overview.
 */
export function ShowInList({
  filter,
  className,
  children,
}: {
  filter: ListFilter;
  className?: string;
  children: React.ReactNode;
}) {
  const [, setFilter] = useListFilter();
  const [, setDrawerOpen] = useListDrawer();
  return (
    <button
      type="button"
      className={className}
      onClick={() => {
        setFilter(filter);
        if (matchMedia(DRAWER_MEDIA).matches) setDrawerOpen(true);
        if (matchMedia("(width < 64rem)").matches) document.getElementById("monitors")?.scrollIntoView({ block: "start" });
      }}
    >
      {children}
    </button>
  );
}
