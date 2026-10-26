import assert from "node:assert/strict";
import { test } from "node:test";
import { canonicalHost, externalTarget, inCluster, targetHost } from "./target-class.ts";

test("every spelling of an address is one host, as the backend's store.CanonicalHost has it", () => {
  assert.equal(canonicalHost("Example.COM."), "example.com");
  assert.equal(canonicalHost("203.0.113.1"), "203.0.113.1");
  assert.equal(canonicalHost("2001:0db8::1"), "2001:db8::1");
  assert.equal(canonicalHost("2001:db8:0::1"), "2001:db8::1");
  assert.equal(canonicalHost("2001:db8:0:0:0:0:0:1"), "2001:db8::1");
  assert.equal(canonicalHost("2001:DB8::1"), "2001:db8::1");
  assert.equal(canonicalHost("::ffff:203.0.113.1"), "203.0.113.1");
  assert.equal(canonicalHost("::ffff:cb00:7101"), "203.0.113.1");
  assert.equal(canonicalHost("::FFFF:127.0.0.1"), "127.0.0.1");
  assert.equal(canonicalHost("fe80::1%eth0"), "fe80::1");
  assert.equal(canonicalHost("::1"), "::1");
  assert.equal(canonicalHost("::"), "::");
  assert.equal(canonicalHost("1:0:0:1:0:0:0:1"), "1:0:0:1::1"); // the longer zero run, not the first
  assert.equal(canonicalHost("2001:db8:0:1:1:1:1:1"), "2001:db8:0:1:1:1:1:1"); // a lone zero group stays
  assert.equal(canonicalHost("0x7f000001"), "0x7f000001"); // not an address: a name, which the server refuses
  assert.equal(canonicalHost("2001:db8::1::2"), "2001:db8::1::2"); // not an address either
  assert.equal(targetHost("http", "http://[2001:0db8::1]/"), "2001:db8::1");
  assert.equal(targetHost("tcp", "[::ffff:203.0.113.1]:443"), "203.0.113.1");
  assert.equal(inCluster("::ffff:0a00:0001"), true);
  assert.equal(inCluster("::ffff:cb00:7101"), false);
});

test("the host of a target, by type", () => {
  assert.equal(targetHost("http", "https://API.Example.com:8443/healthz"), "api.example.com");
  assert.equal(targetHost("websocket", "wss://ws.example.com/socket"), "ws.example.com");
  assert.equal(targetHost("http", "http://[fd00::1]:8080/"), "fd00::1");
  assert.equal(targetHost("tcp", "db.shop.svc:5432"), "db.shop.svc");
  assert.equal(targetHost("tcp", "[fe80::1%eth0]:22"), "fe80::1");
  assert.equal(targetHost("grpc", "Cart.shop.svc:9000"), "cart.shop.svc");
  assert.equal(targetHost("ping", "Example.COM."), "example.com");
  assert.equal(targetHost("dns", "example.com"), "example.com");
  assert.equal(targetHost("push", ""), "");
  assert.equal(targetHost("workload", "shop/deployment/api"), "");
  assert.equal(targetHost("tcp", "example.com"), "");
  assert.equal(targetHost("http", "not a url"), "");
});

test("addresses and names inside the cluster", () => {
  for (const host of ["10.0.0.1", "172.30.0.10", "172.16.0.1", "192.168.1.1", "100.64.0.1", "127.0.0.1", "169.254.169.254"]) {
    assert.equal(inCluster(host), true, host);
  }
  for (const host of ["fd00::1", "fc00::1", "::1", "fe80::1", "::ffff:10.0.0.1"]) {
    assert.equal(inCluster(host), true, host);
  }
  for (const host of ["api", "api.shop.svc", "api.shop.svc.cluster.local", "10-0-0-1.shop.pod.cluster.local", "localhost"]) {
    assert.equal(inCluster(host), true, host);
  }
});

test("everything else is external, a public name resolving privately included", () => {
  for (const host of ["8.8.8.8", "172.32.0.1", "100.128.0.1", "2001:db8::1", "::ffff:8.8.8.8", "example.com", "svc.example.com", "intranet.corp"]) {
    assert.equal(inCluster(host), false, host);
  }
  assert.equal(inCluster(""), false);
});

test("a monitor's target is external when it has a host outside the cluster", () => {
  assert.equal(externalTarget("http", "https://example.com/"), true);
  assert.equal(externalTarget("tcp", "example.com:443"), true);
  assert.equal(externalTarget("http", "http://api.shop.svc/"), false);
  assert.equal(externalTarget("push", ""), false);
  assert.equal(externalTarget("http", "garbage"), false);
});
