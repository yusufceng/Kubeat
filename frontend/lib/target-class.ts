// Whether a monitor's target is inside the cluster or outside it, as the
// backend decides it (netguard.InCluster): the form uses it to say
// beforehand that an external target has a longer shortest interval. The
// rule is on the name as written; a public name that resolves to a
// private address is external, on purpose.
import type { MonitorType } from "@/lib/api";

const clusterSuffixes = [".svc", ".svc.cluster.local", ".cluster.local"];

/** The host of a target, lower-cased, without port, brackets or trailing dot; "" when there is none. */
export function targetHost(type: MonitorType, target: string): string {
  let host = target.trim();
  switch (type) {
    case "push":
    case "workload":
      return "";
    case "http":
    case "websocket":
      try {
        host = new URL(host).hostname;
      } catch {
        return "";
      }
      break;
    case "tcp":
    case "grpc":
    case "postgres":
    case "mysql":
    case "redis": {
      // host:port, or [v6]:port.
      const m = /^\[([^\]]+)\]:\d+$/.exec(host) ?? /^([^:]+):\d+$/.exec(host);
      if (!m) return "";
      host = m[1];
      break;
    }
  }
  return canonicalHost(host.replace(/^\[|\]$/g, ""));
}

/**
 * The one spelling of a host the backend counts its limits by (store.CanonicalHost):
 * a name lower-cased without its root dot; an address as Go's netip writes it, an
 * IPv4-mapped IPv6 address as the IPv4 address it is, without a zone. Every way of
 * writing one address is one host, else the cap per host would be no cap for a
 * target given as an address.
 */
export function canonicalHost(host: string): string {
  host = host.trim().replace(/\.$/, "").replace(/%.*$/, "");
  const v6 = parseV6(host);
  if (v6) return formatV6(v6);
  return host.toLowerCase();
}

/** The eight 16-bit groups of an IPv6 address, or null when host is not one. */
function parseV6(host: string): number[] | null {
  if (!host.includes(":")) return null;
  let text = host.toLowerCase();
  // A dotted quad in the last 32 bits, as in ::ffff:203.0.113.1.
  const dotted = /(^|:)((?:\d{1,3}\.){3}\d{1,3})$/.exec(text);
  if (dotted) {
    const parts = dotted[2].split(".");
    const quad = parts.map(Number);
    if (quad.some((n) => n > 255) || parts.some((p) => p.length > 1 && p.startsWith("0"))) return null;
    text = text.slice(0, text.length - dotted[2].length) + ((quad[0] << 8) | quad[1]).toString(16) + ":" + ((quad[2] << 8) | quad[3]).toString(16);
  }
  const halves = text.split("::");
  if (halves.length > 2) return null;
  const groups = (part: string): number[] | null => {
    if (part === "") return [];
    const out: number[] = [];
    for (const g of part.split(":")) {
      if (!/^[0-9a-f]{1,4}$/.test(g)) return null;
      out.push(parseInt(g, 16));
    }
    return out;
  };
  const head = groups(halves[0]);
  const tail = halves.length === 2 ? groups(halves[1]) : [];
  if (!head || !tail) return null;
  if (halves.length === 1) return head.length === 8 ? head : null;
  if (head.length + tail.length > 7) return null;
  return [...head, ...new Array<number>(8 - head.length - tail.length).fill(0), ...tail];
}

/** RFC 5952 text of the groups, as Go's netip writes it; an IPv4-mapped address as its IPv4 address. */
function formatV6(groups: number[]): string {
  if (groups.slice(0, 5).every((g) => g === 0) && groups[5] === 0xffff) {
    return [groups[6] >> 8, groups[6] & 0xff, groups[7] >> 8, groups[7] & 0xff].join(".");
  }
  // The longest run of zero groups, two or more, the leftmost of equals, becomes "::".
  let best = { at: -1, len: 0 };
  for (let i = 0; i < 8; ) {
    if (groups[i] !== 0) {
      i++;
      continue;
    }
    let j = i;
    while (j < 8 && groups[j] === 0) j++;
    if (j - i > best.len) best = { at: i, len: j - i };
    i = j;
  }
  const hex = (gs: number[]) => gs.map((g) => g.toString(16)).join(":");
  if (best.len < 2) return hex(groups);
  return hex(groups.slice(0, best.at)) + "::" + hex(groups.slice(best.at + best.len));
}

/** An IPv4 address in the ranges clusters are built from, or loopback/link-local. */
function privateV4(host: string): boolean | null {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(host);
  if (!m) return null;
  const [a, b] = [Number(m[1]), Number(m[2])];
  return a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168) || (a === 100 && b >= 64 && b <= 127) || a === 127 || (a === 169 && b === 254);
}

/** An IPv6 address in fc00::/7, loopback or link-local; null when host is not IPv6. */
function privateV6(host: string): boolean | null {
  if (!host.includes(":")) return null;
  const mapped = /^::ffff:(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})$/i.exec(host);
  if (mapped) return privateV4(mapped[1]) ?? false;
  if (host === "::1") return true;
  const first = host.split(":")[0].toLowerCase();
  return /^f[cd][0-9a-f]{0,2}$/.test(first) || /^fe[89ab][0-9a-f]?$/.test(first);
}

/** Whether the host is in the cluster: a private address, a *.svc or *.cluster.local name, or a name without a dot. */
export function inCluster(host: string): boolean {
  host = canonicalHost(host);
  if (host === "") return false;
  const v4 = privateV4(host);
  if (v4 !== null) return v4;
  const v6 = privateV6(host);
  if (v6 !== null) return v6;
  return !host.includes(".") || clusterSuffixes.some((s) => host.endsWith(s));
}

/** Whether a monitor of the type checks a target outside the cluster. */
export function externalTarget(type: MonitorType, target: string): boolean {
  const host = targetHost(type, target);
  return host !== "" && !inCluster(host);
}
