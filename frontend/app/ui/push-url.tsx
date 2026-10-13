"use client";

import { useState } from "react";
import { useT } from "@/lib/i18n/client";
import { useMounted } from "@/lib/use-mounted";
import { CopyIcon } from "@/app/ui/icons";

function CopyRow({ label, value }: { label: string; value: string }) {
  const t = useT();
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
          <CopyIcon /> {copied ? t("monitorPage.copied") : t("monitorPage.copy")}
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
  const t = useT();
  const mounted = useMounted();
  const path = `/api/push/${token}`;
  const external = `${mounted ? window.location.origin : ""}${path}`;

  return (
    <section aria-label={t("monitorPage.pushUrl.title")} className="card flex flex-col gap-4 p-5">
      <div>
        <h2 className="text-sm font-semibold">{t("monitorPage.pushUrl.title")}</h2>
        <p className="mt-0.5 text-xs text-muted">{t("monitorPage.pushUrl.intro")}</p>
      </div>
      <CopyRow label={t("monitorPage.pushUrl.external")} value={external} />
      <CopyRow label={t("monitorPage.pushUrl.internal")} value={`${internalBase}${path}`} />
      <CopyRow
        label={t("monitorPage.pushUrl.example")}
        value={`curl -fsS "${external}?status=up&msg=${t("monitorPage.pushUrl.exampleMessage")}"`}
      />
      <p className="text-xs text-muted">
        {t("monitorPage.pushUrl.params.intro")} <code className="font-mono">status=up|down</code>{" "}
        {t("monitorPage.pushUrl.params.status")} <code className="font-mono">msg=</code>{" "}
        {t("monitorPage.pushUrl.params.msg")} <code className="font-mono">ping=</code>{" "}
        {t("monitorPage.pushUrl.params.ping")}
      </p>
    </section>
  );
}
