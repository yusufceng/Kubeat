"use client";

import { useRouter } from "next/navigation";
import { useTransition } from "react";

/**
 * Asks the server for the page again, in place: the next action where the
 * backend could not answer. label is already translated by the server
 * component that shows the error.
 */
export function RetryButton({ label }: { label: string }) {
  const router = useRouter();
  const [pending, start] = useTransition();
  return (
    <button type="button" className="btn btn-sm" disabled={pending} onClick={() => start(() => router.refresh())}>
      {label}
    </button>
  );
}
