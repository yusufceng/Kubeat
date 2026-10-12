import { Suspense } from "react";
import { getHealth } from "@/lib/api";

async function BackendStatus() {
  let health;
  try {
    health = await getHealth();
  } catch {
    return (
      <StatusCard up={false} label="Backend'e ulaşılamıyor">
        API yanıt vermiyor. Backend&apos;in çalıştığını kontrol edin.
      </StatusCard>
    );
  }

  return (
    <StatusCard up label="Backend çalışıyor">
      Sürüm <span className="font-mono">{health.version}</span> · son yanıt{" "}
      <span className="font-mono">{health.time}</span>
    </StatusCard>
  );
}

function StatusCard({
  up,
  label,
  children,
}: {
  up: boolean;
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="rounded-lg border border-black/10 p-5 dark:border-white/15">
      <div className="flex items-center gap-3">
        <span
          className={`h-3 w-3 rounded-full ${up ? "bg-emerald-500" : "bg-red-500"}`}
          aria-hidden
        />
        <h2 className="text-lg font-medium">{label}</h2>
      </div>
      <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">{children}</p>
    </div>
  );
}

export default function Home() {
  return (
    <main className="mx-auto flex w-full max-w-2xl flex-1 flex-col gap-6 px-4 py-16">
      <h1 className="text-3xl font-semibold tracking-tight">Uptime</h1>
      <Suspense
        fallback={<p className="text-sm text-zinc-500">Kontrol ediliyor…</p>}
      >
        <BackendStatus />
      </Suspense>
    </main>
  );
}
