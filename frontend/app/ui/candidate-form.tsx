"use client";

import Link from "next/link";
import { useActionState, useState } from "react";
import { importCandidatesAction, type ImportState } from "@/app/actions";
import type { Candidate } from "@/lib/api";
import { typeLabel } from "@/lib/monitor-types";

const kindLabels: Record<Candidate["kind"], string> = {
  route: "Route",
  ingress: "Ingress",
  service: "Service",
  deployment: "Deployment",
  statefulset: "StatefulSet",
  daemonset: "DaemonSet",
};

const workloadKinds: Candidate["kind"][] = ["deployment", "statefulset", "daemonset"];

const reasonLabels: Record<Candidate["reason"], string> = {
  "readiness-probe": "Readiness probe'undan",
  "liveness-probe": "Liveness probe'undan",
  "tcp-fallback": "HTTP ya da gRPC probe yok, port bağlantısı",
  "route-host": "Route adresi",
  "ingress-rule": "Ingress kuralı",
  workload: "Hazır pod sayısı",
};

const inputClass = "field";

export function CandidateForm({
  namespace,
  candidates,
  defaults,
}: {
  namespace: string;
  candidates: Candidate[];
  /** What the imported monitors start with; the same as a monitor made by hand. */
  defaults: { interval: number; timeout: number; failureThreshold: number };
}) {
  const [state, action, pending] = useActionState<ImportState, FormData>(
    importCandidatesAction,
    null,
  );
  const available = candidates.filter((c) => c.monitor_id === null);
  // What is reachable and not yet monitored starts selected. Workloads are
  // offered without being selected: they say how many pods are ready, which
  // adds to a check of the service in front of them rather than replacing it.
  const [selected, setSelected] = useState(
    () => new Set(available.filter((c) => !workloadKinds.includes(c.kind)).map((c) => c.source)),
  );

  // Sources that have since been imported no longer count.
  const chosen = available.filter((c) => selected.has(c.source));
  const allChosen = available.length > 0 && chosen.length === available.length;

  const toggle = (source: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (!next.delete(source)) next.add(source);
      return next;
    });

  return (
    <form action={action} className="flex flex-col gap-4">
      <input type="hidden" name="namespace" value={namespace} />
      <div className="card overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-line text-xs text-muted">
            <tr>
              <th className="w-10 px-4 py-2">
                <input
                  type="checkbox"
                  aria-label="Tümünü seç"
                  checked={allChosen}
                  disabled={available.length === 0}
                  onChange={() =>
                    setSelected(allChosen ? new Set() : new Set(available.map((c) => c.source)))
                  }
                />
              </th>
              <th className="px-4 py-2 font-medium">Kaynak</th>
              <th className="px-4 py-2 font-medium">Önerilen kontrol</th>
              <th className="px-4 py-2 font-medium">Dayanak</th>
              <th className="px-4 py-2 font-medium">Durum</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {candidates.map((c) => {
              const monitored = c.monitor_id !== null;
              return (
                <tr key={c.source} className={monitored ? "text-muted" : undefined}>
                  <td className="px-4 py-2">
                    {!monitored && (
                      <input
                        type="checkbox"
                        name="source"
                        value={c.source}
                        aria-label={`${kindLabels[c.kind]} ${c.name}`}
                        checked={selected.has(c.source)}
                        onChange={() => toggle(c.source)}
                      />
                    )}
                  </td>
                  <td className="whitespace-nowrap px-4 py-2">
                    <span className="mr-2 rounded bg-sunken px-1.5 py-0.5 text-xs">
                      {kindLabels[c.kind]}
                    </span>
                    <span className="font-medium">
                      {c.name}
                      {c.port > 0 && `:${c.port}`}
                    </span>
                  </td>
                  <td className="px-4 py-2">
                    <span className="mr-2 text-xs text-muted">{typeLabel(c.type)}</span>
                    <span className="break-all font-mono text-xs">{c.target}</span>
                  </td>
                  <td className="px-4 py-2 text-xs text-muted">{reasonLabels[c.reason]}</td>
                  <td className="whitespace-nowrap px-4 py-2 text-xs">
                    {monitored ? (
                      <Link href={`/monitors/${c.monitor_id}`} className="hover:underline">
                        İzleniyor →
                      </Link>
                    ) : (
                      "İzlenmiyor"
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      <div className="grid gap-4 sm:grid-cols-6">
        <label className="sm:col-span-1">
          <span className="mb-1 block text-xs font-medium text-muted">Kontrol aralığı (sn)</span>
          <input
            name="interval_seconds"
            type="number"
            min={1}
            defaultValue={defaults.interval}
            required
            className={inputClass}
          />
        </label>
        <label className="sm:col-span-1">
          <span className="mb-1 block text-xs font-medium text-muted">Zaman aşımı (sn)</span>
          <input
            name="timeout_seconds"
            type="number"
            min={1}
            defaultValue={defaults.timeout}
            required
            className={inputClass}
          />
        </label>
        <label className="sm:col-span-1">
          <span className="mb-1 block text-xs font-medium text-muted">Alarm eşiği</span>
          <input
            name="failure_threshold"
            type="number"
            min={1}
            defaultValue={defaults.failureThreshold}
            required
            className={inputClass}
          />
        </label>
        <div className="flex items-end sm:col-span-3">
          <button
            type="submit"
            disabled={pending || chosen.length === 0}
            className="w-full btn btn-primary"
          >
            {pending ? "Ekleniyor…" : `Seçilen ${chosen.length} hedefi izlemeye al`}
          </button>
        </div>
      </div>

      {state && !pending && (
        <p
          role="status"
          className={`text-sm ${
            state.error ? "text-down" : "text-up"
          }`}
        >
          {state.error ??
            `${state.created} monitor eklendi${state.skipped > 0 ? `, ${state.skipped} hedef atlandı` : ""}.`}
        </p>
      )}
    </form>
  );
}
