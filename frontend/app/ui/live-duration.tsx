"use client";

import { useEffect, useState } from "react";
import { span, spanShort } from "@/lib/format";
import { useT } from "@/lib/i18n/client";

/**
 * Time elapsed since a moment, counting up while the page is open. short
 * keeps its largest unit alone ("3 dk"), for where a row has no room for
 * two.
 */
export function LiveDuration({ since, short = false }: { since: string; short?: boolean }) {
  const [now, setNow] = useState<number | null>(null);
  const t = useT();

  useEffect(() => {
    const update = () => setNow(Date.now());
    update();
    const id = setInterval(update, 1000);
    return () => clearInterval(id);
  }, []);

  return <span>{now === null ? "…" : (short ? spanShort : span)(now - new Date(since).getTime(), t)}</span>;
}
