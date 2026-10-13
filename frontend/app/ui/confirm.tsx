"use client";

import { useEffect, useId, useRef, useState } from "react";
import { useT } from "@/lib/i18n/client";

/**
 * The frame of a question asked in the interface's own look instead of the
 * browser's: a modal <dialog> that Escape and a click outside dismiss.
 */
function Modal({ title, onCancel, children }: { title: string; onCancel: () => void; children: React.ReactNode }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const titleId = useId();

  useEffect(() => {
    const el = dialog.current;
    if (el && !el.open) el.showModal();
    return () => el?.close();
  }, []);

  return (
    <dialog
      ref={dialog}
      aria-labelledby={titleId}
      className="m-auto w-[min(28rem,calc(100vw-2rem))] rounded-xl border border-line bg-panel p-0 text-ink shadow-2xl backdrop:bg-black/55"
      onCancel={(e) => {
        e.preventDefault();
        onCancel();
      }}
      // The dialog has no padding of its own, so a click on it is a click
      // on the backdrop.
      onClick={(e) => {
        if (e.target === e.currentTarget) onCancel();
      }}
    >
      <div className="p-5">
        <h2 id={titleId} className="text-base font-semibold">
          {title}
        </h2>
        {children}
      </div>
    </dialog>
  );
}

/**
 * Asks for one line of text, such as the name of a copy. The field starts
 * with the suggestion selected, so typing replaces it; an empty answer
 * cannot be submitted.
 */
export function PromptDialog({
  title,
  label,
  initial,
  submitLabel,
  onSubmit,
  onCancel,
}: {
  title: string;
  label: string;
  initial: string;
  submitLabel: string;
  onSubmit: (value: string) => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [value, setValue] = useState(initial);
  const fieldId = useId();
  return (
    <Modal title={title} onCancel={onCancel}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (value.trim()) onSubmit(value.trim());
        }}
      >
        <label htmlFor={fieldId} className="mt-3 block text-sm text-muted">
          {label}
        </label>
        <input
          id={fieldId}
          className="field mt-1.5"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onFocus={(e) => e.currentTarget.select()}
          autoFocus
          autoComplete="off"
        />
        <div className="mt-5 flex justify-end gap-2">
          <button type="button" className="btn" onClick={onCancel}>
            {t("common.cancel")}
          </button>
          <button type="submit" className="btn btn-primary" disabled={!value.trim()}>
            {submitLabel}
          </button>
        </div>
      </form>
    </Modal>
  );
}

/**
 * A question asked before something that cannot be undone. The cancel
 * button has the focus, so that Enter does not confirm by accident.
 */
export function ConfirmDialog({
  title,
  children,
  confirmLabel,
  onConfirm,
  onCancel,
}: {
  title: string;
  children: React.ReactNode;
  /** The destructive button's text; without it only "close" is offered. */
  confirmLabel?: string;
  onConfirm?: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  return (
    <Modal title={title} onCancel={onCancel}>
      <div className="mt-2 text-sm text-muted">{children}</div>
      <div className="mt-5 flex justify-end gap-2">
        <button type="button" className="btn" autoFocus onClick={onCancel}>
          {confirmLabel ? t("common.cancel") : t("common.close")}
        </button>
        {confirmLabel && (
          <button type="button" className="btn btn-danger-solid" onClick={onConfirm}>
            {confirmLabel}
          </button>
        )}
      </div>
    </Modal>
  );
}

/**
 * A form whose submission is confirmed first. Without a question it
 * submits like any form.
 */
export function ConfirmForm({
  action,
  question,
  confirmLabel,
  className,
  children,
}: {
  action: (formData: FormData) => void | Promise<void>;
  question?: string;
  /** The destructive button's text; "Delete" by default. */
  confirmLabel?: string;
  className?: string;
  children: React.ReactNode;
}) {
  const t = useT();
  const form = useRef<HTMLFormElement>(null);
  const confirmed = useRef(false);
  const [asking, setAsking] = useState(false);

  return (
    <>
      <form
        ref={form}
        action={action}
        className={className}
        onSubmit={(e) => {
          if (!question || confirmed.current) {
            confirmed.current = false;
            return;
          }
          e.preventDefault();
          setAsking(true);
        }}
      >
        {children}
      </form>
      {asking && (
        <ConfirmDialog
          title={t("confirm.title")}
          confirmLabel={confirmLabel ?? t("common.delete")}
          onCancel={() => setAsking(false)}
          onConfirm={() => {
            setAsking(false);
            confirmed.current = true;
            form.current?.requestSubmit();
          }}
        >
          {question}
        </ConfirmDialog>
      )}
    </>
  );
}
