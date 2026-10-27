# Ertelenen fikirler

Konuşulup bilerek sonraya bırakılan işler. Buradaki bir madde, plana alınana
kadar yapılacak iş değildir.

## Alertmanager'a doğrudan kanal

Kubeat'in olayları Alertmanager API'sine (`/api/v2/alerts`) kendisinin
göndermesi. 2026-10-12'de bunun yerine chart'a PrometheusRule eklendi:
cluster'ın Prometheus'u `/metrics`'ten `kubeat_monitor_incident_open`
serisini okuyup alarmı kendisi üretir; token, CA ve `resolve_timeout` için
tekrar gönderme döngüsü gerekmez. Doğrudan kanal yalnızca user workload
monitoring kapalı cluster'larda anlam kazanır; böyle bir ortam görülürse
Kubeat'in kanal türü olarak eklenir (OpenShift'te `monitoring-alertmanager-
edit` rolü ve kube-rbac-proxy üzerinden 9095 portu).

## Pod bazında sağlık

Service üzerinden tek kontrol, arkadaki pod'lardan birkaçı bozukken çoğunlukla
başarılı döner. İki olası çözüm konuşuldu (2026-10-12):

Güncelleme (2026-10-12): monitor sayfasına "Pod'lar" paneli eklendi. Panel
hazır pod sayısını, pod durumlarını ve restart'ları cluster API'sinden canlı
okur, CPU/belleği Prometheus'tan alır. Aşağıdaki iki madde hâlâ geçerli:
panel yalnızca gösterir, saklamaz ve alarm üretmez.

Güncelleme (2026-10-12, sonra): 1. madde `workload` monitor tipiyle
karşılandı: hazır pod sayısı istenenin altına düşünce olay açılır ve bildirim
gider. Restart sayıları için alarm yok; panelde görünür. 2. madde hâlâ açık.

1. **Hazır/istenen replika oranı ve restart sayıları.** Bu veri OCP'nin kendi
   Prometheus'unda (kube-state-metrics) zaten var ve hazır alarm kuralları ile
   geliyor (`KubeDeploymentReplicasMismatch`, `KubePodCrashLooping`). Kendimiz
   toplamak tekerleği yeniden icat etmek olur. Gerekirse plandaki Thanos
   entegrasyonu sırasında yalnızca okunup gösterilir.
2. **Pod'ları tek tek yoklamak.** EndpointSlice'tan pod IP'leri alınıp her
   birine gerçek iş yolundan istek atılır. "Ready görünüyor ama cevap vermiyor"
   durumunu yalnızca bu yakalar ve hazır araçlarda karşılığı yoktur.

   Güncelleme (2026-10-12, en son): `each_pod` seçeneğiyle yapıldı; agent ve
   monitor başına birden çok örnek gerekmedi, çünkü backend cluster içinde
   çalışır ve pod'lara doğrudan erişir; sonuç tek kayıttır, düşen pod'lar
   hata metninde adlarıyla yer alır. Pod başına ayrı geçmiş tutulmaz.

## Sohbet kanallarında kalan biçim kaçışlaması

2026-10-16'da Slack, Mattermost ve Google Chat'te `&`, `<`, `>` kaçışlandı,
Discord mesajı kimseyi etiketlemez oldu: push raporunun mesajı ya da bir
hedefin hata metni `<!channel>` ile herkesi çağıramıyor, etiketi adresini
gizleyen bağlantı kuramıyor. Kalanlar: Mattermost düz `@channel`, `@all`,
`@here` ve markdown `[etiket](adres)` yazımını da tanır; Teams'in TextBlock'u
ve Gotify'ın mesajı markdown işler. Konuşulan çözüm, dışarıdan gelen
değerleri satır içi kod olarak yazmak ya da `@` işaretini bozmaktır;
metinlerin okunurluğunu değiştirdiği için ayrıca karar verilmeli.

