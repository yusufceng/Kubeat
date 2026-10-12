"use server";

import { refresh } from "next/cache";
import { redirect } from "next/navigation";
import {
  ApiError,
  type ChannelType,
  cloneMonitor,
  createChannel,
  createMaintenance,
  createMonitor,
  createStatusPage,
  deleteChannel,
  deleteMaintenance,
  deleteMonitor,
  deleteStatusPage,
  type DnsOptions,
  type HttpOptions,
  importCandidates,
  type MaintenanceRepeat,
  type Monitor,
  type MonitorChanges,
  type MonitorType,
  type NewMonitor,
  type StatusPageSettings,
  testChannel,
  updateChannel,
  updateMonitor,
  updateStatusPage,
} from "@/lib/api";

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

/**
 * Creates a monitor, or with an id saves changes to one, from the monitor
 * form. On success it goes to the monitor's page; otherwise it returns what
 * was wrong, and the form stays as it is.
 */
export async function saveMonitorAction(formData: FormData): Promise<{ error: string } | undefined> {
  const text = (name: string) => String(formData.get(name) ?? "").trim();
  const number = (name: string) => Number(formData.get(name) ?? 0);
  const checked = (name: string) => formData.get(name) === "on";

  const id = number("id");
  const type = text("type") as MonitorType;
  const push = type === "push";

  const settings: MonitorChanges = {
    description: text("description"),
    target: push ? "" : text("target"),
    interval_seconds: number("interval_seconds"),
    timeout_seconds: number("timeout_seconds"),
    failure_threshold: number("failure_threshold"),
    retry_interval_seconds: push ? 0 : number("retry_interval_seconds"),
    inverted: !push && checked("inverted"),
  };
  // Discovered monitors are named after their source and stay in its
  // namespace; their form has neither field.
  if (formData.get("name") !== null) settings.name = text("name");
  if (formData.get("group") !== null) settings.namespace = text("group");
  settings.tags = text("tags")
    .split(",")
    .map((tag) => tag.trim())
    .filter(Boolean);

  switch (type) {
    case "http": {
      const headers = parseHeaders(String(formData.get("http_headers") ?? ""));
      if (typeof headers === "string") return { error: headers };
      const auth = text("http_auth") as HttpOptions["auth"];
      const jsonPath = text("http_json_path");
      settings.http = {
        method: text("http_method") as HttpOptions["method"],
        expected_status: text("http_expected_status"),
        max_redirects: number("http_max_redirects"),
        // A keyword may begin or end with a space on purpose.
        keyword: String(formData.get("http_keyword") ?? ""),
        invert_keyword: checked("http_invert_keyword"),
        json_path: jsonPath,
        json_operator: jsonPath ? (text("http_json_operator") as HttpOptions["json_operator"]) : "",
        json_expected: jsonPath ? text("http_json_expected") : "",
        headers,
        body: String(formData.get("http_body") ?? ""),
        content_type: text("http_content_type"),
        auth,
        auth_user: auth === "basic" ? text("http_auth_user") : "",
        auth_secret: auth ? String(formData.get("http_auth_secret") ?? "") : "",
        tls_skip_verify: checked("tls_skip_verify"),
      };
      break;
    }
    case "tcp":
      settings.tcp = { tls: checked("tcp_tls"), tls_skip_verify: checked("tls_skip_verify") };
      break;
    case "dns":
      settings.dns = {
        record_type: text("dns_record_type") as DnsOptions["record_type"],
        resolver: text("dns_resolver"),
        expected: text("dns_expected"),
      };
      break;
    case "grpc":
      settings.grpc = {
        service: text("grpc_service"),
        tls: checked("grpc_tls"),
        tls_skip_verify: checked("tls_skip_verify"),
      };
      break;
    case "postgres":
    case "mysql":
    case "redis":
      settings.database = {
        username: text("db_username"),
        password: String(formData.get("db_password") ?? ""),
        database: type === "redis" ? "" : text("db_database"),
        query: type === "redis" ? "" : text("db_query"),
        tls: checked("db_tls"),
        tls_skip_verify: checked("tls_skip_verify"),
      };
      break;
    case "workload":
      settings.workload = { min_ready: number("workload_min_ready") };
      break;
    case "websocket":
      settings.websocket = {
        subprotocol: text("ws_subprotocol"),
        tls_skip_verify: checked("tls_skip_verify"),
      };
      break;
  }

  let saved: Monitor;
  try {
    saved = id
      ? await updateMonitor(id, settings)
      : await createMonitor({ ...settings, type } as NewMonitor);
  } catch (err) {
    return { error: describe(err) };
  }
  refresh(); // the monitor list shows the change at once
  redirect(`/monitors/${saved.id}`);
}

