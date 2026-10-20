import assert from "node:assert/strict";
import { test } from "node:test";
import { oidcStateCookie, sessionCookie, signOutCookies } from "./cookie-names.ts";

test("Secure cookies carry the __Host- prefix", () => {
  assert.equal(sessionCookie(true), "__Host-kubeat_session");
  assert.equal(sessionCookie(false), "kubeat_session");
  assert.equal(oidcStateCookie(true), "__Host-kubeat_oidc_state");
  assert.equal(oidcStateCookie(false), "kubeat_oidc_state");
});

test("signing out over HTTPS also deletes the names of before the prefix", () => {
  // A kubeat_session left from before the upgrade would sign the browser
  // in again if the install went back to plain names.
  assert.deepEqual(signOutCookies(true), [
    { name: "__Host-kubeat_session", path: "/", secure: true },
    { name: "kubeat_session", path: "/", secure: true },
    { name: "kubeat_oidc_state", path: "/auth", secure: true },
  ]);
});

test("signing out over plain HTTP deletes the session cookie", () => {
  assert.deepEqual(signOutCookies(false), [{ name: "kubeat_session", path: "/", secure: false }]);
});
