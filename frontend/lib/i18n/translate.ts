import { type Locale, messages } from "@/lib/i18n/messages";

export type Params = Record<string, string | number>;

/** Translates keys; `n` picks the singular or plural form of a key. */
export type T = ((key: string, params?: Params) => string) & {
  locale: Locale;
  /** The locale as Intl wants it, for numbers and dates. */
  intl: string;
  n: (key: string, count: number, params?: Params) => string;
};

const intlLocales: Record<Locale, string> = { en: "en-US", tr: "tr-TR" };

function fill(text: string, params?: Params): string {
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (match, name: string) => (name in params ? String(params[name]) : match));
}

/** A translator for the locale; unknown keys fall back to English, then to the key itself. */
export function translator(locale: Locale): T {
  const table = messages[locale];
  const t = ((key: string, params?: Params) => fill(table[key] ?? messages.en[key] ?? key, params)) as T;
  t.locale = locale;
  t.intl = intlLocales[locale];
  t.n = (key, count, params) => {
    const form = count === 1 && (table[`${key}.one`] ?? messages.en[`${key}.one`]) !== undefined ? `${key}.one` : `${key}.other`;
    return t(form, { count, ...params });
  };
  return t;
}
