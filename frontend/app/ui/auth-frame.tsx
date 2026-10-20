import { BackendWarning } from "@/app/ui/backend-warning";
import { LanguageToggle } from "@/app/ui/language-toggle";
import { Logo } from "@/app/ui/logo";

/**
 * The frame of the pages before a session: signing in and the first
 * account. A 360px column in the middle of the canvas, under the mark and
 * the name; the other language at the foot.
 */
export function AuthFrame({
  title,
  intro,
  children,
}: {
  title: string;
  intro?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="auth">
      <BackendWarning />
      <main className="auth-col">
        <Logo size={30} className="auth-logo" />
        <h1 className="auth-title">{title}</h1>
        {intro && <p className="auth-intro">{intro}</p>}
        {children}
      </main>
      <footer className="auth-foot">
        <LanguageToggle variant="inline" />
      </footer>
    </div>
  );
}
