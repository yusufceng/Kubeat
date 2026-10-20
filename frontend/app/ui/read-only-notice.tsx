import Link from "next/link";
import { getT } from "@/lib/i18n/server";
import { LockIcon } from "@/app/ui/page-icons";

/** What a viewer sees in place of a form they may not submit. */
export async function ReadOnlyNotice() {
  const t = await getT();
  return (
    <p className="pg-callout">
      <span className="pg-callout-icon" aria-hidden>
        <LockIcon />
      </span>
      <span className="pg-callout-body">
        {t("readOnly.notice")} <Link href="/">{t("readOnly.back")}</Link>.
      </span>
    </p>
  );
}
