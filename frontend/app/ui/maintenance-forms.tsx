"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useActionState, useEffect, useState } from "react";
import {
  createMaintenanceAction,
  deleteMaintenanceAction,
  type MaintenanceFormState,
} from "@/app/actions";
import { clock, dayDistance, duration } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { useMounted } from "@/lib/use-mounted";
import { ConfirmForm } from "@/app/ui/confirm";
import { TrashIcon } from "@/app/ui/icons";

const initialState: MaintenanceFormState = { error: null, created: false };

/** A scope a window can have: "all", "ns:<namespace>" or "mon:<monitor id>", and how it reads. */
export type ScopeOption = { value: string; label: string };

/** The repeats a window can have, with the message key of each. */
const repeats = [
  { value: "", label: "maintenance.form.once", echo: "" },
  { value: "daily", label: "maintenance.repeat.daily", echo: "maintenance.echo.daily" },
  { value: "weekly", label: "maintenance.repeat.weekly", echo: "maintenance.echo.weekly" },
  { value: "monthly", label: "maintenance.repeat.monthly", echo: "maintenance.echo.monthly" },
] as const;

/** A day near now as people say it: "bugün", "yarın", or "Çar 22 Eki". */
function nearDay(date: Date, now: Date, t: T): string {
  const days = dayDistance(date, now);
  if (days === 0) return t("maintenance.echo.today");
  if (days === 1) return t("maintenance.echo.tomorrow");
  return date.toLocaleDateString(t.intl, { weekday: "short", day: "numeric", month: "short" });
}

/** "bugün 19:30 – 20:30", "bugün 23:00 – yarın 01:00": a window from its start and length. */
function windowRange(start: Date, seconds: number, now: Date, t: T): string {
  const end = new Date(start.getTime() + seconds * 1000);
  const from = `${nearDay(start, now, t)} ${clock(start, t)}`;
  const to = dayDistance(end, start) === 0 ? clock(end, t) : `${nearDay(end, now, t)} ${clock(end, t)}`;
  return `${from} – ${to}`;
}

/**
 * Plans a maintenance window for everything, a namespace or one monitor:
 * what it is and when in one section, what it covers in another, and a
 * sentence that reads the window back before it is saved. closeHref is
 * where Cancel and a successful save lead.
 */
