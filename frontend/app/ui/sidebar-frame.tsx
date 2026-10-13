"use client";

import { usePathname } from "next/navigation";
import { useT } from "@/lib/i18n/client";

/**
 * The column that holds the monitor list. On a wide screen it is always
 * there; on a narrow one there is room for one column, so it shows only on
 * the overview, under the overview itself.
 */
export function SidebarFrame({ children }: { children: React.ReactNode }) {
  const t = useT();
  const onOverview = usePathname() === "/";
  return (
    <aside
      aria-label={t("monitorsList.sidebar.label")}
      className={`order-2 shrink-0 border-line bg-panel lg:order-1 lg:flex lg:h-full lg:w-[23rem] lg:flex-col lg:border-r ${
        onOverview ? "flex flex-col border-t" : "hidden"
      }`}
    >
      {children}
    </aside>
  );
}