/**
 * Makes a copy of a monitor under the given name and opens the copy's
 * settings, where it usually needs another target.
 */
export async function cloneMonitorAction(id: number, name: string): Promise<{ error: string } | undefined> {
  let copy: Monitor;
  try {
    copy = await cloneMonitor(id, name);
  } catch (err) {
    return { error: describe(err) };
  }
  refresh();
  redirect(`/monitors/${copy.id}/edit`);
}

export type BulkAction = "pause" | "resume" | "delete";

/**
 * Pauses, resumes or deletes several monitors. Returns how many of them it
 * could not change; the others are done.
 */
export async function bulkMonitorsAction(ids: number[], action: BulkAction): Promise<{ failed: number }> {
  const results = await Promise.allSettled(
    ids.map((id) => (action === "delete" ? deleteMonitor(id) : updateMonitor(id, { enabled: action === "resume" }))),
  );
  refresh();
  // A monitor that is already gone is what a delete asked for.
  const failed = results.filter(
    (r) => r.status === "rejected" && !(action === "delete" && r.reason instanceof ApiError && r.reason.status === 404),
  );
  return { failed: failed.length };
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
  refresh(); // the monitor list drops it at once
  redirect("/");
}

/**
 * Creates a channel, or with an id saves changes to one. Returns what was
 * wrong, or nothing when it was saved.
 */
export async function saveChannelAction(formData: FormData): Promise<{ error: string } | undefined> {
  const text = (name: string) => String(formData.get(name) ?? "").trim();
  const id = Number(formData.get("id") ?? 0);
  const settings = {
    name: text("name"),
    target: text("target"),
    secret: text("secret"),
    namespaces: formData.getAll("namespace").map(String),
  };
  try {
    if (id) await updateChannel(id, settings);
    else await createChannel({ ...settings, type: text("type") as ChannelType });
  } catch (err) {
    return { error: describe(err) };
  }
  refresh();
}

export async function setChannelEnabledAction(formData: FormData): Promise<void> {
  await updateChannel(Number(formData.get("id")), { enabled: formData.get("enabled") === "true" });
  refresh();
}

export async function deleteChannelAction(formData: FormData): Promise<void> {
  try {
    await deleteChannel(Number(formData.get("id")));
  } catch (err) {
    if (!(err instanceof ApiError && err.status === 404)) throw err;
  }
  refresh();
}

/**
 * Creates a status page, or with an id saves changes to one. Returns what
 * was wrong, or nothing when it was saved.
 */
export async function saveStatusPageAction(formData: FormData): Promise<{ error: string } | undefined> {
  const text = (name: string) => String(formData.get(name) ?? "").trim();
  const id = Number(formData.get("id") ?? 0);
  const settings: StatusPageSettings = {
    slug: text("slug"),
    title: text("title"),
    description: text("description"),
    announcement: text("announcement"),
    published: formData.get("published") === "on",
    // The checked monitors, in the order the form lists them.
    monitors: formData.getAll("monitor").map((value) => ({
      monitor_id: Number(value),
      display_name: text(`name_${value}`),
    })),
  };
  try {
    if (id) await updateStatusPage(id, settings);
    else await createStatusPage(settings);
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) return { error: "Bu adres başka bir durum sayfasında kullanılıyor." };
    return { error: describe(err) };
  }
  refresh();
}

export async function deleteStatusPageAction(formData: FormData): Promise<void> {
  try {
    await deleteStatusPage(Number(formData.get("id")));
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
  const repeat = String(formData.get("repeat") ?? "") as MaintenanceRepeat;
  const timezone = String(formData.get("timezone") ?? "");
  try {
    await createMaintenance({
      name: String(formData.get("name") ?? ""),
      duration_seconds: Number(formData.get("duration_seconds")),
      ...(startsAt && { starts_at: startsAt }),
      // A repeating window keeps its time of day in the viewer's zone.
      ...(repeat && { repeat, ...(timezone && { timezone }) }),
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
    if (err.status === 401) return "Backend API anahtarını kabul etmedi (BACKEND_TOKEN).";
  }
  return "Backend'e ulaşılamadı. Biraz sonra tekrar deneyin.";
}
