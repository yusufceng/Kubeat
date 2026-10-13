import "server-only";

import { cookies, headers } from "next/headers";
import { cache } from "react";
import { config } from "@/lib/config";
import { type Locale, locales } from "@/lib/i18n/messages";
import { type T, translator } from "@/lib/i18n/translate";

/** The visitor's choice, kept until they change it. */
export const LOCALE_COOKIE = "kubeat_lang";

function isLocale(value: string | undefined): value is Locale {
  return locales.includes(value as Locale);
}

/** The first language the browser prefers that we have. */
function negotiate(acceptLanguage: string | null): Locale | null {
  if (!acceptLanguage) return null;
  const wanted = acceptLanguage
    .split(",")
    .map((part) => {
      const [tag, ...rest] = part.trim().split(";");
      const q = rest.find((p) => p.trim().startsWith("q="));
      return { tag: tag.toLowerCase(), q: q ? Number(q.trim().slice(2)) : 1 };
    })
    .sort((a, b) => b.q - a.q);
  for (const { tag } of wanted) {
    const base = tag.split("-")[0];
    if (isLocale(base)) return base;
  }
  return null;
}

/**
 * The locale of the request: the cookie, else what the browser asks for,
 * else the server's default. Request data, so only for dynamic rendering.
 */
export const getLocale = cache(async (): Promise<Locale> => {
  const chosen = (await cookies()).get(LOCALE_COOKIE)?.value;
  if (isLocale(chosen)) return chosen;
  return negotiate((await headers()).get("accept-language")) ?? config.defaultLocale;
});

/** The translator of the request. */
export const getT = cache(async (): Promise<T> => translator(await getLocale()));
