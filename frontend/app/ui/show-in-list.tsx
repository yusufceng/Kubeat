"use client";

import { type ListFilter, useListFilter } from "@/app/ui/shell-state";

/**
 * A button that filters the monitor list (to those down, say) and brings
 * it into view: beside the page on a wide screen it is already there; on
 * a narrow one it is under the overview.
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
  return (
    <button
      type="button"
      className={className}
      onClick={() => {
        setFilter(filter);
        if (matchMedia("(width < 64rem)").matches) document.getElementById("monitors")?.scrollIntoView({ block: "start" });
      }}
    >
      {children}
    </button>
  );
}
