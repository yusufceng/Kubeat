import { SkipLink } from "@/app/ui/nav";
import { SessionNav } from "@/app/ui/session-controls";

/**
 * The frame of the app for signed-in users: the rail on the left of a wide
 * screen (the tab bar at the bottom of a narrow one) and, beside it, what
 * the sections put there: the workspace (the monitor list and a page) or a
 * page alone, each under the context bar (app/ui/main-column.tsx). The
 * sign-in pages and the public status pages are outside it.
 */
export default function AppLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-dvh pt-[52px] pb-[calc(62px+env(safe-area-inset-bottom))] lg:h-dvh lg:min-h-0 lg:overflow-hidden lg:p-0">
      <SkipLink />
      <SessionNav />
      <div className="flex min-w-0 flex-1 flex-col lg:flex-row">{children}</div>
    </div>
  );
}
