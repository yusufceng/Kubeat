import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import { Suspense } from "react";
import "./globals.css";
import { config } from "@/lib/config";
import { I18nProvider } from "@/lib/i18n/client";
import { getLocale } from "@/lib/i18n/server";
import { BackendWarning } from "@/app/ui/backend-warning";
import { LanguageToggle } from "@/app/ui/language-toggle";
import { Nav } from "@/app/ui/nav";
import { AdminNav, SessionControls } from "@/app/ui/session-controls";
import { ThemeToggle, themeScript } from "@/app/ui/theme-toggle";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Kubeat",
  description: "OCP uptime monitoring",
};

/**
 * The language is request data (a cookie, else the browser's preference),
 * so everything that shows words waits for it; the provider also sets
 * <html lang> once it knows.
 */
async function Localized({ children }: { children: React.ReactNode }) {
  const locale = await getLocale();
  return (
    <I18nProvider locale={locale}>
      <Nav tail={<AdminNav />}>
        <LanguageToggle />
        <ThemeToggle />
        <SessionControls />
      </Nav>
      <BackendWarning />
      {/* The one scrolling area of pages that are a single column; the
          workspace fills it and scrolls its two panes instead. */}
      <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
    </I18nProvider>
  );
}

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    // The theme script changes data-theme before React hydrates.
    <html
      lang={config.defaultLocale}
      className={`${geistSans.variable} ${geistMono.variable} h-full antialiased`}
      suppressHydrationWarning
    >
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body className="flex h-dvh flex-col overflow-hidden">
        <Suspense fallback={null}>
          <Localized>{children}</Localized>
        </Suspense>
      </body>
    </html>
  );
}
