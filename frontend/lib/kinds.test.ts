import assert from "node:assert/strict";
import { test } from "node:test";
import { kindShort, objectRef, splitName } from "./kinds.ts";

test("discovered monitors take kubectl's short names", () => {
  assert.equal(kindShort("service/pricing:8080", "http"), "svc");
  assert.equal(kindShort("ingress/api-gateway@api.example.com/v1", "http"), "ing");
  assert.equal(kindShort("route/checkout-web", "http"), "route");
  assert.equal(kindShort("deployment/payments-worker", "tcp"), "deploy");
  assert.equal(kindShort("statefulset/ledger-db", "tcp"), "sts");
  assert.equal(kindShort("daemonset/node-exporter", "tcp"), "ds");
  // A kind this does not know is kept as it is.
  assert.equal(kindShort("gateway/edge", "http"), "gateway");
});

test("monitors entered by hand are named by their check", () => {
  assert.equal(kindShort("", "http"), "http");
  assert.equal(kindShort("", "push"), "push");
  assert.equal(kindShort("", "websocket"), "ws");
  assert.equal(kindShort("", "postgres"), "pg");
  assert.equal(kindShort("", "workload", "shop/deployment/web"), "deploy");
  assert.equal(kindShort("", "workload", "shop/statefulset/db"), "sts");
  assert.equal(kindShort("", "workload", ""), "workload");
});

test("the object of a discovered monitor reads like kubectl", () => {
  assert.equal(objectRef("service/pricing:8080"), "svc/pricing:8080");
  assert.equal(objectRef("route/checkout-web"), "route/checkout-web");
  assert.equal(objectRef(""), null);
});

test("a port or a host is split off a discovered name only", () => {
  assert.deepEqual(splitName("pricing:8080", true), ["pricing", ":8080"]);
  assert.deepEqual(splitName("web@shop.example.com/", true), ["web", "@shop.example.com/"]);
  assert.deepEqual(splitName("checkout-web", true), ["checkout-web", ""]);
  assert.deepEqual(splitName("Partner SFTP: backup", false), ["Partner SFTP: backup", ""]);
});
