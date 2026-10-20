"use client";

import { useEffect, useRef, useState } from "react";
import { useT } from "@/lib/i18n/client";
import { CheckIcon } from "@/app/ui/fleet-icons";
import { CopyIcon } from "@/app/ui/icons";

/**
 * A line to put into a manifest, in mono, with a button that copies it.
 * Where the browser offers no clipboard (plain HTTP away from localhost),
 * the button selects the line for the viewer to copy.
 */
export function CopyText({ text }: { text: string }) {
  const t = useT();
  const code = useRef<HTMLElement>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const id = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(id);
  }, [copied]);

  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
    } catch {
      const range = document.createRange();
      if (code.current) range.selectNodeContents(code.current);
      const selection = window.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
    }
  }

  const label = copied ? t("monitorsList.overview.empty.copied") : t("monitorsList.overview.empty.copy");
  return (
    <div className="fl-code">
      <code ref={code}>{text}</code>
      <button type="button" className="btn btn-ghost btn-sm fl-icon-btn" onClick={copy} aria-label={label} title={label}>
        {copied ? <CheckIcon className="text-up-ink" /> : <CopyIcon />}
      </button>
      <span className="sr-only" aria-live="polite">
        {copied ? label : ""}
      </span>
    </div>
  );
}
