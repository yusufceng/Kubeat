"use client";

import { useActionState, useState } from "react";
import {
  createMaintenanceAction,
  deleteMaintenanceAction,
  type MaintenanceFormState,
} from "@/app/actions";
import { duration } from "@/lib/format";
import { useMounted } from "@/lib/use-mounted";

const inputClass = "field";
const buttonClass = "btn";

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
  // The zone is the browser's; the server cannot know it.
  const mounted = useMounted();
  const timezone = mounted ? Intl.DateTimeFormat().resolvedOptions().timeZone : "";

  return (
    <form action={action} className="grid gap-4 sm:grid-cols-6">
      <input type="hidden" name="timezone" value={timezone} />
      <input
        type="hidden"
        name="starts_at"
        value={startsAt ? new Date(startsAt).toISOString() : ""}
      />
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">Açıklama</span>
        <input name="name" required placeholder="Sürüm geçişi" className={inputClass} />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">Kapsam</span>
        <select name="scope" defaultValue="all" className={inputClass}>
          {scopes.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">
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
        <span className="mb-1 block text-xs font-medium text-muted">Süre</span>
        <select name="duration_seconds" defaultValue={durations[1] ?? durations[0]} className={inputClass}>
          {durations.map((d) => (
            <option key={d} value={d}>
              {duration(d)}
            </option>
          ))}
        </select>
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">Tekrar</span>
        <select name="repeat" defaultValue="" className={inputClass}>
          <option value="">Tek seferlik</option>
          <option value="daily">Her gün aynı saatte</option>
          <option value="weekly">Her hafta aynı gün ve saatte</option>
          <option value="monthly">Her ay aynı gün ve saatte</option>
        </select>
        <span className="mt-1 block text-xs text-muted">
          Tekrarlayan pencere saatini {timezone || "tarayıcınızın"} saat diliminde korur.
        </span>
      </label>
      <div className="flex items-end gap-3 sm:col-span-2">
        <button
          type="submit"
          disabled={pending}
          className="btn btn-primary"
        >
          {pending ? "Ekleniyor…" : "Bakım penceresi ekle"}
        </button>
        {state.error && !pending && (
          <span role="alert" className="text-sm text-down">
            {state.error}
          </span>
        )}
      </div>
    </form>
  );
}

/**
 * Ends an active window, or removes an upcoming or past one. confirm, when
 * given, is asked first: deleting a repeating window ends all of it.
 */
export function DeleteMaintenanceButton({ id, label, confirm: question }: { id: number; label: string; confirm?: string }) {
  return (
    <form
      action={deleteMaintenanceAction}
      onSubmit={(e) => {
        if (question && !confirm(question)) e.preventDefault();
      }}
    >
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
        className="field w-auto"
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
        <span role="alert" className="text-sm text-down">
          {state.error}
        </span>
      )}
    </form>
  );
}
