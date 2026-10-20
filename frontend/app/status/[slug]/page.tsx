import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { connection } from "next/server";
import { cache, Suspense } from "react";
import { ApiError, getStatusPageView, type PublicState, type StatusPageView } from "@/lib/api";
import { config } from "@/lib/config";
import { span, uptime } from "@/lib/format";
import { getT } from "@/lib/i18n/server";
import type { T } from "@/lib/i18n/translate";
import { type Beat, BeatStrip, type SlotState } from "@/app/ui/beat-strip";
import { LanguageToggle } from "@/app/ui/language-toggle";
import { LiveDuration } from "@/app/ui/live-duration";
import { Wordmark } from "@/app/ui/logo";
import { AnnounceIcon, UnplugIcon } from "@/app/ui/page-icons";
import { RefreshCountdown } from "@/app/ui/refresh-countdown";
import { type GlyphKind, glyphTone, StatusGlyph } from "@/app/ui/status-glyph";
import { SystemTheme } from "@/app/ui/theme-toggle";
import { IncidentWhen, MomentInSentence } from "./incident-when";

const DAY_MS = 86_400_000;

/** How many of the newest days a narrow screen has room for. */
const NARROW_DAYS = 30;

/** The page's view, read once per request for both the page and its title. */
const readView = cache((slug: string) => getStatusPageView(slug));

/** A published page is titled after itself, not after Kubeat. */
export async function generateMetadata({ params }: PageProps<"/status/[slug]">): Promise<Metadata> {
  try {
    const view = await readView((await params).slug);
    return { title: { absolute: view.title }, description: view.description || undefined };
  } catch {
    return {}; // the page itself says what is wrong
  }
}

/** How each state of a service is drawn and named; label is a message key, shown with t(). */
const serviceStates: Record<PublicState, { glyph: GlyphKind; label: string }> = {
  up: { glyph: "up", label: "statusPage.state.up" },
  down: { glyph: "down", label: "statusPage.state.down" },
  maintenance: { glyph: "maint", label: "statusPage.state.maintenance" },
  pending: { glyph: "pending", label: "statusPage.state.pending" },
  paused: { glyph: "paused", label: "statusPage.state.paused" },
};

/** The page as a whole: its glyph and its sentence (a message key). */
const pageStates: Record<StatusPageView["state"], { glyph: GlyphKind; title: string }> = {
  up: { glyph: "up", title: "statusPage.overall.up" },
  degraded: { glyph: "down", title: "statusPage.overall.degraded" },
  down: { glyph: "down", title: "statusPage.overall.down" },
  maintenance: { glyph: "maint", title: "statusPage.overall.maintenance" },
};

/** A day by how much of it the service was up: healthy, partly failing, or mostly down. */
function dayState(percent: number | null): SlotState {
  if (percent === null) return "none";
  if (percent >= 99.9) return "ok";
  if (percent >= 95) return "partial";
  return "down";
}

/** The text color of an uptime: left alone while healthy, amber under 99.9%, red under 99%. */
function uptimeColor(percent: number | null): string | undefined {
  if (percent === null || percent >= 99.9) return undefined;
  return percent < 99 ? "var(--down-ink)" : "var(--warn-ink)";
}

/** The days of a service as beats, oldest first; a day without checks stays a baseline. */
function dayBeats(history: (number | null)[], firstDay: number, t: T): Beat[] {
  return history.map((percent, i) => ({
    state: dayState(percent),
    at: firstDay + i * DAY_MS,
    day: true,
    text: percent === null ? t("statusPage.noData") : t("statusPage.uptime", { value: uptime(percent, t) }),
  }));
}

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

/** The services' uptime over the page's days, the mean of those measured; null when none was. */
function averageUptime(view: StatusPageView): number | null {
  const measured = view.monitors.flatMap((m) => (m.uptime_percent === null ? [] : [m.uptime_percent]));
  return measured.length === 0 ? null : measured.reduce((sum, p) => sum + p, 0) / measured.length;
}

/** One line under the page's state: what is down since when, what is in maintenance, or when the last outage was. */
function Summary({ view, t }: { view: StatusPageView; t: T }) {
  const down = view.monitors.filter((m) => m.state === "down");
  const maintenance = view.monitors.filter((m) => m.state === "maintenance");
  const others = view.monitors.some((m) => m.state === "up");

  if (down.length > 0) {
    // The outage began when the first of the services still down went down.
    const since = view.incidents
      .filter((i) => !i.resolved_at && down.some((m) => m.name === i.monitor_name))
      .map((i) => i.started_at)
      .sort()[0];
    const names = serviceNames(down.map((m) => m.name), t);
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
        {t("statusPage.summary.maintenance", { names: serviceNames(maintenance.map((m) => m.name), t) })}
        {others && ` · ${t("statusPage.summary.others")}`}
      </>
    );
  }
  const services = t.n("statusPage.summary.services", view.monitors.length);
  // The incidents come newest first.
  const last = view.incidents[0];
  return (
    <>
      {services} ·{" "}
      {last ? withTime(t("statusPage.summary.lastOutage"), last.started_at) : t("statusPage.summary.noOutages")}
    </>
  );
}

