import Link from "next/link";
import { Suspense } from "react";
import { getT } from "@/lib/i18n/server";
import { countStates, workspaceMonitors } from "@/lib/workspace";
import { CommandPalette } from "@/app/ui/command-palette";
import { PlusIcon } from "@/app/ui/icons";
import { Logo } from "@/app/ui/logo";
import { Breadcrumb } from "@/app/ui/nav";
import { PulsePill } from "@/app/ui/pulse-pill";
import { shellUser } from "@/app/ui/session-controls";
import { ListButton } from "@/app/ui/sidebar-frame";

/**
 * The bar over a page: where the page is on the left, after the button of
 * the monitor list where that is a drawer; on the right the command
 * palette's button (⌘K), the fleet's pulse and, for those who may, a new
 * monitor. On a narrow screen it stays at the top and shows the name in
 * place of the breadcrumb.
 */
export async function ContextBar() {
  const t = await getT();
  return (
    <header className="fixed inset-x-0 top-0 z-30 flex h-[52px] items-center gap-3 border-b border-line bg-panel px-4 lg:static lg:z-auto lg:flex-none lg:bg-canvas lg:px-6">
      <Link href="/" aria-label={t("shell.home")} className="flex-none rounded-md lg:hidden">
        <Logo size={18} className="block" />
      </Link>
      <Suspense>
        <ListButton />
      </Suspense>
      <div className="hidden min-w-0 lg:block">
        <Breadcrumb />
      </div>
      <div className="ml-auto flex flex-none items-center gap-2.5">
        {/* Until the session is known the palette offers the pages alone. */}
        <Suspense fallback={<CommandPalette canEdit={false} admin={false} />}>
          <Palette />
        </Suspense>
        <Suspense>
          <FleetPulse />
        </Suspense>
        <Suspense>
          <NewMonitorButton />
        </Suspense>
      </div>
    </header>
  );
}

/** The command palette with what the signed-in user may do. */
async function Palette() {
  const me = await shellUser();
  return <CommandPalette canEdit={me?.can_edit ?? false} admin={me?.admin ?? false} />;
}

/** The pulse of the monitors the viewer can see; nothing when there are none or they cannot be read. */
async function FleetPulse() {
  let monitors;
  try {
    monitors = await workspaceMonitors();
  } catch {
    return null; // the page says what is wrong
  }
  if (monitors.length === 0) return null;
  const c = countStates(monitors);
  return <PulsePill up={c.up} down={c.down} degraded={c.verifying + c.recovering} />;
}

async function NewMonitorButton() {
  const me = await shellUser();
  if (!me?.can_edit) return null;
  const t = await getT();
  // Below the widest screens the button keeps to its icon, leaving the room to the breadcrumb.
  return (
    <Link
      href="/monitors/new"
      className="btn btn-primary max-xl:px-2"
      aria-label={t("monitorsList.list.newMonitor")}
      title={t("monitorsList.list.newMonitor")}
    >
      <PlusIcon />
      <span className="max-xl:hidden">{t("monitorsList.list.newMonitor")}</span>
    </Link>
  );
}
