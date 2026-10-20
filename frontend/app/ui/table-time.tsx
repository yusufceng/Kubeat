"use client";

import { compactTime } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
import { useMounted } from "@/lib/use-mounted";

/**
 * A moment in a table row, in the viewer's zone on a 24-hour clock and no
 * longer than it must be: "19:11" today, "Çar 19:11" this week, "14 Eki
 * 19:11" further off (lib/format.ts, compactTime). The exact moment is in
 * the title. The server cannot know the viewer's zone, so the first render
 * shows UTC, labelled as such, until the browser takes over.
 */
export function TableTime({ iso }: { iso: string }) {
  const mounted = useMounted();
  const t = useT();
  const date = new Date(iso);
  return (
    <time dateTime={iso} title={mounted ? date.toISOString() : undefined}>
      {mounted ? compactTime(date, new Date(), t) : `${iso.slice(5, 10)} ${iso.slice(11, 16)} UTC`}
    </time>
  );
}
