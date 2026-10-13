import { Suspense } from "react";
import { getHealth, type Health, REQUIRED_API_REVISION } from "@/lib/api";
import { getT } from "@/lib/i18n/server";

/**
 * Says so when the backend is older than this frontend expects, which
 * happens when only one of the two was restarted or rolled out.
 */
export function BackendWarning() {
  return (
    <Suspense>
      <RevisionCheck />
    </Suspense>
  );
}

async function RevisionCheck() {
  let health: Health;
  try {
    health = await getHealth();
  } catch {
    return null; // an unreachable backend is reported by the page itself
  }
  if ((health.api_revision ?? 0) >= REQUIRED_API_REVISION) return null;
  const t = await getT();

  return (
    <p
      role="alert"
      className="border-b border-warn/40 bg-warn/12 px-4 py-2 text-center text-sm text-warn"
    >
      {t("backend.outdated")}
    </p>
  );
}
