import assert from "node:assert/strict";
import { test } from "node:test";
import { forwardedHttps, securityHeaders, type SecurityOptions } from "./security-headers.ts";

const install: SecurityOptions = {
  dev: false,
  https: true,
  publicUrl: "",
  cookieSecure: "auto",
  statusPageFrameAncestors: "'none'",
  hstsMaxAge: 31536000,
};

function headers(opts: Partial<SecurityOptions>, pathname = "/"): Record<string, string> {
  return securityHeaders(pathname, { ...install, ...opts });
}

function upgrades(opts: Partial<SecurityOptions>): boolean {
  return headers(opts)["Content-Security-Policy"].includes("upgrade-insecure-requests");
}

test("only the first, client-facing X-Forwarded-Proto counts", () => {
  assert.equal(forwardedHttps("https"), true);
  assert.equal(forwardedHttps("https, http"), true);
  assert.equal(forwardedHttps("http, https"), false);
  assert.equal(forwardedHttps(null), false);
});

test("upgrade-insecure-requests goes to HTTPS browsers only", () => {
  assert.equal(upgrades({}), true);
  assert.equal(upgrades({ publicUrl: "https://kubeat.apps.example" }), true);
  assert.equal(upgrades({ cookieSecure: "always" }), true);
  assert.equal(upgrades({ https: false }), false);
  assert.equal(headers({}, "/api/badge/x")["Content-Security-Policy"].includes("upgrade-insecure-requests"), false);
});

test("no upgrade-insecure-requests where the install says users come over plain HTTP", () => {
  // A plain-HTTP proxy that adds no X-Forwarded-Proto of its own, in front
  // of an HTTPS Route: the router's "https" is the first value.
  assert.equal(upgrades({ cookieSecure: "never" }), false);
  assert.equal(upgrades({ publicUrl: "http://kubeat.corp" }), false);
  assert.equal(upgrades({ publicUrl: "HTTP://kubeat.corp" }), false);
});

test("Strict-Transport-Security for HTTPS requests and HTTPS public addresses", () => {
  assert.equal(headers({})["Strict-Transport-Security"], "max-age=31536000");
  assert.equal(headers({ https: false, publicUrl: "https://kubeat.apps.example" })["Strict-Transport-Security"], "max-age=31536000");
  assert.equal(headers({ https: false })["Strict-Transport-Security"], undefined);
  assert.equal(headers({ hstsMaxAge: 0 })["Strict-Transport-Security"], undefined);
});