Güncelleme (2026-10-16, sonra): Mattermost için yapıldı. Ona giden metinde
her `@` ve `]` işaretinden sonra görünmez bir boşluk (U+200B) var: hiçbir
bahsetme kimseyi çağırmıyor, `[etiket](adres)` bağlantı ya da resim
olmuyor ve metin aynı okunuyor. Teams ve Gotify hâlâ açık.

## Başarısız kontrollerin ayrıntılarında kalanlar

2026-10-16'da başarısız kontroller nedenlerini saklamaya başladı (README,
"Başarısız kontrolün ayrıntıları"). Bilerek sonraya bırakılanlar:

- Webhook gövdesinde açıklamanın kodu: genel webhook kanalı şimdilik
  yalnızca hata metnini (`incident.cause`) alır; ayrıntı alanı olan
  kanallarda (PagerDuty dahil) "Neden" alanı açıklamayı taşır.
- TCP, gRPC ve veritabanı kontrollerinde zamanlama, adres ve sertifika
  ayrıntıları; bu tiplerde yalnızca açıklama saklanır.
- Olayın kendisinde açıklama: bugün yalnızca kesinti bildirimi taşır; olay
  sayfası gelince açılıştaki başarısızlığın açıklaması olaya yazılabilir.
- Monitor listesindeki kısa geçmişte (`history`) ve ızgaranın en sık
  hatalarında açıklama kodu.

## UptimeRobot karşılaştırmasından ertelenenler

