/**
 * The headers that tell the browser what a response may do: a
 * Content-Security-Policy for each kind of answer, and
 * Strict-Transport-Security. proxy.ts puts them on every answer it lets
 * through or makes; they depend on the install, so they cannot go into
 * next.config.ts, whose headers are fixed when the bundle is built.
 *
 * Pages may run inline scripts. A nonce would be the stricter way, but it
 * needs every page rendered per request: with Cache Components (partial
 * prerendering), the shell of a page is rendered ahead of any request and
 * its inline scripts (the theme script, React's and Turbopack's own) can
 * carry no per-request nonce, and the data scripts each answer streams
 * cannot be listed by hash either. What the policy still forbids: scripts
 * from anywhere else, eval, plugins, <base>, forms posting elsewhere,
 * connections elsewhere, and being shown in someone else's frame.
 *
 * Nothing here depends on the server's environment; the caller passes it in.
 */

export type SecurityOptions = {
  /** `next dev`: React rebuilds server error stacks in the browser with eval. */
  dev: boolean;
  /** The request came over HTTPS (see forwardedHttps). */
  https: boolean;
  /** Users reach Kubeat at an HTTPS address (PUBLIC_URL). */
  publicHttps: boolean;
  /** The CSP source list of the pages that may show a status page in a frame. */
  statusPageFrameAncestors: string;
  /** Strict-Transport-Security max-age in seconds; 0 sends none. */
  hstsMaxAge: number;
};

/**
 * Whether the proxies in front of us say the request came over HTTPS.
 * X-Forwarded-Proto may hold a value per proxy ("https, http" when TLS ended
 * at a load balancer in front of the router); one HTTPS hop is enough. A
 * client that sends the header itself only changes its own answer.
 */
export function forwardedHttps(forwardedProto: string | null | undefined): boolean {
  return (forwardedProto ?? "").split(",").some((proto) => proto.trim().toLowerCase() === "https");
}

/** Whether an address such as PUBLIC_URL is an HTTPS one. */
export function isHttpsUrl(url: string): boolean {
  return /^https:\/\//i.test(url);
}

/** The headers for the answer to a request for pathname. */
export function securityHeaders(pathname: string, opts: SecurityOptions): Record<string, string> {
  const headers: Record<string, string> = { "Content-Security-Policy": contentSecurityPolicy(pathname, opts) };
  // Browsers ignore it over plain HTTP, so an HTTPS public address is
  // reason enough; no includeSubDomains or preload, since the host may be
  // one name under a shared apps domain.
  if (opts.hstsMaxAge > 0 && (opts.https || opts.publicHttps)) {
    headers["Strict-Transport-Security"] = `max-age=${opts.hstsMaxAge}`;
  }
  return headers;
}

function contentSecurityPolicy(pathname: string, opts: SecurityOptions): string {
  // A badge is an SVG image that other sites embed (a README, a wiki).
  // Embedded with <img> it runs nothing anyway; opened on its own, it may
  // draw but not run anything either.
  if (pathname.startsWith("/api/badge/")) {
    return "default-src 'none'; style-src 'unsafe-inline'; sandbox";
  }
  // Push reports (JSON for jobs) and the CSV download: nothing to render.
  if (pathname.startsWith("/api/push/") || pathname === "/reports/csv") {
    return "default-src 'none'; frame-ancestors 'none'; sandbox";
  }
  // Status pages may be shown in a frame where the install allows it; the
  // rest of the UI never, so that no other page can lay itself over it.
  const frameAncestors = pathname.startsWith("/status/") ? opts.statusPageFrameAncestors : "'none'";
  return [
    "default-src 'self'",
    `script-src 'self' 'unsafe-inline'${opts.dev ? " 'unsafe-eval'" : ""}`,
    // React's style={{...}} props become style attributes.
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data:",
    "font-src 'self'",
    "connect-src 'self'",
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'self'",
    "frame-src 'none'",
    `frame-ancestors ${frameAncestors}`,
    // Over plain HTTP (localhost, a port-forward) the upgrade would break
    // every script and stylesheet.
    ...(opts.https ? ["upgrade-insecure-requests"] : []),
  ].join("; ");
}
