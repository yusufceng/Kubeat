import Link from "next/link";
import { ApiError } from "@/lib/api";
import { getT } from "@/lib/i18n/server";
import { LockIcon, UnplugIcon } from "@/app/ui/page-icons";

/**
 * Shown where a page could not get its data from the backend. A rejected
 * session is called out, since restarting things will not fix it. `what`
 * is the text already translated by the page ("Monitor durumu" / "Monitor status").
 */
export async function BackendError({ what, err }: { what: string; err?: unknown }) {
  const t = await getT();
  if (err instanceof ApiError && err.status === 401) {
    return (
      <p role="alert" className="pg-callout">
        <span className="pg-callout-icon" aria-hidden>
          <LockIcon />
        </span>
        <span className="pg-callout-body">
          {t("backend.sessionExpired")}{" "}
          <Link href="/login?expired=1">{t("backend.signInAgain")}</Link>.
        </span>
      </p>
    );
  }
  return (
    <p role="alert" className="pg-callout pg-callout-down">
      <span className="pg-callout-icon" aria-hidden>
        <UnplugIcon />
      </span>
      <span className="pg-callout-body">
        <span className="pg-callout-title">{t("error.unreachableTitle")}</span>
        {t("backend.cannotShow", { what })}
      </span>
    </p>
  );
}