2026-10-20'de UptimeRobot (canlı hesap ve dokümanlar) Kubeat ile
karşılaştırıldı. Konum: UptimeRobot dışarıdan bakan bir SaaS, Kubeat
cluster'ın içinden bakar; rakipten çok tamamlayıcıdır. Plana alınanlar
(durum sayfası bölümleri, kurallarla üyelik, adımlı duyurular, erişim
ayarları, servis detay sayfası, NOC modu; kanal başına gecikme ve tekrar,
monitor'dan test bildirimi, yavaş yanıt eşiği) 0.40 turunda yapılıyor.
Bilerek sonraya bırakılanlar:

- **Durum sayfasına e-postayla abonelik.** SMTP zaten var; abone listesi,
  doğrulama ve abonelikten çıkma akışı gerekir. Duyurular oturunca
  değerlendirilir.
- **Üçüncü taraf servislerin durumu** (AWS, GitHub, quay.io, registry.redhat.io
  gibi tedarikçilerin durum sayfaları; çoğu Atlassian Statuspage JSON'u).
  Monitor tipi olarak değil, olay sayfasında "aynı anda tedarikçi X de
  bozukluk bildiriyordu" bağlamı olarak düşünülecek.
- **SLO hedefi ve hata bütçesi.** Hedefin nerede tutulacağı (annotation mı,
  monitor alanı mı) ve raporlardaki yeri ayrı bir karar.
- **Durum sayfası markalaması** (logo, yazı tipi, renkler) yapılmayacaklar
  listesinde kalıyor; yalnızca sıkışık yerleşim ve bölümler yapılıyor.

## TypeScript 7 ve ESLint 10

Dependabot'un iki major güncellemesi 2026-10-26'da denendi ve bekletildi.
TypeScript 7.0.2 ile `next build` tip kontrolü geçiyor, ama
typescript-eslint 8.72 TypeScript 7'yi desteklemediği için `npm run lint`
açılışta çöküyor. ESLint 10.13 ile lint geçiyor, ama eslint-config-next
16.4.3'ün içindeki `eslint-plugin-import`, `eslint-plugin-jsx-a11y` ve
`eslint-plugin-react` eklentileri peer olarak `eslint ^9` istiyor. İkisi de
bu eklentiler destek verdiğinde yeniden denenir. O zamana kadar
Dependabot'un PR'ları (#12, #13) kapatılabilir.

Güncelleme (2026-10-27): PR'lar kapatıldı; `.github/dependabot.yml`
TypeScript, ESLint ve `@types/node` major sürümlerini yok sayıyor (sonuncusu
çalışma zamanının Node major'ına bağlı). Üçü de elle denenir.

## Kurumsal kodun lisansı

2026-10-26'da open-core seçildi; 0.42'den itibaren kurumsal özellikler
imzalı bir lisans dosyasıyla açılıyor (README, "Kubeat Enterprise
lisansı"). Kodun tamamı hâlâ Apache-2.0: lisans kontrolünü koddan çıkaran
biri bunu yasal olarak yapabilir. Lisans dosyası teknik bir kilit değil,
sözleşmenin bir parçasıdır. Satış ciddileşirse şunlar kararlaştırılır:

- Kurumsal özelliklerin kodunu (`ldapauth`, `oidcauth` ve kapıların
  arkasındaki kısımlar) ayrı bir dizine (`ee/`) ve kaynak açık ama ticari
  bir lisansa (Elastic License 2.0, BSL ya da özel bir EULA) taşımak.
  Yayınlanmış sürümler Apache olarak kalır.
- EULA, üçüncü taraf lisans taraması, marka araştırması, destek süreci.
- Dış katkı gelmeye başlarsa CLA ya da DCO.

İmzalama aracı Kubeat deposunda değil, özel bir depodadır; verilen
lisansların kaydı da orada tutulur.

Güncelleme (2026-10-27): kararlar şöyle verildi.

- **Kodun lisansı.** Pilot boyunca ve ilk dış müşteri görüşmesine kadar her
  şey Apache-2.0 kalır: pilot şirket içidir, dışarıda kullanan yoktur,
  bugün ayırmak yalnızca iş çıkarır. Tetikleyici: ilk dış müşteri görüşmesi
  ya da ilk kamuya duyuru, hangisi önce gelirse. O zaman `ldapauth`,
  `oidcauth`, `license` ve kapıların arkasındaki handler'lar `backend/ee/`
  altına taşınır ve GitLab EE / Elastic License 2.0 örneğindeki gibi
  "geçerli lisansla kullanılır" diyen kaynağı açık ticari bir lisans alır;
  kalanı Apache-2.0 kalır. Tek telif sahibi olduğundan yeniden lisanslama
  için kimsenin onayı gerekmez.
- **Üçüncü taraf lisansları** tarandı. Backend'e bağlanan 73 Go modülü:
  Apache-2.0 (37), BSD (19), MIT (15), ISC (1), MPL-2.0 (1,
  `go-sql-driver/mysql`; dosya bazlı copyleft, değiştirilmeden kullanılıyor,
  ticari dağıtıma engel değil). Frontend'in üretim paketleri MIT, Apache-2.0,
  ISC, BSD, 0BSD; `caniuse-lite` verisi CC-BY-4.0, Next.js'in görsel
  kütüphanesi `sharp`'ın libvips ikilileri LGPL-3.0 (dinamik bağlanır, her
  Next.js kurulumunda aynı; Kubeat `next/image` kullanmaz). GPL/AGPL yok.
  Satıştan önce araçla (go-licenses, license-checker) bir kez daha
  tekrarlanır.
- **Ad.** "Kubeat" için web'de bir ürün ya da marka çakışması bulunmadı; en
  yakını farklı bir ad olan KUBECOST (IBM) markası. TÜRKPATENT ve EUIPO'da
  resmî sorgu satıştan önce yapılır; pilot için gerekmez.
- **EULA ve destek.** Şirket içi pilotta sözleşme yok; destek kullanıcının
  kendisi, aynı iş günü. İlk dış müşteride kısa bir Kubeat Enterprise lisans
  sözleşmesi taslağı hazırlanır ve imzadan önce avukata gösterilir.
- **Anahtar.** Backend tek açık anahtar tanır. Anahtar kaybolursa yeni
  anahtar üretilir, açık anahtar yeni sürümle gömülür ve verilmiş lisanslar
  yeniden verilir; üç lisansla bu ucuz, dış müşteriyle pahalılaşır. Birden
  fazla açık anahtar tanıma (rotasyon) ilk dış müşteriyle eklenir.
