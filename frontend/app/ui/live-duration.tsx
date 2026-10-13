"use client";

import { useEffect, useState } from "react";
import { span } from "@/lib/format";
import { useT } from "@/lib/i18n/client";

/** Time elapsed since a moment, counting up while the page is open. */
export function LiveDuration({ since }: { since: string }) {
  const [now, setNow] = useState<number | null>(null);
  const t = useT();

  useEffect(() => {
    const update = () => setNow(Date.now());
    update();
    const id = setInterval(update, 1000);
    return () => clearInterval(id);
  }, []);

  return <span>{now === null ? "…" : span(now - new Date(since).getTime(), t)}</span>;
}
