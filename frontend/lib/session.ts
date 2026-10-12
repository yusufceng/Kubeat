import "server-only";

import { createHash, createHmac, timingSafeEqual } from "node:crypto";

/**
 * Sign-in with a single administrator password. A session is a signed
 * cookie holding only its expiry time; nothing is stored on the server.
 */

export const SESSION_COOKIE = "kubeat_session";

function positiveNumber(value: string | undefined, fallback: number): number {
  const n = Number(value);
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

const adminPassword = process.env.ADMIN_PASSWORD ?? "";

/** Whether sign-in is required. Without a password the UI is open. */
export const loginEnabled = adminPassword !== "";

/** How long a session lasts. */
export const sessionTtlMs = positiveNumber(process.env.SESSION_TTL_HOURS, 12) * 3600_000;

// Failed attempts are throttled for everyone at once: with a single shared
// password there is no account to lock, only guessing to slow down.
const maxFailures = positiveNumber(process.env.LOGIN_MAX_FAILURES, 10);
const lockoutMs = positiveNumber(process.env.LOGIN_LOCKOUT_MINUTES, 15) * 60_000;

// Changing the password ends every session when no separate secret is set.
const signingKey = createHash("sha256")
  .update(process.env.SESSION_SECRET || `kubeat-session:${adminPassword}`)
  .digest();

function sign(payload: string): string {
  return createHmac("sha256", signingKey).update(payload).digest("hex");
}

/** Compares in constant time, whatever the lengths. */
function sameString(a: string, b: string): boolean {
  const digest = (s: string) => createHash("sha256").update(s).digest();
  return timingSafeEqual(digest(a), digest(b));
}

/** The cookie value of a session starting now. */
export function createSession(now: number = Date.now()): string {
  const expires = String(now + sessionTtlMs);
  return `${expires}.${sign(expires)}`;
}

/** Whether a cookie value is a session this server issued and that has not expired. */
export function validSession(value: string | undefined, now: number = Date.now()): boolean {
  if (!value) return false;
  const dot = value.indexOf(".");
  if (dot <= 0) return false;
  const expires = value.slice(0, dot);
  if (!/^\d+$/.test(expires) || !sameString(value.slice(dot + 1), sign(expires))) return false;
  return Number(expires) > now;
}

const failures: number[] = [];

/** How long sign-in stays blocked after too many wrong passwords; 0 if it is not. */
export function lockedForMs(now: number = Date.now()): number {
  while (failures.length > 0 && now - failures[0] >= lockoutMs) failures.shift();
  return failures.length >= maxFailures ? failures[0] + lockoutMs - now : 0;
}

/** Checks a password attempt, counting it against the throttle if wrong. */
export function checkPassword(attempt: string, now: number = Date.now()): boolean {
  if (!loginEnabled || lockedForMs(now) > 0) return false;
  if (sameString(attempt, adminPassword)) {
    failures.length = 0;
    return true;
  }
  failures.push(now);
  return false;
}
