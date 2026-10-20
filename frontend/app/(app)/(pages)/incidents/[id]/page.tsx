import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { cache, Suspense } from "react";
import { ApiError, getIncident, type IncidentPage } from "@/lib/api";
import { duration, milliseconds, span } from "@/lib/format";
import { filterHref, emptyFilter } from "@/lib/incident-filter";
import { deliveries, kubectlName, typeWord } from "@/lib/incident-timeline";
import { getT } from "@/lib/i18n/server";
import type { T } from "@/lib/i18n/translate";
import { MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { BackendError } from "@/app/ui/backend-error";
import { ChartIcon, NoteIcon, OpenIcon } from "@/app/ui/incident-icons";
import { Clock, DayWord } from "@/app/ui/incident-time";
import { IncidentTimeline } from "@/app/ui/incident-timeline";
import { LiveDuration } from "@/app/ui/live-duration";
import { LiveRefresh } from "@/app/ui/live-refresh";
import { monitorLabel } from "@/app/ui/monitor-name";
import { type Readout, Readouts } from "@/app/ui/readouts";
import { Crumbs } from "@/app/ui/shell-state";
import { StateChip } from "@/app/ui/state-chip";

type Params = Promise<{ id: string }>;

/** The incident's page, read once per request for both the page and its title. */
const readIncident = cache((id: number) => getIncident(id));

/** The incident's id in the address, or null when it cannot be one. */
async function incidentId(params: Params): Promise<number | null> {
  const id = Number((await params).id);
  return Number.isInteger(id) && id > 0 ? id : null;
}

/** Titled after its monitor, so that a tab opened from a notification says what it is about. */
export async function generateMetadata({ params }: PageProps<"/incidents/[id]">): Promise<Metadata> {
  const id = await incidentId(params);
  const t = await getT();
  if (id === null) return { title: t("incident.notFound.title") };
  try {
    const page = await readIncident(id);
    return { title: `${monitorLabel(page.monitor).name} · ${t("incident.number", { id })}` };
  } catch (err) {
    // Otherwise the page itself says what is wrong.
    return err instanceof ApiError && err.status === 404 ? { title: t("incident.notFound.title") } : {};
  }
}

/** The moments of the incident and its checks, as figures. */
function figures(page: IncidentPage, t: T): Readout[] {
  const { incident, checks } = page;
  const cert = incident.kind === "cert";
  const n = (v: number) => v.toLocaleString(t.intl);
  const started = Date.parse(incident.started_at);
  // The moments are clock times, with their day under them; what each means
  // (the first failed check, the threshold reached, the first success) the
  // timeline tells at its row.
  const items: Readout[] = [
    { label: t("incident.started"), value: <Clock iso={incident.started_at} />, hint: <DayWord iso={incident.started_at} /> },
  ];
  if (!cert) {
    items.push(
      incident.opened_at
        ? {
            label: t("incident.opened"),
            value: <Clock iso={incident.opened_at} />,
            hint:
              Date.parse(incident.opened_at) - started < 1000
                ? t("incident.opened.atOnce")
                : t("incident.opened.after", { delay: span(Date.parse(incident.opened_at) - started, t) }),
          }
        : { label: t("incident.opened"), value: "—", tone: "muted", hint: t("incident.opened.unknown.hint") },
    );
  }
  items.push(
    incident.resolved_at
      ? {
          label: t("incident.resolved"),
          glyph: "up",
          value: <Clock iso={incident.resolved_at} />,
          hint: <DayWord iso={incident.resolved_at} />,
        }
      : {
          label: t("incident.resolved"),
          value: "—",
          tone: "muted",
          hint: <span className={cert ? "text-warn-ink" : "text-down-ink"}>{t("incident.ongoing")}</span>,
        },
  );
  if (checks) {
    const passed = checks.total - checks.failed;
    items.push({
      label: t("incident.readout.failed"),
      glyph: checks.failed > 0 ? "down" : undefined,
      value: n(checks.failed),
      tone: checks.failed > 0 ? "down" : "muted",
      // Of how many checks; only of the later ones when the earlier are no longer kept.
      hint: t(checks.partial ? "incident.readout.ofLastChecks" : "incident.readout.ofChecks", { count: n(checks.total) }),
    });
    // Response times exist only for the checks that got through, which an
    // outage seldom has; without any the figure would only say "none".
    if (passed > 0) {
      items.push({
        label: t("incident.readout.response"),
        ...ms(checks.avg_latency_ms, t),
        hint: t("incident.readout.slowest", { ms: milliseconds(checks.max_latency_ms, t) }),
      });
    }
  }
  const sent = deliveries(page.timeline);
  items.push({
    label: t("incident.readout.notifications"),
    value: n(sent.attempts),
    tone: sent.attempts === 0 ? "muted" : undefined,
    hint:
      sent.attempts === 0 ? (
        t("incident.readout.notifications.none")
      ) : sent.failed > 0 ? (
        <span className="text-down-ink">{t.n("incident.readout.notifications.failed", sent.failed, { count: n(sent.failed) })}</span>
      ) : sent.omitted > 0 ? (
        // The outcome of those the timeline leaves out is not known here.
        t("incident.readout.notifications.shownOk")
      ) : (
        t("incident.readout.notifications.ok")
      ),
  });
  return items;
}

/** A response time as a readout's value and unit: "38 ms", "1,2 s". */
function ms(value: number, t: T): Pick<Readout, "value" | "unit"> {
  const text = milliseconds(value, t);
  const [figure, unit] = text.split(" ");
  return { value: figure, unit };
}

async function IncidentDetail({ params }: { params: Params }) {
  const id = await incidentId(params);
  if (id === null) notFound();
  const t = await getT();
  let page: IncidentPage;
  try {
    page = await readIncident(id);
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) notFound();
    return <BackendError what={t("incident.what")} err={err} />;
  }

  const { incident, monitor } = page;
  const open = !incident.resolved_at;
  const cert = incident.kind === "cert";
  const object = kubectlName(monitor.source);
  const name = monitorLabel(monitor).name;
  const nsLabel = monitor.namespace || t(MANUAL_LABEL);
  // The chip's words are split around the duration, which counts on while the incident is open.
  const [before, after] = t(open ? (cert ? "incident.chip.cert" : "incident.chip.down") : "incident.chip.resolved").split(
    "{duration}",
  );
  const lasted = open ? (
    <LiveDuration since={incident.started_at} />
  ) : (
    span(Date.parse(incident.resolved_at!) - Date.parse(incident.started_at), t)
  );
  // The monitor's chart, framed on the incident (open-ended while it goes on).
  const chart = `/monitors/${monitor.id}?from=${encodeURIComponent(incident.started_at)}${
    incident.resolved_at ? `&to=${encodeURIComponent(incident.resolved_at)}` : ""
  }`;

  const notices = (
    <>
      {!cert && monitor.source !== "" && page.events_reason && (
        <p className="inc-tl-notice">
          <span>{t("incident.events.reason")}</span> <span className="inc-err">{page.events_reason}</span>
        </p>
      )}
      {page.events_truncated && <p className="inc-tl-notice">{t("incident.events.truncated")}</p>}
    </>
  );

  return (
    <>
      <Crumbs
        items={[
          { label: t("nav.incidents"), href: "/incidents" },
          {
            label: nsLabel,
            href: filterHref(emptyFilter, { namespaces: [monitor.namespace] }),
            mono: monitor.namespace !== "",
          },
          { label: `${name} #${incident.id}` },
        ]}
      />
      {open && <LiveRefresh />}
      <header className="inc-hero">
        <div className="inc-hero-main">
          <p className="inc-number">
            {t("incident.number", { id: incident.id })} · {t(cert ? "incident.kind.cert" : "incident.kind.down")}
          </p>
          <div className="inc-title-row">
            <h1 className="type-display max-sm:type-display-s inc-title">{name}</h1>
            <span className="inc-kind" title={monitor.source || monitor.type}>
              {object && <span>{object}</span>}
              {object && <i aria-hidden />}
              <span>{typeWord(monitor.type)}</span>
            </span>
          </div>
          <div className="inc-stline">
            <StateChip kind={open ? (cert ? "warn" : "down") : "up"} live={open && !cert}>
              {before}
              {lasted}
              {after}
            </StateChip>
            {incident.cause && (
              <span className="inc-cause" title={incident.cause}>
                {incident.cause}
              </span>
            )}
          </div>
          <p className="inc-meta">
            <Link href={namespaceHref(monitor.namespace)} className={monitor.namespace ? "inc-mono inc-ns" : "inc-ns"}>
              {nsLabel}
            </Link>
            {monitor.target && (
              <span className="inc-target" title={monitor.target}>
                {monitor.target}
              </span>
            )}
            <span>
              {t("incident.monitor.every", { interval: duration(monitor.interval_seconds, t) })}
              {/* The threshold opens outages; a certificate warning has none. */}
              {!cert && <> · {t.n("incident.monitor.threshold", monitor.failure_threshold)}</>}
            </span>
            {monitor.managed && <span className="inc-tag">{t("incident.monitor.managed")}</span>}
          </p>
        </div>
        <div className="inc-actions">
          <Link href={`/monitors/${monitor.id}`} className="btn btn-ghost">
            <OpenIcon />
            {t("incident.monitor.open")}
          </Link>
          <Link href={chart} className="btn btn-ghost">
            <ChartIcon />
            {t("incident.monitor.chart")}
          </Link>
          {page.can_note && (
            <a href="#note" className="btn btn-ghost">
              <NoteIcon />
              {t("incident.note.add")}
            </a>
          )}
        </div>
      </header>
      <Readouts items={figures(page, t)} label={t("incident.readouts")} className="inc-readouts" />
      {!page.checks && <p className="inc-checks-gone">{t(cert ? "incident.checks.cert" : "incident.checks.gone")}</p>}
      <IncidentTimeline incident={incident} timeline={page.timeline} canNote={page.can_note} notices={notices} />
    </>
  );
}

export default async function Page({ params }: PageProps<"/incidents/[id]">) {
  const t = await getT();
  return (
    <main className="page inc-page">
      <Suspense fallback={<p className="text-sm text-ink-3">{t("common.loading")}</p>}>
        <IncidentDetail params={params} />
      </Suspense>
    </main>
  );
}
