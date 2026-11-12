"use client";

import { useTransition } from "react";
import { setLocaleAction } from "@/app/actions";
import { LanguageIcon } from "@/app/ui/icons";
import { useT } from "@/lib/i18n/client";

/**
 * One click switches to the other language; the choice is kept in a cookie.
 * In a menu (the rail's, the tab bar's) it is a row with the other
 * language's name; on a status page, a plain link-like button.
 */
export function LanguageToggle({ variant = "menu" }: { variant?: "menu" | "inline" }) {
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

  if (variant === "inline") {
    return (
      <button {...props} className="underline-offset-2 hover:text-ink hover:underline">
        {t("lang.switchTo")}
      </button>
    );
  }
  return (
    <button {...props} className="menu-item">
      <LanguageIcon />
      {t("lang.switchTo")}
    </button>
  );
}
