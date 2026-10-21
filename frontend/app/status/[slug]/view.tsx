import Link from "next/link";
import type { StatusPageView, StatusPageViewMonitor } from "@/lib/api";
import { span, uptime } from "@/lib/format";
import type { T } from "@/lib/i18n/translate";
import { inProgress } from "@/lib/status-page";
import { BeatStrip } from "@/app/ui/beat-strip";
import { LiveDuration } from "@/app/ui/live-duration";
import { AnnounceIcon } from "@/app/ui/page-icons";
import { glyphTone, StatusGlyph } from "@/app/ui/status-glyph";
import { IncidentWhen, MomentInSentence } from "./incident-when";
import { allMonitors, dayBeats, NARROW_DAYS, pageStates, StateWords, UptimeInline, UptimeWindows, windowsText } from "./shared";
import { UpdatesTimeline } from "./updates";
import { WhenRange } from "./when";

/** Names of services, joined as a sentence would ("A, B and C"), or a count when there are many. */
function serviceNames(names: string[], t: T): string {
  if (names.length > 3) return t.n("statusPage.summary.many", names.length);
  return new Intl.ListFormat(t.intl, { type: "conjunction" }).format(names);
}

/** A sentence with a moment in it: the moment is a component, so the sentence is split around {time}. */
function withTime(sentence: string, iso: string) {
  const [before, after] = sentence.split("{time}");
  return (
    <>
      {before}
      <MomentInSentence iso={iso} />
      {after}
    </>
  );
}

/** One line under the page's state: what is down since when, what is in maintenance, or when the last outage was. */
function Summary({ view, t }: { view: StatusPageView; t: T }) {
  const monitors = allMonitors(view);
  const down = monitors.filter((m) => m.state === "down");
  const maintenance = monitors.filter((m) => m.state === "maintenance");
  const others = monitors.some((m) => m.state === "up");

  if (down.length > 0) {
    // The outage began when the first of the services still down went down.
    const since = view.incidents
      .filter((i) => !i.resolved_at && down.some((m) => m.id === i.monitor_id))
      .map((i) => i.started_at)
      .sort()[0];
    const names = serviceNames(
      down.map((m) => m.name),
      t,
    );
    return (
      <>
        {since ? withTime(t("statusPage.summary.downSince", { names }), since) : t("statusPage.summary.down", { names })}
        {others && ` · ${t("statusPage.summary.others")}`}
      </>
    );
  }
  if (view.state === "maintenance" && maintenance.length > 0) {
    return (
      <>
        {t("statusPage.summary.maintenance", {
          names: serviceNames(
            maintenance.map((m) => m.name),
            t,
          ),
        })}
        {others && ` · ${t("statusPage.summary.others")}`}
      </>
    );
  }
  const services = t.n("statusPage.summary.services", monitors.length);
  // The incidents come newest first.
  const last = view.incidents[0];
  return (
    <>
      {services} · {last ? withTime(t("statusPage.summary.lastOutage"), last.started_at) : t("statusPage.summary.noOutages")}
    </>
  );
}

/** The state of the page in a sentence, over a soft light of its color, with its uptime over each window. */
export function Hero({ view, t }: { view: StatusPageView; t: T }) {
  const state = pageStates[view.state];
  return (
    <section role="status" className="panel sp-hero">
      <span aria-hidden className="sp-aura" style={{ "--aura": glyphTone[state.glyph].color } as React.CSSProperties} />
      <div className="sp-hero-main">
        <StatusGlyph kind={state.glyph} size={16} live={state.glyph === "down"} />
        <div className="min-w-0">
          <h2 className="sp-state">{t(state.title)}</h2>
          <p className="sp-summary">
            <Summary view={view} t={t} />
          </p>
        </div>
      </div>
      {view.uptime.some((w) => w.uptime_percent !== null) && <UptimeWindows windows={view.uptime} label={t("statusPage.windows.aria")} t={t} className="sp-hero-windows" />}
    </section>
  );
}

