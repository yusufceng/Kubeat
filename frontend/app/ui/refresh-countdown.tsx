"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState, useTransition } from "react";
import { useT } from "@/lib/i18n/client";

/**
 * "Updated 19:42 · refreshes in 24 s", with a ring that empties as the next
 * refresh nears, and the refresh itself: when the time is up and the tab is
 * in view, the page's data is read again. The clock runs in the browser
 * alone, from when the data on screen arrived, so a server whose clock is
 * off cannot skew it.
 */
export function RefreshCountdown({ intervalMs }: { intervalMs: number }) {
  const t = useT();
  const router = useRouter();
  const [now, setNow] = useState<number | null>(null);
  const [updatedAt, setUpdatedAt] = useState<number | null>(null);
  const [refreshing, startRefresh] = useTransition();

  useEffect(() => {
    const tick = () => setNow(Date.now());
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, []);

  // The data is as of the first render here, and then of each refresh's end.
  useEffect(() => {
    const arrived = () => setUpdatedAt(Date.now());
    if (!refreshing) arrived();
  }, [refreshing]);

  const due = updatedAt === null ? null : updatedAt + intervalMs;
  useEffect(() => {
    if (now === null || due === null || now < due || refreshing) return;
    if (document.visibilityState !== "visible") return; // once the tab is back in view
    startRefresh(() => router.refresh());
  }, [now, due, refreshing, router]);

  if (now === null || updatedAt === null || due === null) return null;
  const left = Math.max(0, due - now);
  const share = Math.round((100 * left) / intervalMs);
  const updated = new Date(updatedAt).toLocaleTimeString(t.intl, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  return (
    <p className="flex items-center gap-2 text-xs whitespace-nowrap text-ink-3">
      <span
        aria-hidden
        className="h-[18px] w-[18px] flex-none rounded-full [mask:radial-gradient(circle_6px,transparent_98%,#000)]"
        style={{ background: `conic-gradient(var(--accent) 0 ${share}%, var(--line) ${share}% 100%)` }}
      />
      {t("statusPage.updated", { time: updated })} · {t("statusPage.refreshesIn", { count: Math.ceil(left / 1000) })}
    </p>
  );
}
