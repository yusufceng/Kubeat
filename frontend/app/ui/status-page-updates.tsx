"use client";

import { useEffect, useId, useState, useTransition } from "react";
import { createUpdateAction, deleteUpdateAction, editUpdateAction } from "@/app/(app)/(pages)/status-pages/actions";
import type { StatusPageUpdate, StatusPageUpdateKind, StatusPageUpdateSettings } from "@/lib/api";
import { compactTime, span } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { byDay, UPDATE_BODY_MAX, UPDATE_TITLE_MAX_BYTES, updateKinds, updateMarks, utf8Length } from "@/lib/status-page";
import { useMounted } from "@/lib/use-mounted";
import { ConfirmDialog } from "@/app/ui/confirm";
import { EditIcon, PlusIcon, TrashIcon } from "@/app/ui/icons";
import { NoteIcon } from "@/app/ui/incident-icons";
import { Clock, DayLabel } from "@/app/ui/incident-time";
import { AnnounceIcon } from "@/app/ui/page-icons";
import { StatusGlyph } from "@/app/ui/status-glyph";

/** An incident an update can point at: one of a monitor the page shows. */
export type IncidentOption = { id: number; monitor: string; started_at: string; resolved_at: string | null };

/** The message key of a kind's name. */
export const kindLabel = (kind: StatusPageUpdateKind | "note") => `statusPage.kind.${kind}`;

/** How a kind is drawn: its state's glyph, or an icon for a notice and a note. */
export function KindMark({ kind, size = 10 }: { kind: StatusPageUpdateKind | "note"; size?: 10 | 12 | 16 }) {
  const mark = updateMarks[kind];
  if (mark === "notice") return <AnnounceIcon width={size + 4} height={size + 4} />;
  if (mark === "note") return <NoteIcon width={size + 4} height={size + 4} />;
  return <StatusGlyph kind={mark} size={size} />;
}

/** "pricing · 14 Eki 19:11 · 12 dk" / "… · sürüyor": an incident as the picker names it. */
function incidentWords(i: IncidentOption, now: number, t: T): string {
  const when = compactTime(new Date(i.started_at), new Date(now), t);
  const how = i.resolved_at ? span(Date.parse(i.resolved_at) - Date.parse(i.started_at), t) : t("statusPage.incidents.ongoing");
  return `${i.monitor} · ${when} · ${how}`;
}

/** The fields of an update, for a new one and for a change: kind, title, body, and the incident it is about. */
function UpdateFields({
  draft,
  setDraft,
  incidents,
  autoFocus = false,
}: {
  draft: StatusPageUpdateSettings;
  setDraft: (change: Partial<StatusPageUpdateSettings>) => void;
  incidents: IncidentOption[];
  autoFocus?: boolean;
}) {
  const t = useT();
  const ids = useId();
  // The clock and the zone are the browser's: the incidents' moments are
  // worded once it is known (as app/ui/live-duration.tsx reads the clock).
  const [now, setNow] = useState<number | null>(null);
  useEffect(() => {
    const arrived = () => setNow(Date.now());
    arrived();
  }, []);
  const titleBytes = utf8Length(draft.title);
  const bodyLength = [...(draft.body ?? "")].length;
  // The incident of a saved update may be older than the picker's list; it is still offered, by its number.
  const known = incidents.some((i) => i.id === draft.incident_id);
  return (
    <>
      <fieldset>
        <legend className="pg-label">{t("statusPage.update.kind")}</legend>
        <div className="pg-seg pg-kinds">
          {updateKinds.map((kind) => (
            <label key={kind}>
              <input type="radio" name={`${ids}-kind`} value={kind} checked={draft.kind === kind} onChange={() => setDraft({ kind })} />
              <KindMark kind={kind} />
              {t(kindLabel(kind))}
            </label>
          ))}
        </div>
      </fieldset>
      <div className="pg-grid">
        <label className="pg-span-3">
          <span className="pg-label">{t("statusPage.update.title")}</span>
          <input
            value={draft.title}
            onChange={(e) => setDraft({ title: e.target.value })}
            required
            autoFocus={autoFocus}
            placeholder={t("statusPage.update.titlePlaceholder")}
            className="field"
            aria-describedby={`${ids}-title-hint`}
          />
          <span id={`${ids}-title-hint`} className={`pg-help ${titleBytes > UPDATE_TITLE_MAX_BYTES ? "text-down-ink" : ""}`}>
            {t("statusPage.update.titleCount", { count: titleBytes.toLocaleString(t.intl), max: UPDATE_TITLE_MAX_BYTES })}
          </span>
        </label>
        <label className="pg-span-3">
          <span className="pg-label">
            {t("statusPage.update.incident")}
            <small>{t("form.optional")}</small>
          </span>
          <select className="field" value={draft.incident_id ?? 0} onChange={(e) => setDraft({ incident_id: Number(e.target.value) })}>
            <option value={0}>{t("statusPage.update.noIncident")}</option>
            {!known && draft.incident_id ? <option value={draft.incident_id}>{t("statusPage.update.incidentNumber", { id: draft.incident_id })}</option> : null}
            {incidents.map((i) => (
              <option key={i.id} value={i.id}>
                {now !== null ? incidentWords(i, now, t) : `${i.monitor} · ${i.started_at.slice(0, 16).replace("T", " ")} UTC`}
              </option>
            ))}
          </select>
          <span className="pg-help">{t("statusPage.update.incidentHint")}</span>
        </label>
      </div>
      <label>
        <span className="pg-label">
          {t("statusPage.update.body")}
          <small>{t("form.optional")}</small>
        </span>
        <textarea
          value={draft.body ?? ""}
          onChange={(e) => setDraft({ body: e.target.value })}
          rows={4}
          placeholder={t("statusPage.update.bodyPlaceholder")}
          className="field"
          aria-describedby={`${ids}-body-hint`}
        />
        <span id={`${ids}-body-hint`} className={`pg-help ${bodyLength > UPDATE_BODY_MAX ? "text-down-ink" : ""}`}>
          {t("statusPage.update.bodyCount", { count: bodyLength.toLocaleString(t.intl), max: UPDATE_BODY_MAX.toLocaleString(t.intl) })}
        </span>
      </label>
    </>
  );
}

