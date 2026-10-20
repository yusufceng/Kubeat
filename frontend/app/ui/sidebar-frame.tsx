"use client";

import { usePathname } from "next/navigation";
import { useEffect, useRef } from "react";
import { useT } from "@/lib/i18n/client";
import { ListPaneIcon } from "@/app/ui/icons";
import { inWorkspace } from "@/app/ui/nav";
import { useListDrawer } from "@/app/ui/shell-state";

/** The id of the context bar's button that opens the list as a drawer. */
export const LIST_BUTTON_ID = "list-drawer-button";

/**
 * The column that holds the monitor list, 340px wide. From 1200px it is
 * always there. Between 1024 and 1199px the page gets the width instead,
 * and the list is a drawer over it, opened from the context bar; it closes
 * on Escape, on a click beside it, and once a monitor is chosen. On a
 * narrow screen there is room for one column, so it shows only on the
 * overview, under the overview itself.
 */
export function SidebarFrame({ children }: { children: React.ReactNode }) {
  const t = useT();
  const pathname = usePathname();
  const onOverview = pathname === "/";
  const [open, setOpen] = useListDrawer();
  const frame = useRef<HTMLElement>(null);

  // Another page, a monitor chosen in the list say, closes the drawer; the
  // first one does not, where the pulse asked for the list on the way here.
  const shownPath = useRef(pathname);
  useEffect(() => {
    if (shownPath.current === pathname) return;
    shownPath.current = pathname;
    setOpen(false);
  }, [pathname, setOpen]);

  // Opened, the drawer takes the focus to its filter.
  useEffect(() => {
    if (open) frame.current?.querySelector<HTMLInputElement>("input[type=search]")?.focus();
  }, [open]);

  const close = () => {
    setOpen(false);
    document.getElementById(LIST_BUTTON_ID)?.focus();
  };

  return (
    <>
      <aside
        ref={frame}
        id="monitors"
        aria-label={t("monitorsList.sidebar.label")}
        data-open={open || undefined}
        onKeyDown={(e) => {
          // Escape while monitors are being selected stops selecting first.
          if (e.key === "Escape" && open && !(e.target as HTMLElement).closest("[data-selecting]")) close();
        }}
        className={`order-2 shrink-0 scroll-mt-[52px] border-line bg-panel ${onOverview ? "flex flex-col border-t" : "hidden"} lg:fixed lg:inset-y-0 lg:left-[60px] lg:z-[35] lg:w-[340px] lg:flex-col lg:border-t-0 lg:border-r lg:shadow-[var(--shadow-pop)] ${
          open ? "lg:flex" : "lg:hidden"
        } min-[75rem]:static min-[75rem]:z-auto min-[75rem]:order-1 min-[75rem]:flex min-[75rem]:h-dvh min-[75rem]:shadow-none`}
      >
        {children}
      </aside>
      {open && <div aria-hidden className="fixed inset-0 z-[34] hidden lg:block min-[75rem]:hidden" onClick={close} />}
    </>
  );
}

/** The context bar's button for the list, where the list is a drawer: between 1024 and 1199px, in the workspace. */
export function ListButton() {
  const t = useT();
  const pathname = usePathname();
  const [open, setOpen] = useListDrawer();
  if (!inWorkspace(pathname)) return null;
  return (
    <button
      id={LIST_BUTTON_ID}
      type="button"
      onClick={() => setOpen(!open)}
      aria-expanded={open}
      aria-controls="monitors"
      className="btn btn-ghost hidden flex-none lg:inline-flex min-[75rem]:hidden"
    >
      <ListPaneIcon />
      {t("monitorsList.sidebar.label")}
    </button>
  );
}
