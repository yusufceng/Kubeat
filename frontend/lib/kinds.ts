/*
 * Cluster objects named the way kubectl names them, for the mono kind tags
 * of the monitor list, the cluster map and the command palette: the short
 * names of `kubectl get` (svc, ing, deploy, sts, ds; a Route has none
 * shorter). A monitor entered by hand is named by what it checks (http,
 * tcp, push, ...), and a workload monitor by the kind it watches.
 *
 * No imports, so that the tests run it as it is.
 */

const shortKinds: Record<string, string> = {
  service: "svc",
  ingress: "ing",
  route: "route",
  deployment: "deploy",
  statefulset: "sts",
  daemonset: "ds",
};

const shortTypes: Record<string, string> = {
  websocket: "ws",
  postgres: "pg",
};

/** The kind of a monitor as a short tag: "service/pricing:8080" is "svc", a TCP check entered by hand "tcp". */
export function kindShort(source: string, type: string, target = ""): string {
  const slash = source.indexOf("/");
  if (slash > 0) {
    const kind = source.slice(0, slash).toLowerCase();
    return shortKinds[kind] ?? kind;
  }
  if (type === "workload") {
    // The target of a workload check is namespace/kind/name.
    const kind = (target.split("/")[1] ?? "").toLowerCase();
    return shortKinds[kind] ?? type;
  }
  return shortTypes[type] ?? type;
}

/** The object a discovered monitor follows, as kubectl writes it: "svc/pricing:8080"; null for one entered by hand. */
export function objectRef(source: string): string | null {
  const slash = source.indexOf("/");
  if (slash <= 0) return null;
  const kind = source.slice(0, slash).toLowerCase();
  return `${shortKinds[kind] ?? kind}/${source.slice(slash + 1)}`;
}

/**
 * A discovered monitor's name split where the object's own name ends:
 * "pricing:8080" is ["pricing", ":8080"], "web@shop.example.com/" is
 * ["web", "@shop.example.com/"]. The rest, a port or an ingress's host and
 * path, is drawn muted. A name entered by hand is kept whole.
 */
export function splitName(name: string, discovered: boolean): [string, string] {
  if (!discovered) return [name, ""];
  const cut = name.search(/[:@]/);
  return cut > 0 ? [name.slice(0, cut), name.slice(cut)] : [name, ""];
}
