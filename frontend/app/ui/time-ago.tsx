"use client";

import { useEffect, useState } from "react";
import { timeAgo } from "@/lib/format";
import { useT } from "@/lib/i18n/client";

/** Relative time that keeps counting between data refreshes. */
export function TimeAgo({ iso }: { iso: string }) {
  // The clock is only read on the client, so the server render stays pure
  // and hydration cannot mismatch.
  const [now, setNow] = useState<number | null>(null);
  const t = useT();

  useEffect(() => {
    const update = () => setNow(Date.now());
    update();
    const id = setInterval(update, 1000);
    return () => clearInterval(id);
  }, []);

  return (
    <time dateTime={iso} title={new Date(iso).toISOString()}>
      {now === null ? "…" : timeAgo(iso, now, t)}
    </time>
  );
}