/** A percentage as a large figure, its sign set small where the language puts it: 99.77% / %99,77. */
function PercentFigure({ value, t }: { value: number; t: T }) {
  // Floored, so that 99.996 is not shown as a perfect 100.
  const floored = Math.floor(value * 100) / 100;
  const digits = floored === 100 ? 0 : 2;
  const parts = new Intl.NumberFormat(t.intl, {
    style: "percent",
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).formatToParts(floored / 100);
  return (
    <>
      {parts.map((p, i) =>
        p.type === "percentSign" ? (
          <small key={i} className="sp-kpi-unit">
            {p.value}
          </small>
        ) : (
          p.value
        ),
      )}
    </>
  );
}

/** The state of the page in a sentence, over a soft light of its color, with the uptime of the period. */
function Hero({ view, t }: { view: StatusPageView; t: T }) {
  const state = pageStates[view.state];
  const average = averageUptime(view);
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
      {average !== null && (
        <div className="sp-kpi">
          <p className="sp-kpi-value" style={{ color: uptimeColor(average) }}>
            <PercentFigure value={average} t={t} />
          </p>
          <p className="sp-kpi-label">{t("statusPage.kpi", { days: view.days })}</p>
        </div>
      )}
    </section>
  );
}

/** One service: its name and state, and its days as a strip, today on the right. */
function Service({ monitor, view, t }: { monitor: StatusPageView["monitors"][number]; view: StatusPageView; t: T }) {
  const s = serviceStates[monitor.state];
  // In words for the public, where the app writes "90d".
  const daysAgo = (days: number) => t.n("statusPage.daysAgo", days);
  return (
    <li className="sp-service">
      <div className="sp-service-head">
        <h3 className="sp-service-name" title={monitor.name}>
          {monitor.name}
        </h3>
        <span className="sp-service-state" style={{ color: glyphTone[s.glyph].ink }}>
          <StatusGlyph kind={s.glyph} live={monitor.state === "down"} />
          {t(s.label)}
        </span>
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
        <span className="sp-axis-uptime" style={{ color: uptimeColor(monitor.uptime_percent) }}>
          {monitor.uptime_percent === null
            ? t("statusPage.noData")
            : t("statusPage.uptime", { value: uptime(monitor.uptime_percent, t) })}
        </span>
        <span>{t("statusPage.today")}</span>
      </div>
    </li>
  );
}

/** The outages of the recent past, newest first, as a thread of glyphs. */
function Incidents({ view, t }: { view: StatusPageView; t: T }) {
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
            <li key={`${i.monitor_name}-${i.started_at}`} className="sp-event">
              <StatusGlyph kind={i.resolved_at ? "up" : "down"} live={!i.resolved_at} />
              <div className="min-w-0">
                <p className="sp-event-what">
                  <b>{i.monitor_name}</b>
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

async function View({ params }: { params: Promise<{ slug: string }> }) {
  // The page is read from the backend each time it is asked for.
  await connection();
  const { slug } = await params;
  const t = await getT();
  // How often the page reads itself again: a setting of the running server.
  const refresh = <RefreshCountdown intervalMs={config.statusPageRefreshMs} />;

  let view: StatusPageView;
  try {
    view = await readView(slug);
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) notFound();
    return (
      <>
        <div className="sp-head justify-end">{refresh}</div>
        <section role="alert" className="panel pg-empty mt-6">
          <span className="pg-empty-icon" aria-hidden>
            <UnplugIcon width={20} height={20} />
          </span>
          <h1 className="pg-empty-title">{t("statusPage.unavailableTitle")}</h1>
          <p className="pg-empty-body">{t("statusPage.unavailable")}</p>
        </section>
      </>
    );
  }

  return (
    <>
      <header className="sp-head">
        <div className="sp-head-main">
          <h1 className="sp-title">{view.title}</h1>
          {view.description && <p className="sp-desc">{view.description}</p>}
        </div>
        {refresh}
      </header>

      {view.announcement && (
        <section aria-labelledby="sp-announcement" className="panel sp-announce">
          <span className="pg-callout-icon" aria-hidden>
            <AnnounceIcon />
          </span>
          <div className="min-w-0">
            <h2 id="sp-announcement" className="sp-announce-title">
              {t("statusPage.announcement")}
            </h2>
            <p className="sp-announce-text">{view.announcement}</p>
          </div>
        </section>
      )}

      <Hero view={view} t={t} />

      <section aria-label={t("statusPage.services")}>
        {view.monitors.length === 0 ? (
          <p className="panel sp-calm mt-4">{t("statusPage.noMonitors")}</p>
        ) : (
          <ul className="panel sp-services">
            {view.monitors.map((m) => (
              <Service key={m.id} monitor={m} view={view} t={t} />
            ))}
          </ul>
        )}
      </section>

      <Incidents view={view} t={t} />
    </>
  );
}

/** What stands in for the page while it is read: its frame, without words. */
function Skeleton({ label }: { label: string }) {
  return (
    <div aria-busy="true" aria-label={label} className="sp-skeleton">
      <span className="h-[34px] w-[min(20rem,70%)]" />
      <span className="mt-3 h-4 w-[min(28rem,90%)]" />
      <span className="panel mt-6 h-[104px] w-full" />
      <span className="panel mt-4 h-[320px] w-full" />
    </div>
  );
}

/** "Powered by kubeat", and the other language. */
async function Footer() {
  const t = await getT();
  const [before, after] = t("statusPage.footer").split("{kubeat}");
  return (
    <footer className="sp-foot">
      <span>
        {before}
        <Wordmark className="text-ink-2" />
        {after}
      </span>
      <span aria-hidden>·</span>
      <LanguageToggle variant="inline" />
    </footer>
  );
}

/**
 * A published status page, for anyone: its own plain frame, without the
 * app's navigation or anything about who is signed in, in the system's
 * light or dark.
 */
export default async function Page({ params }: PageProps<"/status/[slug]">) {
  const t = await getT();
  return (
    <main className="sp-page">
      <SystemTheme />
      <Suspense fallback={<Skeleton label={t("common.loading")} />}>
        <View params={params} />
      </Suspense>
      <Footer />
    </main>
  );
}
