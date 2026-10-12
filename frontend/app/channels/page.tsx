import { Suspense } from "react";
import { type Channel, listChannels } from "@/lib/api";
import { BackendError } from "@/app/ui/backend-error";
import { AddChannelForm, ChannelActions } from "@/app/ui/channel-forms";
import { channelTypes } from "@/lib/channel-types";

async function Channels() {
  let channels: Channel[];
  try {
    channels = await listChannels();
  } catch (err) {
    return <BackendError what="Bildirim kanalları" err={err} />;
  }

  if (channels.length === 0) {
    return (
      <p className="rounded-lg border border-dashed border-line p-8 text-center text-sm text-muted">
        Henüz kanal yok. Kanal eklenmeden olaylar kaydedilir ama kimseye bildirim gitmez.
      </p>
    );
  }

  return (
    <ul className="card divide-y divide-line">
      {channels.map((c) => (
        <li key={c.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-sm font-medium">
              {c.name}
              <span className="rounded bg-sunken px-1.5 py-0.5 text-xs font-normal">
                {channelTypes[c.type]?.label ?? c.type}
              </span>
            </div>
            <div className="truncate font-mono text-xs text-muted">{c.target}</div>
          </div>
          <ChannelActions id={c.id} name={c.name} />
        </li>
      ))}
    </ul>
  );
}

export default function Page() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Bildirimler</h1>
        <p className="mt-1 text-sm text-muted">
          Bir olay açıldığında ve kapandığında etkin tüm kanallara bildirim gönderilir.
        </p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
        <Channels />
      </Suspense>
      <section className="card p-5">
        <h2 className="mb-4 text-sm font-semibold">Yeni kanal</h2>
        <AddChannelForm />
      </section>
    </main>
  );
}
