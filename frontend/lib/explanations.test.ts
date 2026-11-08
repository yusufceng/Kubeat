import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

// The message modules import through the "@/" alias, which node does not
// resolve: they are read as text. Each is defineMessages({ tr }, { en }).
function source(path: string): string {
  return readFileSync(new URL(path, import.meta.url), "utf8");
}

/** The messages of a module, by language: every "key": "value" pair of its Turkish and its English half. */
function messages(path: string): { tr: Map<string, string>; en: Map<string, string> } {
  const text = source(path);
  const halves = text.split(/\n {2}\},\n {2}\{\n/);
  assert.equal(halves.length, 2, `${path} is not defineMessages({ tr }, { en })`);
  const pairs = (half: string) =>
    new Map([...half.matchAll(/^ {4}"([^"]+)":\s*"((?:[^"\\]|\\.)*)",?$/gm)].map((m) => [m[1], m[2]] as [string, string]));
  return { tr: pairs(halves[0]), en: pairs(halves[1]) };
}

/** Every explanation code the API gives, as lib/api.ts lists them. */
function explanationCodes(): string[] {
  const block = source("./api.ts").match(/export type ExplanationCode =([\s\S]*?);/);
  assert.ok(block, "ExplanationCode not found in lib/api.ts");
  return [...block[1].matchAll(/\| "([a-z0-9_]+)"/g)].map((m) => m[1]);
}

const monitorPage = messages("./i18n/messages/monitor-page.ts");
const incidents = messages("./i18n/messages/incidents.ts");

test("every explanation code is worded in Turkish and English, on the monitor page and in the incident timeline", () => {
  const codes = explanationCodes();
  assert.ok(codes.includes("blocked_by_scope") && codes.includes("blocked_by_scope_object"), `codes: ${codes}`);
  for (const code of codes) {
    for (const [module, key] of [
      [monitorPage, `monitorPage.why.code.${code}`],
      [incidents, `incident.why.${code}`],
    ] as const) {
      const tr = module.tr.get(key);
      const en = module.en.get(key);
      assert.ok(tr, `${key} has no Turkish`);
      assert.ok(en, `${key} has no English`);
      assert.notEqual(tr, en, `${key} is not translated`);
    }
  }
});

test("the scope's refusal is worded by whose record holds the monitor, with a remedy that fits", () => {
  const user = {
    tr: monitorPage.tr.get("monitorPage.why.code.blocked_by_scope") ?? "",
    en: monitorPage.en.get("monitorPage.why.code.blocked_by_scope") ?? "",
  };
  const object = {
    tr: monitorPage.tr.get("monitorPage.why.code.blocked_by_scope_object") ?? "",
    en: monitorPage.en.get("monitorPage.why.code.blocked_by_scope_object") ?? "",
  };
  // Both say both causes: the scope, and a name that did not resolve.
  for (const text of [user.tr, object.tr]) {
    assert.match(text, /Hedef kapsamı/);
    assert.match(text, /çözülemedi/);
  }
  for (const text of [user.en, object.en]) {
    assert.match(text, /target scope/);
    assert.match(text, /DNS failed to resolve its name/);
  }
  // An administrator only if DNS works, and only for a user's monitor.
  assert.match(user.tr, /DNS arızası değilse monitor'ü bir yönetici kurabilir/);
  assert.match(user.en, /if DNS works, an administrator can set it up/);
  assert.doesNotMatch(object.tr, /yönetici/);
  assert.doesNotMatch(object.en, /administrator/);
  assert.match(object.tr, /uygulamanın kendi host'unda, oturum istemeyen bir sağlık yolunda/);
  assert.match(object.en, /the app's own host at a health path needing no sign-in/);
  // No address in any of them.
  for (const text of [user.tr, user.en, object.tr, object.en]) {
    assert.doesNotMatch(text, /[0-9]|[a-z]\.[a-z]/);
  }
});

test("the reason of a scope's refusal, which only administrators get, is worded in both languages", () => {
  for (const key of [
    "monitorPage.why.scopeReason",
    "monitorPage.why.scopeReason.hint",
    "monitorPage.why.scopeReason.connection",
    "monitorPage.why.scopeReason.resolve_error",
  ]) {
    const tr = monitorPage.tr.get(key);
    const en = monitorPage.en.get(key);
    assert.ok(tr && en && tr !== en, `${key}: ${tr} / ${en}`);
  }
});
