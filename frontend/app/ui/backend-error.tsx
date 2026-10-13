import Link from "next/link";
import { ApiError } from "@/lib/api";
import { getT } from "@/lib/i18n/server";

/**
 * Shown where a page could not get its data from the backend. A rejected
 * API token is called out, since restarting things will not fix it. `what`
 * is the text already translated by the page ("Monitor durumu" / "Monitor status").
 */
export async function BackendError({ what, err }: { what: string; err?: unknown }) {
  const t = await getT();
  if (err instanceof ApiError && err.status === 401) {
    return (
      <p role="alert" className="rounded-lg border border-line bg-sunken p-4 text-sm">
        {t("backend.sessionExpired")}{" "}
        <Link href="/login?expired=1" className="underline">
          {t("backend.signInAgain")}
        </Link>
        .
      </p>
    );
  }
  return (
    <p
      role="alert"
      className="rounded-lg border border-down/40 bg-down/8 p-4 text-sm text-down"
    >
      {t("backend.unreachable", { what })}
    </p>
  );
}