/** Whether a draft can be saved: a title within bounds, a body within bounds. */
function valid(draft: StatusPageUpdateSettings): boolean {
  const title = draft.title.trim();
  return title.length > 0 && utf8Length(title) <= UPDATE_TITLE_MAX_BYTES && [...(draft.body ?? "")].length <= UPDATE_BODY_MAX;
}

/**
 * The form that writes a new update or changes one: an update is a kind,
 * a title, a body and, when it is about an outage, the incident. Saving
 * or cancelling closes it.
 */
function UpdateEditor({
  pageId,
  update,
  incidents,
  onDone,
}: {
  pageId: number;
  /** The update being changed; absent for a new one. */
  update?: StatusPageUpdate;
  incidents: IncidentOption[];
  onDone: () => void;
}) {
  const t = useT();
  const [draft, setDraftState] = useState<StatusPageUpdateSettings>({
    kind: update?.kind ?? "investigating",
    title: update?.title ?? "",
    body: update?.body ?? "",
    incident_id: update?.incident_id ?? 0,
  });
  const setDraft = (change: Partial<StatusPageUpdateSettings>) => setDraftState((prev) => ({ ...prev, ...change }));
  const [error, setError] = useState<string | null>(null);
  const [pending, start] = useTransition();

  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!valid(draft)) return;
    const settings: StatusPageUpdateSettings = { kind: draft.kind, title: draft.title.trim(), body: draft.body ?? "", incident_id: draft.incident_id ?? 0 };
    setError(null);
    start(async () => {
      const outcome = update ? await editUpdateAction(pageId, update.id, settings) : await createUpdateAction(pageId, settings);
      if (outcome?.error) {
        setError(outcome.error);
        return;
      }
      onDone();
    });
  }

  return (
    <form onSubmit={submit} className="pg-update-editor" aria-label={update ? t("statusPage.update.editTitle") : t("statusPage.update.newTitle")}>
      <UpdateFields draft={draft} setDraft={setDraft} incidents={incidents} autoFocus />
      <div className="pg-update-actions">
        {error && <p role="alert">{error}</p>}
        <button type="button" className="btn btn-ghost" onClick={onDone}>
          {t("common.cancel")}
        </button>
        <button type="submit" className="btn btn-primary" disabled={pending || !valid(draft)}>
          {pending ? t("common.saving") : update ? t("common.save") : t("statusPage.update.publish")}
        </button>
      </div>
    </form>
  );
}

