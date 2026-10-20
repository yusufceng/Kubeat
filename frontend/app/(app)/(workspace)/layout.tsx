import { Suspense } from "react";
import { LiveRefresh } from "@/app/ui/live-refresh";
import { MainColumn } from "@/app/ui/main-column";
import { Sidebar, SidebarSkeleton } from "@/app/ui/sidebar";
import { SidebarFrame } from "@/app/ui/sidebar-frame";

/**
 * The workspace: the monitor list on the left and, beside it, the overview
 * or the monitor that is open. The two scroll independently, so the list
 * stays in place while a long monitor page is read.
 */
export default function WorkspaceLayout({ children }: { children: React.ReactNode }) {
  return (
    <>
      <LiveRefresh />
      {/* The frame reads the path, which is request data. */}
      <Suspense>
        <SidebarFrame>
          <Suspense fallback={<SidebarSkeleton />}>
            <Sidebar />
          </Suspense>
        </SidebarFrame>
      </Suspense>
      <MainColumn>
        <main className="page">{children}</main>
      </MainColumn>
    </>
  );
}
