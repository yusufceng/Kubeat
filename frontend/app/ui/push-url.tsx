"use client";

import { useRef, useState } from "react";
import { renewPushTokenAction } from "@/app/actions";
import { ConfirmDialog } from "@/app/ui/confirm";
import { useT } from "@/lib/i18n/client";
import { useMounted } from "@/lib/use-mounted";
import { CopyIcon, RenewIcon } from "@/app/ui/icons";

function CopyRow({ label, value }: { label: string; value: string }) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  return (
    <div>
      <div className="mon-h3 mb-1.5">{label}</div>
      <div className="mon-copy">
        <code>{value}</code>
        <button
          type="button"
          className="btn"
          aria-label={`${t("monitorPage.copy")}: ${label}`}
          onClick={async () => {
            await navigator.clipboard.writeText(value);
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
          }}
        >
          <CopyIcon />
          <span aria-live="polite">{copied ? t("monitorPage.copied") : t("monitorPage.copy")}</span>
        </button>
      </div>
    </div>
  );
}

/**
 * Where the source of a push monitor reports to. The public address is this
 * page's own origin, which only the browser knows; inside the cluster the
 * backend's service accepts the same path. Only those who may change the
 * monitor get the token, and they can renew it when it has leaked.
 */
export function PushUrl({ monitorId, token, internalBase }: { monitorId: number; token: string; internalBase: string }) {
  const t = useT();
  const mounted = useMounted();
  const [renewing, setRenewing] = useState(false);
  const renewForm = useRef<HTMLFormElement>(null);
  const path = `/api/push/${token}`;
  const external = `${mounted ? window.location.origin : ""}${path}`;

  return (
    <section aria-labelledby="mon-push-title" className="panel">
      <div className="mon-ph">
        <h2 id="mon-push-title">{t("monitorPage.pushUrl.title")}</h2>
        <span className="mon-ph-end">
          <form ref={renewForm} action={renewPushTokenAction}>
            <input type="hidden" name="id" value={monitorId} />
          </form>
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => setRenewing(true)}>
            <RenewIcon width={14} height={14} />
            {t("monitorPage.pushUrl.renew")}
          </button>
          {renewing && (
            <ConfirmDialog
              title={t("monitorPage.pushUrl.renew.title")}
              confirmLabel={t("monitorPage.pushUrl.renew.action")}
              onCancel={() => setRenewing(false)}
              onConfirm={() => {
                setRenewing(false);
                renewForm.current?.requestSubmit();
              }}
            >
              {t("monitorPage.pushUrl.renew.confirm")}
            </ConfirmDialog>
          )}
        </span>
      </div>
      <p className="px-4 pt-1 text-[13px] text-ink-2">{t("monitorPage.pushUrl.intro")}</p>
      <div className="flex flex-col gap-4 px-4 pt-4 pb-4">
        <CopyRow label={t("monitorPage.pushUrl.external")} value={external} />
        <CopyRow label={t("monitorPage.pushUrl.internal")} value={`${internalBase}${path}`} />
        <CopyRow
          label={t("monitorPage.pushUrl.example")}
          value={`curl -fsS "${external}?status=up&msg=${t("monitorPage.pushUrl.exampleMessage")}"`}
        />
        <p className="text-xs text-ink-3">
          {t("monitorPage.pushUrl.params.intro")} <code className="mon-mono text-ink-2">status=up|down</code>{" "}
          {t("monitorPage.pushUrl.params.status")} <code className="mon-mono text-ink-2">msg=</code>{" "}
          {t("monitorPage.pushUrl.params.msg")} <code className="mon-mono text-ink-2">ping=</code>{" "}
          {t("monitorPage.pushUrl.params.ping")}
        </p>
      </div>
    </section>
  );
}
