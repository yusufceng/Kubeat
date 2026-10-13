"use client";

import { useT } from "@/lib/i18n/client";
import { useMounted } from "@/lib/use-mounted";

/**
 * A moment shown in the viewer's own time zone. The server cannot know that
 * zone, so the first render shows UTC, labelled as such, and the browser
 * replaces it once the component mounts.
 */
export function LocalTime({ iso, seconds = true }: { iso: string; seconds?: boolean }) {
  const mounted = useMounted();
  const t = useT();

  const date = new Date(iso);
  const text = mounted
    ? date.toLocaleString(t.intl, {
        day: "2-digit",
        month: "2-digit",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        ...(seconds && { second: "2-digit" }),
      })
    : `${iso.slice(0, 10)} ${iso.slice(11, seconds ? 19 : 16)} UTC`;

  return (
    <time dateTime={iso} title={mounted ? date.toISOString() : undefined}>
      {text}
    </time>
  );
}
