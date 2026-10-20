"use client";

import { useTransition } from "react";
import { setLocaleAction } from "@/app/actions";
import { LanguageIcon } from "@/app/ui/icons";
import { useT } from "@/lib/i18n/client";

/**
 * One click switches to the other language; the choice is kept in a cookie.
 * In the rail it is a chip with the other language's code (TR, EN); in a
 * menu, a row with its name; on a status page, a plain link-like button.
 */
export function LanguageToggle({ variant = "rail" }: { variant?: "rail" | "menu" | "inline" }) {
  const t = useT();
  const [pending, start] = useTransition();
  const other = t.locale === "tr" ? "en" : "tr";
  const props = {
    type: "button" as const,
    onClick: () => start(() => setLocaleAction(other)),
    disabled: pending,
    title: t("lang.switchToTitle"),
    lang: other,
  };

  if (variant === "menu") {
    return (
      <button {...props} className="menu-item">
        <LanguageIcon />
        {t("lang.switchTo")}
      </button>
    );
  }
  if (variant === "inline") {
    return (
      <button {...props} className="underline-offset-2 hover:text-ink hover:underline">
        {t("lang.switchTo")}
      </button>
    );
  }
  return (
    <button
      {...props}
      className="rounded-xs border border-line px-[5px] py-1 font-mono text-[11px] leading-none font-semibold text-ink-3 hover:border-line-strong hover:text-ink"
    >
      {/* A code, not a word: written in capitals rather than transformed,
          which would follow Turkish casing rules. */}
      {other === "tr" ? "TR" : "EN"}
    </button>
  );
}
