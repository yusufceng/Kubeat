/*
 * The client's address, for the backend's sign-in throttle. Browsers reach
 * the UI, never the backend: the Route or Ingress sends them here, so the
 * backend sees this pod connect for every client. It keys failed attempts
 * on the source address (CWE-307, ADR-0004), and reads that address from
 * X-Forwarded-For when UPTIME_TRUST_PROXY_HOPS says how many entries to
 * trust; without the header it can only key on this pod, one shared lock
 * for everyone.
 *
 * So the throttled calls (sign-in, password change, status-page unlock)
 * pass on the X-Forwarded-For the request came with, as it came: nothing
 * is added, dropped or reordered, since the hop count in the README is
 * worked out for exactly this. Next.js writes the header with its own peer
 * when no proxy in front did, and leaves one a proxy wrote alone. This has
 * no imports, so that the tests run it as it is.
 */

/**
 * The X-Forwarded-For header to send to the backend, given the one the
 * request came with: the same value, or none when there was none.
 */
export function forwardedForHeader(incoming: string | null | undefined): Record<string, string> {
  if (incoming == null || incoming.trim() === "") return {};
  return { "X-Forwarded-For": incoming };
}
