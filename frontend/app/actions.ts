"use server";

import { refresh } from "next/cache";
import { redirect } from "next/navigation";
import {
  ApiError,
  type ChannelType,
  createChannel,
  createMaintenance,
  createMonitor,
  deleteChannel,
  deleteMaintenance,
  deleteMonitor,
  type HttpOptions,
  importCandidates,
  type MonitorType,
  type MonitorChanges,
  testChannel,
  updateMonitor,
} from "@/lib/api";

/** Field values as submitted, so a rejected form can be shown filled in again. */
type FormValues = {
  name: string;
  type: string;
  target: string;
  interval_seconds: string;
  timeout_seconds: string;
  failure_threshold: string;
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
    failure_threshold: field("failure_threshold"),
  };

  try {
    await createMonitor({
      name: values.name,
      type: values.type as MonitorType,
      target: values.target,
      interval_seconds: Number(values.interval_seconds),
      timeout_seconds: Number(values.timeout_seconds),
      failure_threshold: Number(values.failure_threshold),
    });
  } catch (err) {
    return { error: describe(err), values };
  }
  refresh();
  return { error: null, values: null };
}

/** null before the first save; afterwards whether it was accepted. */
export type EditState = { saved: boolean; error: string | null } | null;

export async function updateMonitorAction(_prev: EditState, formData: FormData): Promise<EditState> {
  const changes: MonitorChanges = {
    target: String(formData.get("target") ?? ""),
    interval_seconds: Number(formData.get("interval_seconds")),
    timeout_seconds: Number(formData.get("timeout_seconds")),
    failure_threshold: Number(formData.get("failure_threshold")),
  };
  // Discovered monitors are named after their source and have no name field.
  const name = formData.get("name");
  if (name !== null) changes.name = String(name);

  // The HTTP fields are only in the form of an HTTP monitor.
  if (formData.get("http_method") !== null) {
    const headers = parseHeaders(String(formData.get("http_headers") ?? ""));
    if (typeof headers === "string") return { saved: false, error: headers };
    changes.http = {
      method: String(formData.get("http_method")) as HttpOptions["method"],
      expected_status: String(formData.get("http_expected_status") ?? ""),
      keyword: String(formData.get("http_keyword") ?? ""),
      headers,
      tls_skip_verify: formData.get("tls_skip_verify") === "on",
    };
  }

  try {
    await updateMonitor(Number(formData.get("id")), changes);
  } catch (err) {
    return { saved: false, error: describe(err) };
  }
  refresh();
  return { saved: true, error: null };
}

/** Parses "Name: value" lines; returns a message for a line it cannot read. */
function parseHeaders(text: string): Record<string, string> | string {
  const headers: Record<string, string> = {};
  for (const line of text.split("\n")) {
    if (line.trim() === "") continue;
    const colon = line.indexOf(":");
    if (colon <= 0) return `Header satırı "Ad: değer" biçiminde olmalı: ${line.trim()}`;
    headers[line.slice(0, colon).trim()] = line.slice(colon + 1).trim();
  }
  return headers;
}

export async function setMonitorEnabledAction(formData: FormData): Promise<void> {
  await updateMonitor(Number(formData.get("id")), { enabled: formData.get("enabled") === "true" });
  refresh();
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

export type ChannelFormState = {
  error: string | null;
  values: { name: string; type: string; target: string } | null;
};

export async function createChannelAction(
  _prev: ChannelFormState,
  formData: FormData,
): Promise<ChannelFormState> {
  const values = {
    name: String(formData.get("name") ?? ""),
    type: String(formData.get("type") ?? ""),
    target: String(formData.get("target") ?? ""),
  };
  try {
    await createChannel({ ...values, type: values.type as ChannelType });
  } catch (err) {
    return { error: describe(err), values };
  }
  refresh();
  return { error: null, values: null };
}

export async function deleteChannelAction(formData: FormData): Promise<void> {
  try {
    await deleteChannel(Number(formData.get("id")));
  } catch (err) {
    if (!(err instanceof ApiError && err.status === 404)) throw err;
  }
  refresh();
}

/** null before the first test; afterwards whether the channel accepted it. */
export type ChannelTestState = { delivered: boolean; error: string | null } | null;

export async function testChannelAction(
  _prev: ChannelTestState,
  formData: FormData,
): Promise<ChannelTestState> {
  try {
    const result = await testChannel(Number(formData.get("id")));
    return { delivered: result.delivered, error: result.error ?? null };
  } catch (err) {
    return { delivered: false, error: describe(err) };
  }
}

export type MaintenanceFormState = { error: string | null; created: boolean };

export async function createMaintenanceAction(
  _prev: MaintenanceFormState,
  formData: FormData,
): Promise<MaintenanceFormState> {
  // Scope is "all", "ns:<namespace>" or "mon:<monitor id>".
  const scope = String(formData.get("scope") ?? "all");
  const startsAt = String(formData.get("starts_at") ?? "");
  try {
    await createMaintenance({
      name: String(formData.get("name") ?? ""),
      duration_seconds: Number(formData.get("duration_seconds")),
      ...(startsAt && { starts_at: startsAt }),
      ...(scope.startsWith("ns:") && { namespace: scope.slice(3) }),
      ...(scope.startsWith("mon:") && { monitor_id: Number(scope.slice(4)) }),
    });
  } catch (err) {
    return { error: describe(err), created: false };
  }
  refresh();
  return { error: null, created: true };
}

export async function deleteMaintenanceAction(formData: FormData): Promise<void> {
  try {
    await deleteMaintenance(Number(formData.get("id")));
  } catch (err) {
    if (!(err instanceof ApiError && err.status === 404)) throw err;
  }
  refresh();
}

/** null before the first import; afterwards its outcome. */
export type ImportState = { created: number; skipped: number; error: string | null } | null;

export async function importCandidatesAction(
  _prev: ImportState,
  formData: FormData,
): Promise<ImportState> {
  const sources = formData.getAll("source").map(String);
  if (sources.length === 0) {
    return { created: 0, skipped: 0, error: "En az bir hedef seçin." };
  }
  try {
    const result = await importCandidates({
      namespace: String(formData.get("namespace") ?? ""),
      sources,
      interval_seconds: Number(formData.get("interval_seconds")),
      timeout_seconds: Number(formData.get("timeout_seconds")),
      failure_threshold: Number(formData.get("failure_threshold")),
    });
    refresh();
    return { created: result.created.length, skipped: result.skipped.length, error: null };
  } catch (err) {
    return { created: 0, skipped: 0, error: describe(err) };
  }
}

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 409) return "Bu isim zaten kullanılıyor.";
    if (err.status === 400) return `Geçersiz değer: ${err.message}`;
    if (err.status === 502) return `Cluster'a erişilemedi: ${err.message}`;
  }
  return "Backend'e ulaşılamadı. Biraz sonra tekrar deneyin.";
}
