import { Suspense } from "react";
import { getHealth, type Health, REQUIRED_API_REVISION } from "@/lib/api";
import { getT } from "@/lib/i18n/server";
import { WarningIcon } from "@/app/ui/page-icons";

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
    <p role="alert" className="pg-warning-bar">
      <WarningIcon width={14} height={14} aria-hidden />
      <span>{t("backend.outdated")}</span>
    </p>
  );
}
