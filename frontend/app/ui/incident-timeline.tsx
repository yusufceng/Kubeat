"use client";

import type { CheckResult, Explanation, IncidentNotification, IncidentPage, TimelineItem } from "@/lib/api";
import { channelTypes } from "@/lib/channel-types";
import { span } from "@/lib/format";
import { byDay, checkError, type TimelineRow, timelineRows } from "@/lib/incident-timeline";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { useMounted } from "@/lib/use-mounted";
import { BellIcon } from "@/app/ui/icons";
import {
  CheckIcon,
  ChevronDownIcon,
  ClusterIcon,
  CrossIcon,
  FlagIcon,
  GapIcon,
  NoteIcon,
  ReminderIcon,
} from "@/app/ui/incident-icons";
import { NoteBody, NoteComposer } from "@/app/ui/incident-notes";
import { Clock, ClockRange, DayLabel, dayKey } from "@/app/ui/incident-time";
import { StatusGlyph } from "@/app/ui/status-glyph";

/** What a failure most likely means, in a few words in the viewer's language; the API's English for a code this version does not know. */
function whyText(e: Explanation, t: T): string | null {
  const key = `incident.why.${e.code}`;
  const text = t(key, e.params);
  return text !== key ? text : e.text || null;
}

/** The word for a value the API may add to: its translation, or the value itself for one this version does not know. */
function word(t: T, key: string, value: string): string {
  const text = t(key);
  return text === key ? value : text;
}

/**
 * A failed check in one line: what it most likely means, the error as the
 * check gave it, where it was sent, the pod that failed. What does not fit
 * ends in an ellipsis; all of it is in the title.
 */
function CheckSummary({ check, t }: { check: CheckResult; t: T }) {
  const d = check.diagnostics ?? null;
  const why = d?.explanation ? whyText(d.explanation, t) : null;
  const error = checkError(check);
  const parts = [why, error, d?.remote_addr, d?.pod].filter(Boolean);
  if (parts.length === 0) return null;
  return (
    <p className="inc-tl-detail inc-oneline" title={parts.join(" · ")}>
      {why && <span className="inc-why">{why}</span>}
      {error && <span className="inc-err">{error}</span>}
      {d?.remote_addr && <span className="inc-err">→ {d.remote_addr}</span>}
      {d?.pod && <span className="inc-err">pod/{d.pod}</span>}
    </p>
  );
}

/** One line of the timeline: when, a mark for what kind of thing it was, and what it was. */
function Row({
  at,
  mark,
  tone,
  children,
  id,
}: {
  at: string;
  mark: React.ReactNode;
  /** Colours the mark: the state it stands for, or nothing for the plain ones. */
  tone?: "down" | "up" | "warn" | "maint";
  children: React.ReactNode;
  id?: string;
}) {
  return (
    <li className="inc-tl-row" id={id}>
      <span className="inc-tl-time">
        <Clock iso={at} />
      </span>
      <span className="inc-tl-mark" data-tone={tone}>
        {mark}
      </span>
      <div className="inc-tl-body">{children}</div>
    </li>
  );
}

/** A notification's channel type in words: "Slack", "Webhook (JSON)". */
const channelLabel = (n: IncidentNotification, t: T) => (channelTypes[n.channel_type] ? t(channelTypes[n.channel_type].label) : n.channel_type);

function NotificationRow({ at, n, t }: { at: string; n: IncidentNotification; t: T }) {
  const waited = Date.parse(at) - Date.parse(n.event_at);
  return (
    <Row at={at} mark={<BellIcon width={14} height={14} />} tone={n.delivered ? undefined : "down"}>
      <p className="inc-tl-title">
        <span>{word(t, `incident.tl.notify.${n.event}`, n.event)}</span>
        <span className="inc-sep" aria-hidden>
          ·
        </span>
        <span className="inc-channel" title={n.channel_name}>
          {n.channel_name}
        </span>
        <span className="inc-muted">{channelLabel(n, t)}</span>
        {n.delivered ? (
          <span className="inc-result text-up-ink">
            <CheckIcon width={14} height={14} />
            {t("incident.tl.delivered")}
          </span>
        ) : (
          <span className="inc-result text-down-ink">
            <CrossIcon width={14} height={14} />
            {t("incident.tl.notDelivered")}
          </span>
        )}
      </p>
      {(n.error || !n.delivered || n.grouped || n.channel_deleted || waited >= 60_000) && (
        <p className="inc-tl-detail">
          {n.error ? (
            <span className="inc-err text-down-ink" title={n.error}>
              {n.error}
            </span>
          ) : (
            !n.delivered && <span>{t("incident.tl.errorHidden")}</span>
          )}
          {n.grouped && <span>{t("incident.tl.grouped", { count: n.group_size })}</span>}
          {waited >= 60_000 && <span>{t("incident.tl.waited", { duration: span(waited, t) })}</span>}
          {n.channel_deleted && <span>{t("incident.tl.channelDeleted")}</span>}
        </p>
      )}
    </Row>
  );
}

