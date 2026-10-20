import { percentParts } from "@/lib/monitor-page";

/** The sign set small and quiet beside the figure, as the readouts set their units. */
const SIGN = "text-[13px] leading-none font-[550] text-ink-3 [font-stretch:100%]";

/**
 * A percent for the large figures: the number in the figure's style, the
 * sign small and muted where the language puts it (%99,55 in Turkish,
 * 99.55% in English). Floored, like every uptime Kubeat shows.
 */
export function Percent({ value, intl }: { value: number; intl: string }) {
  const p = percentParts(value, intl);
  return p.signFirst ? (
    <>
      <small className={`mr-0.5 ${SIGN}`}>{p.sign}</small>
      {p.number}
    </>
  ) : (
    <>
      {p.number}
      <small className={`ml-0.5 ${SIGN}`}>{p.sign}</small>
    </>
  );
}
