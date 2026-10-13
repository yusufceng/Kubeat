import { auth } from "./auth";
import { channels } from "./channels";
import { discovery } from "./discovery";
import { monitorForm } from "./monitor-form";
import { monitorPage } from "./monitor-page";
import { monitorsList } from "./monitors-list";
import { shell } from "./shell";

export const locales = ["en", "tr"] as const;
export type Locale = (typeof locales)[number];

const modules = [shell, auth, monitorsList, monitorPage, monitorForm, discovery, channels];

/** Every message of every area, by locale. */
export const messages: Record<Locale, Record<string, string>> = {
  tr: Object.assign({}, ...modules.map((m) => m.tr)),
  en: Object.assign({}, ...modules.map((m) => m.en)),
};
