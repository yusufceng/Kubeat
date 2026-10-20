import Link from "next/link";
import { getT } from "@/lib/i18n/server";

/** A monitor or a namespace that is not there (any more), answered beside the monitor list. */
export default async function NotFound() {
  const t = await getT();
  return (
    <div className="flex flex-col items-start gap-2 py-10">
      <p className="type-display-l text-ink-3">404</p>
      <h1 className="type-title">{t("notFound.title")}</h1>
      <p className="text-sm text-ink-2">{t("notFound.body")}</p>
      <Link href="/" className="btn mt-3">
        {t("notFound.home")}
      </Link>
    </div>
  );
}
