import Link from "next/link";
import { Suspense } from "react";
import { type Channel, listChannels, listMonitors } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { getT, titled } from "@/lib/i18n/server";
import { BackendError } from "@/app/ui/backend-error";
import { ChannelForm, ChannelRow } from "@/app/ui/channel-forms";
import { EmptyState } from "@/app/ui/empty-state";
import { BellIcon, PlusIcon } from "@/app/ui/icons";
import { PageHead } from "@/app/ui/page-head";

type Search = PageProps<"/channels">["searchParams"];

async function Channels({ searchParams }: { searchParams: Search }) {
  const t = await getT();
  const me = await currentUser();
  const params = await searchParams;
  let channels: Channel[];
  let namespaces: string[];
  try {
    const [list, monitors] = await Promise.all([listChannels(), listMonitors()]);
    channels = list;
    namespaces = [...new Set(monitors.map((m) => m.namespace).filter(Boolean))].sort();
  } catch (err) {
    return <BackendError what={t("channels.what")} err={err} />;
  }
  // The form open on the page: a new channel (?new) or one being edited (?edit=<id>).
  const adding = me.can_edit && params.new !== undefined;
  const editing = me.can_edit ? Number(params.edit) || null : null;

  return (
    <>
      {adding && (
        <section className="panel pg-create" aria-label={t("channels.new.title")}>
          <ChannelForm namespaces={namespaces} closeHref="/channels" />
        </section>
      )}
      {channels.length === 0 ? (
        !adding && (
          <EmptyState
            icon={<BellIcon width={20} height={20} />}
            title={t("channels.empty.title")}
            action={
              me.can_edit && (
                <Link href="/channels?new" className="btn btn-primary">
                  <PlusIcon />
                  {t("channels.new.title")}
                </Link>
              )
            }
          >
            {me.can_edit ? t("channels.empty.editor") : t("channels.empty.viewer")}
          </EmptyState>
        )
      ) : (
        <div className="panel pg-table-panel">
          <table className="pg-table">
            <thead>
              <tr>
                <th scope="col">{t("channels.col.channel")}</th>
                <th scope="col" className="sm:max-xl:hidden">
                  {t("channels.col.target")}
                </th>
                <th scope="col">{t("channels.col.routing")}</th>
                <th scope="col">{t("channels.col.last")}</th>
                {me.can_edit && (
                  <th scope="col" className="pg-actions">
                    <span className="sr-only">{t("table.actions")}</span>
                  </th>
                )}
              </tr>
            </thead>
            <tbody>
              {channels.map((c) => (
                <ChannelRow
                  key={c.id}
                  channel={c}
                  namespaces={namespaces}
                  canEdit={me.can_edit}
                  editing={editing === c.id}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}

/** The head's action: a new channel, for those who may add one. */
async function NewChannel() {
  const me = await currentUser();
  if (!me.can_edit) return null;
  const t = await getT();
  return (
    <Link href="/channels?new" className="btn pg-head-new">
      <PlusIcon />
      {t("channels.new.title")}
    </Link>
  );
}

export const generateMetadata = titled("nav.channels");

export default async function Page({ searchParams }: PageProps<"/channels">) {
  const t = await getT();
  return (
    <main className="page">
      <PageHead title={t("nav.channels")} intro={t("channels.intro")}>
        <Suspense>
          <NewChannel />
        </Suspense>
      </PageHead>
      <Suspense fallback={<p className="text-sm text-ink-3">{t("common.loading")}</p>}>
        <Channels searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
