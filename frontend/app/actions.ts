"use server";

import { refresh } from "next/cache";
import { redirect } from "next/navigation";
import { ApiError, createMonitor, deleteMonitor, type MonitorType } from "@/lib/api";

/** Field values as submitted, so a rejected form can be shown filled in again. */
type FormValues = {
  name: string;
  type: string;
  target: string;
  interval_seconds: string;
  timeout_seconds: string;
};

export type FormState = { error: string | null; values: FormValues | null };

export async function createMonitorAction(
  _prev: FormState,
  formData: FormData,
): Promise<FormState> {
  const field = (name: keyof FormValues) => String(formData.get(name) ?? "");
  const values: FormValues = {
    name: field("name"),
    type: field("type"),
    target: field("target"),
    interval_seconds: field("interval_seconds"),
    timeout_seconds: field("timeout_seconds"),
  };

  try {
    await createMonitor({
      name: values.name,
      type: values.type as MonitorType,
      target: values.target,
      interval_seconds: Number(values.interval_seconds),
      timeout_seconds: Number(values.timeout_seconds),
    });
  } catch (err) {
    return { error: describe(err), values };
  }
  refresh();
  return { error: null, values: null };
}

export async function deleteMonitorAction(formData: FormData): Promise<void> {
  try {
    await deleteMonitor(Number(formData.get("id")));
  } catch (err) {
    // Already gone is what the user asked for.
    if (!(err instanceof ApiError && err.status === 404)) throw err;
  }
  redirect("/");
}

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 409) return "Bu isimde bir monitor zaten var.";
    if (err.status === 400) return `Geçersiz değer: ${err.message}`;
  }
  return "Backend'e ulaşılamadı. Biraz sonra tekrar deneyin.";
}
