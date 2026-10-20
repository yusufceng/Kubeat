import { defineMessages } from "@/lib/i18n/define";

/**
 * Words the section pages share: spans of time, tables, dialogs, forms and
 * the states of a page with nothing in it yet.
 */
export const common = defineMessages(
  {
    // lib/format.ts: span(), as a table writes a length of time.
    "format.span.lessThanSecond": "<1 sn",
    "format.span.s": "{count} sn",
    "format.span.m": "{count} dk",
    "format.span.h": "{count} sa",
    "format.span.d": "{count} gün",
    "format.justNow": "şimdi",

    // Tables.
    "table.actions": "İşlemler",

    // Dialogs.
    "dialog.affected": "Etkilenenler",

    // Forms.
    "form.optional": "isteğe bağlı",

    // A page that has nothing to show for want of monitors.
    "noMonitors.title": "Önce izlenecek bir şey ekleyin",
    "noMonitors.body": "Kubeat cluster'ınızda çalışanları izler. Üç yoldan biriyle başlayın; ardından burası dolar.",
    "noMonitors.discoverTitle": "Cluster'ı tarayın",
    "noMonitors.discoverBody": "Bir namespace'teki Service, Route ve Ingress'leri bulur; hangilerinin izleneceğini siz seçersiniz.",
    "noMonitors.discover": "Cluster'dan keşfet",
    "noMonitors.annotateTitle": "Annotation ekleyin",
    "noMonitors.annotateBody": "Bir Service, Route ya da Ingress'in manifest'ine bu satırı ekleyin; Kubeat onu kendiliğinden izlemeye başlar.",
    "noMonitors.manualTitle": "Elle ekleyin",
    "noMonitors.manualBody": "Cluster dışındaki bir adres de olur: HTTP, TCP, DNS, ping ya da kendi rapor veren bir iş.",
    "noMonitors.add": "Monitor ekle",
    "noMonitors.viewerTitle": "Henüz görebileceğiniz bir monitor yok",
    "noMonitors.viewerBody": "Namespace'lerinizde monitor eklendiğinde burada görünür. Eklemek için bir editöre başvurun.",
    "copy.copy": "Kopyala",
    "copy.copied": "Kopyalandı",

    // app/ui/backend-error.tsx
    "backend.cannotShow":
      "{what} gösterilemiyor. Backend başlıyor ya da yeniden başlıyor olabilir; birkaç saniye sonra sayfayı yenileyin.",
  },
  {
    "format.span.lessThanSecond": "<1s",
    "format.span.s": "{count}s",
    "format.span.m": "{count}m",
    "format.span.h": "{count}h",
    "format.span.d": "{count}d",
    "format.justNow": "just now",

    "table.actions": "Actions",

    "dialog.affected": "Affected",

    "form.optional": "optional",

    "noMonitors.title": "First, something to watch",
    "noMonitors.body": "Kubeat watches what runs in your cluster. Start in one of three ways; this page fills in after.",
    "noMonitors.discoverTitle": "Scan the cluster",
    "noMonitors.discoverBody": "Finds the Services, Routes and Ingresses of a namespace; you choose which of them to watch.",
    "noMonitors.discover": "Discover in the cluster",
    "noMonitors.annotateTitle": "Add an annotation",
    "noMonitors.annotateBody": "Put this line into the manifest of a Service, Route or Ingress; Kubeat starts watching it on its own.",
    "noMonitors.manualTitle": "Add one by hand",
    "noMonitors.manualBody": "An address outside the cluster works too: HTTP, TCP, DNS, ping, or a job that reports in.",
    "noMonitors.add": "Add a monitor",
    "noMonitors.viewerTitle": "No monitors you can see yet",
    "noMonitors.viewerBody": "Monitors show up here once they are added in your namespaces. Ask an editor to add them.",
    "copy.copy": "Copy",
    "copy.copied": "Copied",

    "backend.cannotShow":
      "{what} cannot be shown. The backend may be starting or restarting; reload the page in a few seconds.",
  },
);
