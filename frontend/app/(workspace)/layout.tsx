import { Suspense } from "react";
import { LiveRefresh } from "@/app/ui/live-refresh";
import { Sidebar, SidebarSkeleton } from "@/app/ui/sidebar";
import { SidebarFrame } from "@/app/ui/sidebar-frame";

/**
 * The workspace: the monitor list on the left and, beside it, the overview
 * or the monitor that is open. The two scroll independently, so the list
 * stays in place while a long monitor page is read.
 */
export default function WorkspaceLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto lg:flex-row lg:overflow-hidden">
      <LiveRefresh />
      {/* The frame reads the path, which is request data. */}
      <Suspense>
        <SidebarFrame>
          <Suspense fallback={<SidebarSkeleton />}>
            <Sidebar />
          </Suspense>
        </SidebarFrame>
      </Suspense>
      <main className="order-1 min-w-0 flex-1 lg:order-2 lg:overflow-y-auto">
        <div className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">{children}</div>
      </main>
    </div>
  );
}
