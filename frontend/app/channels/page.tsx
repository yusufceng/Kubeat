import { Suspense } from "react";
import { type Channel, listChannels, listMonitors } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { getT } from "@/lib/i18n/server";
import { BackendError } from "@/app/ui/backend-error";
import { ChannelForm, ChannelRow } from "@/app/ui/channel-forms";

async function Channels() {
  const t = await getT();
  const me = await currentUser();
  let channels: Channel[];
  let namespaces: string[];
  try {
    const [list, monitors] = await Promise.all([listChannels(), listMonitors()]);
    channels = list;
    namespaces = [...new Set(monitors.map((m) => m.namespace).filter(Boolean))].sort();
  } catch (err) {
    return <BackendError what={t("channels.what")} err={err} />;
  }

  return (
    <>
      {channels.length === 0 ? (
        <p className="rounded-xl border border-dashed border-line p-8 text-center text-sm text-muted">
          {me.can_edit ? t("channels.empty.editor") : t("channels.empty.viewer")}
        </p>
      ) : (
        <ul className="card divide-y divide-line">
          {channels.map((c) => (
            <ChannelRow key={c.id} channel={c} namespaces={namespaces} />
          ))}
        </ul>
      )}
      {me.can_edit && (
        <section className="card p-5">
          <h2 className="mb-4 text-sm font-semibold">{t("channels.new.title")}</h2>
          <ChannelForm namespaces={namespaces} />
        </section>
      )}
    </>
  );
}

export default async function Page() {
  const t = await getT();
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("nav.channels")}</h1>
        <p className="mt-1 text-sm text-muted">{t("channels.intro")}</p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
        <Channels />
      </Suspense>
    </main>
  );
}
