"use client";

import Link from "next/link";
import { useActionState, useState } from "react";
import { importCandidatesAction, type ImportState } from "@/app/actions";
import type { Candidate } from "@/lib/api";
import { duration } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
import { typeLabel } from "@/lib/monitor-types";
import { OpenIcon } from "@/app/ui/page-icons";
import { StatusGlyph } from "@/app/ui/status-glyph";

/** Kinds as kubectl shortens them; a Route has no shorter name. */
const kindShort: Record<Candidate["kind"], string> = {
  route: "route",
  ingress: "ing",
  service: "svc",
  deployment: "deploy",
  statefulset: "sts",
  daemonset: "ds",
};

/** Kinds as Kubernetes names them, for screen readers and the title. */
const kindNames: Record<Candidate["kind"], string> = {
  route: "Route",
  ingress: "Ingress",
  service: "Service",
  deployment: "Deployment",
  statefulset: "StatefulSet",
  daemonset: "DaemonSet",
};

const workloadKinds: Candidate["kind"][] = ["deployment", "statefulset", "daemonset"];

/** Message keys of why a check was suggested; show them with t(). */
const reasonLabels: Record<Candidate["reason"], string> = {
  "readiness-probe": "discovery.reason.readinessProbe",
  "liveness-probe": "discovery.reason.livenessProbe",
  "tcp-fallback": "discovery.reason.tcpFallback",
  "route-host": "discovery.reason.routeHost",
  "ingress-rule": "discovery.reason.ingressRule",
  workload: "discovery.reason.workload",
};

/**
 * What a scan of a namespace found, as a table: the object, the check
 * Kubeat suggests for it and why, and whether it is monitored already.
 * Editors tick what to monitor and add it from the bar under the table,
 * which keeps its place while the table scrolls by and reads the checks
 * back before they are made; viewers see the table alone.
 */
