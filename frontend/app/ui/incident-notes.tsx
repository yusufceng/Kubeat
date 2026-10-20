"use client";

import { useActionState, useId, useState, useTransition } from "react";
import { createNoteAction, deleteNoteAction, type NoteOutcome, updateNoteAction } from "@/app/(app)/(pages)/incidents/[id]/actions";
import type { IncidentNote } from "@/lib/api";
import { NOTE_MAX_LENGTH } from "@/lib/incident-timeline";
import { useT } from "@/lib/i18n/client";
import { ConfirmDialog } from "@/app/ui/confirm";
import { EditIcon, TrashIcon } from "@/app/ui/icons";
import { Moment } from "@/app/ui/incident-time";

/** Characters as the backend counts them: an emoji is one. */
const length = (text: string) => [...text.replace(/\r\n/g, "\n").trim()].length;

/** The text of a note and whether status pages may show it: for a new note and for a change. */
function NoteFields({
  id,
  text,
  setText,
  isPublic,
  autoFocus = false,
}: {
  id: string;
  text: string;
  setText: (text: string) => void;
  isPublic: boolean;
  autoFocus?: boolean;
}) {
  const t = useT();
  const count = length(text);
  const hintId = useId();
  return (
    <>
      <textarea
        id={id}
        name="text"
        className="field inc-note-field"
        rows={3}
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={t("incident.note.placeholder")}
        aria-describedby={hintId}
        autoFocus={autoFocus}
      />
      <div className="inc-note-row">
        <label className="inc-check">
          <input type="checkbox" name="show_on_status_page" defaultChecked={isPublic} />
          <span>
            {t("incident.note.public")}
            <small>{t("incident.note.public.hint")}</small>
          </span>
        </label>
        {/* Read with the field; said aloud only once the note is too long. */}
        <span id={hintId} className={`inc-count ${count > NOTE_MAX_LENGTH ? "text-down-ink" : ""}`}>
          {t("incident.note.count", { count: count.toLocaleString(t.intl), max: NOTE_MAX_LENGTH.toLocaleString(t.intl) })}
        </span>
        <span role="status" className="sr-only">
          {count > NOTE_MAX_LENGTH ? t("incident.note.tooLong", { max: NOTE_MAX_LENGTH.toLocaleString(t.intl) }) : ""}
        </span>
      </div>
    </>
  );
}

/**
 * The form at the end of the timeline that adds a note. It empties itself
 * once the note is saved; the note then shows in the timeline above.
 */
export function NoteComposer({ incidentId }: { incidentId: number }) {
  const t = useT();
  const [text, setText] = useState("");
  // Counts the notes saved here, so that the next one starts with the box unticked.
  const [round, setRound] = useState(0);
  const [state, action, pending] = useActionState<NoteOutcome, FormData>(async (prev, formData) => {
    const outcome = await createNoteAction(incidentId, prev, formData);
    if (outcome && "saved" in outcome) {
      setText("");
      setRound((r) => r + 1);
    }
    return outcome;
  }, undefined);
  const count = length(text);
  return (
    <form action={action} className="inc-composer" aria-labelledby="note-label">
      <label id="note-label" htmlFor="note" className="inc-composer-label">
        {t("incident.note.label")}
      </label>
      <NoteFields key={round} id="note" text={text} setText={setText} isPublic={false} />
      {state && "error" in state && (
        <p role="alert" className="inc-form-error">
          {state.error}
        </p>
      )}
      <div className="inc-composer-actions">
        <button type="submit" className="btn btn-primary" disabled={pending || count === 0 || count > NOTE_MAX_LENGTH}>
          {pending ? t("common.saving") : t("incident.note.add")}
        </button>
      </div>
    </form>
  );
}

/**
 * A note in the timeline: who wrote it, the text with its line breaks, and
 * for its author or an administrator the means to change or delete it.
 */
export function NoteBody({ note, incidentId }: { note: IncidentNote; incidentId: number }) {
  const t = useT();
  const [editing, setEditing] = useState(false);
  const [asking, setAsking] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [deleting, startDelete] = useTransition();
  const edited = note.updated_at !== note.created_at;

  return (
    <div className="inc-note" id={`note-${note.id}`}>
      <p className="inc-tl-title">
        <span className="font-semibold">{note.author}</span>
        {edited && (
          <span className="inc-muted" title={new Date(note.updated_at).toISOString()}>
            {t("incident.note.edited")}
          </span>
        )}
        {note.show_on_status_page && <span className="inc-tag">{t("incident.note.public.tag")}</span>}
        {note.can_edit && !editing && (
          <span className="inc-note-tools">
            <button
              type="button"
              className="btn btn-ghost btn-sm inc-icon-btn"
              onClick={() => setEditing(true)}
              aria-label={t("incident.note.edit")}
              title={t("incident.note.edit")}
            >
              <EditIcon width={14} height={14} />
            </button>
            <button
              type="button"
              className="btn btn-ghost btn-sm inc-icon-btn"
              onClick={() => setAsking(true)}
              disabled={deleting}
              aria-label={t("incident.note.delete")}
              title={t("incident.note.delete")}
            >
              <TrashIcon width={14} height={14} />
            </button>
          </span>
        )}
      </p>
      {editing ? (
        <NoteEditor note={note} incidentId={incidentId} onDone={() => setEditing(false)} />
      ) : (
        <p className="inc-note-text">{note.text}</p>
      )}
      {deleteError && (
        <p role="alert" className="inc-form-error">
          {deleteError}
        </p>
      )}
      {asking && (
        <ConfirmDialog
          title={t("incident.note.delete.title")}
          confirmLabel={t("incident.note.delete")}
          onCancel={() => setAsking(false)}
          onConfirm={() => {
            setAsking(false);
            setDeleteError(null);
            startDelete(async () => {
              const outcome = await deleteNoteAction(incidentId, note.id);
              if (outcome && "error" in outcome) setDeleteError(outcome.error);
            });
          }}
        >
          <p>{t("incident.note.delete.body", { author: note.author })}</p>
          <p className="inc-quote">
            <Moment iso={note.created_at} /> · {note.text}
          </p>
        </ConfirmDialog>
      )}
    </div>
  );
}

/** A note's text being changed in place; saving or cancelling closes it. */
function NoteEditor({ note, incidentId, onDone }: { note: IncidentNote; incidentId: number; onDone: () => void }) {
  const t = useT();
  const [text, setText] = useState(note.text);
  const [state, action, pending] = useActionState<NoteOutcome, FormData>(async (prev, formData) => {
    const outcome = await updateNoteAction(incidentId, note.id, prev, formData);
    if (outcome && "saved" in outcome) onDone();
    return outcome;
  }, undefined);
  const count = length(text);
  const id = useId();
  return (
    <form action={action} className="inc-note-edit">
      <label htmlFor={id} className="sr-only">
        {t("incident.note.edit")}
      </label>
      <NoteFields id={id} text={text} setText={setText} isPublic={note.show_on_status_page} autoFocus />
      {state && "error" in state && (
        <p role="alert" className="inc-form-error">
          {state.error}
        </p>
      )}
      <div className="inc-composer-actions">
        <button type="button" className="btn btn-ghost" onClick={onDone}>
          {t("common.cancel")}
        </button>
        <button type="submit" className="btn btn-primary" disabled={pending || count === 0 || count > NOTE_MAX_LENGTH}>
          {pending ? t("common.saving") : t("common.save")}
        </button>
      </div>
    </form>
  );
}
