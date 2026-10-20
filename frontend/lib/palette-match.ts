/*
 * How the command palette matches what is typed: every word of the query
 * must be found somewhere in an entry's fields. A word at the start of the
 * name counts most, then at the start of a word inside it (after a space,
 * -, ., /, :, _ or @), then anywhere; the first field (the name) weighs
 * more than the rest (a namespace, a kind, an address). Case and accents
 * are folded, and so are Turkish's dotted and dotless i, so that "istanbul"
 * finds "İstanbul" and "sertifika" finds "SERTİFİKA".
 *
 * No imports, so that the tests run it as it is.
 */

/** Text as it is compared: lower case, without accents, every kind of i an i. */
export function fold(text: string): string {
  return text
    .replace(/[İIı]/g, "i")
    .toLowerCase()
    .normalize("NFD")
    .replace(/\p{M}/gu, "");
}

const boundary = /[\s\-./:_@]/;

/** 3 at the start of the text, 2 at the start of a word, 1 elsewhere, 0 when not found. */
function place(haystack: string, needle: string): number {
  let best = 0;
  let at = haystack.indexOf(needle);
  while (at >= 0 && best < 3) {
    best = Math.max(best, at === 0 ? 3 : boundary.test(haystack[at - 1]) ? 2 : 1);
    at = haystack.indexOf(needle, at + 1);
  }
  return best;
}

/**
 * The score of an entry for a query: 0 when a word of it is found in none
 * of the fields, higher for better matches. An empty query matches all.
 */
export function matchScore(query: string, fields: string[]): number {
  const words = fold(query).split(/\s+/).filter(Boolean);
  if (words.length === 0) return 1;
  const folded = fields.map(fold);
  let score = 0;
  for (const word of words) {
    let best = 0;
    folded.forEach((field, i) => {
      const p = place(field, word);
      if (p > 0) best = Math.max(best, i === 0 ? p * 2 : p);
    });
    if (best === 0) return 0;
    score += best;
  }
  // The whole name typed out goes first.
  if (folded[0] === fold(query).trim()) score += 10;
  return score;
}