/** One update in the editor's list: when, what kind, the title and body, who wrote it, and the means to change or delete it. */
function UpdateRow({ pageId, update, incidents }: { pageId: number; update: StatusPageUpdate; incidents: IncidentOption[] }) {
  const t = useT();
  const [editing, setEditing] = useState(false);
  const [asking, setAsking] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [deleting, startDelete] = useTransition();
  const incident = update.incident_id ? incidents.find((i) => i.id === update.incident_id) : null;
  const edited = update.updated_at !== update.created_at;

  return (
    <li className="pg-update">
      <span className="pg-update-time">
        <Clock iso={update.created_at} seconds={false} />
      </span>
      <span className={`pg-update-mark pg-update-mark-${updateMarks[update.kind]}`} aria-hidden>
        <KindMark kind={update.kind} />
      </span>
      <div className="pg-update-body">
        {editing ? (
          <UpdateEditor pageId={pageId} update={update} incidents={incidents} onDone={() => setEditing(false)} />
        ) : (
          <>
            <p className="pg-update-title">
              <b>{update.title}</b>
              <span className="pg-tag">{t(kindLabel(update.kind))}</span>
              {update.incident_id ? (
                <span className="pg-update-incident">{incident ? incident.monitor : t("statusPage.update.incidentNumber", { id: update.incident_id })}</span>
              ) : null}
            </p>
            {update.body && <p className="pg-update-text">{update.body}</p>}
            <p className="pg-update-meta">
              {update.created_by}
              {edited && ` · ${t("statusPage.update.edited")}`}
            </p>
            <span className="pg-update-tools">
              <button type="button" className="pg-icon-btn" onClick={() => setEditing(true)} aria-label={`${t("common.edit")}: ${update.title}`} title={t("common.edit")}>
                <EditIcon />
              </button>
              <button type="button" className="pg-icon-btn pg-icon-btn-danger" disabled={deleting} onClick={() => setAsking(true)} aria-label={`${t("common.delete")}: ${update.title}`} title={t("common.delete")}>
                <TrashIcon />
              </button>
            </span>
          </>
        )}
        {error && (
          <p role="alert" className="pg-update-error">
            {error}
          </p>
        )}
      </div>
      {asking && (
        <ConfirmDialog
          title={t("statusPage.update.delete.title", { title: update.title })}
          confirmLabel={t("common.delete")}
          onCancel={() => setAsking(false)}
          onConfirm={() => {
            setAsking(false);
            setError(null);
            startDelete(async () => {
              const outcome = await deleteUpdateAction(pageId, update.id);
              if (outcome?.error) setError(outcome.error);
            });
          }}
        >
          {t("statusPage.update.delete.body")}
        </ConfirmDialog>
      )}
    </li>
  );
}

/**
 * The updates a page has told its visitors, newest first by day, and the
 * means to write, change and delete them. An incident note flagged for the
 * page also shows to visitors, but is written on the incident's page, so
 * it is not listed here.
 */
export function StatusPageUpdates({ pageId, updates, incidents }: { pageId: number; updates: StatusPageUpdate[] | null; incidents: IncidentOption[] }) {
  const t = useT();
  const mounted = useMounted();
  const [composing, setComposing] = useState(false);
  const days = byDay(updates ?? [], mounted);

  return (
    <section className="pg-updates" aria-labelledby={`updates-${pageId}`}>
      <div className="pg-updates-head">
        <div>
          <h3 id={`updates-${pageId}`}>{t("statusPage.updates.title")}</h3>
          <p>{updates === null ? t("statusPage.updates.error") : updates.length === 0 ? t("statusPage.updates.none") : t.n("statusPage.updates.count", updates.length)}</p>
        </div>
        {!composing && (
          <button type="button" className="btn btn-sm" onClick={() => setComposing(true)}>
            <PlusIcon width={14} height={14} />
            {t("statusPage.update.new")}
          </button>
        )}
      </div>
      {composing && (
        <div className="pg-updates-compose">
          <UpdateEditor pageId={pageId} incidents={incidents} onDone={() => setComposing(false)} />
        </div>
      )}
      {days.length > 0 && (
        <div className="pg-updates-list">
          {days.map((day) => (
            <div key={day.day}>
              <h4 className="pg-updates-day">
                <DayLabel iso={day.updates[0].created_at} />
              </h4>
              <ol>
                {day.updates.map((u) => (
                  <UpdateRow key={u.id} pageId={pageId} update={u} incidents={incidents} />
                ))}
              </ol>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
