import { getT, titled } from "@/lib/i18n/server";
import { NotFound } from "@/app/ui/empty-state";
import { LanguageToggle } from "@/app/ui/language-toggle";
import { Logo } from "@/app/ui/logo";

export const generateMetadata = titled("notFound.title");

/**
 * Shown for an address that leads nowhere, to visitors with or without a
 * session, outside the app's frame; in the same form as a missing monitor
 * or namespace inside it (app/(app)/(workspace)/not-found.tsx and those
 * beside the pages).
 */
export default async function RootNotFound() {
  const t = await getT();
  return (
    <div className="auth">
      <main className="auth-col">
        <Logo size={22} className="auth-logo" />
        <NotFound title={t("notFound.title")} body={t("notFound.body")} back={{ href: "/", label: t("notFound.home") }} />
      </main>
      <footer className="auth-foot">
        <LanguageToggle variant="inline" />
      </footer>
    </div>
  );
}
