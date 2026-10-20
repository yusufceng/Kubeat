import { getT } from "@/lib/i18n/server";
import { LanguageToggle } from "@/app/ui/language-toggle";
import { LogoMark, Wordmark } from "@/app/ui/logo";
import { StatusPagesIcon } from "@/app/ui/icons";
import { SystemTheme } from "@/app/ui/theme-toggle";

/**
 * An address under /status/ with no published page behind it. Its
 * visitors are the public, so it offers no way into the app: only what
 * happened, and the other language.
 */
export default async function NotFound() {
  const t = await getT();
  const [before, after] = t("statusPage.footer").split("{kubeat}");
  return (
    <main className="sp-page">
      <SystemTheme />
      <section className="panel pg-empty mt-6">
        <span className="pg-empty-icon" aria-hidden>
          <StatusPagesIcon width={20} height={20} />
        </span>
        <h1 className="pg-empty-title">{t("statusPage.notFound.title")}</h1>
        <p className="pg-empty-body">{t("statusPage.notFound.body")}</p>
      </section>
      <footer className="sp-foot">
        <span className="inline-flex items-center gap-2">
          <LogoMark size={14} />
          <span>
            {before}
            <Wordmark className="text-ink-2" />
            {after}
          </span>
        </span>
        <span aria-hidden>·</span>
        <LanguageToggle variant="inline" />
      </footer>
    </main>
  );
}
