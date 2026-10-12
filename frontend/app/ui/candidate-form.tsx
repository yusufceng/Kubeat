"use client";

import Link from "next/link";
import { useActionState, useState } from "react";
import { importCandidatesAction, type ImportState } from "@/app/actions";
import type { Candidate } from "@/lib/api";

const kindLabels: Record<Candidate["kind"], string> = {
  route: "Route",
  ingress: "Ingress",
  service: "Service",
};

const reasonLabels: Record<Candidate["reason"], string> = {
  "readiness-probe": "Readiness probe'un yolu",
  "liveness-probe": "Liveness probe'un yolu",
  "tcp-fallback": "HTTP probe yok, port bağlantısı",
  "route-host": "Route adresi",
  "ingress-rule": "Ingress kuralı",
};

const inputClass =
  "w-full rounded-md border border-black/15 bg-transparent px-3 py-2 text-sm outline-none focus:border-emerald-600 dark:border-white/20";

export function CandidateForm({
  namespace,
  candidates,
}: {
  namespace: string;
  candidates: Candidate[];
}) {
  const [state, action, pending] = useActionState<ImportState, FormData>(
    importCandidatesAction,
    null,
  );
  const available = candidates.filter((c) => c.monitor_id === null);
  // Everything not yet monitored starts selected.
  const [selected, setSelected] = useState(() => new Set(available.map((c) => c.source)));

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
      <div className="overflow-x-auto rounded-lg border border-black/10 dark:border-white/15">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-black/10 text-xs text-zinc-500 dark:border-white/15">
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
          <tbody className="divide-y divide-black/5 dark:divide-white/10">
            {candidates.map((c) => {
              const monitored = c.monitor_id !== null;
              return (
                <tr key={c.source} className={monitored ? "text-zinc-500" : undefined}>
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
                    <span className="mr-2 rounded bg-black/5 px-1.5 py-0.5 text-xs dark:bg-white/10">
                      {kindLabels[c.kind]}
                    </span>
                    <span className="font-medium">
                      {c.name}
                      {c.port > 0 && `:${c.port}`}
                    </span>
                  </td>
                  <td className="px-4 py-2">
                    <span className="mr-2 text-xs uppercase text-zinc-500">{c.type}</span>
                    <span className="break-all font-mono text-xs">{c.target}</span>
                  </td>
                  <td className="px-4 py-2 text-xs text-zinc-500">{reasonLabels[c.reason]}</td>
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
          <span className="mb-1 block text-xs font-medium text-zinc-500">Kontrol aralığı (sn)</span>
          <input
            name="interval_seconds"
            type="number"
            min={1}
            defaultValue={30}
            required
            className={inputClass}
          />
        </label>
        <label className="sm:col-span-1">
          <span className="mb-1 block text-xs font-medium text-zinc-500">Zaman aşımı (sn)</span>
          <input
            name="timeout_seconds"
            type="number"
            min={1}
            defaultValue={5}
            required
            className={inputClass}
          />
        </label>
        <label className="sm:col-span-1">
          <span className="mb-1 block text-xs font-medium text-zinc-500">Alarm eşiği</span>
          <input
            name="failure_threshold"
            type="number"
            min={1}
            defaultValue={3}
            required
            className={inputClass}
          />
        </label>
        <div className="flex items-end sm:col-span-3">
          <button
            type="submit"
            disabled={pending || chosen.length === 0}
            className="w-full rounded-md bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-60"
          >
            {pending ? "Ekleniyor…" : `Seçilen ${chosen.length} hedefi izlemeye al`}
          </button>
        </div>
      </div>

      {state && !pending && (
        <p
          role="status"
          className={`text-sm ${
            state.error ? "text-red-600 dark:text-red-400" : "text-emerald-700 dark:text-emerald-400"
          }`}
        >
          {state.error ??
            `${state.created} monitor eklendi${state.skipped > 0 ? `, ${state.skipped} hedef atlandı` : ""}.`}
        </p>
      )}
    </form>
  );
}