/** The pinned notice at the top of the page. */
export function Announcement({ text, t }: { text: string; t: T }) {
  return (
    <section aria-labelledby="sp-announcement" className="panel sp-announce">
      <span className="pg-callout-icon" aria-hidden>
        <AnnounceIcon />
      </span>
      <div className="min-w-0">
        <h2 id="sp-announcement" className="sp-announce-title">
          {t("statusPage.announcement")}
        </h2>
        <p className="sp-announce-text">{text}</p>
      </div>
    </section>
  );
}

/** A service's name, a link to its own page where the page gives one. */
function ServiceName({ monitor, view, className }: { monitor: StatusPageViewMonitor; view: StatusPageView; className: string }) {
  return (
    <h3 className={className} title={monitor.name}>
      {view.detail_pages ? (
        <Link href={`/status/${encodeURIComponent(view.slug)}/${monitor.id}`} className="sp-service-link">
          {monitor.name}
        </Link>
      ) : (
        monitor.name
      )}
    </h3>
  );
}

/** One service: its name and state, its days as a strip, today on the right, and its uptime over each window. */
function Service({ monitor, view, t }: { monitor: StatusPageViewMonitor; view: StatusPageView; t: T }) {
  // In words for the public, where the app writes "90d".
  const daysAgo = (days: number) => t.n("statusPage.daysAgo", days);
  return (
    <li className="sp-service">
      <div className="sp-service-head">
        <ServiceName monitor={monitor} view={view} className="sp-service-name" />
        <StateWords state={monitor.state} t={t} />
      </div>
      <BeatStrip
        beats={dayBeats(monitor.history, new Date(view.from).getTime(), t)}
        slots={view.days}
        narrowSlots={NARROW_DAYS}
        label={t.n("statusPage.history.aria", monitor.history.length)}
      />
      <div className="sp-axis">
        <span className="max-sm:hidden">{daysAgo(view.days)}</span>
        <span className="sm:hidden">{daysAgo(Math.min(view.days, NARROW_DAYS))}</span>
        <UptimeInline windows={monitor.uptime} t={t} />
        <span>{t("statusPage.today")}</span>
      </div>
    </li>
  );
}

/** A service on one line, for a page that shows many: name, a low strip, the uptime over the page's days, the state. */
function CompactService({ monitor, view, t }: { monitor: StatusPageViewMonitor; view: StatusPageView; t: T }) {
  const longest = monitor.uptime[monitor.uptime.length - 1];
  return (
    <li className="sp-service sp-service-compact">
      <ServiceName monitor={monitor} view={view} className="sp-service-name" />
      <BeatStrip
        beats={dayBeats(monitor.history, new Date(view.from).getTime(), t)}
        slots={view.days}
        narrowSlots={NARROW_DAYS}
        height={14}
        label={t.n("statusPage.history.aria", monitor.history.length)}
        className="sp-compact-strip"
      />
      <span className="sp-compact-uptime" title={windowsText(monitor.uptime, t)}>
        {longest ? uptime(longest.uptime_percent, t) : t("common.none")}
      </span>
      <StateWords state={monitor.state} t={t} />
    </li>
  );
}

/**
 * The page's sections, each a list of its services. The unnamed group,
 * when the page has one, comes first without a heading. Above
 * `compactAbove` services in all, each takes one line.
 */
export function Sections({ view, t, compactAbove }: { view: StatusPageView; t: T; compactAbove: number }) {
  const total = allMonitors(view).length;
  const compact = total > compactAbove;
  if (total === 0) {
    return (
      <section aria-label={t("statusPage.services")}>
        <p className="panel sp-calm mt-4">{t("statusPage.noMonitors")}</p>
      </section>
    );
  }
  return (
    <section aria-label={t("statusPage.services")} className="sp-sections">
      {view.sections.map((section, i) => (
        <div key={`${i}-${section.name}`} className="sp-group">
          {section.name && (
            <h2 className="sp-group-title">
              {section.name}
              <span className="sp-group-count">{t.n("statusPage.summary.services", section.monitors.length)}</span>
            </h2>
          )}
          <ul className={`panel sp-services ${compact ? "sp-services-compact" : ""}`}>
            {section.monitors.map((m) => (compact ? <CompactService key={m.id} monitor={m} view={view} t={t} /> : <Service key={m.id} monitor={m} view={view} t={t} />))}
          </ul>
        </div>
      ))}
    </section>
  );
}

