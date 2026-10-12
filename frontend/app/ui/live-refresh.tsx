import { connection } from "next/server";
import { Suspense } from "react";
import { config } from "@/lib/config";
import { AutoRefresh } from "@/app/ui/auto-refresh";

/**
 * Keeps the page's data fresh. The interval is read per request, so it comes
 * from the running server's environment instead of being frozen into the
 * prerendered shell at build time.
 */
export function LiveRefresh({ every = "page" }: { every?: "page" | "statusPage" }) {
  return (
    <Suspense>
      <RequestTimeRefresh every={every} />
    </Suspense>
  );
}

async function RequestTimeRefresh({ every }: { every: "page" | "statusPage" }) {
  await connection();
  const intervalMs = every === "statusPage" ? config.statusPageRefreshMs : config.refreshIntervalMs;
  return <AutoRefresh intervalMs={intervalMs} />;
}
