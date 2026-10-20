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

/**
 * A tag the Kubeat Signal way: a neutral outlined chip, its hue only in a
 * small dot, so that tags never read as a state.
 */
export function TagChip({ name }: { name: string }) {
  return (
    <span className="inline-flex h-[22px] max-w-full items-center gap-1.5 rounded-xs border border-line px-2 text-xs leading-none font-medium whitespace-nowrap text-ink-2">
      <span
        aria-hidden
        className="h-1.5 w-1.5 flex-none rounded-full"
        style={{ backgroundColor: `oklch(0.65 0.13 ${hueOf(name)})` }}
      />
      <span className="truncate">{name}</span>
    </span>
  );
}