export function CandidateForm({
  namespace,
  candidates,
  defaults,
  canEdit = true,
}: {
  namespace: string;
  candidates: Candidate[];
  /** What the imported monitors start with; the same as a monitor made by hand. */
  defaults: { interval: number; timeout: number; failureThreshold: number };
  canEdit?: boolean;
}) {
  const t = useT();
  const [state, action, pending] = useActionState<ImportState, FormData>(importCandidatesAction, null);
  const available = candidates.filter((c) => c.monitor_id === null);
  // Workloads complement the service check rather than replace it, and a
  // service with nothing ready behind it would alarm at once: neither
  // starts selected.
  const [selected, setSelected] = useState(
    () => new Set(available.filter((c) => !workloadKinds.includes(c.kind) && !c.no_endpoints).map((c) => c.source)),
  );
  const [every, setEvery] = useState(String(defaults.interval));
  const [limit, setLimit] = useState(String(defaults.timeout));
  const [threshold, setThreshold] = useState(String(defaults.failureThreshold));

  // Sources that have since been imported no longer count.
  const chosen = available.filter((c) => selected.has(c.source));
  const allChosen = available.length > 0 && chosen.length === available.length;

  const toggle = (source: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (!next.delete(source)) next.add(source);
      return next;
    });

  const number = (value: string) => Math.max(1, Math.round(Number(value) || 0));

  /** The checks read back: how often, how long a check may take, after how many failures an incident opens. */
  function echo(): React.ReactNode {
    if (chosen.length === 0) return t("discovery.import.echo.none");
    const parts = t.n("discovery.import.echo", chosen.length, {
      every: duration(number(every), t),
      timeout: duration(number(limit), t),
      threshold: number(threshold),
    }).split(/(\*[^*]+\*)/);
    // The values are set off in the message between asterisks.
    return parts.map((part, i) => (part.startsWith("*") ? <b key={i}>{part.slice(1, -1)}</b> : part));
  }

  const table = (
    <table className="pg-table pg-candidates">
      <thead>
        <tr>
          {canEdit && (
            <th scope="col" className="pg-check-cell">
              <input
                type="checkbox"
                aria-label={t("discovery.import.selectAll")}
                checked={allChosen}
                disabled={available.length === 0}
                onChange={() => setSelected(allChosen ? new Set() : new Set(available.map((c) => c.source)))}
              />
            </th>
          )}
          <th scope="col">{t("discovery.candidates.object")}</th>
          <th scope="col">{t("discovery.candidates.check")}</th>
          <th scope="col">{t("discovery.import.basis")}</th>
          <th scope="col">{t("discovery.candidates.status")}</th>
        </tr>
      </thead>
      <tbody>
        {candidates.map((c) => {
          const monitored = c.monitor_id !== null;
          const object = `${c.name}${c.port > 0 ? `:${c.port}` : ""}`;
          return (
            <tr key={c.source} className={monitored ? "pg-row-quiet" : undefined}>
              {canEdit && (
                <td className="pg-check-cell">
                  {!monitored && (
                    <input
                      type="checkbox"
                      name="source"
                      value={c.source}
                      aria-label={`${kindNames[c.kind]} ${object}`}
                      checked={selected.has(c.source)}
                      onChange={() => toggle(c.source)}
                    />
                  )}
                </td>
              )}
              <td className="pg-shrink">
                <span className="flex items-center gap-2" title={`${kindNames[c.kind]} ${c.namespace}/${object}`}>
                  <span className="pg-tag pg-tag-mono w-[3.25rem] justify-center">{kindShort[c.kind]}</span>
                  <span className="pg-trunc pg-mono max-w-48 text-ink xl:max-w-64">{object}</span>
                </span>
              </td>
              <td className="pg-grow">
                <span className="flex min-w-0 items-center gap-2" title={c.target}>
                  <span className="flex-none text-ink-3">{t(typeLabel(c.type))}</span>
                  <span className="pg-trunc pg-mono text-ink-2">{c.target}</span>
                </span>
              </td>
              <td className="pg-shrink text-ink-2" data-label={`${t("discovery.import.basis")}:`}>
                <span className="pg-trunc max-w-44 xl:max-w-none" title={t(reasonLabels[c.reason])}>
                  {t(reasonLabels[c.reason])}
                </span>
              </td>
              <td className="pg-shrink">
                {monitored ? (
                  <Link href={`/monitors/${c.monitor_id}`} className="pg-state text-ink-2 hover:text-ink">
                    <StatusGlyph kind="up" />
                    {t("discovery.candidates.monitored")}
                    <OpenIcon width={13} height={13} aria-hidden />
                  </Link>
                ) : c.no_endpoints ? (
                  <span className="pg-state text-warn-ink" title={t("discovery.import.noEndpointsTitle")}>
                    <StatusGlyph kind="warn" />
                    {t("discovery.import.noEndpoints")}
                  </span>
                ) : (
                  <span className="text-ink-3">{canEdit ? t("discovery.import.notMonitored") : t("discovery.candidates.notMonitoredViewer")}</span>
                )}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );

  if (!canEdit) return <div className="panel pg-table-panel">{table}</div>;

  return (
    <form action={action} className="panel pg-table-panel">
      <input type="hidden" name="namespace" value={namespace} />
      {table}
      <div className="pg-savebar pg-savebar-sticky pg-importbar">
        <div className="pg-importbar-fields">
          <label>
            <span className="pg-label">{t("discovery.import.interval")}</span>
            <span className="pg-unit">
              <input
                name="interval_seconds"
                type="number"
                min={1}
                value={every}
                onChange={(e) => setEvery(e.target.value)}
                required
                className="field"
              />
              <span>{t("discovery.import.seconds")}</span>
            </span>
          </label>
          <label>
            <span className="pg-label">{t("discovery.import.timeout")}</span>
            <span className="pg-unit">
              <input
                name="timeout_seconds"
                type="number"
                min={1}
                value={limit}
                onChange={(e) => setLimit(e.target.value)}
                required
                className="field"
              />
              <span>{t("discovery.import.seconds")}</span>
            </span>
          </label>
          <label>
            <span className="pg-label">{t("discovery.import.threshold")}</span>
            <span className="pg-unit">
              <input
                name="failure_threshold"
                type="number"
                min={1}
                value={threshold}
                onChange={(e) => setThreshold(e.target.value)}
                required
                className="field"
              />
              <span>{t("discovery.import.checks")}</span>
            </span>
          </label>
        </div>
        <p className="pg-importbar-echo" aria-live="polite">
          {echo()}
        </p>
        {state && !pending && (
          <p role={state.error ? "alert" : "status"} className={state.error ? "" : "pg-importbar-done"}>
            {state.error ??
              `${t.n("discovery.import.created", state.created)}${state.skipped > 0 ? `, ${t.n("discovery.import.skipped", state.skipped)}` : ""}.`}
          </p>
        )}
        <div className="pg-savebar-end">
          <button type="submit" disabled={pending || chosen.length === 0} className="btn btn-primary">
            {pending ? t("discovery.import.adding") : t.n("discovery.import.submit", chosen.length)}
          </button>
        </div>
      </div>
    </form>
  );
}
