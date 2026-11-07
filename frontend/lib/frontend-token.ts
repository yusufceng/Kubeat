/*
 * The UI's token, by which the backend tells this UI from any other peer.
 * The backend reads X-Forwarded-For (lib/forwarded-for.ts) only from a
 * request that carries it: any pod in the cluster can reach the backend's
 * Service, and pod addresses come from one network, so no address tells
 * this pod from another. Without the token a pod could write a header of
 * its own and pick the source its sign-ins count against (CWE-307,
 * ADR-0004).
 *
 * Every request to the backend carries it, from FRONTEND_TOKEN (the
 * backend's UPTIME_FRONTEND_TOKEN; the chart gives both from one Secret).
 * A header of the same name that came with the browser's request is never
 * passed on: only this server writes it. The token is never logged. This
 * has no imports, so that the tests run it as it is.
 */

/** The header the backend reads the token from (frontendTokenHeader there). */
export const FRONTEND_TOKEN_HEADER = "X-Frontend-Token";

/**
 * The token as configured, ready to send: without surrounding spaces, as a
 * header value arrives at the backend; empty when it is unset, or when it
 * holds a character a header cannot carry as it is (only printable ASCII
 * without spaces can, the backend's rule too), which would otherwise make
 * every request to the backend fail.
 */
export function frontendToken(value: string | undefined): string {
  const token = (value ?? "").trim();
  return /^[\x21-\x7e]+$/.test(token) ? token : "";
}

/**
 * The headers of a request to the backend: the ones given, less any named
 * like the token whatever its case, and the token when there is one.
 */
export function withFrontendToken(
  headers: Record<string, string> | undefined,
  token: string,
): Record<string, string> {
  const name = FRONTEND_TOKEN_HEADER.toLowerCase();
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(headers ?? {})) {
    if (key.toLowerCase() !== name) out[key] = value;
  }
  if (token) out[FRONTEND_TOKEN_HEADER] = token;
  return out;
}
