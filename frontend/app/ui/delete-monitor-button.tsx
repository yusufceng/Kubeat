"use client";

import { deleteMonitorAction } from "@/app/actions";

export function DeleteMonitorButton({ id, name }: { id: number; name: string }) {
  return (
    <form
      action={deleteMonitorAction}
      onSubmit={(e) => {
        if (!confirm(`"${name}" ve tüm kontrol geçmişi silinsin mi?`)) e.preventDefault();
      }}
    >
      <input type="hidden" name="id" value={id} />
      <button
        type="submit"
        className="rounded-md border border-red-600/40 px-3 py-1.5 text-sm text-red-600 hover:bg-red-600/10 dark:text-red-400"
      >
        Sil
      </button>
    </form>
  );
}
