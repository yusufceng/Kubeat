"use client";

import { useEffect, useId, useRef, useState } from "react";
import { useT } from "@/lib/i18n/client";
import { EditIcon } from "@/app/ui/icons";
import { WarningIcon } from "@/app/ui/page-icons";

/**
 * The frame of a question asked in the interface's own look instead of the
 * browser's: a modal <dialog> that Escape and a click outside dismiss. Its
 * head is an icon in a tile, tinted for what the question is about (danger
 * for what cannot be undone), and a title that says what is at stake.
 */
function Modal({
  title,
  icon,
  tone,
  onCancel,
  children,
}: {
  title: string;
  icon: React.ReactNode;
  tone: "danger" | "accent";
  onCancel: () => void;
  children: React.ReactNode;
}) {
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
      className="dlg"
      onCancel={(e) => {
        e.preventDefault();
        onCancel();
      }}
      // The inner frame fills the dialog, so a click on the dialog itself
      // is a click on the backdrop.
      onClick={(e) => {
        if (e.target === e.currentTarget) onCancel();
      }}
    >
      <div className="dlg-inner">
        <div className="dlg-head">
          <span className={`dlg-icon ${tone === "danger" ? "dlg-icon-danger" : ""}`} aria-hidden>
            {icon}
          </span>
          <h2 id={titleId} className="dlg-title">
            {title}
          </h2>
        </div>
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
  icon,
}: {
  title: string;
  label: string;
  initial: string;
  submitLabel: string;
  onSubmit: (value: string) => void;
  onCancel: () => void;
  /** The icon of the dialog's head; a pen by default. */
  icon?: React.ReactNode;
}) {
  const t = useT();
  const [value, setValue] = useState(initial);
  const fieldId = useId();
  return (
    <Modal title={title} icon={icon ?? <EditIcon />} tone="accent" onCancel={onCancel}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (value.trim()) onSubmit(value.trim());
        }}
      >
        <label htmlFor={fieldId} className="pg-label mt-4">
          {label}
        </label>
        <input
          id={fieldId}
          className="field"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onFocus={(e) => e.currentTarget.select()}
          autoFocus
          autoComplete="off"
        />
        <div className="dlg-foot">
          <button type="button" className="btn btn-ghost" onClick={onCancel}>
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
 * A question asked before something that cannot be undone. The title says
 * what will happen to what ("Delete the channel ops?"), the children why it
 * matters, and items list what is affected, one per line in mono. The
 * cancel button has the focus, so that Enter does not confirm by accident.
 */
export function ConfirmDialog({
  title,
  children,
  items,
  confirmLabel,
  onConfirm,
  onCancel,
  icon,
  tone,
}: {
  title: string;
  children?: React.ReactNode;
  /** What the action affects, such as the names of the monitors it deletes. */
  items?: string[];
  /** The destructive button's text; without it only "close" is offered. */
  confirmLabel?: string;
  onConfirm?: () => void;
  onCancel: () => void;
  /** The icon of the dialog's head; a warning sign by default. */
  icon?: React.ReactNode;
  /** danger (the default with a confirmLabel) or accent, for a question that loses nothing. */
  tone?: "danger" | "accent";
}) {
  const t = useT();
  const dangerous = (tone ?? (confirmLabel ? "danger" : "accent")) === "danger";
  return (
    <Modal title={title} icon={icon ?? <WarningIcon />} tone={dangerous ? "danger" : "accent"} onCancel={onCancel}>
      {children && <div className="dlg-text">{children}</div>}
      {items && items.length > 0 && (
        <ul className="dlg-items" aria-label={t("dialog.affected")}>
          {items.map((item, i) => (
            <li key={i} title={item}>
              {item}
            </li>
          ))}
        </ul>
      )}
      <div className="dlg-foot">
        <button type="button" className="btn btn-ghost" autoFocus onClick={onCancel}>
          {confirmLabel ? t("common.cancel") : t("common.close")}
        </button>
        {confirmLabel && (
          <button type="button" className={`btn ${dangerous ? "btn-danger-solid" : "btn-primary"}`} onClick={onConfirm}>
            {confirmLabel}
          </button>
        )}
      </div>
    </Modal>
  );
}

/**
 * A form whose submission is confirmed first. With a title, the dialog is
 * headed by it and the question explains; without one, the question itself
 * heads the dialog, so it should name what is at stake ("Delete the channel
 * ops?"). Without a question it submits like any form.
 */
export function ConfirmForm({
  action,
  question,
  title,
  items,
  icon,
  confirmLabel,
  className,
  children,
}: {
  action: (formData: FormData) => void | Promise<void>;
  question?: string;
  title?: string;
  /** What the action affects, listed in mono under the question. */
  items?: string[];
  icon?: React.ReactNode;
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
      {asking && question && (
        <ConfirmDialog
          title={title ?? question}
          items={items}
          icon={icon}
          confirmLabel={confirmLabel ?? t("common.delete")}
          onCancel={() => setAsking(false)}
          onConfirm={() => {
            setAsking(false);
            confirmed.current = true;
            form.current?.requestSubmit();
          }}
        >
          {title ? question : null}
        </ConfirmDialog>
      )}
    </>
  );
}
