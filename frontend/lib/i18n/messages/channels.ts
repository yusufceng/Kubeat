import { defineMessages } from "@/lib/i18n/define";

/** Notification channels, status pages, and the public status page. */
export const channels = defineMessages(
  {
    // app/channels/page.tsx
    "channels.what": "Bildirim kanalları",
    "channels.intro":
      "Bir olay açıldığında ve kapandığında, kapsamı o monitor'u içeren etkin kanallara bildirim gönderilir.",
    "channels.empty.editor": "Henüz kanal yok. Kanal eklenmeden olaylar kaydedilir ama kimseye bildirim gitmez.",
    "channels.empty.viewer": "Bildirim kanallarını editörler ve yöneticiler yönetir.",
    "channels.new.title": "Yeni kanal",

    // app/ui/channel-forms.tsx — the form
    "channelForm.type.label": "Tür",
    "channelForm.name.label": "Ad",
    "channelForm.name.placeholder": "ops-kanali",
    "channelForm.target.masked": "Kayıtlı adres gizlidir; değiştirmezseniz korunur.",
    "channelForm.secret.masked": "Kayıtlı değer gösterilmez; değiştirmezseniz korunur.",
    "channelForm.scope.legend": "Kapsam",
    "channelForm.scope.allMonitors":
      "Tüm monitor'lar. Namespace'li monitor eklendiğinde kanal belirli namespace'lerle sınırlanabilir.",
    "channelForm.scope.hint":
      "Hiçbiri seçilmezse kanal tüm monitor'ların bildirimlerini alır; seçilirse yalnızca o namespace'lerdekileri.",
    "channelForm.tags.label": "Etiketler (isteğe bağlı)",
    "channelForm.tags.placeholder": "pager, kritik",
    "channelForm.tags.hint":
      "Virgülle ayırın. Verilirse yalnızca bu etiketlerden birini taşıyan monitor'lar; monitor başına yönlendirme böyle yapılır.",
    "channelForm.events.legend": "Olaylar",
    "channelForm.events.hint": "Örneğin PagerDuty yalnızca kesinti ve düzelme, sohbet kanalı hepsi.",
    "channelForm.create": "Kanalı ekle",

    // app/ui/channel-forms.tsx — one channel in the list
    "channelRow.paused": "Durduruldu",
    "channelRow.allMonitors": "tüm monitor'lar",
    "channelRow.onlyNamespaces": "yalnızca {namespaces}",
    "channelRow.tags": "etiket: {tags}",
    "channelRow.test.send": "Test gönder",
    "channelRow.test.sending": "Gönderiliyor…",
    "channelRow.test.delivered": "Test bildirimi gönderildi.",
    "channelRow.test.failed": "Gönderilemedi: {error}",
    "channelRow.pause.title": "Bildirim göndermeyi durdur",
    "channelRow.resume.title": "Yeniden etkinleştir",
    "channelRow.pause": "Durdur",
    "channelRow.resume": "Devam ettir",
    "channelRow.delete.confirm": "\"{name}\" kanalı silinsin mi?",

    // app/status-pages/page.tsx
    "statusPages.intro":
      "Seçilen servislerin durumunu ve uptime geçmişini, giriş gerektirmeyen bir adreste gösterir: ekip dışındakiler, müşteriler ya da duvardaki ekran için.",
    "statusPages.empty":
      "Henüz durum sayfası yok. Bir sayfa, seçtiğiniz monitor'ların durumunu giriş yapmadan görülebilen bir adreste yayınlar.",
    "statusPages.new.title": "Yeni durum sayfası",

    // app/ui/status-page-forms.tsx — the form
    "statusPageForm.title.label": "Başlık",
    "statusPageForm.title.placeholder": "Mağaza servisleri",
    "statusPageForm.slug.label": "Adres",
    "statusPageForm.slug.title": "Küçük harf, rakam ve tire",
    "statusPageForm.slug.placeholder": "magaza",
    "statusPageForm.description.label": "Açıklama (isteğe bağlı)",
    "statusPageForm.description.placeholder": "Müşterilerin kullandığı servislerin durumu",
    "statusPageForm.announcement.label": "Duyuru (isteğe bağlı)",
    "statusPageForm.announcement.placeholder": "Cumartesi 02:00-04:00 arasında planlı bakım yapılacaktır.",
    "statusPageForm.announcement.hint": "Sayfanın en üstünde gösterilir; kaldırmak için boşaltın.",
    "statusPageForm.monitors.legend": "Gösterilecek monitor'lar",
    "statusPageForm.monitors.empty": "Henüz monitor yok.",
    "statusPageForm.monitors.displayName.aria": "{label} için sayfada görünen ad",
    "statusPageForm.monitors.displayName.placeholder": "Sayfada görünen ad (isteğe bağlı)",
    "statusPageForm.monitors.hint":
      "Sayfa yalnızca adı, durumu ve uptime geçmişini gösterir; adresler ve hata ayrıntıları gösterilmez.",
    "statusPageForm.published.label": "Yayında (işaretli değilse sayfa açılmaz)",
    "statusPageForm.create": "Sayfayı oluştur",

    // app/ui/status-page-forms.tsx — one page in the list
    "statusPageRow.published": "Yayında",
    "statusPageRow.draft": "Taslak",
    "statusPageRow.monitors.one": "{count} monitor",
    "statusPageRow.monitors.other": "{count} monitor",
    "statusPageRow.hasAnnouncement": "duyuru var",
    "statusPageRow.open": "Sayfayı aç",
    "statusPageRow.delete.confirm": "\"{title}\" durum sayfası silinsin mi? Monitor'lar silinmez.",
    "statusPageRow.badges.summary": "Rozet adresleri (README ya da wiki için)",

    // app/status/[slug]/page.tsx — the public page
    "statusPage.state.up": "Çalışıyor",
    "statusPage.state.down": "Kesinti",
    "statusPage.state.maintenance": "Bakımda",
    "statusPage.state.pending": "Bekleniyor",
    "statusPage.state.paused": "İzlenmiyor",
    "statusPage.overall.up": "Tüm sistemler çalışıyor",
    "statusPage.overall.degraded": "Bazı sistemlerde kesinti var",
    "statusPage.overall.down": "Sistemlerde kesinti var",
    "statusPage.overall.maintenance": "Bakım çalışması sürüyor",
    "statusPage.history.aria.one": "Son {count} günün uptime geçmişi",
    "statusPage.history.aria.other": "Son {count} günün uptime geçmişi",
    "statusPage.noData": "veri yok",
    "statusPage.unavailable": "Durum bilgisi şu anda alınamıyor. Sayfa kendiliğinden yeniden deneyecek.",
    "statusPage.announcement": "Duyuru",
    "statusPage.noMonitors": "Bu sayfada henüz gösterilen bir servis yok.",
    "statusPage.uptime": "{value} uptime",
    "statusPage.today": "bugün",
    "statusPage.incidents.title": "Son kesintiler",
    "statusPage.incidents.none": "Yakın zamanda kesinti olmadı.",
    "statusPage.incidents.lasted": "{duration} sürdü",
    "statusPage.incidents.ongoing": "devam ediyor",
    "statusPage.footer": "Kubeat ile izleniyor",
  },
  {
    // app/channels/page.tsx
    "channels.what": "Notification channels",
    "channels.intro":
      "When an incident opens and when it closes, a notification goes to the enabled channels whose scope includes that monitor.",
    "channels.empty.editor": "No channels yet. Without a channel, incidents are recorded but nobody is notified.",
    "channels.empty.viewer": "Notification channels are managed by editors and administrators.",
    "channels.new.title": "New channel",

    // app/ui/channel-forms.tsx — the form
    "channelForm.type.label": "Type",
    "channelForm.name.label": "Name",
    "channelForm.name.placeholder": "ops-channel",
    "channelForm.target.masked": "The saved address is hidden; it is kept unless you change it.",
    "channelForm.secret.masked": "The saved value is not shown; it is kept unless you change it.",
    "channelForm.scope.legend": "Scope",
    "channelForm.scope.allMonitors":
      "All monitors. Once a monitor with a namespace is added, the channel can be limited to certain namespaces.",
    "channelForm.scope.hint":
      "With none selected, the channel receives the notifications of all monitors; with some selected, only those of the monitors in these namespaces.",
    "channelForm.tags.label": "Tags (optional)",
    "channelForm.tags.placeholder": "pager, critical",
    "channelForm.tags.hint":
      "Comma-separated. If given, only monitors that carry one of these tags; this is how routing per monitor is done.",
    "channelForm.events.legend": "Events",
    "channelForm.events.hint": "For example PagerDuty only outages and recoveries, a chat channel all of them.",
    "channelForm.create": "Add channel",

    // app/ui/channel-forms.tsx — one channel in the list
    "channelRow.paused": "Paused",
    "channelRow.allMonitors": "all monitors",
    "channelRow.onlyNamespaces": "only {namespaces}",
    "channelRow.tags": "tags: {tags}",
    "channelRow.test.send": "Send a test",
    "channelRow.test.sending": "Sending…",
    "channelRow.test.delivered": "The test notification was sent.",
    "channelRow.test.failed": "Could not be sent: {error}",
    "channelRow.pause.title": "Stop sending notifications",
    "channelRow.resume.title": "Enable again",
    "channelRow.pause": "Pause",
    "channelRow.resume": "Resume",
    "channelRow.delete.confirm": "Delete the channel \"{name}\"?",

    // app/status-pages/page.tsx
    "statusPages.intro":
      "Shows the state and the uptime history of the chosen services at an address that needs no sign-in: for people outside the team, customers or the screen on the wall.",
    "statusPages.empty":
      "No status pages yet. A page publishes the state of the monitors you choose at an address that can be seen without signing in.",
    "statusPages.new.title": "New status page",

    // app/ui/status-page-forms.tsx — the form
    "statusPageForm.title.label": "Title",
    "statusPageForm.title.placeholder": "Shop services",
    "statusPageForm.slug.label": "Address",
    "statusPageForm.slug.title": "Lowercase letters, digits and hyphens",
    "statusPageForm.slug.placeholder": "shop",
    "statusPageForm.description.label": "Description (optional)",
    "statusPageForm.description.placeholder": "The state of the services customers use",
    "statusPageForm.announcement.label": "Announcement (optional)",
    "statusPageForm.announcement.placeholder": "Planned maintenance on Saturday between 02:00 and 04:00.",
    "statusPageForm.announcement.hint": "Shown at the top of the page; clear it to remove it.",
    "statusPageForm.monitors.legend": "Monitors to show",
    "statusPageForm.monitors.empty": "No monitors yet.",
    "statusPageForm.monitors.displayName.aria": "Name shown on the page for {label}",
    "statusPageForm.monitors.displayName.placeholder": "Name shown on the page (optional)",
    "statusPageForm.monitors.hint":
      "The page shows only the name, the state and the uptime history; addresses and error details are not shown.",
    "statusPageForm.published.label": "Published (if unchecked, the page does not open)",
    "statusPageForm.create": "Create the page",

    // app/ui/status-page-forms.tsx — one page in the list
    "statusPageRow.published": "Published",
    "statusPageRow.draft": "Draft",
    "statusPageRow.monitors.one": "{count} monitor",
    "statusPageRow.monitors.other": "{count} monitors",
    "statusPageRow.hasAnnouncement": "has an announcement",
    "statusPageRow.open": "Open the page",
    "statusPageRow.delete.confirm": "Delete the status page \"{title}\"? The monitors are not deleted.",
    "statusPageRow.badges.summary": "Badge URLs (for a README or a wiki)",

    // app/status/[slug]/page.tsx — the public page
    "statusPage.state.up": "Operational",
    "statusPage.state.down": "Outage",
    "statusPage.state.maintenance": "Under maintenance",
    "statusPage.state.pending": "Pending",
    "statusPage.state.paused": "Not monitored",
    "statusPage.overall.up": "All systems operational",
    "statusPage.overall.degraded": "Some systems are experiencing an outage",
    "statusPage.overall.down": "Systems are experiencing an outage",
    "statusPage.overall.maintenance": "Maintenance is in progress",
    "statusPage.history.aria.one": "Uptime history of the last day",
    "statusPage.history.aria.other": "Uptime history of the last {count} days",
    "statusPage.noData": "no data",
    "statusPage.unavailable": "The status cannot be retrieved right now. The page will retry by itself.",
    "statusPage.announcement": "Announcement",
    "statusPage.noMonitors": "This page does not show any service yet.",
    "statusPage.uptime": "{value} uptime",
    "statusPage.today": "today",
    "statusPage.incidents.title": "Recent outages",
    "statusPage.incidents.none": "No outages recently.",
    "statusPage.incidents.lasted": "lasted {duration}",
    "statusPage.incidents.ongoing": "ongoing",
    "statusPage.footer": "Monitored with Kubeat",
  },
);
