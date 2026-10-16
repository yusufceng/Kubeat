/**
 * A 303 to a path of this application, or to the identity provider. A path
 * stays relative: the request's own URL carries the address this server
 * listens on (0.0.0.0:3000 in the container), never the one the browser
 * used, and the browser resolves a relative Location against its own. No
 * cache may keep it: each one belongs to one sign-in.
 */
export function seeOther(location: string): Response {
  return new Response(null, { status: 303, headers: { Location: location, "Cache-Control": "no-store" } });
}
