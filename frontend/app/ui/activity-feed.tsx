"use client";

import Link from "next/link";
import { span } from "@/lib/format";
import type { ActivityEvent } from "@/lib/fleet";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { useMounted } from "@/lib/use-mounted";
import { ArrowIcon } from "@/app/ui/fleet-icons";
import { type GlyphKind, StatusGlyph } from "@/app/ui/status-glyph";

const glyphs: Record<ActivityEvent["kind"], GlyphKind> = { down: "down", recovered: "up", cert: "warn", renewed: "up" };

/** The sentence of an event around its monitor's name, which is a link. */
function sentence(e: ActivityEvent, t: T): [string, string] {
  const text =
    e.kind === "down"
      ? t("monitorsList.activity.down")
      : e.kind === "recovered"
        ? t("monitorsList.activity.recovered", { lasted: span(e.lasted ?? 0, t) })
        : e.kind === "cert"
          ? t("monitorsList.activity.cert")
          : t("monitorsList.activity.renewed");
  const [before, after = ""] = text.split("{name}");
  return [before, after];
}

/**
 * What happened lately, newest first, under a header per day: "Today",
 * "Yesterday", then the weekday and date. Each line is the time on a
 * 24-hour clock, a glyph and a sentence: "pricing went down · HTTP 503",
 * "cart-service recovered after 3 min".
 *
 * Days and times are the viewer's; the server cannot know their zone, so
 * until the browser takes over the days are UTC's and the times are held
 * back. asOf is when the data was read, which "today" is measured from.
 */
export function ActivityFeed({ events, asOf }: { events: ActivityEvent[]; asOf: number }) {
  const t = useT();
  const mounted = useMounted();
  const zone = mounted ? undefined : "UTC";
  const dayOf = (ms: number) => new Date(ms).toLocaleDateString("en-CA", { timeZone: zone });
  const today = dayOf(asOf);
  const yesterday = dayOf(asOf - 86_400_000);
  const thisYear = new Date(asOf).getFullYear();

  const days: { key: string; label: string; events: ActivityEvent[] }[] = [];
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
      {events.length === 0 ? (
        <p className="px-4 pt-2 pb-5 text-sm text-ink-3">{t("monitorsList.overview.recent.empty")}</p>
      ) : (
        <ol className="fl-feed">
          {days.map((day) => (
            <li key={day.key}>
              <h3 className="fl-day">{day.label}</h3>
              <ol>
                {day.events.map((e) => {
                  const [before, after] = sentence(e, t);
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
                      <StatusGlyph kind={glyphs[e.kind]} className="fl-event-glyph" />
                      <p className="fl-event-text" title={e.cause || undefined}>
                        {before}
                        <Link href={`/monitors/${e.monitorId}`} className="fl-event-name">
                          {e.name}
                        </Link>
                        {after}
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
