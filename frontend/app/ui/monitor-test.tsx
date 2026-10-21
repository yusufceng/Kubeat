"use client";

import { useState, useTransition } from "react";
import { type MonitorTestOutcome, testMonitorNotificationAction } from "@/app/actions";
import { channelTypes } from "@/lib/channel-types";
import { useT } from "@/lib/i18n/client";
import { ConfirmDialog } from "@/app/ui/confirm";
import { SendIcon } from "@/app/ui/page-icons";
import { StatusGlyph } from "@/app/ui/status-glyph";

/**
 * "Test the notification": an item of the monitor's ⋯ menu, and the dialog
 * that says how each channel took the test. The item is rendered inside
 * the menu and the dialog outside it, since a popover that has closed
 * shows nothing of what is in it. With no channel hearing about the
 * monitor the item is disabled and says so; null (the channels could not
 * be read) leaves it on, since the backend knows and answers. undefined
 * (not offered) renders nothing.
 */
export function useTestNotification(
  monitor: { id: number; name: string },
  channelCount: number | null | undefined,
): { item: React.ReactNode; dialog: React.ReactNode } {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [outcome, setOutcome] = useState<MonitorTestOutcome | null>(null);
  const [pending, startTransition] = useTransition();
  if (channelCount === undefined) return { item: null, dialog: null };
  const none = channelCount === 0;

  function send() {
    setOutcome(null);
    setOpen(true);
    startTransition(async () => {
      const result = await testMonitorNotificationAction(monitor.id);
      // After an await, a state update is part of the transition only inside another startTransition.
      startTransition(() => setOutcome(result));
    });
  }

  const item = (
    <button type="button" role="menuitem" className="menu-item al-menu-item" disabled={none || pending} onClick={send}>
      <SendIcon />
      <span className="al-menu-text">
        {t("monitorPage.actions.test")}
        {none && <span className="al-menu-hint">{t("monitorPage.actions.test.noChannel")}</span>}
      </span>
    </button>
  );

  const dialog = open ? (
    <ConfirmDialog title={t("monitorPage.test.title", { name: monitor.name })} icon={<SendIcon />} tone="accent" onCancel={() => setOpen(false)}>
      {outcome === null ? (
        <p role="status">{t("monitorPage.test.sending")}</p>
      ) : "error" in outcome ? (
        <p role="alert" className={outcome.noChannel ? "text-warn-ink" : "text-down-ink"}>
          {outcome.error}
        </p>
      ) : (
        <>
          <p role="status">
            {t.n("monitorPage.test.summary", outcome.results.length, {
              delivered: outcome.results.filter((r) => r.delivered).length,
            })}
          </p>
          <ul className="al-test-rows" aria-label={t("monitorPage.test.rows")}>
            {outcome.results.map((r) => (
              <li key={r.channel_id} className="al-test-row">
                <span className="al-test-name">
                  <b title={r.name}>{r.name}</b>
                  <span className="pg-tag flex-none">{t(channelTypes[r.type]?.label ?? r.type)}</span>
                </span>
                <span className={`al-test-outcome ${r.delivered ? "text-up-ink" : "text-down-ink"}`}>
                  <StatusGlyph kind={r.delivered ? "up" : "down"} />
                  {t(r.delivered ? "monitorPage.test.delivered" : "monitorPage.test.failed")}
                </span>
                {!r.delivered &&
                  (r.error ? (
                    <span className="al-test-error">{r.error}</span>
                  ) : (
                    <span className="al-test-hidden">{t("monitorPage.test.reasonHidden")}</span>
                  ))}
              </li>
            ))}
          </ul>
        </>
      )}
    </ConfirmDialog>
  ) : null;

  return { item, dialog };
}
