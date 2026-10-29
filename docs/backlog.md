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

Güncelleme (2026-10-28, sprint 1 kapanışı): Müşteri B'nin ortamı (GKE,
operator yok, Grafana Cloud ya da Managed Prometheus) tam bu ortam; karara
itirazı yok, kod değil belge istiyor: kuralların PromQL'i (`helm template …
--show-only templates/prometheusrule.yaml` ya da `docs/` altında düz bir
rules dosyası), Grafana Cloud'da `mimir.rules.kubernetes` ve GMP'de `Rules`
CRD'ye taşıma cümleleri, `metrics.prometheusRule.enabled=true`'nun CRD'siz
cluster'da kurulumu bozduğu uyarısı. Sprint 2 doküman paketi adayı (S12 ile).

Güncelleme (2026-10-28, sprint 2 planı): belge S30 olarak sprint 3 doküman
paketinde (S12 ve S15 ile). Önce kurallar düzelir: sprint 2'de S29 (#36),
böylece kopyalanan kurallar düzeltilmiş olanlardır. Kararlar: chart CRD
yokken fail etmez (`helm template` ile çalışan GitOps hatlarını kırar),
`docs/` altında statik kural dosyası yok; Grafana Cloud ve GMP adları
resmî belgeden doğrulanır.

Güncelleme (2026-10-29, sprint 2 kapanışı): Müşteri B'ye göre (önemli)
S29'un `max by (namespace, job)` ve `unless on (namespace, job)` ifadeleri
sorunlu. İki cluster tek depoda (ör. Grafana Cloud) değerlendirilince
`cluster` gibi dış etiketleri atıyorlar; bir cluster'ın düşüşü
diğerininkiyle örtülüyor. Önerisi üç parça: scrape etiketlerini `without
(...)` ile atmak, iki `cluster` değerli bir promtool senaryosu eklemek, S30
belgesine cluster başına değerlendirme notu yazmak. PMO önerisi: kural
düzeltmesi S26 ile birlikte ve S30'dan önce yapılır
([sprints/2026-10-28-sprint-02.md](sprints/2026-10-28-sprint-02.md)
"Müşterilerin yeni bulguları" 4).

Güncelleme (2026-10-29, sprint 3 planı): kural düzeltmesi S37 olarak sprint
3'te geliyor, S26 ile değil S38 ile aynı maddede (#50): yeni chart değeri
`metrics.prometheusRule.clusterLabel` (varsayılan boş; boşken kurallar 0.44
ile aynı, verilince ifadeler bu etiketi `by` ile taşır, yokluk penceresi 24
saat). `without (...)` seçilmedi, çünkü dedup'suz depoda replica etiketini
tutar. Belge (S30) sprint 4 doküman paketinde ve S37'den sonra; S37 bu
sprintte çıkarılırsa S30 "kuralları cluster başına değerlendirin"
uyarısıyla yayımlanır. 2026-10-12 kararı (PrometheusRule) korunur
([sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md)).

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

Güncelleme (2026-10-28, sprint 2 planı): sprint 2 migration eklemiyor, 0.44
şema 26 ile çıkar ve 0.43.0'a `helm rollback` mümkün. Tetikleyici adayı
artık S33 ("Sustur"u bildirim susturmasına çevirmek, aşağıda) ya da S4.

## Yedekleri cluster dışına taşıma belgesi ve backend imajına `tar`

2026-10-27'de ertelendi (S7). S boyut, kod yok: README "Yedekleme ve geri
yükleme"ye "Cluster dışına" bölümü (yedek PVC'sinin OADP/Velero
snapshot'ının tutarlı olduğu, `kubeat-secret-key` Secret'ının aynı sete
girdiği, 0600 dosyalar için aynı UID'de çalışan CronJob örneği) ve
`kubectl cp`/`oc rsync` için ubi-minimal imaja `tar`. İhtiyaç pilot sonu
"devam" kararıyla doğar (pilot.md "Pilot sonunda"); sprint 3 adayı. Karar:
imaja `tar` kabul (UBI paketi, taban imaj güncellemesiyle Dependabot
kapsar).

Güncelleme (2026-10-29, sprint 3 planı): tetikleyici (pilot kararı)
bilerek açıldı. Müşteri A filoya yaygınlaştırma için tek ve doğrulanabilir
bir yükseltme öncesi yedek adımını şart koşuyor; sprint 2 retrosunda
sandbox yükseltmesinden önceki yerel kopya bir komut hatası yüzünden
alınamamıştı. Sprint 3'te dolgu yeri yok, sprint 4 kuyruğunda 8. sırada.
Ürün kısmı (imaja `tar`, README'de "Yükseltmeden önce yedek" bloğu,
kopyaları cluster dışına almak) S12'nin `docs/yukseltme.md` "Yapılacak"
adımına bağlanır. Retronun süreç kısmı `tar` gerektirmez: kopya `exec ...
cat` ile alınıp sağlama toplamıyla pod'daki dosyaya karşı doğrulanır; bu
planla [pilot.md](pilot.md) "Yükseltmeden önce yedek"e yazıldı.

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

Güncelleme (2026-10-28, sprint 2 planı): Müşteri B'nin GitHub Actions'tan
kanal ve durum sayfası yönetme isteği yeni bir kullanıcıdan gelmiyor (S14'ün
kaynağı da Müşteri B), tetikleyici yanmadı. API'ye cluster dışından erişim
yolunun belgesi ve arayüzün `/api/v1` isteğine JSON 404 dönmesi S34'te
(sonraki sprint havuzu).

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

Güncelleme (2026-10-28, sprint 2 planı): Müşteri B'nin GKE zonal notu
(control plane yükseltmesi sırasında yanlış kesinti) ortam bilgisi,
gözlem değil; tetikleyici (pilotta API kaynaklı yanlış alarm) yanmadı.
BA'nın "sprint 3'ün ilk adayı" önerisi kabul edilmedi, çünkü sprint 3'ün
ilk yerleri müşteri engelleyicilerinin (S10+S11, S1); sprint 3–4 adayı.
README'deki GKE zonal notu sprint 3'te S12 ile gelir.

Güncelleme (2026-10-29, sprint 3 planı): yeni kanıt yok ve tetikleyici
bilerek genişletildi, çünkü sandbox pod'ları uyuttuğu için pilotta
ölçülemiyor ([pilot.md](pilot.md) "Güncelleme (2026-10-27)"). Yeni
tetikleyici: ilk gerçek cluster kurulumunda ya da GKE zonal control plane
yükseltmesinde görülen, API kaynaklı yanlış kesinti. GKE zonal notu
README'ye S12 ile, sprint 4'te gelir.

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

Güncelleme (2026-10-28, sprint 2 planı): tetikleyici yanmadı; Müşteri B'nin
durum sayfasını kendi alan adında açma isteği bir persona isteği, gerçek
kullanıcı ya da ölçüm değil. S35 ile birlikte alınır (aşağıda "Durum
sayfasını yönetim arayüzünü açmadan kendi alan adında yayımlamak"): dışarı
açmayı anlatan belge S21'den önce yayımlanmaz, ikisi aynı sürümde çıkar.

## Kontrol zaman aşımına üst sınır `UPTIME_MAX_CHECK_TIMEOUT`

2026-10-27'de ertelendi (S22). Zaman aşımının mutlak üst sınırı yok; SYN'e
cevap vermeyen 20 hedef 60 s/60 s ile tüm worker'ları tutup kalan
monitor'lerin kontrolünü fiilen durdurabilir (SRE orta/risk). Sprint 1'deki
`KubeatWorkersSaturated` alarmı (S9) tespiti sağladığından önleme sonraya
kaldı. Tetikleyici: alarm yanınca ya da sprint 3. Karar: varsayılan `60s`,
`0` kapalı; API ve annotation doğrulaması aşan değeri reddeder; mevcut aşan
monitor'lere dokunulmaz, açılışta WARN ile listelenir, sonraki düzenlemede
reddedilir; `UPTIME_WORKERS` açıklamasına boyutlandırma kuralı.

Güncelleme (2026-10-29, sprint 3 planı): tetikleyicinin "sprint 3" yarısı
bilerek kaldırıldı. Dört yer daha yüksek risklere gitti, yeni kanıt yok,
tespit yolu (`KubeatWorkersSaturated`) yerinde ve yanmadı. Yeni tetikleyici:
`KubeatWorkersSaturated`'ın yanması ya da S38'in "`Healthy` takılı
worker'ları da saysın mı" sorusunun açılması; ikisi birlikte
değerlendirilir.

## .env.example'ı tamamlamak ve config ile karşılaştıran CI denetimi

2026-10-27'de ertelendi (S24). `.env.example` 11 ayarı eksik (OIDC claim/CA/
zaman aşımı, Prometheus sorguları, olay önbelleği); chart dışı kurulum yapan
biri tüm ayarları oradan görebilmeli (SRE düşük/docs). Sprint ekibinin
"Tamamlanma tanımı" zaten her hikâyede `.env.example` ister, reviewer
denetler. Sprint 3 dolgusu (S); tetikleyici: chart dışı kurulum isteyen ilk
kullanıcı ya da sprint 1-2'de bir sapma görülürse. Karar: betik Makefile'da
(`make lint`), CI onu çağırır; chart values ile karşılaştırma yok (chart her
ayarı `extraEnv` ile alır).

Güncelleme (2026-10-29, sprint 3 planı): sprint 3'te de alınmadı. Sapma
büyümedi, eksik yine aynı 11 değişken; tamamlanma tanımı yeni ayarlarda
işliyor ve dolgu yeri yok. Tetikleyici aynı: chart dışı kurulum isteyen ilk
kullanıcı ya da yeni bir sapma.

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

Güncelleme (2026-10-28, sprint 1 kapanışı): tetikleyici kısmen yandı. Müşteri
A, user workload monitoring Alertmanager'ını ayrı kuran kurumda varsayılan
inhibit kuralının olmadığını, `KubeatLicenseExpiring`'in warning ve
critical'inin ikisinin birden gideceğini yazdı; README "Aracın kendi
sağlığı"na kısa bir `inhibit_rules` örneği yeter (sprint 2 doküman paketi
adayı). Kurallar promtool'dan geçmedi; `promtool check rules` yerelde ve
cluster'sız çalıştığından `test-chart.sh`'a eklenmesi önerildi. Pano için
istek yok.

Güncelleme (2026-10-28, sprint 2 planı): yalnızca `inhibit_rules` örneği
(lisansın warning/critical'i ve `KubeatDown` yanarken aynı namespace'teki
`KubeatMonitorDown`) ve promtool denetimi sprint 2'de S29 (#36) ile
geliyor. Kalan yönlendirme örnekleri ve pano backlog'da kalıyor,
tetikleyici aynı.

Güncelleme (2026-10-29, S29 #36): `inhibit_rules` örneği README "Aracın
kendi sağlığı"nda ("Alertmanager'da susturma kuralları"): lisansın
critical'i aynı kurulumun warning'ini, `KubeatDown` aynı namespace'teki
`KubeatMonitorDown`, `KubeatCertificateExpiring` ve `KubeatBackupFailing`'i
susturur; üçü de scrape boşluğunda durumunu 10 dakika tutar. İkinci kural
OpenShift'in ve kube-prometheus-stack'in varsayılanında yoktur, alarmları
alan Alertmanager'a (OpenShift'te `alertmanager-main` dahil) eklenir.
Bedeli README'de: Kubeat 25 dakikadan uzun düşükken biten bir kesintinin
düzelmesi Alertmanager'dan gitmeyebilir. Kurallar artık promtool ile
sınanıyor (`deploy/helm/test-chart.sh`, CI chart işi). `AlertmanagerConfig`
nesnesiyle yazılmış örnek, kalan yönlendirme örnekleri ve pano backlog'da,
tetikleyici aynı.

## Öteki öz-sağlık alarmları scrape boşluğunda; ServiceMonitor'süz kural uyarısı

2026-10-29'da ertelendi (S29'un kapsam dışı parçaları, #36). S29 dört alarmı
scrape boşluğuna karşı düzeltti: `KubeatDown` backend pod'u hiç yokken de
yanar; `KubeatMonitorDown`, `KubeatCertificateExpiring` ve
`KubeatBackupFailing` durumlarını 10 dakika tutar. Kalan öz-sağlık
alarmlarından dördü boşlukta eskisi gibi çözülür, ikisi gürültüyle:
`KubeatLicenseExpiring` (`for: 1h`) her Recreate'te, yükseltmede ve node
drain'de "resolved" gider, yeni pod scrape edilince 1 saat bekleyip yeniden
yanar; `KubeatBackupStale` boşluğun ilk değerlendirmesinde çözülür, yeni
pod'un ilk scrape'inde yeniden yanar. README'deki susturma kuralı bu ikisini
kapsamaz: ikisi de boşluğun başında, `KubeatDown`'ın 5 dakikası dolmadan
çözülür, bu yüzden uzun bir Kubeat kesintisinde de "resolved" gönderirler.
`KubeatSchedulerStalled` ve `KubeatMetricsIncomplete` de boşlukta çözülür;
boşluğu `KubeatDown` bildirdiği için zararları küçük. İkinci parça:
`metrics.prometheusRule.enabled=true` iken `metrics.serviceMonitor.enabled=false`
ise ve aynı `job`/`namespace` etiketlerini veren başka bir scrape yoksa
`KubeatDown` sürekli yanar. Bu README'de ("Aracın kendi sağlığı") ve
values.yaml yorumunda yazılı, kurulum notu (`NOTES.txt`) söylemiyor.
Üçüncü parça: veritabanı 10 dakikadan uzun okunamazken `/metrics` yanıt
verdiği için `up` 1 kalır ve `KubeatDown` yanmaz; `KubeatMonitorDown` ile
`KubeatCertificateExpiring` 10 dakika sonra çözülür, yerlerine
`KubeatMetricsIncomplete` yanar ve README'deki susturma kuralı bu durumu
kapsamaz. Ertelendi, çünkü S29'un kapsamı dört alarmdı ve `NOTES.txt` bu sprintte
#38'in dosyası. Tetikleyici: pilotta lisans alarmının bir yükseltmede gidip
gelmesi, `KubeatBackupStale`'in bir Recreate'te "resolved" göndermesi ya da
ServiceMonitor'süz kurulumda sürekli yanan `KubeatDown` için ilk kullanıcı
şikâyeti.

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

Güncelleme (2026-10-28, sprint 1 kapanışı): Müşteri A (hoş olur) makine
kanalında düşen düzelme bildiriminin kapanmayan kayıt demek olduğunu yazdı:
PagerDuty `dedup_key` ile resolve, ITSM webhook'u `up` ile ticket kapatır;
bakım penceresi 2–4 saat olan kurumda `up` pencere içinde düşerse incident ve
ticket açık kalır. Önerisi: makine kanalı için `up` da olay kapanana kadar (ya
da en az 24 saat) denensin ve yeniden başlatmada "kapanmış olay + teslim
edilmiş `down` + teslim edilmemiş `up`" kayıttan kurulsun (`DeliveryAt` ve
`Delivered` var). En azı: README PagerDuty ve webhook satırlarına "kanal uzun
süre ulaşılmaz kalabiliyorsa `UPTIME_NOTIFY_RETRY_FOR`'u bakım pencerenizden
uzun tutun" notu (sprint 2 doküman paketi adayı). Müşteri B (hoş olur):
`KubeatNotificationsFailing` `increase(...[15m]) > 0` ve `for` yok; 30
saniyelik Slack hıçkırığında bildirim bir dakika sonra teslim edilmişken
alarm 15 dakika yanar; önerisi kanal türü başına "son deneme başarısız"
gauge'ı ya da README ve description'ın "son 15 dakikada başarısız deneme
oldu" demesi ve `for` önerisi. Patlamada ölü makine kanalı ayrı madde.

Güncelleme (2026-10-28, sprint 2 planı): kapanmış olayın teslim edilmemiş
`up`'ı S28'e (PagerDuty ve webhook'a kurulum kimliği, sonraki sprint)
eklenmedi; SRE backlog'da kalsın dedi, tetikleyici değişmedi. S25 (#37) hiç
denenmemiş kesinti bildirimini yeniden başlatmada bildirim kaydından kurar;
kalıcı kuyruk yine yok. `KubeatNotificationsFailing`'in kanal başına
gauge'ı ve `for: 5m` S26'da (sonraki sprint havuzu).

## Patlamada ölü makine kanalının teslimat kuyruğuna maliyeti

2026-10-28'de ertelendi (S18'in kapsam dışı parçası, #26; PO kararı "kanal
başına kalıcı kuyruk yok"). Kanallar artık birbirinden bağımsız teslim
edilir, ama döngü her bildirim için makine kanallarını eşzamanlı çağırır ve
#27'nin devre kesicisi yalnızca yeniden denemeleri tutar: takılı tek makine
kanalı patlamada geçiş başına, bildirim başına bir gönderim bekletir
(`UPTIME_NOTIFY_ATTEMPTS × (TIMEOUT + RETRY_DELAY)`, varsayılanlarla 25–30
sn). Sprint 1 kapanışında Müşteri A bunu "önemli" bulgu olarak yazdı: ölü
PagerDuty ya da ITSM kanalıyla çakışan bir Ingress/DNS kesintisinde
varsayılan 100'lük kuyruk ~42 dakikada boşalır, bu arada enqueue'da düşen
bildirim (`kubeat_notifications_dropped_total` sayar) ne yeniden denenir ne
yeniden başlatmada kayıttan kurulur (kayıtta satırı yok), Slack ve e-posta da
duymaz. Önerilen parça: bir geçişte almayan makine kanalı yeniden deneme
süresine kadar "tripped" sayılsın ve yeni `down` ona gönderilmeden yeniden
deneme listesine yazılsın; `UPTIME_NOTIFY_QUEUE_SIZE` için README'de "en az
monitor sayısı kadar" kuralı ya da varsayılanın monitor sayısından
türetilmesi; patlama testine ölü makine kanalı senaryosu. Tetikleyici yandı
sayılır: sprint 2 adayı (PMO önerisi,
[sprints/2026-10-27-sprint-01.md](sprints/2026-10-27-sprint-01.md) "Sonraki
sprint önerisi"); PO karar verir.

Güncelleme (2026-10-28, sprint 2 planı): sprint 2'ye alındı (S25, #37):
kirli çıkışta kesinti bildirimi kaybıyla aynı kök, tek maddede. Karar:
"almıyor" sayılan makine kanalına yeni bildirim denenmez, yeniden deneme
listesine yazılır; `UPTIME_NOTIFY_QUEUE_SIZE` için "en az monitor sayısı
kadar" kuralı README'ye girer, otomatik türetme yok. Merge ile "kapandı"
notu düşülür (S25'in kabul ölçütü).

Kapandı (2026-10-29, S25, #37): almayan makine kanalı (webhook, PagerDuty)
yeniden deneme zamanına kadar "almıyor" sayılır; bu sürede gelen
bildirimler ona denenmeden yeniden deneme listesine yazılır ve kanalın
sıradaki geçişinde gider (yeniden denenmeyen hatırlatma, sertifika ve
yavaşlık bildirimi, `UPTIME_NOTIFY_RETRY_FOR=0` iken düzelme de, bir kez
denenir: o geçişte, önündeki bir bildirim alınmazsa sonraki bir geçişte).
Ölü makine kanalıyla 100 bildirimlik patlamada kuyruk dolmaz,
`kubeat_notifications_dropped_total` artmaz, insan kanallarının grubu
`UPTIME_NOTIFY_GROUP_WAIT` içinde gider (`go test -race`). Kuyruk kuralı
README "Alarm" ve "Yapılandırma"da.
Geçişte kanalın daha az reddettiği bildirim önce denenir: ölü kanal geçiş
başına bir kez denenir, sürekli reddedilen bir bildirim (409, 422)
diğerlerini bekletmez. Kalan: `UPTIME_NOTIFY_GROUP_WAIT=0` iken yanıt
vermeyen bir insan kanalı her bildirimde yine denenir (README'de sınır
olarak); sürekli reddedilen bir bildirim kanalı her geçişten sonra yeniden
duraklatır, yeni bildirimler en çok `UPTIME_NOTIFY_RETRY_MAX` (bir
bildirimin ilk reddedildiği geçişte bir geçiş daha) gecikir (4xx'i kalıcı
hata sayan S26'ya girdi: 4xx dönen kanal canlıdır).

Güncelleme (2026-10-29, sprint 2 kapanışı): Müşteri A iki önemli bulgu
yazdı. Birincisi: 4xx alan tek bir bildirim paylaşılan makine kanalını her
geçişten sonra yeniden duraklatıyor ve diğer ekiplerin sayfaları 15
dakikaya kadar gecikiyor. Önerisi, S26'nın kabul ölçütüne şunun yazılması:
"4xx alan kanal canlıdır ve duraklamaz; yalnızca bağlantı hatası, zaman
aşımı ve 5xx duraklatır." İkincisi: varsayılan kuyruk (100), README'deki
"en az monitor sayısı kadar" kuralıyla ve 1000'lik monitor kotasıyla
çelişiyor. Önerisi varsayılanı 1000'e çekmek ve sınır aşılınca WARN
vermek; otomatik türetme gerekmiyor. İkisi de sprint 3 PO turunda
değerlendirilir ([sprints/2026-10-28-sprint-02.md](sprints/2026-10-28-sprint-02.md)
"Müşterilerin yeni bulguları" 2 ve 3).

Güncelleme (2026-10-29, sprint 3 planı): ikisi de sprint 3'te. Birincisi
S26 (#49): 4xx dönen makine kanalı canlı sayılır ve duraklamaz; yalnızca
"ulaşılamadı" ve "meşgul" (429, `Retry-After`'lı 503) sınıfları duraklatır.
İkincisi S38 (#50): `UPTIME_NOTIFY_QUEUE_SIZE` varsayılanı 1000 olur
(`UPTIME_MAX_MONITORS` ile aynı), etkin monitor sayısı kuyruğun üstüne
çıkınca WARN verilir. "Otomatik türetme yok" kararı korunur
([sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md)).

## "Sustur"u durum sayfasını etkilemeyen bir bildirim susturmasına çevirmek

2026-10-28'de ertelendi (S33,
[sprints/2026-10-28-sprint-02.md](sprints/2026-10-28-sprint-02.md)). Müşteri
B (önemli): monitor menüsündeki "Sustur" sıradan bir bakım penceresi açar
(`frontend/app/ui/monitor-settings.tsx:173`), bu yüzden müşterilere açık
durum sayfasında "Bakım: Susturma" görünür; susturma sürerken servis
gerçekten düşerse olay açılmadığı için sayfa yine "bakım" der
(`backend/internal/api/statuspage_view.go:31-44`). Susturma nöbetçinin
aracıdır, müşteriye duyuru değildir. L boyut ve migration (pencereye tür
alanı): backlog kuralıyla önce S6 gerekir, yani iki slot. Tetikleyici:
durum sayfasının dışarı açılması (S35/S21 paketi) ya da gerçek bir şikâyet.
Karar şimdiden: yalnızca sayfadan gizlemek yok, çünkü susturmada olay
açılmadığından sayfa gerçek bir kesintide "çalışıyor" der. Susturmada olay
açılır; bildirim, hatırlatma ve yeniden deneme tutulur; pencere sonunda
hâlâ açık olay duyurulur; susturma içinde açılıp kapanan olayın düzelme
bildirimi gitmez. Ara adım S27 (sonraki sprint havuzu): "Sustur" ipucu iki
dilde durum sayfasında "bakım" görüneceğini söyler.

## Durum sayfasını yönetim arayüzünü açmadan kendi alan adında yayımlamak

2026-10-28'de ertelendi (S35). Müşteri B (önemli): müşterilerine kendi
alan adında durum sayfası göstermek, giriş ekranı ve API iç ağda kalsın;
bugün sayfayı açmak giriş ekranını da açmak demek (giriş kilidi kullanıcı
adına göre, yol haritası "Bilinen sınırlar"). Belge dışarı açmayı
kolaylaştırır ve S21'in tetikleyicisini kendisi yakar; müşteriye dönük
sayfa izlenen cluster'la birlikte düşer, o iş için dışarıdan bakan
tamamlayıcı araç daha doğru yer ("UptimeRobot karşılaştırmasından
ertelenenler"; yol haritası Kuma tablosu "özel alan adı yok"). Tetikleyici:
gerçek bir kullanıcının isteği ya da pilotta "devam" kararı. Kural: S21 ile
aynı sürümde yayımlanır, ondan önce değil. Karar: chart'a
`statusPage.ingress` yok, OpenShift Route örneği yok.

## Kararlı minor ve güvenlik düzeltmeleri için destek penceresi

2026-10-28'de ertelendi (S1'in açık sorusu). Müşteri A (önemli): 15 günde
12 minor çıktı (v0.32.0 → v0.43.0); güvenlik yaması almak için davranış
değiştiren son minor'a geçmek zorunda. Karar (kullanıcı adına): ilk dış
müşteri ya da satın alma görüşmesine kadar taahhüt yok. S1 (sonraki sprint)
ile gelecek SECURITY.md "yalnızca son minor, kritik/yüksek CVE 7 gün" der;
S1 Makefile'ın `release/v*` dalından etiketlemesini teknik olarak açar, bu
bir yetenektir, söz değil. Tetikleyici: ilk satın alma görüşmesi. Başlangıç
önerisi BA'nın varsayılanı: çeyrekte bir kararlı minor, ardılı çıktıktan
sonra 90 gün kritik/yüksek CVE ve taban imaj düzeltmesi, aynı anda en çok
iki hat. Gerekçe: söz sonradan verilebilir; verilip geri alınırsa güven
kaybolur.

Güncelleme (2026-10-29, S1 #48): SECURITY.md geldi. "Yalnızca son minor"
der: düzeltme o hatta yeni bir yama olarak çıkar, eski minor'lara geri
taşınmaz; kritik/yüksek açık için 7 günlük hedef (sözleşmeli bir hizmet
düzeyi değil) ve taban imaj düzeltmesinde yeniden derleme. `make release`
`release/v*` dalından da etiketler; CONTRIBUTING.md "Sürümler" bunu destek
sözü olmayan bir yetenek olarak yazar ve böyle bir yamanın GitHub Release'i
"Latest" olmaz. Karar ve tetikleyici değişmedi.

## containers/image'ın okuduğu biçimde ikinci imza

2026-10-29'da ertelendi (S1'in geliştirmesinde çıktı, #48). 0.45'ten beri
imajlar ve chart cosign v3 ile imzalanır: imza Sigstore bundle'ı olarak OCI
1.1 referrer'ında durur (GHCR'de `sha256-<digest>` etiketi). containers/image
(skopeo, podman, CRI-O) yalnızca eski biçimin `sha256-<digest>.sig` ekini
okur; bu imzaları ne taşır ne doğrular, yani CRI-O'nun imza politikası
(`policy.json`'da `sigstoreSigned`) onları görmez. Bugünkü yol: aynalamada
`oras cp -r`, doğrulama `cosign verify` ile (README "Tedarik zinciri").
Seçenek: yayımda ikinci bir imza, `cosign sign --new-bundle-format=false`;
bayrak cosign v3'te kullanımdan kalktı ve v4'te kalkacak, her artefakta
ikinci bir Rekor kaydı demek. Tetikleyici: imzayı cluster'da CRI-O'nun
politikasıyla doğrulamak isteyen ilk kurulum ya da containers/image'ın
bundle desteği.

## Aynı adla yeniden kurulumda yeniden üretilen Secret'lar

2026-10-29'da ertelendi (S31'in geliştirmesinde çıktı, #38). 0.44'ten beri
`helm uninstall` veri ve yedek PVC'lerini ve `<release>-secret-key`'i
bırakır, ama chart'ın ürettiği `<release>-admin`, `<release>-api-token` ve
`<release>-metrics-token` Secret'ları kaldırmayla gider. Aynı ad ve
namespace'le yeniden kurulumda yeniden üretilirler: kurulum notları
yönetici parolasını yeni `<release>-admin` Secret'ından okutur, o parola
kalan veritabanında geçmez; API ve metrik anahtarları da değişir. README
"Yedekleme ve geri yükleme" ("Kazayla kaldırmadan dönüş") davranışı ve
`UPTIME_ADMIN_PASSWORD_RESET` yolunu yazıyor; kullanıcı belgesinde eksik
yok, eksik olan kurulum anındaki uyarı.
İki seçenek: kurulum notlarında `lookup` ile "veri PVC'si var, yönetici
Secret'ı yeni üretildi" uyarısı (yalnızca metin; `helm template` ve Argo
CD'de `lookup` boş döner, uyarı çıkmaz) ya da bu üç Secret'a da
`helm.sh/resource-policy: keep` (yeniden kurulum eski değerleri sahiplenir;
bilerek kaldırmada silinecek nesne artar, kurulum notlarındaki silme komutu
onları da saymalı). Tetikleyici: kullanıcının minikube provası (sprint 2
"Kullanıcıda kalanlar" 3) yeni Secret'taki parolanın geçmediğini doğrularsa
ya da gerçek bir yeniden kurulumda yönetici bu yüzden takılırsa.

Güncelleme (2026-10-29, sprint 2 kapanışı): Müşteri B (önemli), tetikleyicinin
özellikle birlikte geldiğini yazdı. 0.44 README'si takılı bir release'i
kaldırıp yeniden kurmayı öneriyor, NOTES ise parolayı yeni Secret'tan
okutuyor. İstediği, ilk seçenek (NOTES uyarısı) ve README "Cluster'a
kurulum"a bir cümle. PMO bunu sprint 3 doküman paketine öneriyor
([sprints/2026-10-28-sprint-02.md](sprints/2026-10-28-sprint-02.md)
"Müşterilerin yeni bulguları" 5). Minikube provası henüz yapılmadı; kararı
PO verir.

Güncelleme (2026-10-29, sprint 3 planı): tetikleyici 2026-10-29 minikube
provasında yandı: eski yönetici parolası 200, kurulum notlarının gösterdiği
parola 401 aldı ([yedek-geri-yukleme.md](yedek-geri-yukleme.md) "Prova:
2026-10-29"). PO kararı ikinci seçenek: `helm.sh/resource-policy: keep` üç
Secret'a da girer, metrik anahtarı dahil, ve
`backend.persistence.keepOnUninstall`'a bağlanır. NOTES'ta `lookup` uyarısı
seçilmedi, çünkü `helm template`'te ve Argo CD'de boş kalır. Hikâye S39;
sprint 4 kuyruğunda 3. sırada, S41 ile
([sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md)).

## Dolan veri volume'u: büyütme, kotaya göre saklama, yazılamayan sonuçlar

2026-10-29'da ertelendi (S32'nin kapsam dışı parçaları, #39). 0.44'ten
beri veri volume'u dolup sonuçlar yazılamaz olunca heartbeat kesilir ve
backend yazım başına tek satır loglar (`batch insert failed`, düşen sonuç
sayısıyla); boyut kuralı README "Geçmiş verisi"nde. Yapılmayanlar: PVC'nin
kendiliğinden büyütülmesi (StorageClass `allowVolumeExpansion` isterse
`backend.persistence.size` artırılır, chart'ta ek iş yok), `UPTIME_RETENTION`'ın
monitor sayısına ve aralığa göre kendiliğinden ayarlanması, yazılamayan
sonuçların bellekte tutulması (volume doluyken de olay açılır ve bildirim
gider, ama bellek sınırı ister ve yeniden başlatmada kaybolur), düşen
satırların `monitor_id`'lerinin ve ilk satır hatasının toplu log satırına
eklenmesi (bugün yalnızca sayı loglanır; bir monitor'un sonucu kalıcı
reddedilirse hangisi olduğu görünmez). Tetikleyici:
boyut kuralını aşan bir kurulum ya da loglarda `dropped` değeri `rows`'a eşit
`batch insert failed` satırları ve `heartbeat withheld`.

Güncelleme (2026-10-29, sprint 2 kapanışı): `KubeatResultsDropped` PO
kararıyla warning kaldı (#36 ve #39'un kapsamı dışında). Müşteri A
(önemli) şunu yazdı: S32'nin denetimi (`scheduler.Healthy`) yalnızca
`UPTIME_HEARTBEAT_URL` verilince çalışıyor. Dış alıcısı olmayan bir
kurulumda dolan volume cluster içinde yalnızca bu warning'i üretiyor;
takılı bir yazımda o sayaç da artmıyor. Önerisi, `Healthy()` sonucunu
heartbeat'ten bağımsız bir gauge olarak yayınlamak ve `critical` bir
"Kubeat sonuç yazamıyor" kuralı eklemek (`for: 5m`). Kısmi düşüşte warning
kalır, böylece PO kararı bozulmaz. Kararı sprint 3 PO turu verir
([sprints/2026-10-28-sprint-02.md](sprints/2026-10-28-sprint-02.md)
"Müşterilerin yeni bulguları" 1; PMO önerisi kuyruğun 5. sırası).

Güncelleme (2026-10-29, sprint 3 planı): Müşteri A'nın gauge ve critical
kural önerisi S38 (#50) ile sprint 3'te geliyor; `KubeatResultsDropped`
warning olarak kalır. Log taşkını notu (sprint 2 provası 4a): volume
doluyken backend her toplu yazımda bir WARN yazıyor; öneri dakikada bir
özet satırı, tetikleyici log hacmi şikâyeti. Dolu volume'dan dönüş runbook'u
S41'de (sprint 4 kuyruğu, 3. sıra).

## Kaldırmaya karşı korumanın kalanları: Argo CD prune, PV düzeyi, prova kaydı

2026-10-29'da ertelendi (S31'in kapsam dışı parçaları, #38). 0.44'ten beri
`helm uninstall` veri ve yedek PVC'lerini bırakır; Argo CD 2.7 ve sonrası
cascade silmede de öyle. Kalanlar:

- **Argo CD'de prune'a karşı koruma (`Prune=false`)** bilerek eklenmedi.
  Kabul ölçütü yalnızca `Delete=false` istiyordu. `Prune=false`, values'tan
  çıkan bir PVC'de (ör. `backup.enabled=false`) uygulamayı sürekli
  OutOfSync bırakır. Sınır README "GitOps ile kurulum"da yazılı.
  Tetikleyici: GitOps kurulumunda values değişikliğiyle prune edilen bir
  PVC.
- **VolumeSnapshot, PV `reclaimPolicy` ve namespace silmeye karşı koruma**
  issue'nun kapsamı dışındaydı. Namespace silmek PVC'leri de siler (README
  "Yedekleme ve geri yükleme"). Kopyaları cluster dışına almak S7'nin işi
  (yukarıda); tetikleyici onunla aynı.
- **Prova kaydı.** `docs/yedek-geri-yukleme.md`'ye "kazayla kaldırma" hedef
  satırı ve prova kaydı, kullanıcının minikube provasından sonra yazılır
  ([sprints/2026-10-28-sprint-02.md](sprints/2026-10-28-sprint-02.md)
  "Kullanıcıda kalanlar" 3).

## S25'in incelemesinde kalan bildirim köşeleri

2026-10-29'da ertelendi (S25, #37; SRE incelemesi ve developer raporu).
İlk ikisi 0.44'ten önce de vardı, yani gerileme değiller. S26 ya da S27
`alert` paketine ve `schedule.go`'ya dokunurken BA bunları kapsama alıp
almayacağına bakar.

- **İnsan kanalında sürekli reddedilen yeniden deneme.** İnsan kanalının
  sürekli reddettiği bir yeniden deneme mesajı, o kanalın diğer yeniden
  denemelerini bekletebilir.
- **Kesintiden sonra oluşturulan gecikmeli ya da tekrarlı kanal.** Böyle
  bir kanal yeniden başlatmada eski kesintiyi yine alır. "Kesintiden önce
  var olan kanal" kuralı (`CreatedAt`), kabul ölçütü gereği yalnızca
  gecikmesiz kanallarda uygulanıyor.
- **Bekleyen listenin büyüklüğü için metrik yok.** Duraklatılmış makine
  kanalında bekleyen bildirimler yalnızca `notifications held back ...
  count` log satırında görünür. S26'nın kanal gauge'u eklenirken bu da
  düşünülür; Müşteri A'nın kuyruk doluluğu gauge'u önerisi de aynı yerde
  ([sprints/2026-10-28-sprint-02.md](sprints/2026-10-28-sprint-02.md)
  "Müşterilerin yeni bulguları" 3).
- **S27 girdisi.** Kesinti açıldıktan sonra başlayan bir bakım penceresinde
  yeniden başlatma olursa gecikmesiz kanal kesintiyi almaz, yalnızca
  düzelmeyi alır. 0.43'te bu bildirim tümden kayboluyordu, yani gerileme
  değil. S27 ile birlikte alınır.

Tetikleyici: S26 ya da S27 sprint'e girdiğinde, ya da pilotta görülürse.

Güncelleme (2026-10-29, sprint 3 planı): 1. köşe S26 (#49) ile sprint 3'te;
kabul ölçütü maddeye kapandı notu ister. 3. köşedeki bekleyen liste ölçüsü,
S26'daki "teslim edilmemiş kesinti bildirimi kanalın alarmını açık tutar"
kararının (PO'nun ek kabul ölçütü) mekanizması olabilir; developer seçer.
2. köşe S40'a, 4. köşe S27'ye gider (sprint 4 kuyruğu, 6. ve 5. sıra)
([sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md)).

## Duyurulan bakımı durum sayfası ve rapor uptime'ından ayırmak

2026-10-29'da ertelendi (S45,
[sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md)).
Müşteri B (önemli): durum sayfası planlı bakımı müşteriye uptime kaybı
olarak gösteriyor; 1 saatlik planlı bir bakım 30 günlük %99,9 hedefini tek
başına deliyor. Müşteri A (önemli, aynı kök): bakımdaki başarısız
kontroller rapor CSV'sinde `uptime_percent`'e giriyor, ama bakımda olay
açılmadığı için `downtime_seconds`'a girmiyor; aynı satırdaki iki sayı
birbirini tutmuyor. L boyutlu ve migration ister (saatlik özetler bakımı
ayırmıyor). Ön koşulu S33 ve S6, yani üç slot: "Sustur" bugün bir bakım
penceresi açtığı için S33'süz ayırmak gerçek bir kesintiyi uptime'dan
silerdi. Müşteriye dönük sayfanın asıl yeri dışarıdan bakan tamamlayıcı
araçtır (S35 kararı). Tetikleyici: durum sayfasının dışarı açılması (S33,
S35 ve S21 ile aynı) ya da gerçek bir SLA şikâyeti. Kararlar: varsayılan
değişmez; ayrı `maintenance_seconds` sütunu eklenir, eski sütunların yeri
değişmez; yalnızca duyurulan bakım penceresi ayrılır, susturma ayrılmaz.
Ara önlem: yol haritası "Bilinen sınırlar" iki sayının uyuşmazlığını ve
SLA için `downtime_seconds`'ı yazar (bu plan), README cümlesi S12 ile.

## Kapsamlarda namespace önek deseni, ör. `odeme-*`

2026-10-29'da ertelendi (S47). Müşteri A (önemli): ekip yetkilendirmesi 60
ekip × 30 cluster'da elle yürüyor. Kapsam yalnızca tam namespace adı
alıyor; ekip yeni bir namespace açınca her cluster'daki grup eşlemesi ve
ekibin kanal kapsamı elle güncelleniyor, o zamana kadar ekip annotation'la
gelen monitor'ü göremiyor. L boyutlu ve yetki modelinin bütün okuyucularına
dokunur: `CanRead`/`CanEdit`, kanalın ve bakım penceresinin `Covers`'ı, olay
listesinin SQL süzgeci, lisansın `narrows`'u; risk orta. Pilot bunu
gerektirmiyor. Tetikleyici: pilot "devam" kararıyla çok cluster'a
yaygınlaştırma ya da ilk kurumsal görüşme. Karar taslağı: yalnızca sondaki
tek `*` kabul edilir (`odeme-*`), başka joker API'de reddedilir; `narrows`
deseni tanır; desenli kapsam koymak `scoped_access` ister; OpenShift
RBAC'tan rol türetilmez (giriş kararı). ADR-0002 notu hikâyeyle girer:
desen kısıtın kendisidir, lisans bitince desene uyan yeni namespace'lerin
görünmesi erişimi genişletmiş sayılmaz.

## Lisans bitiminden sonra sabit ek süre

2026-10-29'da ertelendi (S48). Müşteri A (önemli, risk): lisans bittiği an
LDAP ve OIDC ile giriş kapanıyor; geriye yerel admin ve API anahtarı
kalıyor, izleme durmuyor. Satın alma onayı ve imzalı dosyanın 30 cluster'a
dağıtılması bitişten önceki 30 günlük uyarıyı aşabilir; "lisans bitince
kullanıcıları dışarıda bırakıyor" maddesi risk komitesinde açılıyor.
Tetikleyici yanmadı: satın alma görüşmesi ya da sözleşme taslağı yok, pilot
lisansı süreye bir ay pay içeriyor. Müşteri A kararı sözleşmeden önce
istediği için yön şimdiden verildi: ek süre kodda sabit 30 gün olur; ayar
ya da lisans dosyası alanı eklenmez (ADR-0002: uyarı süresi ürün kararıdır,
ayar değildir). Ek sürede kurumsal özellikler açık kalır ve yöneticiler
uyarı görür; `kubeat_license_expiry_timestamp_seconds` sözleşme bitişini
vermeye devam eder. İzleme durmadığı ve erişim genişlemediği için
ilkelere aykırı değil; ADR-0002'ye tarihli ek not hikâyeyle girer.
Tetikleyici: ilk satın alma görüşmesi ya da sözleşme taslağı.

## Gateway API HTTPRoute keşfi ve annotation hedefi

2026-10-29'da ertelendi (S49). Müşteri B (önemli): Ingress'ten Gateway
API'ye (HTTPRoute) geçince annotation ile izleme uyarısız bitiyor.
HTTPRoute'taki `kubeat.io/monitor` okunmuyor; Ingress silinince monitor
geçmişiyle birlikte kalkıyor ve durum sayfasından düşüyor, kanala bir şey
gitmiyor. Keşif RBAC'ında `gateway.networking.k8s.io` yok; adaylar Route,
Ingress, Service ve workload. HTTPRoute, OpenShift Route'unun üst akımdaki
karşılığı: cluster-native farkın doğal ve stratejik bir genişlemesi.
ingress-nginx'in bakımdan çıkması Müşteri B'nin beyanı (doğrulanmadı). Bugün
Gateway API kullanan bir kurulum yok; iş L boyutlu (backend, chart RBAC, iki
dil, belge). Tetikleyici: Gateway API kullanan ilk kurulum ya da
Ingress'ten HTTPRoute'a ilk gerçek geçiş. Kararlar: CRD ya da okuma izni
yoksa sessizce atlanır (Route gibi); geçişte monitor geçmişi taşınmaz. Ara
önlem: yol haritası "Bilinen sınırlar" satırı (bu plan; geçişten önce elle
monitor oluşturun), README cümlesi S12 ile.

## Prometheus yokken pod CPU ve belleğini metrics.k8s.io'dan okumak

2026-10-29'da ertelendi (S50). Müşteri B (hoş olur): GKE'de monitor
sayfasındaki pod panelinin CPU ve belleği boş kalıyor. Kubeat yalnızca
bearer token'lı bir Prometheus okuyor; her GKE cluster'ında hazır olan
`metrics.k8s.io` (metrics-server) kullanılmıyor. Kanıt zayıf ve aynı işi
başka araç yapıyor: Müşteri B aynı veriyi Grafana Cloud'un Kubernetes
panolarında görüyor; şişirme riski var. Tetikleyici: operator'süz ilk
gerçek kurulum. Karar taslağı: `prometheus.url` boşken anlık değerler
PodMetrics'ten okunur, ek ayar yok, geçmiş grafiği yok. Sınır paragrafı S30
ile (sprint 4 doküman paketi).

## Helm 3 desteğini bırakmak

2026-10-29'da ertelendi (#47'nin yan konusu). 0.45'ten beri CI chart işi
Helm 4 (v4.3.1) ve Helm 3'ün son yamasıyla (v3.22.1) iki kez koşuyor; README
kurulumda Helm 3'ü de destekliyor (`--reset-then-reuse-values` için 3.14+).
Bugün Helm 3 ile kuranlar var: GitOps araçlarının ve kurumsal kurulum
hatlarının çoğu hâlâ Helm 3 kullanıyor. Bırakmak, kullanıcıya kurulum aracını
değiştirtmek demek; README, CONTRIBUTING ("Sabit araç sürümleri") ve ci.yml
matrisi birlikte değişir. Tetikleyici: Helm projesinin Helm 3'e güvenlik
yamalarını bitirmesi, `chart (Helm 3)` koşusunun chart düzeltilerek
onarılamayan bir nedenle kalıcı kırılması ya da yalnızca Helm 4'te olan bir
chart özelliğine ihtiyaç.

## Yayımlanan imajları işletim sistemi paketleri için taramak

2026-10-29'da ertelendi (#48'in kapsam dışı parçası). Haftalık güvenlik
taraması (`security.yml`) Go modüllerini ve frontend'in npm çalışma zamanı
bağımlılıklarını tarar; taban imajların (UBI) işletim sistemi paketleri ve
frontend imajındaki Node.js çalışma zamanı taranmaz. SECURITY.md taban imaj
açıkları için 7 günlük hedef verir, ama bu açıkların fark edilmesi bugün Red
Hat ve Node.js duyurularına ve Dependabot'un taban imaj PR'larına bağlıdır.
Çözüm: yayımlanan imajlara haftalık bir imaj tarayıcısı (Trivy ya da Grype;
SBOM'dan da çalışabilir). Tetikleyici: bir taban imaj açığının 7 günlük
hedefi kaçırması ya da bir müşterinin tarama raporu istemesi.
