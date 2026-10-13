/**
 * A message module: the Turkish strings and their English counterparts,
 * keyed the same way. English must cover every Turkish key; the compiler
 * says so when it does not.
 */
export function defineMessages<const T extends Record<string, string>>(tr: T, en: { [K in keyof T]: string }) {
  return { tr, en };
}
