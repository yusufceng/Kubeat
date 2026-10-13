"use client";

import { createContext, useContext, useEffect, useMemo } from "react";
import type { Locale } from "@/lib/i18n/messages";
import { type T, translator } from "@/lib/i18n/translate";

const LocaleContext = createContext<Locale>("en");

/** Hands the request's locale to client components; keeps <html lang> in step. */
export function I18nProvider({ locale, children }: { locale: Locale; children: React.ReactNode }) {
  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);
  return <LocaleContext.Provider value={locale}>{children}</LocaleContext.Provider>;
}

export function useLocale(): Locale {
  return useContext(LocaleContext);
}

/** The translator for client components. */
export function useT(): T {
  const locale = useLocale();
  return useMemo(() => translator(locale), [locale]);
}
