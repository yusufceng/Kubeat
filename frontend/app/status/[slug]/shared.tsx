import { ThemeToggle } from "@/app/ui/theme-toggle";
import type { PublicState, StatusPageUptime, StatusPageView } from "@/lib/api";
import { uptime } from "@/lib/format";
import type { T } from "@/lib/i18n/translate";
import type { Beat, SlotState } from "@/app/ui/beat-strip";
import { Wordmark } from "@/app/ui/logo";
import { type GlyphKind, StatusGlyph } from "@/app/ui/status-glyph";
import { UnplugIcon } from "@/app/ui/page-icons";

/*
 * What the public page and a monitor's own page share: how states are
 * drawn and named, how days and uptimes read, and the frame around them.
 * Everything here is given the page's translator: a published page speaks
 * the language its editor chose, not the visitor's.
 */

export const DAY_MS = 86_400_000;

/** How many of the newest days a narrow screen has room for. */
export const NARROW_DAYS = 30;

/** How each state of a service is drawn and named; label is a message key, shown with t(). */
export const serviceStates: Record<PublicState, { glyph: GlyphKind; label: string }> = {
  up: { glyph: "up", label: "statusPage.state.up" },
  down: { glyph: "down", label: "statusPage.state.down" },
  maintenance: { glyph: "maint", label: "statusPage.state.maintenance" },
  pending: { glyph: "pending", label: "statusPage.state.pending" },
  paused: { glyph: "paused", label: "statusPage.state.paused" },
};

/** The page as a whole: its glyph and its sentence (a message key). */
export const pageStates: Record<StatusPageView["state"], { glyph: GlyphKind; title: string }> = {
  up: { glyph: "up", title: "statusPage.overall.up" },
  degraded: { glyph: "down", title: "statusPage.overall.degraded" },
  down: { glyph: "down", title: "statusPage.overall.down" },
  maintenance: { glyph: "maint", title: "statusPage.overall.maintenance" },
};

/** A day by how much of it the service was up: healthy, partly failing, or mostly down. */
export function dayState(percent: number | null): SlotState {
  if (percent === null) return "none";
  if (percent >= 99.9) return "ok";
  if (percent >= 95) return "partial";
  return "down";
}

/** The text color of an uptime: left alone while healthy, amber under 99.9%, red under 99%. */
export function uptimeColor(percent: number | null): string | undefined {
  if (percent === null || percent >= 99.9) return undefined;
  return percent < 99 ? "var(--down-ink)" : "var(--warn-ink)";
}

/** The days of a service as beats, oldest first; a day without checks stays a baseline. */
export function dayBeats(history: (number | null)[], firstDay: number, t: T): Beat[] {
  return history.map((percent, i) => ({
    state: dayState(percent),
    at: firstDay + i * DAY_MS,
    day: true,
    text: percent === null ? t("statusPage.noData") : t("statusPage.uptime", { value: uptime(percent, t) }),
  }));
}

/** A window in words for the public: "24 saat" / "24 hours", "7 gün" / "7 days". */
export function windowLabel(seconds: number, t: T): string {
  if (seconds <= DAY_MS / 1000) return t("statusPage.window.hours", { count: Math.round(seconds / 3600) });
  return t.n("statusPage.window.days", Math.round(seconds / 86400));
}

/** Every service of every section, in the page's order. */
export function allMonitors(view: StatusPageView): StatusPageView["sections"][number]["monitors"] {
  return view.sections.flatMap((s) => s.monitors);
}

/** A percentage as a large figure, its sign set small where the language puts it: 99.77% / %99,77. */
export function PercentFigure({ value, t }: { value: number; t: T }) {
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

/**
 * The uptime over each window the page measures (a day, a week, a month,
 * its own days), as figures split by hairlines: the page's as a whole in
 * the hero, a service's on its own page.
 */
export function UptimeWindows({ windows, label, t, className = "" }: { windows: StatusPageUptime[]; label: string; t: T; className?: string }) {
  // The label comes first in the markup, as a dl wants; the figure is drawn above it.
  return (
    <dl className={`sp-windows ${className}`} aria-label={label}>
      {windows.map((w) => (
        <div key={w.window_seconds} className="sp-window">
          <dt className="sp-window-label">{windowLabel(w.window_seconds, t)}</dt>
          <dd className="sp-window-value" style={{ color: uptimeColor(w.uptime_percent) }}>
            {w.uptime_percent === null ? <span className="text-ink-3">{t("common.none")}</span> : <PercentFigure value={w.uptime_percent} t={t} />}
          </dd>
        </div>
      ))}
    </dl>
  );
}

/** A service's windows on one line: "24 saat %100 · 7 gün %99,97 · …"; on a phone only the longest shows. */
export function UptimeInline({ windows, t }: { windows: StatusPageUptime[]; t: T }) {
  return (
    <span className="sp-axis-windows">
      {windows.map((w, i) => (
        <span key={w.window_seconds} className={`sp-win ${i < windows.length - 1 ? "max-sm:hidden" : ""}`} style={{ color: uptimeColor(w.uptime_percent) }}>
          <small>{windowLabel(w.window_seconds, t)}</small> {uptime(w.uptime_percent, t)}
        </span>
      ))}
    </span>
  );
}

/** The windows as one line of text, for a title: "24 saat %100 · 7 gün %99,97". */
export function windowsText(windows: StatusPageUptime[], t: T): string {
  return windows.map((w) => `${windowLabel(w.window_seconds, t)} ${uptime(w.uptime_percent, t)}`).join(" · ");
}

/** "Powered by kubeat", in the page's language. A wall screen hides it. */
export function Footer({ t }: { t: T }) {
  const [before, after] = t("statusPage.footer").split("{kubeat}");
  return (
    <footer className="sp-foot">
      <span>
        {before}
        <Wordmark className="text-ink-2" />
        {after}
      </span>
      <ThemeToggle variant="inline" />
    </footer>
  );
}

/** The page cannot be read right now: the backend is away. It retries by itself. */
export function Unavailable({ t, tools }: { t: T; tools?: React.ReactNode }) {
  return (
    <>
      {tools && <div className="sp-head justify-end">{tools}</div>}
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

/** What stands in for the page while it is read: its frame, without words. */
export function Skeleton({ label }: { label: string }) {
  return (
    <div aria-busy="true" aria-label={label} className="sp-skeleton">
      <span className="h-[34px] w-[min(20rem,70%)]" />
      <span className="mt-3 h-4 w-[min(28rem,90%)]" />
      <span className="panel mt-6 h-[104px] w-full" />
      <span className="panel mt-4 h-[320px] w-full" />
    </div>
  );
}

/** A state in words beside its glyph, in its color: "● Çalışıyor". */
export function StateWords({ state, t, size = 10, className = "" }: { state: PublicState; t: T; size?: 10 | 12 | 16; className?: string }) {
  const s = serviceStates[state];
  return (
    <span className={`sp-service-state sp-state-${s.glyph} ${className}`}>
      <StatusGlyph kind={s.glyph} size={size} live={state === "down"} />
      {t(s.label)}
    </span>
  );
}
