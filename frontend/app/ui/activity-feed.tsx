"use client";

import Link from "next/link";
import { span } from "@/lib/format";
import type { ActivityEvent } from "@/lib/fleet";
import { emptyFilter, filterHref } from "@/lib/incident-filter";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { useMounted } from "@/lib/use-mounted";
import { ArrowIcon } from "@/app/ui/fleet-icons";
import { LiveDuration } from "@/app/ui/live-duration";
import { type GlyphKind, StatusGlyph } from "@/app/ui/status-glyph";

const glyphs: Record<ActivityEvent["kind"], GlyphKind> = {
  down: "down",
  recovered: "up",
  cert: "warn",
  renewed: "up",
  ongoing: "down",
  flapping: "warn",
};

/** The sentence of an event around its monitor's name, which is a link. */
function sentence(e: ActivityEvent, t: T): [string, string] {
  const text =
    e.kind === "down"
      ? t("monitorsList.activity.down")
      : e.kind === "recovered"
        ? t("monitorsList.activity.recovered", { lasted: span(e.lasted ?? 0, t) })
        : e.kind === "cert"
          ? t("monitorsList.activity.cert")
          : e.kind === "renewed"
            ? t("monitorsList.activity.renewed")
            : e.kind === "ongoing"
              ? t("monitorsList.activity.ongoing")
              : t.n("monitorsList.activity.flapping", e.run?.count ?? 0);
  const [before, after = ""] = text.split("{name}");
  return [before, after];
}

/** Where an event leads: its incident's page, or for a run, the list of its outages. */
function eventHref(e: ActivityEvent): string {
  if (!e.run) return `/incidents/${e.incident}`;
  return filterHref(emptyFilter, { monitorId: e.monitorId, from: e.run.from, to: e.run.to });
}

/**
 * The words after the name, a link to what happened; the space before
 * them stays outside it. An ongoing outage's words hold its live duration.
 */
function What({ e, text, t }: { e: ActivityEvent; text: string; t: T }) {
  const space = /^\s*/.exec(text)![0];
  const words = text.slice(space.length);
  const [before, after] = words.split("{duration}");
  return (
    <>
      {space}
      <Link href={eventHref(e)} className="fl-event-what" title={e.run ? undefined : t("incident.number", { id: e.incident })}>
        {before}
        {after !== undefined && (
          <>
            <LiveDuration since={e.at} />
            {after}
          </>
        )}
      </Link>
    </>
  );
}

/**
 * What happened lately: first the outages still going on, then the rest,
 * newest first, under a header per day: "Today", "Yesterday", then the
 * weekday and date. Each line is the time on a 24-hour clock, a glyph and
 * a sentence: "pricing went down · HTTP 503", "cart-service recovered
 * after 3m", "search-api went down 14 times · 10s–30s". The name opens the
 * monitor, the words after it the incident (or a run's outages).
 *
 * Days and times are the viewer's; the server cannot know their zone, so
 * until the browser takes over the days are UTC's and the times are held
 * back. asOf is when the data was read, which "today" is measured from.
 */
export function ActivityFeed({ ongoing, events, asOf }: { ongoing: ActivityEvent[]; events: ActivityEvent[]; asOf: number }) {
  const t = useT();
  const mounted = useMounted();
  const zone = mounted ? undefined : "UTC";
  const dayOf = (ms: number) => new Date(ms).toLocaleDateString("en-CA", { timeZone: zone });
  const today = dayOf(asOf);
  const yesterday = dayOf(asOf - 86_400_000);
  const thisYear = new Date(asOf).getFullYear();

  const days: { key: string; label: string; events: ActivityEvent[] }[] = [];
  if (ongoing.length > 0) days.push({ key: "ongoing", label: t("monitorsList.overview.open.label"), events: ongoing });
  for (const e of events) {
    const at = Date.parse(e.at);
    const key = dayOf(at);
    let day = days.at(-1);
    if (!day || day.key !== key) {
      const date = new Date(at);
      const label =
        key === today
          ? t("time.today")
          : key === yesterday
            ? t("time.yesterday")
            : date.toLocaleDateString(t.intl, {
                weekday: "short",
                day: "numeric",
                month: "short",
                ...(date.getFullYear() !== thisYear && { year: "numeric" }),
                timeZone: zone,
              });
      day = { key, label, events: [] };
      days.push(day);
    }
    day.events.push(e);
  }

  return (
    <section className="panel fl-activity" aria-labelledby="fl-activity-title">
      <header className="fl-panel-head">
        <h2 id="fl-activity-title" className="type-heading">
          {t("monitorsList.activity.title")}
        </h2>
        <Link href="/incidents" className="btn btn-ghost btn-sm ml-auto">
          {t("monitorsList.activity.all")}
          <ArrowIcon width={15} height={15} />
        </Link>
      </header>
      {days.length === 0 ? (
        <p className="px-4 pt-2 pb-5 text-sm text-ink-3">{t("monitorsList.overview.recent.empty")}</p>
      ) : (
        <ol className="fl-feed">
          {days.map((day) => (
            <li key={day.key}>
              <h3 className="fl-day">{day.label}</h3>
              <ol>
                {day.events.map((e) => {
                  const [before, after] = sentence(e, t);
                  const run = e.run && (e.run.shortest === e.run.longest ? span(e.run.longest, t) : `${span(e.run.shortest, t)}–${span(e.run.longest, t)}`);
                  return (
                    <li key={e.key} className="fl-event">
                      <time
                        dateTime={e.at}
                        className="fl-event-time"
                        title={mounted ? new Date(e.at).toLocaleString(t.intl, { hourCycle: "h23" }) : undefined}
                        style={mounted ? undefined : { visibility: "hidden" }}
                      >
                        {new Date(e.at).toLocaleTimeString(t.intl, {
                          hour: "2-digit",
                          minute: "2-digit",
                          hourCycle: "h23",
                          timeZone: zone,
                        })}
                      </time>
                      <StatusGlyph kind={glyphs[e.kind]} live={e.kind === "ongoing"} className="fl-event-glyph" />
                      <p className="fl-event-text" title={e.cause || undefined}>
                        {before}
                        <Link href={`/monitors/${e.monitorId}`} className="fl-event-name">
                          {e.name}
                        </Link>
                        <What e={e} text={after} t={t} />
                        {run && <span className="text-ink-3"> · {run}</span>}
                        {e.cause && <span className="fl-event-cause"> {e.cause}</span>}
                      </p>
                    </li>
                  );
                })}
              </ol>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}
