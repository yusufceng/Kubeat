"use client";

import { useEffect, useRef, useState } from "react";
import { useT } from "@/lib/i18n/client";
import { CopyIcon } from "@/app/ui/icons";
import { CheckIcon } from "@/app/ui/page-icons";

/**
 * A line to put into a manifest or a README, in mono, with a button that
 * copies it. Where the browser offers no clipboard (plain HTTP away from
 * localhost), the button selects the line for the viewer to copy.
 */
export function CopyLine({ text, label }: { text: string; label?: string }) {
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

  const name = copied ? t("copy.copied") : t("copy.copy");
  return (
    <div className="pg-code">
      <code ref={code} aria-label={label}>
        {text}
      </code>
      <button type="button" className="pg-icon-btn" onClick={copy} aria-label={`${name}: ${text}`} title={name}>
        {copied ? <CheckIcon className="text-up-ink" /> : <CopyIcon />}
      </button>
      <span className="sr-only" aria-live="polite">
        {copied ? t("copy.copied") : ""}
      </span>
    </div>
  );
}
