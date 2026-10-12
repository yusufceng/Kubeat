import { Suspense } from "react";
import { type Channel, listChannels, listMonitors } from "@/lib/api";
import { BackendError } from "@/app/ui/backend-error";
import { ChannelForm, ChannelRow } from "@/app/ui/channel-forms";

async function Channels() {
  let channels: Channel[];
  let namespaces: string[];
  try {
    const [list, monitors] = await Promise.all([listChannels(), listMonitors()]);
    channels = list;
    namespaces = [...new Set(monitors.map((m) => m.namespace).filter(Boolean))].sort();
  } catch (err) {
    return <BackendError what="Bildirim kanalları" err={err} />;
  }

  return (
    <>
      {channels.length === 0 ? (
        <p className="rounded-xl border border-dashed border-line p-8 text-center text-sm text-muted">
          Henüz kanal yok. Kanal eklenmeden olaylar kaydedilir ama kimseye bildirim gitmez.
        </p>
      ) : (
        <ul className="card divide-y divide-line">
          {channels.map((c) => (
            <ChannelRow key={c.id} channel={c} namespaces={namespaces} />
          ))}
        </ul>
      )}
      <section className="card p-5">
        <h2 className="mb-4 text-sm font-semibold">Yeni kanal</h2>
        <ChannelForm namespaces={namespaces} />
      </section>
    </>
  );
}

export default function Page() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Bildirimler</h1>
        <p className="mt-1 text-sm text-muted">
          Bir olay açıldığında ve kapandığında, kapsamı o monitor&apos;u içeren etkin kanallara bildirim
          gönderilir.
        </p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
        <Channels />
      </Suspense>
    </main>
  );
}
