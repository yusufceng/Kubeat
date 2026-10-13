"use client";

import { useEffect } from "react";
import { useT } from "@/lib/i18n/client";

/**
 * What a page shows when rendering it failed, most often because the
 * backend could not be reached while it was starting or restarting. The
 * error itself goes to the console; the visitor gets a way back.
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
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-4 px-4 py-10 sm:px-6">
      <h1 className="text-xl font-semibold tracking-tight">
        {unreachable ? t("error.unreachableTitle") : t("error.title")}
      </h1>
      <p className="text-sm text-muted">
        {unreachable ? t("error.unreachableBody") : t("error.body")}{" "}
        {beforeVariable}
        <code>BACKEND_URL</code>
        {afterVariable}
      </p>
      <p className="font-mono text-xs text-muted">{error.message}</p>
      <div>
        <button type="button" onClick={reset} className="btn btn-primary">
          {t("error.retry")}
        </button>
      </div>
    </main>
  );
}
