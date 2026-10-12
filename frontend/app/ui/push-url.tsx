"use client";

import { useState } from "react";
import { useMounted } from "@/lib/use-mounted";
import { CopyIcon } from "@/app/ui/icons";

function CopyRow({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div>
      <div className="mb-1 text-xs font-medium text-muted">{label}</div>
      <div className="flex items-stretch gap-2">
        <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap rounded-lg bg-sunken px-3 py-2 font-mono text-xs">
          {value}
        </code>
        <button
          type="button"
          className="btn"
          onClick={async () => {
            await navigator.clipboard.writeText(value);
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
          }}
        >
          <CopyIcon /> {copied ? "Kopyalandı" : "Kopyala"}
        </button>
      </div>
    </div>
  );
}

/**
 * Where the source of a push monitor reports to. The public address is this
 * page's own origin, which only the browser knows; inside the cluster the
 * backend's service accepts the same path.
 */
export function PushUrl({ token, internalBase }: { token: string; internalBase: string }) {
  const mounted = useMounted();
  const path = `/api/push/${token}`;
  const external = `${mounted ? window.location.origin : ""}${path}`;

  return (
    <section aria-label="Rapor adresi" className="card flex flex-col gap-4 p-5">
      <div>
        <h2 className="text-sm font-semibold">Rapor adresi</h2>
        <p className="mt-0.5 text-xs text-muted">
          İzlenen iş her çalıştığında bu adrese bir istek atmalı. Adresteki anahtar bu monitor&apos;un
          sırrıdır; başka bir kimlik bilgisi gerekmez.
        </p>
      </div>
      <CopyRow label="Bu adresten" value={external} />
      <CopyRow label="Cluster içinden" value={`${internalBase}${path}`} />
      <CopyRow label="Örnek: işin sonunda" value={`curl -fsS "${external}?status=up&msg=tamam"`} />
      <p className="text-xs text-muted">
        İsteğe bağlı parametreler: <code className="font-mono">status=up|down</code> (iş kendi hatasını
        bildirebilir), <code className="font-mono">msg=</code> kısa bir not,{" "}
        <code className="font-mono">ping=</code> işin süresi (ms).
      </p>
    </section>
  );
}