/** Maintenance going on or coming up that touches a shown service: when, and which services. */
export function Maintenance({ view, t, now }: { view: StatusPageView; t: T; now: number }) {
  if (view.maintenance.length === 0) return null;
  return (
    <section aria-labelledby="sp-maintenance">
      <h2 id="sp-maintenance" className="sp-section-title">
        {t("statusPage.maintenance.title")}
      </h2>
      <ul className="panel sp-timeline">
        {view.maintenance.map((w) => {
          const ongoing = inProgress(w, now);
          return (
            <li key={`${w.name}-${w.starts_at}`} className="sp-event">
              <StatusGlyph kind="maint" />
              <div className="min-w-0">
                <p className="sp-event-what">
                  <b>{w.name}</b>
                  {" · "}
                  {ongoing ? <span className="font-semibold text-maint-ink">{t("statusPage.maintenance.ongoing")}</span> : t("statusPage.maintenance.upcoming")}
                </p>
                <p className="sp-event-when">
                  <WhenRange from={w.starts_at} to={w.ends_at} /> · {span(Date.parse(w.ends_at) - Date.parse(w.starts_at), t)}
                </p>
                {w.monitors.length > 0 && <p className="sp-event-who">{new Intl.ListFormat(t.intl, { type: "conjunction" }).format(w.monitors)}</p>}
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

/** What the page told its visitors, when it told anything. */
export function Updates({ view, t }: { view: StatusPageView; t: T }) {
  const updates = view.updates.flatMap((d) => d.updates);
  if (updates.length === 0) return null;
  return (
    <section aria-labelledby="sp-updates">
      <h2 id="sp-updates" className="sp-section-title">
        {t("statusPage.updates.title")}
      </h2>
      <UpdatesTimeline updates={updates} />
    </section>
  );
}

/** The outages of the recent past, newest first, as a thread of glyphs; each names its service, a link where the page gives one. */
export function Incidents({ view, t }: { view: StatusPageView; t: T }) {
  return (
    <section aria-labelledby="sp-incidents">
      <h2 id="sp-incidents" className="sp-section-title">
        {t("statusPage.incidents.title")}
      </h2>
      {view.incidents.length === 0 ? (
        <p className="panel sp-calm">
          <StatusGlyph kind="up" />
          {t("statusPage.incidents.none")}
        </p>
      ) : (
        <ol className="panel sp-timeline">
          {view.incidents.map((i) => (
            <li key={`${i.monitor_id}-${i.started_at}`} className="sp-event">
              <StatusGlyph kind={i.resolved_at ? "up" : "down"} live={!i.resolved_at} />
              <div className="min-w-0">
                <p className="sp-event-what">
                  {view.detail_pages ? (
                    <Link href={`/status/${encodeURIComponent(view.slug)}/${i.monitor_id}`} className="sp-service-link font-semibold">
                      {i.monitor_name}
                    </Link>
                  ) : (
                    <b>{i.monitor_name}</b>
                  )}
                  {" · "}
                  {i.resolved_at ? (
                    t("statusPage.incidents.lasted", {
                      duration: span(new Date(i.resolved_at).getTime() - new Date(i.started_at).getTime(), t),
                    })
                  ) : (
                    <span className="font-semibold text-down-ink">
                      {t("statusPage.incidents.ongoing")} · <LiveDuration since={i.started_at} />
                    </span>
                  )}
                </p>
                <p className="sp-event-when">
                  <IncidentWhen start={i.started_at} end={i.resolved_at} />
                </p>
              </div>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}
