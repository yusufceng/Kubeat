import Link from "next/link";
import { getT } from "@/lib/i18n/server";

/** What a viewer sees in place of a form they may not submit. */
export async function ReadOnlyNotice() {
  const t = await getT();
  return (
    <p className="rounded-lg border border-line bg-sunken p-4 text-sm">
      {t("readOnly.notice")}{" "}
      <Link href="/" className="underline">
        {t("readOnly.back")}
      </Link>
      .
    </p>
  );
}
