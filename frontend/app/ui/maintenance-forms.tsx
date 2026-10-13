"use client";

import { ConfirmForm } from "@/app/ui/confirm";
import { useActionState, useState } from "react";
import {
  createMaintenanceAction,
  deleteMaintenanceAction,
  type MaintenanceFormState,
} from "@/app/actions";
import { duration } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
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
  const t = useT();
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
        <span className="mb-1 block text-xs font-medium text-muted">{t("maintenance.name")}</span>
        <input name="name" required placeholder={t("maintenance.form.namePlaceholder")} className={inputClass} />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">{t("maintenance.scope")}</span>
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
          {t("maintenance.form.start")}
        </span>
        <input
          type="datetime-local"
          value={startsAt}
          onChange={(e) => setStartsAt(e.target.value)}
          className={inputClass}
        />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">{t("maintenance.form.duration")}</span>
        <select name="duration_seconds" defaultValue={durations[1] ?? durations[0]} className={inputClass}>
          {durations.map((d) => (
            <option key={d} value={d}>
              {duration(d, t)}
            </option>
          ))}
        </select>
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">{t("maintenance.repeat")}</span>
        <select name="repeat" defaultValue="" className={inputClass}>
          <option value="">{t("maintenance.form.once")}</option>
          <option value="daily">{t("maintenance.form.daily")}</option>
          <option value="weekly">{t("maintenance.form.weekly")}</option>
          <option value="monthly">{t("maintenance.form.monthly")}</option>
        </select>
        <span className="mt-1 block text-xs text-muted">
          {timezone ? t("maintenance.form.repeatHint", { timezone }) : t("maintenance.form.repeatHintBrowser")}
        </span>
      </label>
      <div className="flex items-end gap-3 sm:col-span-2">
        <button
          type="submit"
          disabled={pending}
          className="btn btn-primary"
        >
          {pending ? t("maintenance.form.adding") : t("maintenance.form.submit")}
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
    <ConfirmForm action={deleteMaintenanceAction} question={question} confirmLabel={label}>
      <input type="hidden" name="id" value={id} />
      <button type="submit" className={buttonClass}>
        {label}
      </button>
    </ConfirmForm>
  );
}

/** Silences one monitor from now for a chosen length. */
export function SilenceForm({ monitorId, durations }: { monitorId: number; durations: number[] }) {
  const t = useT();
  const [state, action, pending] = useActionState(createMaintenanceAction, initialState);

  return (
    <form action={action} className="flex flex-wrap items-center gap-2">
      <input type="hidden" name="name" value={t("maintenance.silence.name")} />
      <input type="hidden" name="scope" value={`mon:${monitorId}`} />
      <select
        name="duration_seconds"
        defaultValue={durations[1] ?? durations[0]}
        aria-label={t("maintenance.silence.duration")}
        className="field w-auto"
      >
        {durations.map((d) => (
          <option key={d} value={d}>
            {duration(d, t)}
          </option>
        ))}
      </select>
      <button type="submit" disabled={pending} className={buttonClass}>
        {pending ? t("maintenance.silence.silencing") : t("maintenance.silence.submit")}
      </button>
      {state.error && !pending && (
        <span role="alert" className="text-sm text-down">
          {state.error}
        </span>
      )}
    </form>
  );
}
