export function BackendError({ what }: { what: string }) {
  return (
    <p
      role="alert"
      className="rounded-lg border border-red-600/40 bg-red-600/5 p-4 text-sm text-red-700 dark:text-red-400"
    >
      Backend&apos;e ulaşılamıyor. {what} gösterilemiyor.
    </p>
  );
}
