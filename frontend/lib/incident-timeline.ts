/*
 * What the incident page works out from its timeline: rows to draw, in
 * which failed checks that say the same thing are told once, the days the
 * rows fall on, and the figures of the notifications. No imports but types,
 * so that the tests run it as it is.
 */
import type { CheckResult, IncidentNotification, TimelineItem } from "@/lib/api";

/** The longest note the backend keeps, in characters (API revision 25). */
export const NOTE_MAX_LENGTH = 4000;

export type TimelineRow =
  /** Any item but a failed check. */
  | { type: "item"; key: string; at: string; item: Exclude<TimelineItem, { kind: "check_failed" }> }
  /** The failed check the incident started with. */
  | { type: "first"; key: string; at: string; check: CheckResult }
  /** Failed checks in a row that failed the same way; one or more, oldest first. */
  | { type: "checks"; key: string; at: string; checks: CheckResult[] };

/** A check's error as it reads, with its status code when the error does not say it. */
export function checkError(c: Pick<CheckResult, "status_code" | "error" | "detail">): string {
  const code = c.status_code ? `HTTP ${c.status_code}` : null;
  const error = c.error ?? c.detail;
  if (!error) return code ?? "";
  return code && !error.includes(String(c.status_code)) ? `${code} · ${error}` : error;
}

/**
 * How a check failed, regardless of the figures in its error (a duration
 * that grows, an address's port): checks with the same signature are told
 * as one row.
 */
export function checkSignature(c: CheckResult): string {
  const code = c.diagnostics?.explanation?.code ?? "";
  return `${code}|${c.status_code ?? ""}|${checkError(c).replace(/\d+/g, "#")}`;
}

/** A key for an item that stays the same when the page is read again. */
function itemKey(item: TimelineItem): string {
  switch (item.kind) {
    case "note":
      return `note-${item.note.id}`;
    case "notification":
      return `notification-${item.at}-${item.notification.channel_id}-${item.notification.event}`;
    case "cluster_event":
      return `event-${item.at}-${item.event.reason}-${item.event.object}`;
    case "maintenance":
      return `maintenance-${item.maintenance.id}-${item.at}`;
    case "checks_omitted":
    case "notifications_omitted":
      // The gap moves along as the incident goes on; it is one per kind.
      return item.kind;
    default:
      return `${item.kind}-${item.at}`;
  }
}

/**
 * The timeline as rows, oldest first. The first failed check stands alone;
 * after it, failed checks in a row that failed the same way on the same day
 * (dayOf names the day a moment falls on, in the viewer's zone) are
 * gathered into one row, so that an hour of the same error does not bury
 * the notifications, notes and cluster events between them.
 */
export function timelineRows(items: TimelineItem[], startedAt: string, dayOf: (iso: string) => string): TimelineRow[] {
  const rows: TimelineRow[] = [];
  const seen = new Map<string, number>();
  const unique = (key: string) => {
    const n = seen.get(key) ?? 0;
    seen.set(key, n + 1);
    return n === 0 ? key : `${key}~${n}`;
  };
  const started = Date.parse(startedAt);
  let firstTaken = false;
  for (const item of items) {
    if (item.kind !== "check_failed") {
      rows.push({ type: "item", key: unique(itemKey(item)), at: item.at, item });
      continue;
    }
    const check = item.check;
    if (!firstTaken && Date.parse(check.checked_at) === started) {
      firstTaken = true;
      rows.push({ type: "first", key: unique(`first-${check.checked_at}`), at: item.at, check });
      continue;
    }
    const last = rows.at(-1);
    if (
      last?.type === "checks" &&
      checkSignature(last.checks[0]) === checkSignature(check) &&
      dayOf(last.at) === dayOf(item.at)
    ) {
      last.checks.push(check);
      continue;
    }
    rows.push({ type: "checks", key: unique(`checks-${check.checked_at}`), at: item.at, checks: [check] });
  }
  return rows;
}

/** Rows by the day they fall on, in order. */
export function byDay(rows: TimelineRow[], dayOf: (iso: string) => string): { day: string; at: string; rows: TimelineRow[] }[] {
  const days: { day: string; at: string; rows: TimelineRow[] }[] = [];
  for (const row of rows) {
    const day = dayOf(row.at);
    const last = days.at(-1);
    if (last?.day === day) last.rows.push(row);
    else days.push({ day, at: row.at, rows: [row] });
  }
  return days;
}

/**
 * The notifications of a timeline: how many attempts there were, how many
 * of them did not get through, and how many the timeline leaves out (whose
 * outcome it does not tell).
 */
export function deliveries(items: TimelineItem[]): { attempts: number; failed: number; omitted: number; channels: number } {
  const shown: IncidentNotification[] = [];
  let omitted = 0;
  for (const item of items) {
    if (item.kind === "notification") shown.push(item.notification);
    if (item.kind === "notifications_omitted") omitted += item.omitted.count;
  }
  return {
    attempts: shown.length + omitted,
    failed: shown.filter((n) => !n.delivered).length,
    omitted,
    channels: new Set(shown.map((n) => n.channel_id)).size,
  };
}

/** kubectl's short names of the kinds Kubeat discovers. */
const shortKinds: Record<string, string> = {
  service: "svc",
  route: "route",
  ingress: "ing",
  deployment: "deploy",
  statefulset: "sts",
  daemonset: "ds",
};

/** A check type as a short lowercase word, for a mono tag beside a name: "http", "tcp", "ws". */
export function typeWord(type: string): string {
  return type === "websocket" ? "ws" : type;
}

/** A discovered object as kubectl names it: "service/pricing:8080" is "svc/pricing:8080"; null for a monitor entered by hand. */
export function kubectlName(source: string): string | null {
  const slash = source.indexOf("/");
  if (slash <= 0) return null;
  const kind = source.slice(0, slash);
  return `${shortKinds[kind] ?? kind}/${source.slice(slash + 1)}`;
}
