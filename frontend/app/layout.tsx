import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import { BackendWarning } from "@/app/ui/backend-warning";
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

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    // The theme script changes data-theme before React hydrates.
    <html
      lang="tr"
      className={`${geistSans.variable} ${geistMono.variable} h-full antialiased`}
      suppressHydrationWarning
    >
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body className="flex h-dvh flex-col overflow-hidden">
        <Nav tail={<AdminNav />}>
          <ThemeToggle />
          <SessionControls />
        </Nav>
        <BackendWarning />
        {/* The one scrolling area of pages that are a single column; the
            workspace fills it and scrolls its two panes instead. */}
        <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
      </body>
    </html>
  );
}
