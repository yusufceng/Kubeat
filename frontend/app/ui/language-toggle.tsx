"use client";

import { useTransition } from "react";
import { setLocaleAction } from "@/app/actions";
import { useT } from "@/lib/i18n/client";

/** One click switches to the other language; the choice is kept in a cookie. */
export function LanguageToggle() {
  const t = useT();
  const [pending, start] = useTransition();
  const other = t.locale === "tr" ? "en" : "tr";
  return (
    <button
      type="button"
      onClick={() => start(() => setLocaleAction(other))}
      disabled={pending}
      title={t("lang.switchToTitle")}
      className="rounded-lg px-2 py-1.5 text-xs font-medium uppercase text-muted hover:text-ink"
    >
      {other}
    </button>
  );
}
