"use client";

import { useActionState, useState } from "react";
import {
  createMaintenanceAction,
  deleteMaintenanceAction,
  type MaintenanceFormState,
} from "@/app/actions";
import { duration } from "@/lib/format";

const inputClass =
  "w-full rounded-md border border-black/15 bg-transparent px-3 py-2 text-sm outline-none focus:border-emerald-600 dark:border-white/20";
const buttonClass =
  "rounded-md border border-black/15 px-3 py-1.5 text-sm hover:bg-black/5 disabled:opacity-60 dark:border-white/20 dark:hover:bg-white/10";

const initialState: MaintenanceFormState = { error: null, created: false };

export type ScopeOption = { value: string; label: string };

/** Plans a maintenance window for everything, a namespace or one monitor. */
export function MaintenanceForm({
  scopes,
  durations,
}: {
  scopes: ScopeOption[];
  durations: number[];
}) {
  const [state, action, pending] = useActionState(createMaintenanceAction, initialState);
  // The picker gives a wall-clock time in the viewer's zone; the server gets
  // the instant it denotes.
  const [startsAt, setStartsAt] = useState("");

  return (
    <form action={action} className="grid gap-4 sm:grid-cols-6">
      <input
        type="hidden"
        name="starts_at"
        value={startsAt ? new Date(startsAt).toISOString() : ""}
      />
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-zinc-500">Açıklama</span>
        <input name="name" required placeholder="Sürüm geçişi" className={inputClass} />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-zinc-500">Kapsam</span>
        <select name="scope" defaultValue="all" className={inputClass}>
          {scopes.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-zinc-500">
          Başlangıç (boş bırakılırsa şimdi)
        </span>
        <input
          type="datetime-local"
          value={startsAt}
          onChange={(e) => setStartsAt(e.target.value)}
          className={inputClass}
        />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-zinc-500">Süre</span>
        <select name="duration_seconds" defaultValue={durations[1] ?? durations[0]} className={inputClass}>
          {durations.map((d) => (
            <option key={d} value={d}>
              {duration(d)}
            </option>
          ))}
        </select>
      </label>
      <div className="flex items-end gap-3 sm:col-span-4">
        <button
          type="submit"
          disabled={pending}
          className="rounded-md bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-60"
        >
          {pending ? "Ekleniyor…" : "Bakım penceresi ekle"}
        </button>
        {state.error && !pending && (
          <span role="alert" className="text-sm text-red-600 dark:text-red-400">
            {state.error}
          </span>
        )}
      </div>
    </form>
  );
}

/** Ends an active window, or removes an upcoming or past one. */
export function DeleteMaintenanceButton({ id, label }: { id: number; label: string }) {
  return (
    <form action={deleteMaintenanceAction}>
      <input type="hidden" name="id" value={id} />
      <button type="submit" className={buttonClass}>
        {label}
      </button>
    </form>
  );
}

/** Silences one monitor from now for a chosen length. */
export function SilenceForm({ monitorId, durations }: { monitorId: number; durations: number[] }) {
  const [state, action, pending] = useActionState(createMaintenanceAction, initialState);

  return (
    <form action={action} className="flex flex-wrap items-center gap-2">
      <input type="hidden" name="name" value="Susturma" />
      <input type="hidden" name="scope" value={`mon:${monitorId}`} />
      <select
        name="duration_seconds"
        defaultValue={durations[1] ?? durations[0]}
        aria-label="Susturma süresi"
        className="rounded-md border border-black/15 bg-transparent px-2 py-1.5 text-sm dark:border-white/20"
      >
        {durations.map((d) => (
          <option key={d} value={d}>
            {duration(d)}
          </option>
        ))}
      </select>
      <button type="submit" disabled={pending} className={buttonClass}>
        {pending ? "Susturuluyor…" : "Alarmı sustur"}
      </button>
      {state.error && !pending && (
        <span role="alert" className="text-sm text-red-600 dark:text-red-400">
          {state.error}
        </span>
      )}
    </form>
  );
}
