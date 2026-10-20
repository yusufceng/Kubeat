import Link from "next/link";
import { Suspense } from "react";
import { listMaintenance, listMonitors, type Maintenance, type Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import { currentUser } from "@/lib/auth";
import { getT, titled } from "@/lib/i18n/server";
import type { T } from "@/lib/i18n/translate";
import { BackendError } from "@/app/ui/backend-error";
import { EmptyState } from "@/app/ui/empty-state";
import { PlusIcon } from "@/app/ui/icons";
import { MaintenanceForm, MaintenanceRowAction, type ScopeOption } from "@/app/ui/maintenance-forms";
import { monitorLabel } from "@/app/ui/monitor-name";
import { CalendarIcon } from "@/app/ui/page-icons";
import { PageHead } from "@/app/ui/page-head";
import { type GlyphKind, StatusGlyph } from "@/app/ui/status-glyph";
import { TableTime } from "@/app/ui/table-time";

/** How each state of a window is drawn and named (a message key); an ended one goes without a glyph. */
const stateLooks: Record<Maintenance["state"], { glyph?: GlyphKind; label: string; color: string }> = {
  active: { glyph: "maint", label: "maintenance.state.active", color: "var(--maint-ink)" },
  upcoming: { glyph: "pending", label: "maintenance.state.upcoming", color: "var(--ink-2)" }, // for a repeating window: its next occurrence
  past: { label: "maintenance.state.past", color: "var(--ink-3)" },
};

/** Message keys; show them with t(). */
const repeatLabels: Record<Maintenance["repeat"], string> = {
  "": "common.none",
  daily: "maintenance.repeat.daily",
  weekly: "maintenance.repeat.weekly",
  monthly: "maintenance.repeat.monthly",
};

/** Going on first, then what is ahead (soonest first), then what has ended (latest first). */
function byRelevance(a: Maintenance, b: Maintenance): number {
  const rank = { active: 0, upcoming: 1, past: 2 };
  if (a.state !== b.state) return rank[a.state] - rank[b.state];
  const order = a.starts_at.localeCompare(b.starts_at);
  return a.state === "past" ? -order : order;
}

/** What a window covers, as a short line: every monitor, ns/<namespace>, or a monitor's name. */
function scopeText(w: Maintenance, t: T): string {
  if (w.monitor_id !== null) return w.monitor_name;
  if (w.namespace) return `ns/${w.namespace}`;
  return t("maintenance.scope.all");
}

/** A window's scope in its cell, never wider than the column allows; the whole of it in the title. */
function Scope({ window: w, t }: { window: Maintenance; t: T }) {
  if (w.monitor_id !== null) {
    return (
      <Link href={`/monitors/${w.monitor_id}`} className="pg-trunc max-w-56 hover:underline" title={w.monitor_name}>
        {w.monitor_name}
      </Link>
    );
  }
  if (w.namespace) {
    return (
      <span className="pg-trunc pg-mono max-w-56 text-ink-2" title={`ns/${w.namespace}`}>
        ns/{w.namespace}
      </span>
    );
  }
  return <span className="text-ink-2">{t("maintenance.scope.all")}</span>;
}

async function MaintenanceList({ adding }: { adding: boolean }) {
  const me = await currentUser();
  const t = await getT();
  let windows: Maintenance[];
  let monitors: Monitor[];
  try {
    [windows, monitors] = await Promise.all([listMaintenance(config.maintenanceLimit), listMonitors()]);
  } catch (err) {
    return <BackendError what={t("maintenance.what")} err={err} />;
  }

  const namespaces = [...new Set(monitors.map((m) => m.namespace).filter(Boolean))].sort();
  const scopes: ScopeOption[] = [
    { value: "all", label: t("maintenance.scope.all") },
    ...namespaces.map((ns) => ({ value: `ns:${ns}`, label: ns })),
    ...monitors.map((m) => ({
      value: `mon:${m.id}`,
      label: `${m.namespace ? `${m.namespace} / ` : ""}${monitorLabel(m).name}`,
    })),
  ];
  const sorted = [...windows].sort(byRelevance);

  return (
    <>
      {adding && me.can_edit && (
        <section className="panel pg-create" aria-label={t("maintenance.new")}>
          <MaintenanceForm scopes={scopes} durations={config.maintenanceDurations} closeHref="/maintenance" />
        </section>
      )}

      {sorted.length === 0 ? (
        !adding && (
          <EmptyState
            icon={<CalendarIcon width={20} height={20} />}
            title={t("maintenance.empty")}
            action={
              me.can_edit && (
                <Link href="/maintenance?new" className="btn btn-primary">
                  <PlusIcon />
                  {t("maintenance.new")}
                </Link>
              )
            }
          >
            {t("maintenance.emptyBody")}
          </EmptyState>
        )
      ) : (
        <div className="panel pg-table-panel">
          <table className="pg-table">
            <thead>
              <tr>
                <th scope="col">{t("maintenance.name")}</th>
                <th scope="col">{t("maintenance.scope")}</th>
                <th scope="col">{t("maintenance.start")}</th>
                <th scope="col">{t("maintenance.end")}</th>
                <th scope="col">{t("maintenance.repeat")}</th>
                <th scope="col">{t("maintenance.status")}</th>
                {me.can_edit && (
                  <th scope="col" className="pg-actions">
                    <span className="sr-only">{t("table.actions")}</span>
                  </th>
                )}
              </tr>
            </thead>
            <tbody>
              {sorted.map((w) => {
                const look = stateLooks[w.state];
                return (
                  <tr key={w.id}>
                    <td className="pg-grow">
                      <span className="pg-trunc pg-name" title={w.name}>
                        {w.name}
                      </span>
                    </td>
                    <td className="pg-shrink" data-label={`${t("maintenance.scope")}:`}>
                      <Scope window={w} t={t} />
                    </td>
                    <td className="pg-time pg-shrink" data-label={`${t("maintenance.start")}:`}>
                      <TableTime iso={w.starts_at} />
                    </td>
                    <td className="pg-time pg-shrink" data-label={`${t("maintenance.end")}:`}>
                      <TableTime iso={w.ends_at} />
                    </td>
                    <td
                      className="pg-shrink text-ink-2"
                      data-label={`${t("maintenance.repeat")}:`}
                      title={w.repeat ? t("maintenance.timezoneTitle", { timezone: w.timezone }) : undefined}
                    >
                      {t(repeatLabels[w.repeat] ?? w.repeat)}
                    </td>
                    <td className="pg-shrink">
                      <span className="pg-state" style={{ color: look.color }}>
                        {look.glyph && <StatusGlyph kind={look.glyph} />}
                        {t(look.label)}
                      </span>
                    </td>
                    {me.can_edit && (
                      <td className="pg-actions">
                        <span className="pg-row-actions">
                          <MaintenanceRowAction
                            id={w.id}
                            name={w.name}
                            scope={scopeText(w, t)}
                            kind={w.repeat ? "deleteSeries" : w.state === "active" ? "end" : "delete"}
                          />
                        </span>
                      </td>
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}

/** The head's action: a new window, for those who may plan one. */
async function NewWindow() {
  const me = await currentUser();
  if (!me.can_edit) return null;
  const t = await getT();
  return (
    <Link href="/maintenance?new" className="btn pg-head-new">
      <PlusIcon />
      {t("maintenance.new")}
    </Link>
  );
}

export const generateMetadata = titled("nav.maintenance");

export default async function Page({ searchParams }: PageProps<"/maintenance">) {
  const t = await getT();
  return (
    <main className="page">
      <PageHead title={t("nav.maintenance")} intro={t("maintenance.intro")}>
        <Suspense>
          <NewWindow />
        </Suspense>
      </PageHead>
      <Suspense fallback={<p className="text-sm text-ink-3">{t("common.loading")}</p>}>
        <Adding searchParams={searchParams} />
      </Suspense>
    </main>
  );
}

/** The list, with the form over it when the address asks for a new window (?new). */
async function Adding({ searchParams }: { searchParams: PageProps<"/maintenance">["searchParams"] }) {
  const adding = (await searchParams).new !== undefined;
  return <MaintenanceList adding={adding} />;
}
