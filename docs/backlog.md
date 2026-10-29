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
