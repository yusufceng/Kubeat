import type { Metadata } from "next";
import { Suspense } from "react";
import "./globals.css";
import { config } from "@/lib/config";
import { I18nProvider } from "@/lib/i18n/client";
import { getLocale, getT } from "@/lib/i18n/server";
import { themeScript } from "@/app/ui/theme-toggle";

// The faces (IBM Plex Sans for words, JetBrains Mono for figures and code)
// are served from public/fonts and declared in globals.css, so a build
// needs no network and a page no third party.

/** The name of the product and what it does, in the visitor's language. */
export async function generateMetadata(): Promise<Metadata> {
  const t = await getT();
  return {
    title: { default: t("meta.title"), template: "%s · Kubeat" },
    description: t("meta.description"),
    applicationName: "Kubeat",
  };
}

/**
 * The language is request data (a cookie, else the browser's preference),
 * so everything that shows words waits for it; the provider also sets
 * <html lang> once it knows. The frame of the app (app/(app)/layout.tsx)
 * and the bare pages (sign-in, public status pages) sit under it.
 */
async function Localized({ children }: { children: React.ReactNode }) {
  const locale = await getLocale();
  return <I18nProvider locale={locale}>{children}</I18nProvider>;
}

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    // The theme script changes data-theme before React hydrates.
    <html lang={config.defaultLocale} className="h-full" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      {/* The app's frame fills the window and scrolls its panes; the bare
          pages scroll as documents do. */}
      <body>
        <Suspense fallback={null}>
          <Localized>{children}</Localized>
        </Suspense>
      </body>
    </html>
  );
}
