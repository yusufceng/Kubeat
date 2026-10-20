/**
 * The logo is the word: "kubeat" in Mona Sans (wdth 118, wght 680),
 * outlined and spaced by measurement, whose k is the mark. The k's stem is
 * cut on the line of its own arm, a rising edge: read along its top, the k
 * is a trace (rise, fall, rise, hold), the tick each check fires on. The k
 * is drawn in the accent and from a heavier instance (720), since blue
 * reads lighter than ink; it never takes a status colour.
 *
 * The files, the sizes they are drawn for and the rules are in
 * docs/assets (README.md there). app/icon.svg is the bare k for the tab.
 */

/** The word: its box runs from the k's tip (-74) to the round letters' foot (+1); one unit is 1/100 em. */
const WORD_VIEWBOX = "0 -74 365.2 75";
const WORD_WIDTH = 365.2;
const WORD_HEIGHT = 75;
const WORD_K = "M0 0V-58.6L16.8 -73.9V-32.2L22.8 -38.1L40 -53.7H60.9L36.9 -32.4L61.5 0H41L28.3 -17.2L24.9 -22.3L16.8 -15.2V0Z";
const WORD_REST =
  "M86.3 0.9Q79.6 0.9 74.6 -1.5Q69.6 -3.8 66.9 -9.1Q64.2 -14.4 64.2 -23V-53.6H79.9V-26.1Q79.9 -21.4 81.1 -18.1Q82.3 -14.7 84.9 -13Q87.6 -11.2 91.9 -11.2Q96.3 -11.2 99.5 -13.1Q102.6 -15.1 104.4 -18.4Q106.2 -21.8 106.2 -26.1V-53.6H121.9V0H106.8L106.2 -9.6H105.4Q103 -4.7 97.8 -1.9Q92.6 0.9 86.3 0.9ZM167.7 0.9Q162.9 0.9 159.1 -0.3Q155.3 -1.5 152.6 -3.7Q149.9 -5.9 148.4 -8.8H147.8L147.2 0H132.3V-72.9H148.1V-44.6H148.7Q151 -49.2 156.1 -51.9Q161.3 -54.5 167.7 -54.5Q173.4 -54.5 178 -52.6Q182.6 -50.7 185.9 -47Q189.2 -43.4 191 -38.3Q192.8 -33.2 192.8 -26.8Q192.8 -18.3 189.6 -12.1Q186.5 -5.8 180.9 -2.5Q175.3 0.9 167.7 0.9ZM162.6 -11.3Q167 -11.3 170.2 -13.1Q173.5 -14.9 175.2 -18.5Q176.9 -22 176.9 -26.8Q176.9 -30.5 176 -33.3Q175 -36.2 173.1 -38.2Q171.3 -40.2 168.7 -41.3Q166 -42.4 162.7 -42.4Q158.1 -42.4 154.8 -40.5Q151.5 -38.6 149.8 -35.5Q148.1 -32.3 148.1 -28.4V-25Q148.1 -21.2 149.8 -18.1Q151.5 -14.9 154.8 -13.1Q158.1 -11.3 162.6 -11.3ZM227.8 0.9Q218.3 0.9 211.6 -2.5Q204.8 -5.9 201.2 -12.1Q197.6 -18.2 197.6 -26.5Q197.6 -35.2 201.3 -41.4Q205 -47.7 211.8 -51.1Q218.6 -54.4 227.8 -54.4Q238 -54.4 244.9 -50.5Q251.7 -46.6 254.9 -39.6Q258 -32.6 257.1 -23.2H213.8Q213.9 -19 215.7 -16.2Q217.6 -13.3 220.8 -11.8Q224.1 -10.3 228.4 -10.3Q233.3 -10.3 236.8 -12.1Q240.3 -14 241.3 -16.9H256.6Q255.7 -11.3 251.7 -7.3Q247.8 -3.3 241.7 -1.2Q235.5 0.9 227.8 0.9ZM213.5 -31.2 212.7 -32.3H242.3L241.4 -31.2Q241.4 -35.3 239.8 -38Q238.2 -40.7 235.1 -42.2Q232 -43.6 227.8 -43.6Q223.5 -43.6 220.4 -42.1Q217.2 -40.6 215.5 -37.8Q213.7 -35 213.5 -31.2ZM286.4 1Q280.8 1 276.2 -1Q271.6 -2.9 268.3 -6.5Q264.9 -10.1 263.1 -15.2Q261.3 -20.4 261.3 -26.7Q261.3 -35.3 264.5 -41.6Q267.6 -47.8 273.3 -51.1Q279 -54.4 286.4 -54.4Q291.3 -54.4 295.1 -53.2Q298.9 -52 301.6 -49.9Q304.3 -47.7 305.7 -44.8H306.4L306.9 -53.6H321.8V0H306.3L306.1 -8.9H305.5Q303.2 -4.4 298.1 -1.7Q292.9 1 286.4 1ZM291.5 -11.2Q296.1 -11.2 299.4 -13.1Q302.6 -14.9 304.3 -18.1Q306 -21.3 306 -25.1V-28.6Q306 -32.5 304.3 -35.6Q302.6 -38.7 299.3 -40.5Q296.1 -42.3 291.6 -42.3Q287.2 -42.3 284 -40.5Q280.8 -38.7 279 -35.2Q277.2 -31.7 277.2 -26.7Q277.2 -21.9 279 -18.4Q280.7 -14.9 283.9 -13Q287 -11.2 291.5 -11.2ZM350.7 0Q343.8 0 340.4 -3.4Q337 -6.9 337 -13.8V-41.5H327V-53.6H337V-67.5L352.5 -70.2V-53.6H365.2V-41.5H352.5V-17.8Q352.5 -15.3 353.7 -14.3Q355 -13.3 357.2 -13.3H365.2V0Z";

