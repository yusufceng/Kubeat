import { getT } from "@/lib/i18n/server";

/** Shown for an address that leads nowhere, to visitors with or without a session. */
export default async function NotFound() {
  const t = await getT();
  return (
    <main className="mx-auto flex min-h-full w-full max-w-md flex-col items-center justify-center gap-2 px-4 py-16 text-center">
      <p className="text-5xl font-semibold tabular-nums text-muted">404</p>
      <h1 className="text-lg font-semibold">{t("notFound.title")}</h1>
      <p className="text-sm text-muted">{t("notFound.body")}</p>
    </main>
  );
}
