"use client";

import { useEffect } from "react";
import { useT } from "@/lib/i18n/client";
import { UnplugIcon, WarningIcon } from "@/app/ui/page-icons";

/**
 * What a page shows when rendering it failed, most often because the
 * backend could not be reached while it was starting or restarting. The
 * error itself goes to the console; the visitor gets what happened, the
 * likeliest reason, and a way to try again.
 */
export default function Error({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  const t = useT();
  useEffect(() => {
    console.error(error);
  }, [error]);
  // In production the server's message does not reach the browser, so the
  // likeliest cause is named either way.
  const unreachable = /fetch failed|ECONNREFUSED|unreachable|timeout/i.test(error.message);
  // The hint names the variable; it is set as code.
  const [beforeVariable, afterVariable] = t("error.hint").split("BACKEND_URL");
  return (
    <main className="page">
      <section role="alert" className="panel pg-empty">
        <span className="pg-empty-icon pg-empty-icon-down" aria-hidden>
          {unreachable ? <UnplugIcon width={20} height={20} /> : <WarningIcon width={20} height={20} />}
        </span>
        <h1 className="pg-empty-title">{unreachable ? t("error.unreachableTitle") : t("error.title")}</h1>
        <div className="pg-empty-body">
          <p>{unreachable ? t("error.unreachableBody") : t("error.body")}</p>
          <p className="mt-2 text-ink-3">
            {beforeVariable}
            <code className="pg-code-inline">BACKEND_URL</code>
            {afterVariable}
          </p>
        </div>
        {(error.message || error.digest) && (
          <pre className="pg-error-detail">{error.message || error.digest}</pre>
        )}
        <div className="pg-empty-action">
          <button type="button" onClick={reset} className="btn btn-primary">
            {t("error.retry")}
          </button>
        </div>
      </section>
    </main>
  );
}
