import Link from "next/link";
import { getT } from "@/lib/i18n/server";

/**
 * An incident that is not there, or not for this viewer (the backend
 * answers both alike), told inside the app's frame.
 */
export default async function NotFound() {
  const t = await getT();
  return (
    <main className="page">
      <div className="flex flex-col items-start gap-2 py-10">
        <p className="type-display-l text-ink-3">404</p>
        <h1 className="type-title">{t("incident.notFound.title")}</h1>
        <p className="max-w-[60ch] text-sm text-ink-2">{t("incident.notFound.body")}</p>
        <Link href="/incidents" className="btn mt-3">
          {t("incident.notFound.back")}
        </Link>
      </div>
    </main>
  );
}
