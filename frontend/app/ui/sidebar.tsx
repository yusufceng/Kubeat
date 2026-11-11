import { connection } from "next/server";
import { config } from "@/lib/config";
import { toFleetItem } from "@/lib/fleet";
import { getT } from "@/lib/i18n/server";
import { workspaceMonitors } from "@/lib/workspace";
import { BackendError } from "@/app/ui/backend-error";
import { MonitorList } from "@/app/ui/monitor-list";
import { shellUser } from "@/app/ui/session-controls";

/**
 * The monitor list of the workspace, read from the backend on each request.
 * When the backend cannot be reached the list says so in its place, and
 * the rest of the frame stays.
 */
export async function Sidebar() {
  // The list settings come from the running server's environment.
  await connection();
  // Without a session this goes to the sign-in page; unreachable, it is null.
  const me = await shellUser();
  const t = await getT();
  let items;
  try {
    items = (await workspaceMonitors()).map(toFleetItem);
  } catch (err) {
    return (
      <div className="p-3">
        <BackendError what={t("monitorsList.sidebar.what")} err={err} />
      </div>
    );
  }
  return (
    <MonitorList
      items={items}
      beats={config.listBeats}
      openByDefault={items.length <= config.dashboardFlatLimit}
      canEdit={me?.can_edit ?? false}
      tones={config.uptimeTones}
    />
  );
}

/**
 * What the list's place shows until the first data arrives: the toolbar
 * and a few rows, without words, at the list's own heights (fl-list sets
 * them in fleet.css).
 */
export function SidebarSkeleton() {
  return (
    <div className="fl-list" aria-hidden>
      <div className="flex flex-col gap-1.5 border-b border-line px-2.5 pt-2.5 pb-2">
        <div className="h-7 rounded-sm bg-well" />
        <div className="h-6.5 rounded-sm bg-well" />
      </div>
      <div className="flex flex-col pt-1.5 pb-3">
        {Array.from({ length: 11 }, (_, i) => (
          <div key={i} className="flex h-(--fl-row-h) items-center gap-1.5 px-3">
            <span className="size-2.5 flex-none rounded-full bg-line" />
            <span className="h-2.5 flex-1 rounded-xs bg-hover" style={{ maxWidth: `${40 + ((i * 37) % 45)}%` }} />
            <span className="ml-auto h-3 w-18 rounded-xs bg-hover" />
          </div>
        ))}
      </div>
    </div>
  );
}
