import Link from "next/link";
import { Suspense } from "react";
import { getLicense, type License } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { getT } from "@/lib/i18n/server";
import { WarningIcon } from "@/app/ui/page-icons";

/**
 * Tells administrators when the license is ending, has ended or cannot be
 * used. Nobody else can do anything about it, so nobody else sees it.
 */
export function LicenseWarning() {
  return (
    <Suspense>
      <LicenseCheck />
    </Suspense>
  );
}

async function LicenseCheck() {
  const me = await currentUser();
  if (!me.admin) return null;
  let license: License;
  try {
    license = await getLicense();
  } catch {
    return null; // an older backend, or one that cannot be reached: the page says so itself
  }
  if (license.state !== "expiring" && license.state !== "expired" && license.state !== "invalid") return null;
  const t = await getT();

  return (
    <p role="alert" className="pg-warning-bar">
      <WarningIcon width={14} height={14} aria-hidden />
      <span>
        {t(`license.banner.${license.state}`, { date: license.expires_at?.slice(0, 10) ?? "" })}{" "}
        <Link href="/users#license" className="underline">
          {t("license.banner.link")}
        </Link>
      </span>
    </p>
  );
}
