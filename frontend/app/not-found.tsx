/** Shown for an address that leads nowhere, to visitors with or without a session. */
export default function NotFound() {
  return (
    <main className="mx-auto flex min-h-full w-full max-w-md flex-col items-center justify-center gap-2 px-4 py-16 text-center">
      <p className="text-5xl font-semibold tabular-nums text-muted">404</p>
      <h1 className="text-lg font-semibold">Sayfa bulunamadı</h1>
      <p className="text-sm text-muted">Bu adreste bir şey yok ya da artık yayında değil.</p>
    </main>
  );
}
