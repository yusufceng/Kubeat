import { ApiError } from "@/lib/api";

/**
 * Shown where a page could not get its data from the backend. A rejected
 * API token is called out, since restarting things will not fix it.
 */
export function BackendError({ what, err }: { what: string; err?: unknown }) {
  const unauthorized = err instanceof ApiError && err.status === 401;
  return (
    <p
      role="alert"
      className="rounded-lg border border-down/40 bg-down/8 p-4 text-sm text-down"
    >
      {unauthorized
        ? "Backend API anahtarını kabul etmedi: arayüzdeki BACKEND_TOKEN ile backend'deki UPTIME_API_TOKEN aynı olmalı."
        : "Backend'e ulaşılamıyor."}{" "}
      {what} gösterilemiyor.
    </p>
  );
}
