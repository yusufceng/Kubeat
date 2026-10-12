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
      <p className="rounded-lg border border-dashed border-black/15 p-8 text-center text-sm text-zinc-500 dark:border-white/20">
        Henüz kanal yok. Kanal eklenmeden olaylar kaydedilir ama kimseye bildirim gitmez.
      </p>
    );
  }

  return (
    <ul className="divide-y divide-black/5 rounded-lg border border-black/10 dark:divide-white/10 dark:border-white/15">
      {channels.map((c) => (
        <li key={c.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-sm font-medium">
              {c.name}
              <span className="rounded bg-black/5 px-1.5 py-0.5 text-xs font-normal dark:bg-white/10">
                {channelTypes[c.type]?.label ?? c.type}
              </span>
            </div>
            <div className="truncate font-mono text-xs text-zinc-500">{c.target}</div>
          </div>
          <ChannelActions id={c.id} name={c.name} />
        </li>
      ))}
    </ul>
  );
}

export default function Page() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-6 px-4 py-10">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Bildirimler</h1>
        <p className="mt-1 text-sm text-zinc-500">
          Bir olay açıldığında ve kapandığında etkin tüm kanallara bildirim gönderilir.
        </p>
      </div>
      <Suspense fallback={<p className="text-sm text-zinc-500">Yükleniyor…</p>}>
        <Channels />
      </Suspense>
      <section className="rounded-lg border border-black/10 p-5 dark:border-white/15">
        <h2 className="mb-4 text-sm font-semibold">Yeni kanal</h2>
        <AddChannelForm />
      </section>
    </main>
  );
}