export function MaintenanceForm({
  scopes,
  durations,
  closeHref,
}: {
  scopes: ScopeOption[];
  durations: number[];
  closeHref: string;
}) {
  const t = useT();
  const router = useRouter();
  const [state, action, pending] = useActionState(createMaintenanceAction, initialState);
  // Every field is kept here: React empties a form after its action, which
  // would throw away what was typed into a refused one.
  const [name, setName] = useState("");
  // The picker gives a wall-clock time in the viewer's zone; the server gets
  // the instant it denotes.
  const [startsAt, setStartsAt] = useState("");
  const [seconds, setSeconds] = useState(durations[1] ?? durations[0]);
  const [repeat, setRepeat] = useState<(typeof repeats)[number]["value"]>("");
  const [scope, setScope] = useState("all");
  // The zone and the clock are the browser's; the server knows neither.
  const mounted = useMounted();
  const timezone = mounted ? Intl.DateTimeFormat().resolvedOptions().timeZone : "";

  useEffect(() => {
    if (state.created) router.replace(closeHref);
  }, [state, router, closeHref]);

  const namespaces = scopes.filter((s) => s.value.startsWith("ns:"));
  const monitors = scopes.filter((s) => s.value.startsWith("mon:"));
  const all = scopes.find((s) => s.value === "all");

  function scopeWords(): React.ReactNode {
    if (scope.startsWith("ns:")) {
      const [before, after] = t("maintenance.echo.namespace").split("{namespace}");
      return (
        <>
          {before}
          <code>{scope.slice(3)}</code>
          {after}
        </>
      );
    }
    if (scope.startsWith("mon:")) {
      const label = monitors.find((m) => m.value === scope)?.label ?? "";
      return <b>{t("maintenance.echo.monitor", { monitor: label })}</b>;
    }
    return <b>{t("maintenance.echo.all")}</b>;
  }

  /** The window read back: when, over what, and how it repeats. Only the browser knows the time now. */
  function echo(): React.ReactNode {
    if (!mounted) return null;
    const now = new Date();
    const start = startsAt ? new Date(startsAt) : now;
    const [before, middle] = t("maintenance.echo.window").split("{range}");
    const [between, after] = middle.split("{scope}");
    const chosen = repeats.find((r) => r.value === repeat);
    const range = windowRange(start, seconds, now, t);
    return (
      <p className="pg-echo" aria-live="polite">
        {before}
        {/* The range opens the sentence in both languages. */}
        <b>{before ? range : range.charAt(0).toLocaleUpperCase(t.intl) + range.slice(1)}</b>
        {between}
        {scopeWords()}
        {after}
        {chosen?.echo && ` ${t(chosen.echo, { timezone })}`}
      </p>
    );
  }

  return (
    <form action={action} className="pg-form">
      <input type="hidden" name="timezone" value={timezone} />
      <input type="hidden" name="starts_at" value={startsAt ? new Date(startsAt).toISOString() : ""} />

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("maintenance.form.whenTitle")}</h2>
          <p>{t("maintenance.form.whenBody")}</p>
        </div>
        <div className="pg-fields">
          <div className="pg-grid">
            <label className="pg-span-3">
              <span className="pg-label">{t("maintenance.name")}</span>
              <input
                name="name"
                required
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t("maintenance.form.namePlaceholder")}
                className="field"
              />
            </label>
            <label className="pg-span-3">
              <span className="pg-label">
                {t("maintenance.start")}
                <small>{t("maintenance.form.startHint")}</small>
              </span>
              <input
                type="datetime-local"
                value={startsAt}
                onChange={(e) => setStartsAt(e.target.value)}
                className="field tabular-nums"
              />
            </label>
          </div>
          <fieldset>
            <legend className="pg-label">{t("maintenance.form.duration")}</legend>
            <div className="pg-seg">
              {durations.map((d) => (
                <label key={d}>
                  <input
                    type="radio"
                    name="duration_seconds"
                    value={d}
                    checked={seconds === d}
                    onChange={() => setSeconds(d)}
                  />
                  {duration(d, t)}
                </label>
              ))}
            </div>
          </fieldset>
          <fieldset>
            <legend className="pg-label">{t("maintenance.repeat")}</legend>
            <div className="pg-seg">
              {repeats.map((r) => (
                <label key={r.value}>
                  <input type="radio" name="repeat" value={r.value} checked={repeat === r.value} onChange={() => setRepeat(r.value)} />
                  {t(r.label)}
                </label>
              ))}
            </div>
            <span className="pg-help">
              {timezone ? t("maintenance.form.repeatHint", { timezone }) : t("maintenance.form.repeatHintBrowser")}
            </span>
          </fieldset>
        </div>
      </div>

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("maintenance.scope")}</h2>
          <p>{t("maintenance.form.scopeBody")}</p>
        </div>
        <div className="pg-fields">
          <label>
            <span className="pg-label">{t("maintenance.form.scopeLabel")}</span>
            <select name="scope" value={scope} onChange={(e) => setScope(e.target.value)} className="field">
              {all && <option value={all.value}>{all.label}</option>}
              {namespaces.length > 0 && (
                <optgroup label={t("maintenance.form.namespaces")}>
                  {namespaces.map((s) => (
                    <option key={s.value} value={s.value}>
                      {s.label}
                    </option>
                  ))}
                </optgroup>
              )}
              {monitors.length > 0 && (
                <optgroup label={t("maintenance.form.monitors")}>
                  {monitors.map((s) => (
                    <option key={s.value} value={s.value}>
                      {s.label}
                    </option>
                  ))}
                </optgroup>
              )}
            </select>
          </label>
          {echo()}
        </div>
      </div>

      <div className="pg-savebar">
        {state.error && !pending && <p role="alert">{state.error}</p>}
        <div className="pg-savebar-end">
          <Link href={closeHref} className="btn btn-ghost">
            {t("common.cancel")}
          </Link>
          <button type="submit" disabled={pending} className="btn btn-primary">
            {pending ? t("maintenance.form.adding") : t("maintenance.form.submit")}
          </button>
        </div>
      </div>
    </form>
  );
}

/**
 * What can be done to a window from its row: end it while it runs, or
 * delete it (a repeating one with all its occurrences). Each asks first,
 * naming the window and what it covers.
 */
export function MaintenanceRowAction({
  id,
  name,
  scope,
  kind,
}: {
  id: number;
  name: string;
  /** How the window's scope reads, for the question. */
  scope: string;
  kind: "end" | "delete" | "deleteSeries";
}) {
  const t = useT();
  const words = {
    end: { button: "maintenance.endNow", title: "maintenance.endNowTitle", body: "maintenance.endNowBody" },
    delete: { button: "common.delete", title: "maintenance.deleteTitle", body: "maintenance.deleteBody" },
    deleteSeries: { button: "maintenance.deleteSeries", title: "maintenance.deleteSeriesTitle", body: "maintenance.deleteSeriesConfirm" },
  }[kind];
  return (
    <ConfirmForm
      action={deleteMaintenanceAction}
      title={t(words.title, { name })}
      question={t(words.body, { name })}
      items={[`${name} · ${scope}`]}
      icon={kind === "end" ? undefined : <TrashIcon />}
      confirmLabel={t(words.button)}
    >
      <input type="hidden" name="id" value={id} />
      {kind === "end" ? (
        <button type="submit" className="btn btn-ghost btn-sm" aria-label={`${t(words.button)}: ${name}`}>
          {t(words.button)}
        </button>
      ) : (
        <button type="submit" className="pg-icon-btn pg-icon-btn-danger" aria-label={`${t(words.button)}: ${name}`} title={t(words.button)}>
          <TrashIcon />
        </button>
      )}
    </ConfirmForm>
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
      <button type="submit" className="btn btn-ghost btn-sm">
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
      <button type="submit" disabled={pending} className="btn">
        {pending ? t("maintenance.silence.silencing") : t("maintenance.silence.submit")}
      </button>
      {state.error && !pending && (
        <span role="alert" className="text-sm text-down-ink">
          {state.error}
        </span>
      )}
    </form>
  );
}
