import Link from "next/link";
import type { Incident } from "@/lib/api";
import { span } from "@/lib/format";
import type { T } from "@/lib/i18n/translate";
import { ChevronIcon } from "@/app/ui/icons";
import { LiveDuration } from "@/app/ui/live-duration";
import { DayTime } from "@/app/ui/local-time";
import { StatusGlyph } from "@/app/ui/status-glyph";

/**
 * A monitor's incidents as a timeline, newest first: a glyph for how it
 * stands (an outage still going on pulses), what it was and how long it
 * lasted, then when it began and its cause. hrefFor, when given, makes
 * each a link: the monitor page frames the incident on its chart.
 */
export function IncidentTimeline({
  incidents,
  shown,
  hrefFor,
  t,
}: {
  incidents: Incident[];
  /** How many are listed before the rest wait behind "Show all". */
  shown: number;
  hrefFor?: (incident: Incident) => string;
  t: T;
}) {
  const first = incidents.slice(0, shown);
  const rest = incidents.slice(shown);
  const item = (i: Incident) => <Item key={i.id} incident={i} href={hrefFor?.(i)} t={t} />;
  return (
    <>
      <ol className="mon-tl">{first.map(item)}</ol>
      {rest.length > 0 && (
        <details className="mon-more">
          <summary>
            <ChevronIcon width={14} height={14} />
            {t("monitorPage.incidents.showAll", { count: incidents.length })}
          </summary>
          <ol className="mon-tl -mx-4 mt-1 pb-0">{rest.map(item)}</ol>
        </details>
      )}
    </>
  );
}

function Item({ incident: i, href, t }: { incident: Incident; href?: string; t: T }) {
  const ongoing = !i.resolved_at;
  const what = t(i.kind === "cert" ? "monitorPage.incidents.cert" : "monitorPage.incidents.outage");
  // The duration is a component while it runs, so the sentence is split around its placeholder.
  const [before, after] = t(ongoing ? "monitorPage.incidents.ongoing" : "monitorPage.incidents.resolvedAfter").split(
    "{duration}",
  );
  const lasted = ongoing ? (
    <LiveDuration since={i.started_at} />
  ) : (
    span(Date.parse(i.resolved_at!) - Date.parse(i.started_at), t)
  );
  const body = (
    <>
      <StatusGlyph kind={ongoing ? (i.kind === "cert" ? "warn" : "down") : "up"} live={ongoing && i.kind === "down"} />
      <div className="min-w-0">
        <p className="mon-tl-w1">
          <b>{what}</b> · {before}
          {lasted}
          {after}
        </p>
        <p className="mon-tl-w2">
          <DayTime iso={i.started_at} />
          {i.cause && <> · {i.cause}</>}
        </p>
      </div>
    </>
  );
  return (
    <li className="mon-tl-item">
      {href ? (
        <Link href={href} className="mon-tl-row" title={t("monitorPage.incidents.show", { what })}>
          {body}
        </Link>
      ) : (
        <div className="mon-tl-row">{body}</div>
      )}
    </li>
  );
}
