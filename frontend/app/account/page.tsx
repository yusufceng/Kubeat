import { Suspense } from "react";
import { currentUser } from "@/lib/auth";
import { PasswordForm, roleLabels } from "@/app/ui/user-forms";

const sourceLabels = { local: "Yerel", ldap: "Kurumsal dizin (LDAP)", oidc: "SSO (OpenID Connect)" } as const;

async function Account() {
  const me = await currentUser();
  return (
    <>
      <dl className="card grid gap-x-6 gap-y-2 p-5 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-xs text-muted">Kullanıcı adı</dt>
          <dd className="font-mono">{me.username}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted">Rol</dt>
          <dd>
            {roleLabels[me.role]}
            {!me.admin && ` · ${me.namespaces.join(", ")}`}
          </dd>
        </div>
        <div>
          <dt className="text-xs text-muted">Hesap</dt>
          <dd>{sourceLabels[me.source]}</dd>
        </div>
        {me.email && (
          <div>
            <dt className="text-xs text-muted">E-posta</dt>
            <dd>{me.email}</dd>
          </div>
        )}
      </dl>
      <section className="card p-5">
        <h2 className="mb-4 text-sm font-semibold">Parola</h2>
        {me.source !== "local" ? (
          <p className="text-sm text-muted">Parolanız burada tutulmaz; {sourceLabels[me.source]} tarafında değiştirilir.</p>
        ) : (
          <PasswordForm />
        )}
      </section>
    </>
  );
}

export default function Page() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <h1 className="text-2xl font-semibold tracking-tight">Hesabım</h1>
      <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
        <Account />
      </Suspense>
    </main>
  );
}
