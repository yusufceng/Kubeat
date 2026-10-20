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

/** What the list's place shows until the first data arrives: the toolbar and a few rows, without words. */
export function SidebarSkeleton() {
  return (
    <div className="flex flex-col" aria-hidden>
      <div className="flex flex-col gap-2.5 border-b border-line px-3 pt-3 pb-2.5">
        <div className="h-[34px] rounded-sm bg-well" />
        <div className="h-[32px] rounded-sm bg-well" />
      </div>
      <div className="flex flex-col py-1.5">
        {Array.from({ length: 9 }, (_, i) => (
          <div key={i} className="flex h-[34px] items-center gap-2 pr-3 pl-3.5">
            <span className="h-2.5 w-2.5 flex-none rounded-full bg-line" />
            <span className="h-2.5 flex-1 rounded-xs bg-hover" style={{ maxWidth: `${40 + ((i * 37) % 45)}%` }} />
            <span className="ml-auto h-3.5 w-[71px] rounded-xs bg-hover" />
          </div>
        ))}
      </div>
    </div>
  );
}
