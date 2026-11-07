import assert from "node:assert/strict";
import { test } from "node:test";
import { FRONTEND_TOKEN_HEADER, frontendToken, withFrontendToken } from "./frontend-token.ts";

test("the configured token goes with the request", () => {
  assert.deepEqual(withFrontendToken({ "Content-Type": "application/json" }, "Gh7k2Lm9"), {
    "Content-Type": "application/json",
    "X-Frontend-Token": "Gh7k2Lm9",
  });
  assert.deepEqual(withFrontendToken(undefined, "Gh7k2Lm9"), { "X-Frontend-Token": "Gh7k2Lm9" });
});

test("a header of the same name that came with the browser is not passed on", () => {
  // Whatever its case: HTTP header names are not case-sensitive, and the
  // backend would read any of them.
  for (const name of ["X-Frontend-Token", "x-frontend-token", "X-FRONTEND-TOKEN", "x-Frontend-token"]) {
    const browser = { [name]: "forged", "X-Forwarded-For": "203.0.113.7" };
    assert.deepEqual(withFrontendToken(browser, "Gh7k2Lm9"), {
      "X-Forwarded-For": "203.0.113.7",
      "X-Frontend-Token": "Gh7k2Lm9",
    });
    // Without a token of our own, none goes at all: not the browser's.
    assert.deepEqual(withFrontendToken(browser, ""), { "X-Forwarded-For": "203.0.113.7" });
  }
});

test("the other headers stay as they are", () => {
  assert.deepEqual(withFrontendToken({ Authorization: "Bearer s", "X-Forwarded-For": "1.2.3.4, 203.0.113.7" }, "t"), {
    Authorization: "Bearer s",
    "X-Forwarded-For": "1.2.3.4, 203.0.113.7",
    "X-Frontend-Token": "t",
  });
});

test("the token is read without surrounding spaces", () => {
  assert.equal(frontendToken("Gh7k2Lm9"), "Gh7k2Lm9");
  assert.equal(frontendToken("  Gh7k2Lm9\n"), "Gh7k2Lm9");
});

test("an unset token, or one a header cannot carry, is not sent", () => {
  for (const value of [undefined, "", "   ", "Gh7k 2Lm9", "Gh7k\n2Lm9", "Gh7kç2Lm9", "Gh7k\u00002Lm9"]) {
    assert.equal(frontendToken(value), "", JSON.stringify(value));
    assert.deepEqual(withFrontendToken({}, frontendToken(value)), {});
  }
});

test("the header is the one the backend reads", () => {
  // backend/internal/api/throttle.go, frontendTokenHeader.
  assert.equal(FRONTEND_TOKEN_HEADER, "X-Frontend-Token");
});
