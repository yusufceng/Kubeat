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

## Denetim kaydı: stdout log satırı, süzgeçler, CSV ve kaynak adresi

Sprint 1'in PO turunda (2026-10-27) ertelendi (hikâye S4,
[sprints/2026-10-27-sprint-01.md](sprints/2026-10-27-sprint-01.md)). Tek
kaynak: kurumsal müşterinin SIEM ihtiyacı ("kim, nereden, ne zaman", 30
cluster); L boyut, migration ister. Tetikleyici: ilk dış kurumsal görüşme.
Kararlar: başarılı denetim yazımından sonra stdout'a yazılan tek log satırı
lisans kapısının dışındadır (ADR-0002'nin "görmek" kapısı arayüz, API ve
CSV'dir; README "Kubeat Enterprise lisansı" zaten "Kayıt yine yazılır" der),
hikâye alındığında ADR'ye güncelleme notu yazılır; `remote_addr` için
X-Forwarded-For yalnızca frontend üzerinden gelen istekte, doğrudan API
isteğinde `r.RemoteAddr`; `user_agent` ve hash zinciri yok; "90 gün
değiştirilemez saklama" SIEM'in işidir. Stdout satırı tek başına S boyutlu
bir parça olarak sprint 2 ya da 3 dolgusu olabilir.

## Oturuma boşta kalma süresi `UPTIME_SESSION_IDLE`

2026-10-27'de ertelendi (S5). Kurumsal güvenlik kontrol listesi maddesi
("etkileşimsiz 15 dakikadan sonra oturum düşer"); bugün yalnızca 12 saatlik
mutlak süre var (`UPTIME_SESSION_TTL`). Varsayılan kapalı olacağından küçük
ekip için değer üretmez; L boyut (backend, frontend, chart). Tetikleyici:
kurumsal güvenlik incelemesi ya da pilot ekibinin isteği. Kararlar: pilot
values'ta 15 dk; 401 `reason=idle` ile giriş sayfasına; çerez max-age
TTL'de kalır; "oturumunuz kapanmak üzere" uyarısı yok.

## Şema yükselten açılışta migration'dan önce otomatik yedek

2026-10-27'de ertelendi (S6). Sprint 1 migration eklemiyor (şema 26 kalır),
bu yüzden sırası şemaya bağlandı. Kural: migration ekleyen ilk hikâye
(bugün S4) sprint'e girdiğinde S6 aynı sprintte ondan önce yapılır.
Kararlar: `UPTIME_BACKUP_DIR` doluysa ve dosyanın şeması ikilininkinden
eskiyse `OpenSQLite`'tan önce `kubeat-pre-schema<N>-<zaman>.db` kopyası
alınır; dizin açıkken kopya alınamazsa açılış durur, dizin kapalıysa WARN;
pre-schema kopyaları `UPTIME_BACKUP_KEEP`'ten bağımsız, son 2 tutulur; down
migration yok.

## Yedekleri cluster dışına taşıma belgesi ve backend imajına `tar`

2026-10-27'de ertelendi (S7). S boyut, kod yok: README "Yedekleme ve geri
yükleme"ye "Cluster dışına" bölümü (yedek PVC'sinin OADP/Velero
snapshot'ının tutarlı olduğu, `kubeat-secret-key` Secret'ının aynı sete
girdiği, 0600 dosyalar için aynı UID'de çalışan CronJob örneği) ve
`kubectl cp`/`oc rsync` için ubi-minimal imaja `tar`. İhtiyaç pilot sonu
"devam" kararıyla doğar (pilot.md "Pilot sonunda"); sprint 3 adayı. Karar:
imaja `tar` kabul (UBI paketi, taban imaj güncellemesiyle Dependabot
kapsar).

## Yedek kopyalarını S3 uyumlu depoya yüklemek

2026-10-27'de ertelendi (S8). `UPTIME_BACKUP_S3_*` ile her başarılı kopyanın
yüklenmesi ve `kubeat_backup_upload_last_success_timestamp_seconds`. Şişirme
ve "başka araç yapıyor": Velero/OADP PVC snapshot'ı ve S7'nin CronJob'u aynı
işi platformla görür; AWS SDK v2 ~10 modül ekler; tek kaynak, 30 cluster'lı
kurumsal senaryo. Tetikleyici: çok cluster'lı ilk dış müşterinin açık
isteği. Kayıt: air-gapped iç S3 (ODF/MinIO) için CA ayarı şart; tercih
aws-sdk-go-v2; endpoint yöneticinin ayarı olduğundan hedef politikasından
muaf.

## CVE tarama raporu yayımlamak

2026-10-27'de karar: hayır. Tedarik zinciri hikâyesinin (S1, sprint 2 adayı)
açık sorusuydu; SBOM varken müşteri raporu kendi tarayıcısıyla (Trivy,
grype) kendisi üretir. Kyverno/ClusterImagePolicy örnek manifest'i de
müşterinin politikasıdır, ürünün işi değildir.

## Yöneticinin kendi alan adları: `UPTIME_OWN_HOSTS`

2026-10-27'de ertelendi (S13). Küçük ekibin kendi genel alan adı "dış host"
sayılıyor: aynı host'a en çok 5 monitor ve 30 sn alt sınır. Tek kaynak;
sandbox pilotunda kota aşılmıyor (her Route ayrı host). Tetikleyici: kendi
alan adında kota reddi yaşayan ilk kullanıcı. Karar şimdiden verildi:
yalnızca yöneticinin listesi (`UPTIME_OWN_HOSTS`, `backend.extraEnv` ile),
keşfin gördüğü Ingress/Route host'larını kendiliğinden "benim" saymak asla
(netguard/incluster.go ve README "Hedef yükü" gerekçesi korunur: namespace
editörü Ingress'e yabancı bir host yazıp kotayı aşamamalı).

## API'de ada göre arama ve `upsert=true`; `kubeat.io/path` Ingress ve Route'ta; openapi.yaml

2026-10-27'de ertelendi (S14). Betik/Terraform ile idempotent uygulama
isteği: aynı adla POST bugün 409, ada göre arama yok, OpenAPI yok;
`kubeat.io/path` yalnızca Service'te tanınıyor. Annotation yolu zaten
idempotent ve cluster-native. Tetikleyici: betik/Terraform ile uygulama
isteyen ilk kullanıcı. `kubeat.io/path`'in Ingress ve Route'ta tanınması
küçük ve cluster-native bir parça; BA isterse ayrı hikâye olarak öne
çekilebilir. OpenAPI dosyası (BA'nın "tek başına hikâye değil" kararı) aynı
maddede. Karar: upsert PATCH değil tam değiştirme; PUT ve ada göre DELETE
yok.

## Ücretsiz katmanda tek OIDC sağlayıcısıyla giriş; alternatifi TOTP 2FA

2026-10-27'de ertelendi (S16). Küçük ekibin "Google Workspace ile giriş,
ayrılan kişinin erişimi orada kapanınca kapansın" isteği. ADR-0002'ye aykırı
(`sso` = LDAP ve OIDC) ve ADR bir günlük; pilotun 4. sorusu tam olarak hangi
ücretli özelliklerin olmazsa olmaz olduğunu ölçer. ADR yeniden açılmadı.
Tetikleyici: pilot kararı (2026-11-27) ve pilot sorusu 4'ün cevabı. Alınırsa
BA önerisi: OIDC girişi kapının dışına, grup→rol eşlemesi, LDAP ve namespace
sınırı kapıda kalır. Alternatifi TOTP 2FA da burada; yol haritasında
bilerek dışarıda.

## Cluster API'ye ulaşılamayan kontrolü hedef hatası saymamak

2026-10-27'de ertelendi (S20). ≥ 90 s süren bir cluster API kesintisi her
`workload` ve `each_pod` monitor'ünde olay açıp bildirim gönderir (SRE orta;
L boyut, 11 dosya). Yanlış alarm pilotun karar sorusu, ama sandbox'ta bu tip
monitor az ve böyle bir kesinti görülmedi (varsayım). Tetikleyici: pilotta
API kesintisi kaynaklı yanlış alarm görülürse öne alınır; aksi halde sprint 3
adayı. Karar: unreachable/timeout/unauthorized/forbidden/sertifika sınıfları
varsayılan olarak atlanır (sonuç yazılmaz, sayaç ilerlemez),
`kubeat_cluster_api_failures_total{reason}` sayacı ve
`KubeatClusterAPIUnreachable` kuralı eklenir; "not found" ve "no ready pod"
başarısızlık kalır; `UPTIME_CLUSTER_API_FAILURE_AS_DOWN=true` eski davranışı
seçer. Bugünkü davranış yol haritası "Bilinen sınırlar"a yazıldı.

## Okuma havuzuna bağlantı sınırı ve durum sayfasına eşzamanlılık sınırı

2026-10-27'de ertelendi (S21). Okuma havuzu sınırsız (`sqlite.go`
`openPool`); kesinti anında herkese açık durum sayfasını yenileyen 200 kişi
200 bağlantı demek, backend 256 Mi limitine itilip OOMKill ile izlemeyi
durdurabilir (SRE orta/risk; doğrulama BA'nın). Sandbox'ın durum sayfasının
dış ziyaretçisi yok. Tetikleyici: durum sayfası dışarıya açıldığında ya da
kullanıcının yük testinde 200 eşzamanlı istek ölçüldüğünde (ajanlar backend
çalıştırmaz, ölçüm kullanıcının); sprint 3 adayı. Karar: `UPTIME_DB_READ_CONNS`
(varsayılan 16) ve `UPTIME_PUBLIC_CONCURRENCY` (varsayılan 32); semafor
dolunca 2 sn bekleme, sonra 503 + `Retry-After: 5`. Durum sayfası önbelleği
ayrı karar olarak bilinen sınır kalır.

## Kontrol zaman aşımına üst sınır `UPTIME_MAX_CHECK_TIMEOUT`

2026-10-27'de ertelendi (S22). Zaman aşımının mutlak üst sınırı yok; SYN'e
cevap vermeyen 20 hedef 60 s/60 s ile tüm worker'ları tutup kalan
monitor'lerin kontrolünü fiilen durdurabilir (SRE orta/risk). Sprint 1'deki
`KubeatWorkersSaturated` alarmı (S9) tespiti sağladığından önleme sonraya
kaldı. Tetikleyici: alarm yanınca ya da sprint 3. Karar: varsayılan `60s`,
`0` kapalı; API ve annotation doğrulaması aşan değeri reddeder; mevcut aşan
monitor'lere dokunulmaz, açılışta WARN ile listelenir, sonraki düzenlemede
reddedilir; `UPTIME_WORKERS` açıklamasına boyutlandırma kuralı.

## .env.example'ı tamamlamak ve config ile karşılaştıran CI denetimi

2026-10-27'de ertelendi (S24). `.env.example` 11 ayarı eksik (OIDC claim/CA/
zaman aşımı, Prometheus sorguları, olay önbelleği); chart dışı kurulum yapan
biri tüm ayarları oradan görebilmeli (SRE düşük/docs). Sprint ekibinin
"Tamamlanma tanımı" zaten her hikâyede `.env.example` ister, reviewer
denetler. Sprint 3 dolgusu (S); tetikleyici: chart dışı kurulum isteyen ilk
kullanıcı ya da sprint 1-2'de bir sapma görülürse. Karar: betik Makefile'da
(`make lint`), CI onu çağırır; chart values ile karşılaştırma yok (chart her
ayarı `extraEnv` ile alır).

## Yedek sıkıştırma ve dolan yedek PVC'sini büyütme

2026-10-28'de ertelendi (S19'un kapsam dışı parçaları, #28). S19 ile yedek
volume'u dolunca yedekleme durmuyor: eski kopyalar yeni kopyadan önce
siliniyor, yine de yer yoksa bir kopya daha silinip yazım bir kez
yineleniyor. `keep ≥ 2`'de son kopya korunur: o da yetmezse yedekleme durur,
her deneme hata loglar ve dolu kalan volume bir aralık sonra yeniden denenir
(`keep=1`'de tek kopya da gidebilir). Boyut kuralı (keep + 1) × veritabanı
boyutu README "Yedekleme ve geri yükleme"de. Daha az yer için kopyaların
sıkıştırılması (geri yüklemede açma adımı gerekir) ve dolan PVC'nin
büyütülmesi (StorageClass `allowVolumeExpansion` isterse
`backup.persistence.size` artırılır; chart'ta ek iş yok, belge yok)
yapılmadı: sandbox'taki veritabanı küçük, `docs/yuk-testi.md`'deki 600 MB
veritabanı bile varsayılan 5 Gi'ye sığıyor. Tetikleyici: boyut kuralını
aşan bir pilot veritabanı ya da loglarda tekrarlanan `backup volume full`
uyarısı.

## Alertmanager yönlendirme ve susturma örnekleri; Grafana panosu

2026-10-27'de kapsam dışı bırakıldı (S9). Chart'ın PrometheusRule'u
alarmı üretir; Alertmanager'da namespace'e göre yönlendirme, `critical`
yanarken aynı addaki `warning`'i susturma (`KubeatLicenseExpiring`) ve
alıcı örnekleri platformun yapılandırmasıdır. OpenShift'in ve
kube-prometheus-stack'in varsayılan yapılandırması susturmayı zaten yapar;
README yalnızca bunu söyler. Grafana panosu da burada: öz-metrikler için
hazır bir pano istenirse `docs/` altına JSON olarak eklenir. Tetikleyici:
kendi Alertmanager'ını elle yapılandıran ya da pano isteyen ilk kullanıcı.

## Teslim edilemeyen bildirimler için dead-letter görünümü ve kalıcı yeniden deneme kuyruğu

2026-10-28'de ertelendi (S17, #27). Kanalın almadığı kesinti bildirimi
artık olay kapanana kadar artan aralıkla yeniden denenir (README "Alarm");
deneme listesi bellektedir, yeniden başlatmada açık kesintininki bildirim
kaydından kurulur, bekleyen düzelme bildirimi düşer. Kapsam dışı bırakılan
iki parça: teslim edilemeyen bildirimleri tek yerde gösteren bir görünüm
(dead-letter) ve veritabanında kalıcı bir yeniden deneme kuyruğu. Bugün
ikisinin yerini bildirim kaydı (olay sayfası), kanal satırındaki "Son
gönderim" ve `kubeat_notifications_total{result="failed"}` ile
`kubeat_notification_attempts_total` sayaçları tutar; S9'un
`KubeatNotificationsFailing` alarmı başarısız denemeyi ve düşürülen
bildirimi sayar, motorun vazgeçmesini beklemez. Tetikleyici: pilotta
yeniden deneme penceresini aşan bir kanal kesintisi (düzelme için 1 saatten
uzun) ya da yeniden başlatmayla düşen bir düzelme bildirimi görülürse.
