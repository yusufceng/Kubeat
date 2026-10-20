import assert from "node:assert/strict";
import { test } from "node:test";
import { fold, matchScore } from "./palette-match.ts";

test("folding takes case, accents and every i away", () => {
  assert.equal(fold("İstanbul"), "istanbul");
  assert.equal(fold("SERTİFİKA"), "sertifika");
  assert.equal(fold("Işık"), "isik");
  assert.equal(fold("Çalışan"), "calisan");
});

test("every word must be found", () => {
  assert.ok(matchScore("pay api", ["payments-api:8080", "payments"]) > 0);
  assert.equal(matchScore("pay zzz", ["payments-api:8080", "payments"]), 0);
  assert.equal(matchScore("", ["anything"]), 1);
});

test("a match at the start ranks above one inside a word", () => {
  const start = matchScore("pri", ["pricing:8080", "checkout"]);
  const word = matchScore("pri", ["db-primary", "data"]);
  const inside = matchScore("pri", ["spring-boot", "apps"]);
  assert.ok(start > word, `${start} > ${word}`);
  assert.ok(word > inside, `${word} > ${inside}`);
});

test("the name weighs more than the other fields", () => {
  const byName = matchScore("checkout", ["checkout-web", "checkout"]);
  const byNamespace = matchScore("checkout", ["pricing:8080", "checkout"]);
  assert.ok(byName > byNamespace);
});

test("Turkish letters are found either way", () => {
  assert.ok(matchScore("bakim", ["Bakım"]) > 0);
  assert.ok(matchScore("Durum", ["Durum sayfaları"]) > 0);
  assert.ok(matchScore("sayfalari", ["Durum sayfaları"]) > 0);
});

test("the whole name typed out goes first", () => {
  assert.ok(matchScore("grafana", ["grafana"]) > matchScore("grafana", ["grafana-operator"]));
});
