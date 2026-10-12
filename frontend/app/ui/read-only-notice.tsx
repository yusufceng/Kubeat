import Link from "next/link";

/** What a viewer sees in place of a form they may not submit. */
export function ReadOnlyNotice() {
  return (
    <p className="rounded-lg border border-line bg-sunken p-4 text-sm">
      Hesabınız izleyici: monitor ekleyemez ve değiştiremezsiniz. Bunun için namespace&apos;inizin editörüne
      ya da bir yöneticiye başvurun.{" "}
      <Link href="/" className="underline">
        Genel bakışa dön
      </Link>
      .
    </p>
  );
}
