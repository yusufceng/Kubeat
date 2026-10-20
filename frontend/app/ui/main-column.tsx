import { BackendWarning } from "@/app/ui/backend-warning";
import { ContextBar } from "@/app/ui/context-bar";

/**
 * The column a page lives in: the context bar over it, and below that the
 * page, which scrolls on its own on a wide screen. On a narrow screen the
 * whole window scrolls, under the bar that stays at the top.
 */
export function MainColumn({ children }: { children: React.ReactNode }) {
  // relative: an absolutely placed element of a page (a screen-reader-only
  // label, a popover) is placed within the column that scrolls. Without it,
  // the nearest positioned ancestor is outside, and focusing such an element
  // scrolls the whole window under the frame.
  return (
    <div className="order-1 flex min-w-0 flex-1 flex-col lg:order-2 lg:h-dvh">
      <ContextBar />
      <BackendWarning />
      <div id="content" className="relative flex-1 lg:min-h-0 lg:overflow-y-auto">
        {children}
      </div>
    </div>
  );
}
