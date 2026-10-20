import Link from "next/link";
import type { CheckResult, Monitor, MonitorChannel } from "@/lib/api";
import type { T } from "@/lib/i18n/translate";
import { kubectlName, typeWords } from "@/lib/monitor-page";
import { MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { ChannelsChip } from "@/app/ui/monitor-channels";
import { MonitorActions } from "@/app/ui/monitor-settings";
import { type DisplayState, StatusPill } from "@/app/ui/status";
import { TagChip } from "@/app/ui/tag";

/**
 * The top of a monitor's page: its name in the display face with what it is
 * in the cluster as kubectl says it ("svc/pricing:8080 | http"), how it
 * stands and for how long, the error of a failing check, then where it
 * points, its tags and where its alerts go. The actions sit on the right
 * as quiet buttons, for those who may use them.
 */
export function MonitorHeader({
  monitor,
  state,
  since,
  channels,
  canEdit,
  silence,
  t,
}: {
  monitor: Monitor;
  state: DisplayState;
  /** When the present state began, for a state that counts its length. */
  since?: string;
  /** null when they could not be read. */
  channels: MonitorChannel[] | null;
  canEdit: boolean;
  /** The silence lengths offered, or null where silencing makes no sense. */
  silence: number[] | null;
  t: T;
}) {
  const object = kubectlName(monitor.source);
  const last = monitor.last_result;
  const failing = monitor.enabled && last !== null && !last.up;
  return (
    <header className="mon-head">
      <div className="mon-head-main">
        {/* The context bar carries the breadcrumb on a wide screen. */}
        <p className="mb-2 flex items-center gap-1.5 text-[13px] text-ink-3 lg:hidden">
          <Link href="/" className="hover:text-ink">
            {t("monitorPage.breadcrumb.overview")}
          </Link>
          <span aria-hidden>/</span>
          <Link href={namespaceHref(monitor.namespace)} className={`hover:text-ink ${monitor.namespace ? "mon-mono" : ""}`}>
            {monitor.namespace || t(MANUAL_LABEL)}
          </Link>
        </p>
        <div className="mon-title-row">
          <h1 className="mon-title type-display-s @min-[42rem]:type-display">{monitor.name}</h1>
          <span className="mon-kind" title={`${t("monitorPage.kind.title")}: ${object ? `${object} · ` : ""}${typeWords[monitor.type]}`}>
            {object && (
              <>
                <span>{object}</span>
                <i aria-hidden />
              </>
            )}
            <span>{typeWords[monitor.type]}</span>
          </span>
        </div>
        {monitor.description && <p className="mon-desc">{monitor.description}</p>}
        <div className="mon-stline">
          <StatusPill state={state} t={t} since={since} />
          {failing && <ErrorLine last={last} />}
        </div>
        <div className="mon-meta">
          {monitor.target && (
            <span className="mon-target">
              <span className="sr-only">{t("monitorPage.target")}: </span>
              {monitor.http && <b>{monitor.http.method} </b>}
              {monitor.target}
            </span>
          )}
          {monitor.tags.length > 0 && (
            <span role="list" aria-label={t("monitorPage.tags")} className="contents">
              {monitor.tags.map((tag) => (
                <span key={tag} role="listitem" className="contents">
                  <TagChip name={tag} />
                </span>
              ))}
            </span>
          )}
          {channels && channels.length > 0 && <ChannelsChip channels={channels} t={t} />}
          {monitor.each_pod && (
            <span className="mon-chip" title={t("monitorPage.eachPod.title")}>
              {t("monitorPage.eachPod")}
            </span>
          )}
          {monitor.managed && (
            <span className="mon-chip mon-mono" title={t("monitorPage.managed.title")}>
              {t("monitorPage.managed")}
            </span>
          )}
        </div>
      </div>
      {canEdit && (
        <MonitorActions
          // Only what the buttons need goes to the browser.
          monitor={{ id: monitor.id, name: monitor.name, enabled: monitor.enabled, managed: monitor.managed }}
          silence={silence}
        />
      )}
    </header>
  );
}

/** The error of the failing check, beside the state chip; the "Why?" panel explains it. */
function ErrorLine({ last }: { last: CheckResult }) {
  const text = last.error ?? (last.status_code ? `HTTP ${last.status_code}` : last.detail);
  return text ? (
    <span className="mon-error" title={text}>
      {text}
    </span>
  ) : null;
}
