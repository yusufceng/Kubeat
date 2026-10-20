"use server";

import { refresh } from "next/cache";
import { ApiError, createIncidentNote, deleteIncidentNote, updateIncidentNote } from "@/lib/api";
import { NOTE_MAX_LENGTH } from "@/lib/incident-timeline";
import { getT } from "@/lib/i18n/server";

/** What a note's form hears back: nothing once saved, else what was wrong. */
export type NoteOutcome = { error: string } | { saved: number } | undefined;

const isId = (n: number) => Number.isInteger(n) && n > 0;

/** A note's text as the backend will keep it, or what is wrong with it. */
async function noteText(formData: FormData): Promise<{ text: string } | { error: string }> {
  const t = await getT();
  // Line breaks are kept, as the backend keeps them: plain, the ends trimmed.
  const text = String(formData.get("text") ?? "")
    .replace(/\r\n/g, "\n")
    .trim();
  if (!text) return { error: t("incident.note.empty") };
  // Counted in characters, as the backend counts: an emoji is one.
  if ([...text].length > NOTE_MAX_LENGTH) return { error: t("incident.note.tooLong", { max: NOTE_MAX_LENGTH }) };
  return { text };
}

async function describe(err: unknown): Promise<string> {
  const t = await getT();
  if (err instanceof ApiError) {
    if (err.status === 400) return t("incident.note.error.invalid", { message: err.message });
    if (err.status === 401) return t("incident.note.error.session");
    if (err.status === 403) return t("incident.note.error.forbidden");
    if (err.status === 404) return t("incident.note.error.gone");
  }
  return t("incident.note.error.backend");
}

/**
 * Adds a note to an incident. The backend decides who may: those who may
 * change the monitor. saved counts the saves, so that the form can tell
 * one from the next and empty itself each time.
 */
export async function createNoteAction(incidentId: number, prev: NoteOutcome, formData: FormData): Promise<NoteOutcome> {
  if (!isId(incidentId)) return { error: await describe(new ApiError(404, "incident not found")) };
  const text = await noteText(formData);
  if ("error" in text) return text;
  try {
    await createIncidentNote(incidentId, { text: text.text, show_on_status_page: formData.get("show_on_status_page") === "on" });
  } catch (err) {
    return { error: await describe(err) };
  }
  refresh();
  return { saved: (prev && "saved" in prev ? prev.saved : 0) + 1 };
}

/** Changes a note's text and whether status pages show it; its author or an administrator may. */
export async function updateNoteAction(
  incidentId: number,
  noteId: number,
  prev: NoteOutcome,
  formData: FormData,
): Promise<NoteOutcome> {
  if (!isId(incidentId) || !isId(noteId)) return { error: await describe(new ApiError(404, "note not found")) };
  const text = await noteText(formData);
  if ("error" in text) return text;
  try {
    await updateIncidentNote(incidentId, noteId, {
      text: text.text,
      show_on_status_page: formData.get("show_on_status_page") === "on",
    });
  } catch (err) {
    return { error: await describe(err) };
  }
  refresh();
  return { saved: (prev && "saved" in prev ? prev.saved : 0) + 1 };
}

/** Deletes a note; one already gone counts as deleted. */
export async function deleteNoteAction(incidentId: number, noteId: number): Promise<NoteOutcome> {
  if (!isId(incidentId) || !isId(noteId)) return undefined;
  try {
    await deleteIncidentNote(incidentId, noteId);
  } catch (err) {
    if (!(err instanceof ApiError && err.status === 404)) return { error: await describe(err) };
  }
  refresh();
  return undefined;
}
