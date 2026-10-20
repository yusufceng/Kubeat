import Link from "next/link";
import { getT } from "@/lib/i18n/server";
import { LanguageToggle } from "@/app/ui/language-toggle";
import { Logo } from "@/app/ui/logo";

/**
 * Shown for an address that leads nowhere, to visitors with or without a
 * session, outside the app's frame. A missing monitor or namespace is
 * answered inside it, by app/(app)/(workspace)/not-found.tsx.
 */
export default async function NotFound() {
  const t = await getT();
  return (
    <div className="auth">
      <main className="auth-col">
        <Logo size={30} className="auth-logo" />
        <p className="pg-mono text-ink-3">404</p>
        <h1 className="auth-title">{t("notFound.title")}</h1>
        <p className="auth-intro mt-0">{t("notFound.body")}</p>
        <div>
          <Link href="/" className="btn">
            {t("notFound.home")}
          </Link>
        </div>
      </main>
      <footer className="auth-foot">
        <LanguageToggle variant="inline" />
      </footer>
    </div>
  );
}
