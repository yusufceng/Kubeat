import { connection } from "next/server";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { toListItem, workspaceMonitors } from "@/lib/workspace";
import { BackendError } from "@/app/ui/backend-error";
import { MonitorList } from "@/app/ui/monitor-list";

/** The monitor list of the workspace, read from the backend on each request. */
export async function Sidebar() {
  // The list settings come from the running server's environment.
  await connection();
  const me = await currentUser();
  let items;
  try {
    items = (await workspaceMonitors()).map(toListItem);
  } catch (err) {
    return (
      <div className="p-3">
        <BackendError what="Monitor listesi" err={err} />
      </div>
    );
  }
  return (
    <MonitorList
      items={items}
      beats={config.listBeats}
      openByDefault={items.length <= config.dashboardFlatLimit}
      canEdit={me.can_edit}
    />
  );
}

/** What the list's place shows until the first data arrives. */
export function SidebarSkeleton() {
  return (
    <div className="flex flex-col gap-3 p-3" aria-hidden>
      <div className="h-9 rounded-lg bg-sunken" />
      <div className="h-9 rounded-lg bg-sunken" />
      {Array.from({ length: 6 }, (_, i) => (
        <div key={i} className="h-11 rounded-lg bg-sunken/60" />
      ))}
    </div>
  );
}