/** Failed checks that failed the same way, in a row: one line, and the checks themselves on request. */
function ChecksRow({ checks, first, t }: { checks: CheckResult[]; first: boolean; t: T }) {
  const latest = checks[checks.length - 1];
  const title = first ? t("incident.tl.first") : checks.length === 1 ? t("incident.tl.failed") : t("incident.tl.failedRun", { count: checks.length });
  if (checks.length === 1) {
    return (
      <Row at={checks[0].checked_at} mark={<StatusGlyph kind="down" />} tone="down">
        <p className="inc-tl-title">
          <span className={first ? "font-semibold" : undefined}>{title}</span>
        </p>
        <CheckSummary check={latest} t={t} />
      </Row>
    );
  }
  return (
    <Row at={checks[0].checked_at} mark={<StatusGlyph kind="down" />} tone="down">
      <details className="inc-run">
        <summary>
          <span className="inc-tl-title">
            {/* The chevron keeps to the count it opens, wherever the line breaks. */}
            <span className="inc-run-label">
              {title}
              <span className="inc-run-toggle">
                <ChevronDownIcon width={14} height={14} />
                <span className="sr-only">{t("incident.tl.failedRun.show")}</span>
              </span>
            </span>
            <span className="inc-muted">
              <ClockRange from={checks[0].checked_at} to={latest.checked_at} />
            </span>
          </span>
          <CheckSummary check={latest} t={t} />
        </summary>
        <ol className="inc-run-list">
          {checks.map((c) => (
            <li key={c.checked_at}>
              <Clock iso={c.checked_at} />
              <span className="inc-run-ms">{c.latency_ms.toLocaleString(t.intl)} ms</span>
              <span className="inc-err" title={checkError(c)}>
                {checkError(c)}
              </span>
            </li>
          ))}
        </ol>
      </details>
    </Row>
  );
}

function ItemRow({
  item,
  incident,
  t,
}: {
  item: Exclude<TimelineItem, { kind: "check_failed" }>;
  incident: IncidentPage["incident"];
  t: T;
}) {
  const cert = incident.kind === "cert";
  switch (item.kind) {
    case "checks_omitted":
    case "notifications_omitted":
      return (
        <Row at={item.at} mark={<GapIcon width={14} height={14} />}>
          <p className="inc-tl-title inc-muted-title">
            <span>
              {t.n(
                item.kind === "checks_omitted" ? "incident.tl.checksOmitted" : "incident.tl.notificationsOmitted",
                item.omitted.count,
                { count: item.omitted.count.toLocaleString(t.intl) },
              )}
            </span>
            <span className="inc-muted">
              <ClockRange from={item.omitted.from} to={item.omitted.to} />
            </span>
          </p>
        </Row>
      );
    case "opened":
      return (
        <Row at={item.at} mark={<FlagIcon width={14} height={14} />} tone={cert ? "warn" : "down"}>
          <p className="inc-tl-title">
            <span className="font-semibold">{t(cert ? "incident.tl.opened.cert" : "incident.tl.opened.down")}</span>
            {!cert && item.opened.failure_threshold > 0 && (
              <span className="inc-muted">{t.n("incident.tl.opened.threshold", item.opened.failure_threshold)}</span>
            )}
          </p>
          {item.opened.cause && (
            <p className="inc-tl-detail inc-oneline" title={item.opened.cause}>
              <span className="inc-err">{item.opened.cause}</span>
            </p>
          )}
        </Row>
      );
    case "notification":
      return <NotificationRow at={item.at} n={item.notification} t={t} />;
    case "reminder":
      return (
        <Row at={item.at} mark={<ReminderIcon width={14} height={14} />}>
          <p className="inc-tl-title">
            <span>{t("incident.tl.reminder")}</span>
            <span className="inc-muted">
              {t("incident.tl.reminder.detail", { duration: span(item.reminder.down_for_seconds * 1000, t) })}
            </span>
          </p>
        </Row>
      );
    case "note":
      return (
        <Row at={item.at} mark={<NoteIcon width={14} height={14} />} id={`note-row-${item.note.id}`}>
          <NoteBody note={item.note} incidentId={incident.id} />
        </Row>
      );
    case "cluster_event": {
      const warning = item.event.type === "Warning";
      return (
        <Row at={item.at} mark={<ClusterIcon width={14} height={14} />} tone={warning ? "warn" : undefined}>
          <p className="inc-tl-title">
            <span className={`inc-mono font-medium ${warning ? "text-warn-ink" : ""}`}>{item.event.reason}</span>
            <span className="inc-mono inc-muted inc-object" title={item.event.object}>
              {item.event.object}
            </span>
            {item.event.count > 1 && <span className="inc-muted">{t("incident.tl.event.count", { count: item.event.count })}</span>}
            <span className="sr-only">{word(t, `incident.tl.event.${item.event.type}`, item.event.type)}</span>
          </p>
          {item.event.message && <p className="inc-tl-detail inc-event-message">{item.event.message}</p>}
        </Row>
      );
    }
    case "maintenance": {
      const m = item.maintenance;
      const scope =
        m.monitor_id !== null
          ? t("incident.tl.maintenance.scope.monitor")
          : m.namespace
            ? t("incident.tl.maintenance.scope.namespace", { namespace: m.namespace })
            : t("incident.tl.maintenance.scope.all");
      return (
        <Row at={item.at} mark={<StatusGlyph kind="maint" />} tone="maint">
          <p className="inc-tl-title">
            <span>{t("incident.tl.maintenance")}</span>
            <span className="inc-sep" aria-hidden>
              ·
            </span>
            <span className="font-[530]">{m.name}</span>
          </p>
          <p className="inc-tl-detail">
            <ClockRange from={m.starts_at} to={m.ends_at} />
            {m.repeat && <span>{word(t, `incident.tl.maintenance.repeat.${m.repeat}`, m.repeat)}</span>}
            <span>{scope}</span>
          </p>
        </Row>
      );
    }
    case "resolved":
      return (
        <Row at={item.at} mark={<StatusGlyph kind="up" />} tone="up">
          <p className="inc-tl-title">
            <span className="font-semibold text-up-ink">{t(cert ? "incident.tl.resolved.cert" : "incident.tl.resolved.down")}</span>
            <span className="inc-muted">
              {t("incident.tl.resolved.detail", { duration: span(item.resolved.duration_seconds * 1000, t) })}
            </span>
          </p>
        </Row>
      );
  }
}

