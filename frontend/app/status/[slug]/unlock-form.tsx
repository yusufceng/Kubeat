import type { T } from "@/lib/i18n/translate";
import { LockIcon } from "@/app/ui/page-icons";
import type { UnlockProblem } from "./unlock/route";

const problems: Record<UnlockProblem, string> = {
  wrong: "statusPage.locked.wrong",
  throttled: "statusPage.locked.throttled",
  unavailable: "statusPage.locked.unavailable",
};

/** The reason in the query, if it is one the unlock gives. */
export function unlockProblem(value: string | string[] | undefined): UnlockProblem | null {
  return typeof value === "string" && value in problems ? (value as UnlockProblem) : null;
}

/**
 * A protected page before its password: the page's title, so that the
 * visitor knows they are at the right place, and one field. The form
 * posts to the unlock route handler, with or without JavaScript, which
 * keeps the token in a cookie and sends the browser back to `next`.
 */
export function UnlockForm({ slug, title, next, problem, t }: { slug: string; title: string; next: string; problem: UnlockProblem | null; t: T }) {
  return (
    <>
      <header className="sp-head">
        <div className="sp-head-main">
          <h1 className="sp-title">{title}</h1>
        </div>
      </header>
      <section className="panel sp-unlock" aria-labelledby="sp-unlock-title">
        <span className="pg-empty-icon" aria-hidden>
          <LockIcon width={20} height={20} />
        </span>
        <h2 id="sp-unlock-title" className="sp-unlock-title">
          {t("statusPage.locked.title")}
        </h2>
        <p className="sp-unlock-body">{t("statusPage.locked.body")}</p>
        <form method="post" action={`/status/${encodeURIComponent(slug)}/unlock`} className="sp-unlock-form">
          <input type="hidden" name="next" value={next} />
          <label>
            <span className="pg-label">{t("statusPage.locked.password")}</span>
            <input type="password" name="password" required autoFocus autoComplete="current-password" className="field" />
          </label>
          {problem && (
            <p role="alert" className="sp-unlock-error">
              {t(problems[problem])}
            </p>
          )}
          <button type="submit" className="btn btn-primary">
            {t("statusPage.locked.open")}
          </button>
        </form>
      </section>
    </>
  );
}
