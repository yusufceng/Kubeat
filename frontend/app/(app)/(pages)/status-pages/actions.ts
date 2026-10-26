"use server";

import { refresh } from "next/cache";
import {
  ApiError,
  createStatusPage,
  createStatusPageUpdate,
  deleteStatusPageUpdate,
  missingFeature,
  type StatusPageSettings,
  type StatusPageUpdateSettings,
  updateStatusPage,
  updateStatusPageUpdate,
} from "@/lib/api";
import { getT } from "@/lib/i18n/server";

/** What the editor is told went wrong, in their language; a 400 carries the backend's own words. */
async function describe(err: unknown): Promise<string> {
  const t = await getT();
  const feature = missingFeature(err);
  if (feature) return t("license.required", { feature: t(`license.feature.${feature}`) });
  if (err instanceof ApiError) {
    if (err.status === 409) return t("monitorForm.actions.slugTaken");
    if (err.status === 400) return t("monitorForm.actions.error.invalid", { message: err.message });
    if (err.status === 403) return t("monitorForm.actions.error.forbidden");
    if (err.status === 404) return t("monitorForm.actions.error.notFound");
    if (err.status === 401) return t("monitorForm.actions.error.sessionExpired");
    if (err.status === 503 && err.message.startsWith("too many")) return t("monitorForm.actions.error.busy");
  }
  return t("monitorForm.actions.error.backend");
}

export type Outcome = { error: string } | undefined;

/**
 * Creates a status page, or with an id saves changes to one: the whole
 * page as the form holds it, every monitor with the section it is in.
 * Returns what was wrong, or nothing when it was saved.
 */
export async function savePageAction(id: number | null, settings: Partial<StatusPageSettings>): Promise<Outcome> {
  try {
    if (id) await updateStatusPage(Number(id), settings);
    else await createStatusPage(settings);
  } catch (err) {
    return { error: await describe(err) };
  }
  refresh();
}

export async function createUpdateAction(pageId: number, update: StatusPageUpdateSettings): Promise<Outcome> {
  try {
    await createStatusPageUpdate(Number(pageId), update);
  } catch (err) {
    return { error: await describe(err) };
  }
  refresh();
}

export async function editUpdateAction(
  pageId: number,
  updateId: number,
  changes: Partial<StatusPageUpdateSettings>,
): Promise<Outcome> {
  try {
    await updateStatusPageUpdate(Number(pageId), Number(updateId), changes);
  } catch (err) {
    return { error: await describe(err) };
  }
  refresh();
}

export async function deleteUpdateAction(pageId: number, updateId: number): Promise<Outcome> {
  try {
    await deleteStatusPageUpdate(Number(pageId), Number(updateId));
  } catch (err) {
    // Gone already: what was asked for is the case.
    if (!(err instanceof ApiError && err.status === 404)) return { error: await describe(err) };
  }
  refresh();
}