function RowFor({ row, incident, t }: { row: TimelineRow; incident: IncidentPage["incident"]; t: T }) {
  if (row.type === "first") return <ChecksRow checks={[row.check]} first t={t} />;
  if (row.type === "checks") return <ChecksRow checks={row.checks} first={false} t={t} />;
  return <ItemRow item={row.item} incident={incident} t={t} />;
}

/**
 * The incident's story, oldest first, under a heading for each day: its
 * failed checks (the same failure told once for a run of them), when it
 * opened, every notification and whether it got through, reminders, notes,
 * what the cluster said, maintenance, and how it ended. Notes are added at
 * its end by those who may.
 */
export function IncidentTimeline({
  incident,
  timeline,
  canNote,
  notices,
}: {
  incident: IncidentPage["incident"];
  timeline: TimelineItem[];
  canNote: boolean;
  /** Said under the heading: why the cluster's events are missing or cut. */
  notices: React.ReactNode;
}) {
  const t = useT();
  const mounted = useMounted();
  const dayOf = (iso: string) => dayKey(iso, mounted);
  const days = byDay(timelineRows(timeline, incident.started_at, dayOf), dayOf);
  return (
    <section className="panel inc-tl" aria-labelledby="timeline-title">
      <header className="inc-tl-head">
        <h2 id="timeline-title" className="type-heading">
          {t("incident.timeline")}
        </h2>
        <span className="inc-hint">{t("incident.timeline.hint")}</span>
      </header>
      {notices}
      {days.length === 0 && <p className="inc-tl-empty">{t("incident.timeline.empty")}</p>}
      {days.map((day) => (
        <section key={day.day} className="inc-tl-day" aria-labelledby={`day-${day.day}`}>
          <h3 id={`day-${day.day}`} className="inc-tl-date">
            <DayLabel iso={day.at} />
          </h3>
          <ol className="inc-tl-list">
            {day.rows.map((row) => (
              <RowFor key={row.key} row={row} incident={incident} t={t} />
            ))}
          </ol>
        </section>
      ))}
      {canNote ? (
        <div className="inc-tl-compose">
          <span className="inc-tl-mark" aria-hidden>
            <NoteIcon width={14} height={14} />
          </span>
          <NoteComposer incidentId={incident.id} />
        </div>
      ) : (
        <p className="inc-tl-foot">{t("incident.note.readOnly")}</p>
      )}
    </section>
  );
}
