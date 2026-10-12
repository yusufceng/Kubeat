/** Hues a tag can take; which one follows from its name, so it never changes. */
const hues = [205, 265, 325, 25, 85, 150, 180, 235, 295, 355];

function hueOf(name: string): number {
  let hash = 0;
  for (const ch of name) hash = (hash * 31 + ch.codePointAt(0)!) >>> 0;
  return hues[hash % hues.length];
}

/** A tag as a small colored label. */
export function Tag({ name }: { name: string }) {
  const hue = hueOf(name);
  return (
    <span
      className="inline-block whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium"
      // Only the hue varies; the lightness of the text follows the theme.
      style={{
        backgroundColor: `oklch(0.62 0.13 ${hue} / 0.16)`,
        color: `oklch(var(--tag-ink) 0.14 ${hue})`,
      }}
    >
      {name}
    </span>
  );
}