/**
 * The k on the tile, drawn for 32px: the k at 2/3 of the 32-unit mark, so
 * every vertical and horizontal edge falls on a whole pixel. A light k on
 * the blue tile spreads, so in the light theme (a white k) its arm and leg
 * are a quarter unit thinner; on the dark theme's light tile the k is dark.
 */
const TILE_K = "M10 24V10.667L14 6.667V16.667L18.667 12H23.333L18.381 16.952L23.667 24H19.333L15.619 19.048L14 20.667V24Z";
const TILE_K_LIGHT = "M10 24V10.667L14 6.667V16.902L18.902 12H23.333L18.381 16.952L23.667 24H19.542L15.738 18.929L14 20.667V24Z";

/** The app's mark: the k on an accent tile, at the top of the rail. size is the tile's side; it is drawn for 32. */
export function LogoMark({ size = 32, className }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden className={className}>
      <rect width="32" height="32" rx="7" fill="var(--accent)" />
      <path className="dark:hidden" fill="var(--accent-ink)" d={TILE_K_LIGHT} />
      <path className="hidden dark:inline" fill="var(--accent-ink)" d={TILE_K} />
    </svg>
  );
}

function Word() {
  return (
    <>
      <path fill="var(--accent)" d={WORD_K} />
      <path fill="currentColor" d={WORD_REST} />
    </>
  );
}

/**
 * The name in running text, at the text's own size: 0.75em high, on the
 * baseline, the k in the accent and the rest in the text's colour.
 */
export function Wordmark({ className = "" }: { className?: string }) {
  return (
    <svg
      viewBox={WORD_VIEWBOX}
      role="img"
      aria-label="kubeat"
      className={`inline-block h-[0.75em] w-auto align-[-0.01em] ${className}`}
    >
      <Word />
    </svg>
  );
}

/**
 * The logo where the product introduces itself: sign-in, the phone's
 * header. size is its height in px, the k's tip to the foot of the round
 * letters; at 18 and 22 the k's stem is close to 4 and 5 whole pixels.
 */
export function Logo({ size = 22, className = "" }: { size?: number; className?: string }) {
  return (
    <svg
      width={(size * WORD_WIDTH) / WORD_HEIGHT}
      height={size}
      viewBox={WORD_VIEWBOX}
      role="img"
      aria-label="kubeat"
      className={className}
    >
      <Word />
    </svg>
  );
}
