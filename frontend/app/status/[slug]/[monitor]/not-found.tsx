import { getT } from "@/lib/i18n/server";
import { LanguageToggle } from "@/app/ui/language-toggle";
import { Wordmark } from "@/app/ui/logo";
import { SearchIcon } from "@/app/ui/icons";
import { SystemTheme } from "@/app/ui/theme-toggle";

/**
 * A service address under a status page that leads nowhere: the page does
 * not show that service, has no detail pages, or is not published. The
 * visitor is the public, so only what happened, the way up, and the other
 * language; the page's own language is not known here.
 */
export default async function NotFound() {
  const t = await getT();
  const [before, after] = t("statusPage.footer").split("{kubeat}");
  return (
    <main className="sp-page">
      <SystemTheme />
      <section className="panel pg-empty mt-6">
        <span className="pg-empty-icon" aria-hidden>
          <SearchIcon width={20} height={20} />
        </span>
        <h1 className="pg-empty-title">{t("statusPage.detail.notFound.title")}</h1>
        <p className="pg-empty-body">{t("statusPage.detail.notFound.body")}</p>
        <p className="pg-empty-action">
          {/* A plain link: the page's address is the segment above this one. */}
          <a href="./" className="btn">
            {t("statusPage.detail.notFound.back")}
          </a>
        </p>
      </section>
      <footer className="sp-foot">
        <span>
          {before}
          <Wordmark className="text-ink-2" />
          {after}
        </span>
        <span aria-hidden>·</span>
        <LanguageToggle variant="inline" />
      </footer>
    </main>
  );
}
