import { auth } from "./auth";
import { channels } from "./channels";
import { common } from "./common";
import { discovery } from "./discovery";
import { incidents } from "./incidents";
import { monitorForm } from "./monitor-form";
import { monitorPage } from "./monitor-page";
import { monitorsList } from "./monitors-list";
import { reports } from "./reports";
import { shell } from "./shell";

export const locales = ["en", "tr"] as const;
export type Locale = (typeof locales)[number];

const modules = [shell, auth, monitorsList, monitorPage, monitorForm, discovery, channels, common, incidents, reports];

/** Every message of every area, by locale. */
export const messages: Record<Locale, Record<string, string>> = {
  tr: Object.assign({}, ...modules.map((m) => m.tr)),
  en: Object.assign({}, ...modules.map((m) => m.en)),
};
