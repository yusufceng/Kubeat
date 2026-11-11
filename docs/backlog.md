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

Güncelleme (2026-10-29, S37, #50): kural düzeltmesi geldi.
`metrics.prometheusRule.clusterLabel` verilince `KubeatDown`,
`KubeatUnhealthy`, `KubeatBackupFailing`, `KubeatMonitorDown` ve
`KubeatCertificateExpiring` bu etiketle cluster başına toplanır, depo
etiketlerinden yalnızca onu taşır (tekilleştirilmeyen HA çiftinin kopya
etiketi toplanır) ve `KubeatDown` son 24 saatte görülüp kaybolan cluster
için de yanar; boşken kurallar 0.44'teki gibi render edilir. İki cluster'lı
senaryolar `deploy/helm/prometheusrule-cluster-test.yaml`'da (promtool). Belge
(S30) sprint 4'te: kuralları bu değerle render ettirir ve README "Aracın
kendi sağlığı"ndaki `clusterLabel` alt başlığına bağlanır. Kalan: toplama
yapmayan, her seriye ayrı bakan öteki öz-sağlık kuralları (ör.
`KubeatSchedulerStalled`, `KubeatResultsDropped`) cluster başınadır, ama
tekilleştirilmeyen bir HA çiftinde kopya başına bir kez yanar; tetikleyici:
böyle bir depoda çift alarm şikâyeti.

Güncelleme (2026-10-29, S26, #49): kanal başına olan
`KubeatNotificationsFailing`'in kanal ifadeleri de `clusterLabel`
verilince bu etiketle, `channel` ve `channel_type`'ın yanında cluster
başına toplanır ve scrape boşluğunda cluster başına tutulur; `channel` ve
`channel_type` `clusterLabel` olarak reddedilir. Aynı kuralın kuyrukta
düşen bildirim ifadesi "Kalan"daki kurallar gibi her seriye ayrı bakar ve
tekilleştirilmeyen HA çiftinde kopya başına bir kez yanar; tetikleyici
aynı.

Güncelleme (2026-10-29, sprint 3 kapanışı): Müşteri B (önemli) 0.45'in
kurulum notlarını eleştiriyor. Notlar heartbeat de kurallar da kapalıyken
uyarıyor, ama gösterdikleri README bölümünde chart'ın `heartbeat.urlSecret`
değeri geçmiyor. Öteki seçenek (`metrics.prometheusRule.enabled=true`)
CRD'siz bir GKE'de `helm upgrade`'i düşürüyor. Önerisi:

- NOTES'ta kopyalanabilir iki satır: Secret'ı oluşturan `kubectl` komutu
  ve `--set heartbeat.urlSecret.name`.
- `monitoring.coreos.com/v1` yoksa NOTES kural seçeneğini önermesin
  (Capabilities). Yalnızca metin olduğu için "chart CRD yokken fail etmez"
  kararına uyar.
- Aynı iki satır README "Aracın kendi sağlığı"nda ve S12'nin hızlı
  başlangıcında olsun.

S30 gelene kadar operator'süz kurulumun tek öz-koruması heartbeat. PMO
önerisi: sprint 4 doküman paketine
([sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md)
"Müşterilerin yeni bulguları" 4).

Güncelleme (2026-10-29, sprint 4 planı): Müşteri B'nin NOTES önerisi (iki
kopyalanabilir satır, PrometheusRule CRD'si yokken kural önerilmemesi) S30
ile sprint 4 doküman paketinde (#60); README tarafındaki iki satır #59'da.
Aynı pakette operator'süz cluster için önerilen yol da yazılır: CRD'ler
kuruluyken Grafana Alloy `mimir.rules.kubernetes` ile kurallar chart'la
yükselir; kopyalayan için her yükseltmede koşacak eşitleme komutu. 2026-10-12
kararı (PrometheusRule) korunur
([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).

Güncelleme (2026-11-04, #60): operator'süz yol README "Aracın kendi
sağlığı"nda (Alloy `mimir.rules.kubernetes`, mimirtool ve GMP kopya yolu,
her yükseltmede koşan Actions adımı); NOTES CRD yokken kural önermiyor.
Ertelenmeye devam edenler: chart'ta Capabilities ile `fail`, `docs/`
altında statik kural dosyası, kopyalanan kuralların sürüm ayrışması için
`kubeat_build_info`'ya bakan bir kural ve metrics.k8s.io yedeği (kendi
maddesinde). Tetikleyici değişmedi ([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).

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

Güncelleme (2026-11-06, sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
stdout satırı S83 olarak hikâyeleştirildi. Müşteri A: `deny`/`target-scope`
satırlarının merkezi loga ya da SIEM'e giden bir makine yolu yok, README ise
"SOC'a bağlanabilir" diyor. Alınmadı: tetikleyici (ilk dış kurumsal görüşme)
gerçekleşmedi, Müşteri A simüle bir müşteri; satır sınırı (`auditCap`) S82'ye
bağlı ve madde ADR-0002'deki lisans kapısına bir not gerektiriyor. SOC'un bugün
de bağlanabileceği sinyal `kubeat_target_scope_denied_total` sayacı. Tetikleyici:
S82'nin merge edilmesi ve gerçek bir SIEM isteği. README "Kapsam ihlali
denetimi"ndeki "SOC'a bağlanabilir" cümlesi sprint 9 belge turunda "kayıtlar API
ve arayüzden okunur, SIEM'e kendiliğinden akmaz" diye daraltılır.

Güncelleme (2026-11-07, sprint 10 planı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)):
yalnız `deny`/`target-scope` dilimi S82'ye çekildi
([#143](https://github.com/yusufceng/Kubeat/issues/143); PO kararı). Müşteri A'nın
sprint 10 keşfindeki bulgusu: API'deki insan denemesi loga düşmüyor, kontrol anı
satırında namespace ve monitor kaynağı yok. API reddi bir `WARN` yazar (kullanıcı,
namespace, reason, host), kontrol anı `WARN`'ı namespace'i ve kaynağı taşır; satırlar
`auditCap`'in dakika sınırına bağlı. ADR-0002'ye aykırı değil: yukarıdaki karara göre
stdout satırı lisans kapısının dışında, kapı arayüz, API ve CSV. ADR notunu S82 yazar;
README "Kapsam ihlali denetimi"ndeki "SOC'a bağlanabilir" cümlesi de S82'de daralır.
Kalan kısmın (denetim kaydının tamamını stdout'a yazmak, süzgeçler, CSV, kaynak
adresi) tetikleyicisi değişmedi: gerçek bir SIEM isteği.

Güncelleme (2026-11-07, S82 [#143](https://github.com/yusufceng/Kubeat/issues/143), 0.52):
`deny`/`target-scope` dilimi teslim edildi; ADR-0002'ye not yazıldı ("Not: denetim
satırının log satırı"). README "Kapsam ihlali denetimi" artık "kayıtlar API ve
arayüzden, log satırı merkezi logdan okunur; SIEM'e kendiliğinden akmaz" diyor.

Güncelleme (2026-11-08, S66 [#164](https://github.com/yusufceng/Kubeat/issues/164), 0.53):
kodsuz ara yol README "Hesaplar ve giriş" → "Denetim kaydını SIEM'e almak"ta: API
anahtarıyla zamanlanmış `GET /api/v1/audit?limit=`, gönderilmiş satırlar `id`'ye göre
ayıklanır, lisansta `audit` gerekir; "Kapsam ihlali denetimi"nin SIEM cümlesi tarife
bağlanır. Bu maddeye eklenen parça (kod): denetim API'sine `since` (ya da son `id`'den
sonrası) parametresi. Bugün her çekme son `limit` satırı alır (en çok
`UPTIME_MAX_RESULTS`); iki çekme arasında bundan çok satır yazılırsa aradakiler kaçar.
Tetikleyici: bu yüzden satır kaçıran bir SIEM çekmesi ya da gerçek bir SIEM isteği. Kalan
kısmın (kaydın tamamını stdout'a yazmak, süzgeçler, CSV, kaynak adresi) tetikleyicisi
değişmedi.

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

Güncelleme (2026-11-07, sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
S86 ([#131](https://github.com/yusufceng/Kubeat/issues/131)) kapsam kaydının yerini bir ADR'de seçiyor; göçlü seçenek (yeni
sütun) bu kuralı tetikler. PO kararı: sprint 9'da göç yok, kayıt var olan
`settings` JSON'unda tutulur ve 0.51.0 şema 26 ile çıkar (0.50.0'a `helm
rollback` mümkün). ADR yine de göç gerektirirse göçlü dilim S6 ile sprint 10'a
kayar ve S6 ondan önce merge edilir.

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

Güncelleme (2026-10-29, sprint 4 planı): S7 sprint 5 kuyruğunda; NOTES'a
dokunduğu için S30'dan (#60) sonra gelir. Sprint 4'te S12'nin
`docs/yukseltme.md`'si (#59) "yükseltmeden önce yedek" için pilot.md
"Yükseltmeden önce yedek"e bağlanır (`exec … cat` ve sağlama toplamı; `tar`
gerekmez). Müşteri B'nin istediği GKE yolu (VolumeSnapshot ya da Backup for
GKE) ve NOTES'taki yedek notunun platformdan bağımsız yazılması, SRE'nin
namespace ya da cluster kaybındaki RPO cümlesi S7'nin kabul ölçütünde
([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).

Güncelleme (2026-11-04, #59): `docs/yukseltme.md` "Yükseltmeden önce
yedek" bölümü pilot.md'nin bloğuna bağlanıyor (`exec … cat` ve sağlama
toplamı); tek komutluk blok ve imaja `tar` S7 ile sprint 5 kuyruğunda
(sprint 4 kapanışı önerisinde 7. sıra, [sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).

Güncelleme (2026-11-06, sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
#117'nin gözlemi kayda geçer. `oc exec` ile düz `cat`, Developer Sandbox'ta
~5,7 MB'lık yedeği 0,6–2,4 MB'a sessizce kesti; pilot.md "Yükseltmeden önce
yedek" bloğu bu yüzden 512 KiB'lik parçalara, parça başına ve tüm dosya için
sha256 doğrulamasına geçti. Bloğun müşteri runbook'una
([yedek-geri-yukleme.md](yedek-geri-yukleme.md)) taşınması ve doğrulamasız
geri yüklemenin kapatılması sprint 8'de S84
([#120](https://github.com/yusufceng/Kubeat/issues/120)). Parçalı blok `tar`
olmadan çalıştığı için imaja `tar` ekleme gerekçesi zayıfladı; `tar` kısmı
backlog'da kalır. Tetikleyici: parçalı bloğun bir platformda (ör. GKE)
yetersiz kalması.

Güncelleme (2026-11-07, sprint 8 kapanışı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
parçalı blok 0.50'de müşteri runbook'unda (S84, [#120](https://github.com/yusufceng/Kubeat/issues/120)) ve sandbox'ta `oc` ile
ilk kez gerçek bir platformda koştu (5,7 MB, 52 sn, "yedek doğrulandı"); `tar`
gerekmedi. Müşteri A (sprint 8 yeni bulgusu 2, önemli) bu maddenin OADP/Velero
kısmını yeniden istiyor: veri PVC'si yalnız CSI snapshot ile (A'ya göre canlı
SQLite + WAL dosya sistemi yedeğiyle tutarlı alınmaz), yedek PVC'si fs-backup ile,
anahtar Vault'ta ayrı tutulur ve `secretKey.existingSecret` ile verilir, Restore
sırası Secret → PVC → Deployment, OpenShift'te bir prova kaydı. README'deki
"gerekmez" cümlesi CSI snapshot koşuluna daraltılmalı. Sprint 9 belge turu adayı,
S41 provasıyla birlikte.

Güncelleme (2026-11-07, sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
sprint 9'a alınmadı: dörtlü sınır güvenlik ve kurulum işleriyle dolu. Yalnız
belge; sprint 11 belge turunda, aynı dosyaya (`docs/yedek-geri-yukleme.md`)
yazan S90 ile ("Doğrulamalı yedek bloklarının kalanları"). S41 OpenShift geri
yükleme provası kullanıcıda.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
S7 sprint 11'e girmedi, S90 ile sonraki sprint kuyruğunda (5. sıra). Zararlı kısım plan
dalında, slot almadan düzeltildi. README "GitOps ile kurulum" → "Kurulum notları görünmez"
tablosunun "Yedek" satırı, Velero ya da OADP'nin yedeğin yerine ancak veri volume'unu CSI
snapshot'la alıyorsa geçtiğini yazıyor. Kurulum notundaki aynı cümle
(`deploy/helm/kubeat/templates/NOTES.txt:323-325`, "Velero, OADP") S7'nin chart kısmında
kaldı; `test-chart.sh` o metni sınıyor. S41'in OADP adımı S98'in
([#162](https://github.com/yusufceng/Kubeat/issues/162)) OpenShift runbook'unda "ölçülmedi,
prova kullanıcıda" diye yer alır.

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

Güncelleme (2026-11-06, sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
S34'ün kod kısmı (arayüzün `/api/v1` ve `/metrics` isteğine giriş sayfası
yönlendirmesi yerine JSON 404 dönmesi; belge kısmı 0.46'da #59/#60 ile yapıldı)
"sonraki sprint" kuyruğundan backlog'a alındı: beş sprinttir kuyrukta kaybediyordu
ve orada tutmak yanıltıcıydı. Yeni kanıt: release manager gözlemi (sandbox
Route'u üzerinden API giriş isteği 307 döndü) ve Müşteri B'nin CI senaryosu
(`curl -f` 307'yi başarı sayıyor). Etkisi yanlış adrese yazılmış bir betikle
sınırlı; doğru yol 0.46'dan beri README "API" ve NOTES'ta yazılı. Tetikleyici:
`frontend/proxy.ts`'e dokunan ilk madde (S34 ona katılır) ya da gerçek bir
kullanıcı raporu.

Güncelleme (2026-11-07, sprint 9 kapanışı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
`kubeat.io/path`'in Route ve Ingress'te tanınması için yeni kanıt (Müşteri A, sprint 9
bulgusu 1, önemli). 0.51'de annotation monitor'leri her kurulumda nesnelerinin
namespace'ine bağlı. SSO arkasındaki bir uygulamanın annotation monitor'ü kendi host'u
dışındaki kimlik sağlayıcı sayfasına ulaşınca `blocked_by_scope` ile DOWN oluyor.
Kapsamı açmayan çare kontrolü uygulamanın kendi host'unda, kimlik doğrulaması istemeyen
bir sağlık yolunda tutmak. Annotation'la çalışan ekip bunu manifestten ancak
`kubeat.io/path` Route ve Ingress'te de geçerse verebilir (bugün yalnız Service;
`backend/internal/discovery/sync.go`). Bu parça (küçük) sprint 10'da S43 ile aynı keşif
paketinde slot almadan, ya da sprint 11 belge turunda SSO çaresiyle birlikte adaydır;
karar PO'da. Ada göre arama, `upsert` ve OpenAPI'nin tetikleyicisi değişmedi.

Güncelleme (2026-11-07, sprint 10 planı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)):
`kubeat.io/path`'in Route ve Ingress'te tanınması ve sertifikası `spec.tls` dışında
verilen GKE Ingress'inin (ManagedCertificate, pre-shared-cert, host'suz tls girdisi)
`https://` sayılması S94 olarak hikâyeleşti. S94 sprint 11 adayı: S95 ile Müşteri
B'nin GKE ve annotation yolunda, `k8s.go`'da S43'ün
([#147](https://github.com/yusufceng/Kubeat/issues/147)) arkasından. Kapsamı açmayan
bugünkü çare: yönetilen monitor'de HTTP seçenekleri (yönlendirme sınırı, kabul edilen
kodlar) arayüzden değiştirilebiliyor (`backend/internal/api/monitors.go:1198-1204`).
Ada göre arama, `upsert` ve OpenAPI backlog'da; tetikleyicileri değişmedi.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
S94 sprint 11'e girmedi. PO'nun gerekçeleri: geçici çare var ve B'nin GKE yolunda S95+S98
öne alındı. Madde sonraki sprint kuyruğunda 2. sırada. Kurum CA'sı reçetesi (S2,
[#163](https://github.com/yusufceng/Kubeat/issues/163)) bu sprintte geliyor; S94 onu hazır
bulacak. SSO çaresi sprint 11'de Service'teki `kubeat.io/path` ile yazılıyor: S98
([#162](https://github.com/yusufceng/Kubeat/issues/162)) `docs/pilot.md`'de, S66
([#164](https://github.com/yusufceng/Kubeat/issues/164)) `docs/yukseltme.md` 0.51'de.

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

## Durum sayfasının son görünümünü "son güncelleme" etiketiyle önbellekten göstermek

2026-11-07'de ertelendi (sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)).
Müşteri B (bulgu 3, önemli): Kubeat yükseltilirken ya da backend pod'u başka
node'a taşınırken müşterinin durum sayfası "Durum şu anda okunamıyor" gösteriyor
(backend tek replica ve Recreate). İsteğin ikinci yarısı: arayüz son görünümü
"son güncelleme" zamanıyla önbellekten göstersin. Kod işi; bugün müşteriye açık
bir durum sayfası yok. Belge kısmı (S75: sayfanın Kubeat ve cluster'la birlikte
düştüğü, yükseltmede bir iki dakika okunamadığı, dışarıdan bakan tamamlayıcı)
sprint 9'da S76 ([#134](https://github.com/yusufceng/Kubeat/issues/134)) ile README "Durum sayfaları" → "Sınırlar"a girdi. Yukarıdaki
maddenin "durum sayfası önbelleği ayrı karar" notunun yanında durur. Tetikleyici:
müşteriye açık ilk durum sayfası.

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

Güncelleme (2026-10-29, S38, #50): yeni `KubeatUnhealthy` de bu gruptadır.
Scrape etiketleri dışında toplandığı için Recreate'te tek alarmdır, ama
boşlukta durumunu tutmaz: volume dolu kalırken backend yeniden başlarsa
alarm boşlukta çözülür ve yeni pod'un ilk penceresinden 5 dakika sonra
yeniden yanar. Tetikleyici aynı; ek olarak bu alarmın bir yeniden başlatmada
"resolved" göndermesi.

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

Güncelleme (2026-10-29, S26, #49): `KubeatNotificationsFailing` kanal
başına (`channel`, `channel_type`) ve `for: 5m` ile yanar; başarısız deneme
sayısına değil kanalın son denemesine (`kubeat_channel_last_delivery_ok`)
ve almadığı kesintilere (`kubeat_channel_undelivered_outages`) bakar.
Müşteri B'nin hıçkırık bulgusu kapandı. Dead-letter görünümü, kalıcı
kuyruk ve kapanmış olayın teslim edilmemiş `up`'ı bu maddede kalır,
tetikleyici değişmedi.

Güncelleme (2026-10-30, S28, #61): Müşteri A'nın "en azı" yapıldı: README
"Bildirim kanalları"nda PagerDuty ve webhook satırları, kanal uzun süre
ulaşılmaz kalabiliyorsa `UPTIME_NOTIFY_RETRY_FOR`'un bakım penceresinden
uzun tutulmasını söyler. Kapanmış olayın teslim edilmemiş `up`'ı, dead-letter
görünümü ve kalıcı kuyruk bu maddede kalır, tetikleyici değişmedi.

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
bildirimin ilk reddedildiği geçişte bir geçiş daha) gecikir (S26 ile
kapandı; bkz. aşağıda "Güncelleme (2026-10-29, S26, #49)").

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

Güncelleme (2026-10-29, S38, #50): ikinci bulgu karşılandı.
`UPTIME_NOTIFY_QUEUE_SIZE` varsayılanı 1000 (`UPTIME_MAX_MONITORS`'ın
varsayılanı; ondan türetilmez). Etkin monitor sayısı kuyruğu aşınca backend
bunu bir kez, iki sayıyla WARN olarak loglar: açılışta ya da sonradan sınır
ilk geçildiğinde. Bellek bedeli README "Alarm"da: 1000'lik kuyruk ≈ 1 MiB,
doluyken ≈ 1,7 MiB.

Güncelleme (2026-10-29, S26, #49): S25'in "Kalan" cümlesindeki 4xx kısmı
kapandı. Reddedilen (4xx) bildirim makine kanalını almıyor saydırmaz ve
geçişi durdurmaz: arkasındakiler aynı geçişte, yeni kesintiler ve
düzelmeler beklemeden gider; almıyor sayılan yalnızca ulaşılamayan ve
meşgul (429, `Retry-After`'lı 503) kanaldır (README "Alarm", hata
sınıfları). Kalanlar "Teslimat hata sınıflarının kalanları"nda: tek bir
monitor'un bildirimlerine sürekli 5xx dönen alıcı kanalı yine her geçişte
duraklatır; her şeyi reddeden makine kanalı ise artık duraklamadığı için
bekleyen her bildirim için `UPTIME_NOTIFY_RETRY_MAX`'ta bir istek alır ve
o geçiş teslimat döngüsünü tutar. `UPTIME_NOTIFY_GROUP_WAIT=0` iken yanıt
vermeyen insan kanalı değişmedi.

Güncelleme (2026-10-30, S55, #62): S25'in "Kalan" cümlesindeki yanıt
vermeyen insan kanalı kısmı kapandı. Ulaşılamayan ya da meşgul insan kanalı
(Slack, e-posta…) da yeniden deneme zamanına kadar almıyor sayılır,
gruplama kapalıyken de: yeni bildirimler ve namespace grupları ona
denenmez, kanalın sıradaki geçişinde gider. Ölü bir e-posta relay'iyle 1, 3
ve 5 namespace'e yayılan kesintide webhook, grup teslimi sürerken gelen
yeni kesintiyi grup sayısından bağımsız olarak en çok bir deneme süresinde
alır (`go test -race`,
`TestADeadMailRelayDoesNotHoldUpThePagesOfAnOutageOverNamespaces`).

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

Güncelleme (2026-10-29, sprint 3 kapanışı): iki müşteri de andı. Müşteri
A'ya göre "yalnızca son minor" kuralı, aynı gün iki minor (0.44.0, 0.45.0)
çıkaran bir üründe 3 yıllık destek sorusunu açık bırakıyor; satın alma ve
filoya yaygınlaştırma için açık üç konudan biri. Müşteri B minor'lar
neredeyse her gün çıktığı için sık yükseltmek zorunda; Release metninin
yükseltme notuna bağlanmasını istiyor ("Yayım hattının kalanları"). Karar
ve tetikleyici (ilk satın alma görüşmesi) değişmedi; Müşteri A da bunu
backlog kararı olarak anıyor
([sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md)
"Müşteri puanları").

Güncelleme (2026-11-07, sprint 9 planı; kullanıcı kararı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
tetikleyici değişti: "ilk satın alma görüşmesi" yerine **kararlı sürüm (1.0)
kararı (sprint 11 sonu)**, [yol-haritasi.md](yol-haritasi.md) satır 32.
Gerekçe kullanıcı kararı (2026-11-07): hiçbir sürüm satılmadı; eski minor'lara
geri taşıma (0.46.1 dahil) yapılmaz, destek penceresi kararlı sürüm çıkarken
düşünülür. Müşteri A'nın 3 yıllık destek sorusu o kararda cevaplanır. 1.0'a
kadar SECURITY.md'nin advisory cümlesi düzeltilir (S69, sprint 9 sürüm PR'ı).

Güncelleme (2026-11-07, sprint 9 kapanışı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
Müşteri A (sprint 9 bulgusu 3; önemli, 1.0'da satın alma için engelleyici) kullanıcı
kararını (1.0'a kadar advisory yok) gerekçesiyle kabul ediyor ve 1.0 kararı için üç şart
öneriyor: kendi bulunanlar dahil her güvenlik düzeltmesine etkilenen ve düzelen sürüm
aralığıyla bir GHSA ve CVE kimliği; o zamana kadar `docs/yukseltme.md`'deki "güvenlik
sürümü" adlandırmalarının etkilenen sürüm aralığını makine okunur bir tabloda vermesi;
1.0 ile en az bir önceki minor'a yama veren bir destek penceresi. Gerekçe: SBOM ve
GHSA/CVE okuyan tarayıcı advisory'siz bir düzeltmeden önceki sürümü açıklı göstermez.
Tetikleyici değişmedi (1.0 kararı, sprint 11 sonu); bu üçü kararın girdisi.

Karar (2026-11-11; kullanıcı release manager'ın önerisini onayladı; SECURITY.md güncellendi):
**destek penceresi yok, 1.0'dan sonra da.** Güvenlik düzeltmesi yalnız son minor'a (yama ya da
sonraki minor), önceki minor'a geri taşıma yok; daha uzun bir pencere ancak sözleşmeyle ve onun
kapsamında. Duyuru: dışarıdan bildirilen açığa GHSA (+CVE); kendi bulunan açığa yükseltme notunda
"güvenlik sürümü" + SECURITY.md "Güvenlik sürümleri" tablosunda etkilenen aralık. Müşteri A'nın
üç şartından ikincisi (makine okunur tablo) karşılandı; birincisi (kendi bulunanlara GHSA/CVE) ve
üçüncüsü (bir önceki minor'a yama) kabul edilmedi: satılmış sürüm ve ekip yok, geri taşımanın
maliyeti tek bakımcıda; gerekirse sözleşmeyle verilir. Bölüm kapandı; 1.0 etiketinin kalan
ön koşulu [yol-haritasi.md](yol-haritasi.md) satır 32'de.

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

Güncelleme (2026-10-29, sprint 4 planı): S39, S41 ile sprint 5'in
korumalı 1. maddesine kaydı. NOTES'taki "helm uninstall leaves" bloğu
sprint 4'te S30 ve S34'ün NOTES değişikliğiyle (#60) çakışırdı. Karar değişmez: `keep`,
metrik anahtarı dahil, `backend.persistence.keepOnUninstall`'a bağlı.
Müşteri B (önemli): Terraform destroy/apply API anahtarını değiştiriyor ve
GitHub Actions'taki secret uyarısız bozuluyor; SRE (orta) API ve metrik
anahtarını alarm açmadan yenileme adımlarını aynı PR'da istiyor. Ara önlem:
README "Kazayla kaldırmadan dönüş" (eski parola,
`UPTIME_ADMIN_PASSWORD_RESET`)
([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).

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

Güncelleme (2026-10-29, S38, #50): Müşteri A'nın önerisi geldi.
Heartbeat'in kararı (`scheduler.Healthy`) `kubeat_self_healthy` gauge'unda
her scrape'te, heartbeat kapalıyken de yayınlanır; chart'ın
`KubeatUnhealthy` kuralı gauge 5 dakika 0 kalınca `critical` yanar.
`KubeatResultsDropped` warning olarak kaldı; liveness ve readiness bu
karara bağlanmadı. Karar değişince backend, heartbeat olmadan da, bir kez
loglar (`self-health failing` nedeniyle, düzelince `self-health restored`);
tetikleyicideki `heartbeat withheld`'in heartbeat'siz kurulumdaki karşılığı
bu satırdır. Bu maddenin kalanları değişmedi: PVC'nin kendiliğinden
büyümesi, saklamanın kotaya göre ayarlanması, yazılamayan sonuçların
bellekte tutulması, toplu log satırına `monitor_id`'ler ve dakikada bir
özet satırı.

Güncelleme (2026-11-04, sprint 4 kapanışı): minikube provası (2) dolu
diskte iki monitor'lük yükle beş dakika hiçbir sonucun kaybolmadığını
gösterdi (SQLite WAL var olan alanını yeniden kullanır); `KubeatUnhealthy`
disk doluluğunu değil kaybolan sonuçları ölçer, seyrek kurulumda dolu disk
uzun süre sessiz kalabilir. Müşteri A (hoş olur): README "Hangi arızayı ne
duyurur"daki "volume doluysa" cümlesi bu sınırı açıkça yazsın ve PVC
doluluğu için platform alarmına yönlendirsin (OpenShift'te
`KubePersistentVolumeFillingUp`, `kubelet_volume_stats_*` ile
`<release>-data` PVC'si). S41'in runbook'uyla (sprint 5 korumalı 1. madde)
yazılır ([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md), "Müşterilerin yeni bulguları" 3).

Güncelleme (2026-11-05, S60 #97): dolu veri diski için platformdan bağımsız
alarm yolu belgelendi (README "Hangi arızayı ne duyurur" ve runbook "Dolu veri
volume'undan dönüş": `KubePersistentVolumeFillingUp` kubernetes-mixin kuralıdır
ve OpenShift, kube-prometheus-stack ve GKE'de Grafana Cloud / Google Managed
Prometheus ile gelir; yoksa `kubelet_volume_stats_*` oranından elle kural).
PO kararıyla chart'ın kendi opsiyonel PVC-doluluk kuralını basması **eklenmedi**:
platform zaten mixin kuralını taşıyor, chart'ın ikinci bir kural basması
çiftleme riski taşır ve yalnız değerle kapatılabilir bir ek olsa bile değeri
belgeyi aşmaz. Tetikleyici: mixin'i taşımayan bir platformda PVC alarmını chart
değeriyle isteyen bir kullanıcı.

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

1. köşe kapandı (S26, #49): reddedilen (4xx) bildirim kanalın geçişini
durdurmaz; insan kanalının sürekli reddettiği bir yeniden deneme o kanalın
öteki yeniden denemelerini bekletmez, aynı geçişte arkasındakiler gider
(`go test -race`, `TestARefusedChatMessageDoesNotHoldBackTheOthers`). Bir
geçişte ulaşılamayan kanal ise öteki yeniden denemeleri yine bekletir:
kapalı kanal geçiş başına bir kez denenir. 3. köşe kısmen: bekleyen
listenin kesinti bildirimleri kanal başına
`kubeat_channel_undelivered_outages` ile sayılır ve PO'nun ek kabul
ölçütünün mekanizmasıdır (kanalın almadığı kesinti beklerken
`KubeatNotificationsFailing` açık kalır). Bekletilen hatırlatma, sertifika
ve yavaşlık bildirimleri ile kuyruk doluluğu sayılmaz; o kısım bu maddede
kalır, tetikleyicisi değişmedi.

Güncelleme (2026-10-29, sprint 4 planı): 3. köşenin kuyruk doluluğu
parçası S55 (#62) ile sprint 4'te: kuyruk doluluğu `/metrics`'te bir
gauge'la. 4. köşe S27, 2. köşe S40 ile sprint 5 kuyruğunda; `alert`
paketinin sırası S55 → S27 → S40 → S57. Notlar merge'de yazılır
([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).

3. köşenin kuyruk doluluğu parçası kapandı (S55, #62):
`kubeat_notification_queue_length` teslimat kuyruğunda o an bekleyen
bildirim sayısını her scrape'te verir (README "Aracın kendi sağlığı").
Almıyor sayılan kanala bekletilen hatırlatma, sertifika ve yavaşlık
bildirimlerinin sayısı hâlâ yalnızca `notifications held back` log
satırında; o kısım bu maddede kalır, tetikleyicisi değişmedi.

4. köşe kapandı (S27, #96): yeniden başlatmada monitor bakımdaysa teslim
edilememiş kesinti artık yeniden gönderilmez (`retryUndelivered`
`InMaintenance`'a bakar), gece bildirimi gitmez; mevcut maintenance testleri
(S25/S26) geçer. Kuyruk en yüksek değeri (high-water) metriği
(`kubeat_notification_queue_length_max`, Müşteri A'nın kuyruk doluluğu gauge
önerisi, scrape'ler arası ani dolmayı yakalardı) S27'nin kapsamında "sığarsa"
sayıldı ama bu sürüme alınmadı: `kubeat_notification_queue_length` anlık
değeri çoğu durumu gösteriyor ve high-water, motorun her geçişte örnekleyip
scrape'te sıfırlayan yeni bir durumunu ister. Bu maddede kalır; tetikleyici
`metrics`/`alert` paketine sonraki gözlemlenebilirlik dokunuşu ya da pilotta
ani kuyruk dolması görülmesi. Bekletilen hatırlatma/sertifika/yavaşlık
sayısı da bu maddede.

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
README cümlesi #60 ile "Raporlar"a girdi ("Bakım ve uptime").

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
monitor oluşturun), README cümlesi S12 ile. README cümlesi #60 ile
"Annotation ile sürekli keşif"e girdi.

Güncelleme (2026-11-07, sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
Müşteri B (bulgu 1) chart'a Gateway API `httpRoute` şablonu da istiyor; ayrı
madde (aşağıda). `ingress-nginx`'in bakımdan çıkması artık yalnız B'nin beyanı
değil: PO bilgisi Kubernetes projesinin onu emekliye ayırdığı (duyuru 2025-11,
bakım sonu 2026-03). Ajan sınırında doğrulanmadı; S87 ([#133](https://github.com/yusufceng/Kubeat/issues/133)) README'ye
varsayım olarak yazar, release manager yayımdan önce duyuru bağlantısını açıp
doğrular. Bu cümle S87 merge edilince güncellenir.

Güncelleme (S87, [#133](https://github.com/yusufceng/Kubeat/issues/133)): README "Cluster'a kurulum" GKE paragrafı
`ingress-nginx`'i artık "önerilen yol" diye değil, emekliye ayrılmış ve mevcut
kurulumlar için diye anıyor; cümlenin yanında, kaynakta, varsayım notu (HTML
yorumu) var ve release manager duyuruyu doğrulayınca kalkar. GKE Gateway
README'de üçüncü yol olarak anıldı (HTTPRoute'u kullanıcı yazar) ve hop
tablosuna "ölçülmedi" satırıyla girdi. Keşif tarafı (bu madde) değişmedi.

## Chart'a isteğe bağlı Gateway API `httpRoute` şablonu

2026-11-07'de ertelendi (sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)).
Müşteri B (bulgu 1): `ingress-nginx` bakımdan çıkınca arayüzü Gateway API ile
açmak istiyor; chart yalnız Ingress ve Route şablonu veriyor. Bugün Gateway API
kullanan bir kurulum yok, istek parite. Bu sprintte yalnız README cümlesi ve hop
tablosuna "GKE Gateway (`gke-l7-global-external-managed`)" satırı "ölçülmedi"
ile giriyor (S87, [#133](https://github.com/yusufceng/Kubeat/issues/133)). Yukarıdaki keşif maddesiyle (S49) birlikte
düşünülür. Tetikleyici: Gateway API'ye geçen ilk kurulum.

## Prometheus yokken pod CPU ve belleğini metrics.k8s.io'dan okumak

2026-10-29'da ertelendi (S50). Müşteri B (hoş olur): GKE'de monitor
sayfasındaki pod panelinin CPU ve belleği boş kalıyor. Kubeat yalnızca
bearer token'lı bir Prometheus okuyor; her GKE cluster'ında hazır olan
`metrics.k8s.io` (metrics-server) kullanılmıyor. Kanıt zayıf ve aynı işi
başka araç yapıyor: Müşteri B aynı veriyi Grafana Cloud'un Kubernetes
panolarında görüyor; şişirme riski var. Tetikleyici: operator'süz ilk
gerçek kurulum. Karar taslağı: `prometheus.url` boşken anlık değerler
PodMetrics'ten okunur, ek ayar yok, geçmiş grafiği yok. Sınır paragrafı S30
ile (sprint 4 doküman paketi); #60 ile README "Pod'lar ve kaynak
kullanımı"na girdi ("GKE ve Grafana Cloud").

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

## Teslimat hata sınıflarının kalanları

2026-10-29'da ertelendi (S26, #49; issue'nun kapsam dışı listesi ve
developer raporu). S26 teslimat hatalarını dört sınıfa ayırdı (README
"Alarm", hata sınıfları); şu parçalar dışarıda kaldı:

- **SMTP'nin kalıcı yanıtları.** 5xx SMTP kodları (ör. 550, alıcı yok)
  "ulaşılamadı" sayılır: kısa tekrar ve 1, 2, 4… dk bekleme alır. "Reddetti"
  sayılmaları e-posta göndericisinin SMTP kodunu taşımasını ister.
- **Gövdedeki `retry_after`.** `Retry-After` yalnızca başlıktan, saniye ya
  da HTTP tarihi olarak okunur. Telegram'ın `parameters.retry_after`'ı,
  Discord'un gövdedeki `retry_after`'ı ve ondalık saniye okunmaz; başlıksız
  429 1 dk bekler.
- **Düzenlemede yeniden deneme.** Düzeltilen kanalın reddettiği bildirim
  sıradaki denemesini (en çok `UPTIME_NOTIFY_RETRY_MAX`) bekler; kanalı
  kaydetmek denemeyi öne almaz.
- **Kanalı durdurmak ve "bozuk" rozeti.** Sürekli reddeden kanal
  kendiliğinden durdurulmaz, Bildirimler sayfasında ayrı bir rozet yoktur;
  son hata kanal satırında görünür, alarm `KubeatNotificationsFailing`'dir.
- **İstemci tarafında hız sınırlayıcı.** Sağlayıcı başına istek hızı
  sınırlanmaz; 429'a `Retry-After` ile uyulur.
- **Tek monitor'a 5xx dönen alıcı.** Bir monitor'un bildirimlerine sürekli
  5xx dönen alıcı kanalı her geçişte yeniden duraklatır, yeni bildirimler
  en çok `UPTIME_NOTIFY_RETRY_MAX` gecikir; 4xx'ten farklı olarak bunu
  "kanal ölü" durumundan ayırmak alıcıyı tanımayı ister.
- **Her şeyi reddeden makine kanalının geçiş maliyeti** (S26'nın SRE
  incelemesi). Reddeden kanal canlı sayıldığı için (PO kararı) süresi
  dolmuş token'a 401 ya da kaldırılmış alıcıya 404 dönen paylaşılan bir
  webhook ya da PagerDuty kanalı, bekleyen her bildirim için
  `UPTIME_NOTIFY_RETRY_MAX`'ta bir istek alır; 0.44'te geçiş başına tek
  deneme alıyordu. Birlikte reddedilenler (kanal bir süre ulaşılamaz
  kaldıktan ya da Kubeat yeniden başladıktan sonra) aynı ana planlanır ve
  tek geçişte sırayla gider; teslimat döngüsü geçişi beklediği için bu
  sırada kuyruk okunmaz. İncelemedeki sonda testinde 200 açık kesintide
  1., 16., 31. ve 46. dakikada geçiş başına 200 istek gitti; 300 ms'lik bir
  ITSM ve 500 kesintide döngü her 15 dakikada ~2,5 dakika tutulur, yavaş
  dönen bir 401'de daha uzun (yanıt başına en çok `UPTIME_NOTIFY_TIMEOUT`).
  Bugünkü çare kanalı düzeltmek ya da kapatmak (README "Alarm", Reddetti).
  Seçenekler: (a) reddedilen bildirimin yeniden denemesini geçişin
  sonundan değil kendi reddedildiği andan saymak: ilk birlikte geçişten
  sonra anlar yayılır, toplam istek aynı kalır ama döngü parça parça
  tutulur ve kuyruk arada okunur, "reddeden kanal canlıdır" kararı
  değişmez; (b) bir geçişte gönderilenlerin hepsini reddeden makine
  kanalında geçişi sınırlamak (ilk birkaç reddetmeden sonra kalanları
  bekletmek): istek sayısı da düşer ama kararı daraltır, PO kararı ister.
  Öneri: önce (a); (b) yalnızca (a)'dan sonra da gecikme görülürse. Aynı
  karar geçici bir 4xx'i de (alıcı yeniden yayımlanırken 404) reddetme
  sayar ve o bildirimi `UPTIME_NOTIFY_RETRY_MAX` kadar geciktirir.
  Tetikleyici: reddeden bir kanal varken Slack ya da e-posta grubunun
  `UPTIME_NOTIFY_GROUP_WAIT`'ten geç gitmesi ya da kuyrukta düşen bildirim
  (`kubeat_notifications_dropped_total`), bir geçişte yüzlerce
  `notification to be sent again` satırı (`failure` = `refused`) ya da
  geçici bir 4xx yüzünden geç kalan bir sayfa.

Tetikleyici: pilotta ya da bir kurulumda bunlardan birinin görülmesi
(5xx SMTP yanıtıyla dönen e-posta kanalı, hız sınırını gövdede bildiren
sağlayıcı, düzeltildikten sonra bekleyen sayfa, tek monitor'a 5xx dönen
ITSM, her şeyi reddeden kanalın geçişinde geç kalan grup ya da düşen
bildirim).

Güncelleme (2026-10-29, sprint 3 kapanışı): Müşteri A (#49'a 4 puan)
bedelleri kendi ortamında somut buluyor: geçici bir 4xx sayfayı 15 dk
geciktirir, süresi dolmuş token'lı paylaşılan kanal toplu kesintide
teslimat döngüsünü tutar. Tetikleyici tek ekipli sandbox pilotunda
yanamaz; A, (a) seçeneğinin filoya yaygınlaştırmanın ön koşulu olmasını
istiyor. PMO önerisi: tetikleyiciye "pilot 'devam' kararıyla filoya
yaygınlaştırma" eklenir; kararı PO verir
([sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md)
"Müşteri puanları").

Güncelleme (2026-10-29, sprint 4 planı): PO PMO önerisini kabul etti;
tetikleyiciye "pilotta 'devam' kararıyla filoya yaygınlaştırma" eklendi.
Müşteri A (a) seçeneğini bunun ön koşulu sayıyor ve tek ekipli sandbox
pilotunda tetikleyici yanamaz. Ulaşılamayan bir insan kanalının makine
kanallarını geciktirmesi S55 (#62) ile duraklatmayla kapanır; yapısal
mekanizma ((a) ve kanal başına goroutine) seçilmedi
([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).

Güncelleme (2026-10-30, S55, #62): ulaşılamayan ya da meşgul insan kanalı
duraklatmayla kapandı (README "Alarm", "Almıyor sayılan kanal"). Yeni bir
kalan:

- **Yeniden denenmeyen bildirimin ulaşamadığı kanal duraklamaz.**
  Hatırlatma, sertifika ve yavaşlık bildirimi (`UPTIME_NOTIFY_RETRY_FOR=0`
  iken düzelme de) ilk denemesinde kanala ulaşamazsa düşer; kanalın
  yeniden denenecek başka bir bildirimi yoksa kanal almıyor sayılmaz,
  çünkü bekleyeceği bir yeniden deneme zamanı yoktur. Kesintileri almış bir
  e-posta relay'i ya da Slack sonradan düşerse, birden çok namespace'te
  aynı anda gelen hatırlatma (`UPTIME_REMINDER_INTERVAL`), sertifika ya da
  yavaşlık grupları ona yine grup başına denenir ve teslimat döngüsünü her
  biri için bir deneme süresi (varsayılanlarla 25 sn) tutar; makine
  kanalında da aynıdır (S25'ten beri). Kanalın kendi tekrarı
  (`repeat_seconds`) tek geçişte gittiği için etkilenmez; hatırlatma
  varsayılan olarak kapalıdır. Seçenek: böyle bir hatadan sonra kanalı,
  bekleyecek bir bildirim olmadan da 1 dakika (sonra katlanarak) almıyor
  saymak; bu, makine kanallarının bugünkü davranışını da değiştirir ve
  PO kararı ister. Tetikleyici: bir hatırlatma ya da yavaşlık patlamasında
  geç kalan bir sayfa, ya da aynı insan kanalına art arda ulaşılamayan
  grupların `notification failed` satırları.

Güncelleme (2026-11-04, sprint 4 kapanışı): S55 (#62) ulaşılamayan ya da
meşgul insan kanalını da yeniden deneme zamanına kadar almıyor sayıyor.
Müşteri A (önemli) ve B (hoş olur) bedelini yazdı: kanal dönünce ya da
başarılı bir kanal testinden sonra duraklatma kalkmıyor (`SendOnce`
kuyruğu ve duraklatmayı atlar), yalnız Teams/Slack'i olan ekip yeni mesajı
15 dakikaya kadar geç alır ve nöbetçi test geçince kanalı bozuk sanır.
İstenen: mekanizma kalsın, kanal testi ya da kanalı kaydetmek duraklatmayı
kaldırsın ve bekleyenler o anda gitsin; isteğe bağlı insan kanalı için ayrı
ve kısa üst sınır; README "Bedeli" ve values yorumuna RETRY_MAX ayar
ipucu. Sprint 5 adayı, S27 ile `alert` paketinde ([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md),
"Müşterilerin yeni bulguları" 1 ve 6).

## Yayım hattının kalanları: sürüm sabitleri, kuru deneme, imza kapsamı

2026-10-29'da ertelendi (#47 ve #48'in kapsam dışı parçaları; developer
raporları). 0.45'ten beri yayım imzalı ve SBOM'lu; kurulum komutlarının
sürümünü `scripts/check-versions.sh` CI'da ve yayımın ilk işinde denetler.
Dışarıda kalanlar:

- **`make bump`.** Sürüm sabitleri (Chart.yaml'ın `version` ve
  `appVersion`'ı, Makefile, README ve docs/pilot.md'deki komutlar) elle
  yükseltiliyor; eksik kalanı denetim yakalıyor. Tetikleyici: bir sürüm
  PR'ının bu denetime takılması.
- **`--version`'sız kurulum.** README komutu sürümlü kalır. Sürümsüz "en
  son" kurulum önerilmez, çünkü OCI'da kayan bir chart etiketi yok; `latest`
  imaj etiketi de 0.45'ten beri itilmiyor. Tetikleyici: sürümsüz kurulum
  isteyen ilk kullanıcı.
- **Kuru denemede etiket denetimi.** `workflow_dispatch` koşusunda etiket
  olmadığı için `versions` işi etiket karşılaştırmasını atlar; kuru deneme,
  atılacak etiketin Chart.yaml ile uyuştuğunu sınamaz. Denenecek sürüm bir
  girdiyle verilebilir. Tetikleyici: kuru denemesi geçen bir etiket
  koşusunun `versions`'ta düşmesi.
- **Ayrı `image.digest` değeri.** PO kararıyla kapsam dışı; README
  `tag: "<sürüm>@<digest>"` örneğini veriyor. Tetikleyici: etiketi ve
  digest'i ayrı yöneten bir politika motoru ya da GitOps hattı.
- **Tarama bulgusu için otomatik issue.** Haftalık `security.yml` bulguyu
  yalnızca başarısız koşu olarak bildirir; bildirim iş akışını main'e
  getiren hesaba gider. Tetikleyici: bir bulgunun, bildirim gözden kaçtığı
  için SECURITY.md'deki 7 günlük hedefi kaçırması.
- **Platform manifest'lerini ayrıca imzalamak (`cosign sign
  --recursive`).** Yalnızca index digest'i imzalanıyor. Bu, etiketle ya da
  index digest'iyle verilen referanslara yetiyor; platform manifest'inin
  digest'iyle verilen referans imzasız görünür. Tetikleyici: ilk politika
  motoru isteği.

Güncelleme (2026-10-29, sprint 3 kapanışı): Müşteri A'nın iki yeni bulgusu
bu hatta, ikisi de önemli:

- **Yayım yeşil CI'a bağlı değil.** Yayım, etiketlenen commit'in CI
  sonucuna ve main'den erişilebilir olduğuna bakmıyor. main'de zorunlu
  durum kontrolü yok; bu sprintte #55 kırmızı check'le merge edildi.
- **Aynalama yolu hiç koşulmadı.** İç registry'ye `oras cp -r` ile
  aynalayıp orada `cosign verify --trusted-root` ile doğrulama yolu
  denenmedi. Verify işi yalnızca diskteki backend imajını ve chart'ı
  doğruluyor; docs/pilot.md imzayı taşımayan komutları veriyor.

Müşteri B (hoş olur): Release metni yükseltme notuna bağlanmıyor,
Chart.yaml'da `home` ve `sources` yok. PMO önerisi: depo ayarları hemen,
kod kısmı sprint 4'ün 4. maddesi
([sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md)
"Müşterilerin yeni bulguları" 1, 2 ve 6).

Güncelleme (2026-10-29, sprint 4 planı): depo ayarları sprint 3 kapanışında
açıldı: main yalnız PR'la ve beş check yeşilken değişir, `v*` etiketlerini
yalnız yönetici atar. Yeşil CI kapısı, CI'da hava boşluklu aynalama, Release
bağlantısı ve Chart.yaml `home`/`sources` sprint 4'te S52 (#63) ile gelir.
Yeni bulgular sprint 5'in korumalı 2. maddesi oldu: etiketin, Release'in ve
registry etiketinin değişmezliği (S53; `v*` kural setinde yönetici bypass'ı
"always", Release değiştirilebilir, images ve chart işleri koşulsuz itiyor)
ve derleme girdilerinin sabitlenmesi (S54; taşınabilir major etiketli
action'lar, iş akışı düzeyinde `packages: write`). S53'ün iki kural seti
ayarı slot almadan sprint 4'ün başında yapılır (`v*` kural seti ikiye
bölünür: oluşturmada yönetici bypass'ı kalır, güncelleme, silme ve
non_fast_forward bypass'sız olur; main kural seti `release/v*` dallarını da
kapsar); "Immutable releases" S53 ile açılır
([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).

Güncelleme (2026-10-30, S52 #63): yeşil CI kapısı
(`scripts/release-gate.sh`), CI'da hava boşluklu aynalama, Release
bağlantısı ve Chart.yaml `home`/`sources` geldi (CONTRIBUTING "Yayım iş
akışı"). Sprint 3'ün iki bulgusu, v0.46.0 etiketinde doğrulanınca kapanır.
Bilerek böyle bırakılanlar:

- **Zorunlu check'ler betikte.** Kapı beş check'i kendi listesinden okur,
  kural setinden değil; kural seti değişirse liste ve CONTRIBUTING de
  değişir. Tetikleyici: kural setine yeni bir check girmesi ya da listenin
  kural setinden ayrışması.
- **Ağ kesintisi bir vekille.** Ağsız doğrulamada bütün bağlantıları
  reddeden bir vekil ağın yerini tutar; Go istemcileri ona uyar ve
  localhost'taki aynaya doğrudan gider. Ayrı bir ağ ad alanı ya da
  `--internal` Docker ağıyla gerçek yalıtım yok. Tetikleyici: cosign'ın vekil
  ayarını atlayan bir bağlantı kurması ya da bir müşterinin gerçek yalıtım
  istemesi.
- **API hatasında kapı hemen düşer.** Geçici bir GitHub API hatası yeniden
  denenmez; "Re-run failed jobs" yeter. Tetikleyici: bir sürümün bu yüzden
  birden çok kez yeniden koşturulması.
- **Registry imajı ve ORAS.** `registry:2.8.3` etiketle, ORAS sürüm ve
  sağlama toplamıyla sabit; registry imajını digest'e S54 çevirir (sprint 5).

"Kuru denemede etiket denetimi", `make bump`, `--version`'sız kurulum, ayrı
`image.digest`, tarama bulgusu için otomatik issue ve `cosign sign
--recursive` değişmedi.

## Öz-sağlık kurallarının ve kuyruk uyarısının kalanları

2026-10-29'da ertelendi (#50; SRE incelemesinin düşük bulguları ve
developer raporu). 0.45'ten beri `KubeatUnhealthy` heartbeat olmadan da
yanar, `metrics.prometheusRule.clusterLabel` kuralları tek depoda cluster
başına değerlendirir ve etkin monitor sayısı bildirim kuyruğunu aşınca
backend WARN yazar. Dışarıda kalanlar:

- **Seyrek kurulumda pencere.** Kontrol aralıkları
  `UPTIME_HEARTBEAT_INTERVAL`'dan uzun monitor'leri olan bir kurulumda
  `kubeat_self_healthy` yalnızca sonuç gelen pencerelerde 0 olur; README
  bunu yalnızca heartbeat için söylüyor. Heartbeat Secret'ı yokken pencere
  `backend.extraEnv` ile `UPTIME_HEARTBEAT_INTERVAL` verilerek ayarlanır.
  README "Heartbeat olmadan" bölümüne ve values.yaml'a birer cümle gerekir;
  inceleyici S30 belgesiyle (sprint 4 doküman paketi) önerdi.
- **Bütün cluster'lar birlikte kaybolunca ek `KubeatDown`.** `clusterLabel`
  verilmiş bir kurulumda bütün cluster'ların serileri birlikte kaybolursa
  `absent` cluster etiketsiz bir `KubeatDown` daha açar. S30 belgesinde
  sınır cümlesi olarak yazılabilir.
- **HA çiftinde kopya başına alarm.** Toplama yapmayan kurallar
  tekilleştirilmeyen bir HA deposunda kopya başına yanar. "Alertmanager'a
  doğrudan kanal" maddesinin S37 notunda; değişmedi.
- **Kuyruk uyarısının tekrarı ve doluluk metriği.** WARN süreç başına bir
  kez yazılır; sayı düşüp yeniden aşınca tekrarlanmaz. Kuyruk doluluğu için
  metrik yok ("S25'in incelemesinde kalan bildirim köşeleri" 3. köşe).
- **`clusterLabel`'ı kendiliğinden bulmak.** Etiket adı elle verilir;
  cluster'dan kendiliğinden bulunmaz.
- **`Healthy` ve takılı worker'lar** S22 ile birlikte değerlendirilir (o
  maddenin tetikleyicisi).

Tetikleyici: ilk iki parça için S30'un belgesi. Ötekiler için tek depoda bu
durumlardan birini gören ilk kurulum ya da kuyrukta düşen bildirim
(`kubeat_notifications_dropped_total`).

Güncelleme (2026-10-29, sprint 4 planı): ilk iki parça (seyrek kurulumda
pencere, bütün cluster'lar birlikte kaybolunca ek `KubeatDown`) S30 ile
sprint 4 doküman paketinde (#60) yazılır. Kuyruk doluluğu metriği S55 (#62)
ile gelir; uyarının tekrarı bu maddede kalır. Kapandı notları merge'de
yazılır ([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).

Güncelleme (2026-11-04, #60): ilk iki parça kapandı. Seyrek kurulumda
pencere cümlesi README "Heartbeat olmadan"ın gauge maddesinde (pencereyi en
kısa kontrol aralığından kısa tutmayın; heartbeat Secret'ı yokken
`heartbeat.interval` uygulanmaz, pencere `backend.extraEnv` ile
`UPTIME_HEARTBEAT_INTERVAL`) ve values.yaml'ın heartbeat yorumunda; bütün
cluster'lar birlikte kaybolunca cluster etiketsiz ek `KubeatDown` sınırı
README "clusterLabel" bölümünün "Yokluk penceresi" maddesinde ve values.yaml
`clusterLabel` yorumunda. Kalan parçalar (HA çiftinde kopya başına alarm,
uyarının tekrarı, `clusterLabel`'ı kendiliğinden bulmak, `Healthy` ve takılı
worker'lar) bu maddede.

Güncelleme (2026-10-30, S55, #62): doluluk metriği kapandı:
`kubeat_notification_queue_length` teslimat kuyruğunda o an bekleyen
bildirim sayısını her scrape'te verir. Kuyruk uyarısının tekrarı bu
maddede kalır.

Güncelleme (2026-11-04, sprint 4 kapanışı): Müşteri A (hoş olur) gauge'un
anlık örnek olduğunu yazdı: 30 sn'lik scrape aralığında saniyeler süren
doluluk görünmez, sandbox provasında üç kesinti ve iki ölü kanalla bile 0
kaldı. İstenen: scrape'ler arası en yüksek doluluk (scrape'te sıfırlanan
`kubeat_notification_queue_length_max`) ya da kuyruğun dolu kaldığı
sürenin sayacı; mevcut gauge kalır, chart kuralı gerekmez (`metrics.go`
`WatchQueue`). Sprint 5'te S27 paketine sığarsa onunla, PO kararı; yoksa
bu maddede, tetikleyici aynı ([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md), "Müşterilerin yeni bulguları" 2).

## Hiçbir kanala ulaşamayan kesintide heartbeat'i kesmek

2026-10-29'da ertelendi (S56'nın kod kısmı,
[sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)). Müşteri
B (önemli): tek Slack kanalı bildirim alamaz olunca heartbeat'li kurulum
bunu duymuyor; kurulum notu heartbeat'i kurallarla eşdeğer gösteriyor
("Turn on either", `NOTES.txt:136-144`). Heartbeat'in kararı
(`scheduler.Healthy`, `scheduler.go:115-138`) yalnızca tick'e, takılı yazıma
ve yazılamayan sonuca bakar; heartbeat ve `kubeat_self_healthy` aynı kararı
kullanır (`main.go:265-271`). Belge kısmı sprint 4 doküman paketinde (#60):
heartbeat bildirim alamayan kanalı duymaz, tek kanallı kurulumda ikinci
türde bir kanal ya da `KubeatNotificationsFailing` gerekir. Kod, S38'in
"`Healthy` genişletilmez" kararını ve heartbeat ile `kubeat_self_healthy`'nin
aynı anlamı taşıması sözünü açar. İhtiyacı, S30'un chart'la yükselen Alloy
yoluyla taşınan kanal başına `KubeatNotificationsFailing` cluster-native
olarak karşılıyor. Tetikleyici: kuralları taşımamış, heartbeat'li bir
kurulumun tek kanalı ölüp kesintiyi kaçırması ya da S30 yolunun
operator'süz ortamda uygulanamaz çıkması. BA varsayılanları kayıtta:
yalnızca heartbeat kesilir, gauge ve `KubeatUnhealthy` değişmez; T =
max(`UPTIME_HEARTBEAT_INTERVAL`, 5 dk), `KubeatNotificationsFailing`'in
`for: 5m`'iyle aynı, ayar değil; kapsayan kanalı olmayan monitor, gecikmesi
dolmamış gecikmeli kanal, bakımda susturulan kesinti ve kapalı kanal
sayılmaz; kanallardan biri aldıysa sağlıklı.

Güncelleme (2026-11-04, #60): belge kısmı girdi. README "Heartbeat" ve
"Hangi arızayı ne duyurur" heartbeat'in bildirim alamayan kanalı
duymadığını ve tek kanallı kurulumda farklı türde ikinci bir kanal ya da
`KubeatNotificationsFailing` gerektiğini söyler; kurulum notu heartbeat'i
kurallarla eşdeğer göstermez (kurallar kapalıyken aynı cümleyi yazar,
`test-chart.sh` denetler). Kod kısmı bu maddede.

## Şifreleme anahtarını döndürmek ve anahtar sızınca izlenecek yol

2026-10-29'da ertelendi (S58,
[sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)). Müşteri
A (önemli): `UPTIME_SECRET_KEY` döndürülemiyor (README "Gizli değerlerin
şifrelenmesi": "Anahtar değiştirme (rotation) yoktur", README:877) ve
anahtar sızınca izlenecek yol yazılı değil; kurumun anahtar yönetimi
standardı anahtarı belli aralıklarla ve sızmadan sonra döndürmeyi istiyor.
Var olan yeniden şifreleme yolu (`protectSecrets`, `main.go:468-528`: düz
değerleri şifreler ve sıkıştırır) kullanılabilir, şema değişmez. Durum
sayfası ziyaretçi token'ları anahtardan türer (`main.go:801-805`):
döndürmede parola korumalı sayfaların ziyaretçileri parolayı yeniden girer.
Ortada gerçek bir kurumsal güvenlik incelemesi yok; S5 (oturum boşta kalma)
ile aynı tetikleyici sınıfında. Tetikleyici: ilk kurumsal güvenlik
incelemesi ya da pilotta "devam" kararıyla filoya yaygınlaştırma. O zaman
belge ("Anahtar sızarsa": yalnızca anahtar sızdıysa döndürmek; veritabanı ya
da yedek de sızdıysa kanal ve monitor sırlarını kaynağında yenilemek;
yedekler eski anahtarla kalır) ve kod birlikte gelir: önceki anahtar ayrı
bir ortam değişkeniyle verilir, backend açılışta yeniden şifreler, kesilen
açılış veriyi iki anahtar arasında okunamaz bırakmaz; chart'ta ikinci
`existingSecret`. Kapsam dışı: çoklu anahtar halkası, push token'larını
şifrelemek, yedekleri yeniden şifrelemek, KMS/Vault entegrasyonu. S39'un
anahtar yenileme bölümü (sprint 5) buna bağlanır. Bilinen sınır yol
haritasında.

## Route'ta reencrypt ve arayüz ile backend arasında cluster içi TLS

2026-10-29'da ertelendi (S59'un kod kısmı,
[sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)). Müşteri
A (önemli): güvenlik incelemesinde uçtan uca şifreli taşıma gösterilmeli,
ama AD parolası cluster içinde düz HTTP'den geçiyor. Route'ta
`termination: edge` sabit (`templates/expose.yaml:24-26`), values'ta Route
TLS alanı yok; frontend backend'e `http://` ile gider
(`templates/frontend.yaml:38-39`) ve kendisi düz HTTP sunar. OVN-Kubernetes
IPsec yoksa pod ağı şifresizdir; bu platformun kararı. Kod L ve iki bileşene
TLS ekler (şişirme riski orta). Sprint 4'te yalnızca bilinen sınır cümlesi
(README "Dışarıya açma", #60) ve yol haritası satırı girer. Tetikleyici: bir
güvenlik incelemesinin IPsec'siz cluster'da düz metin parola yolunu bulgu
olarak yazması. Karar taslağı (BA): `route.tls.termination` (`edge`
varsayılan, `reencrypt`); reencrypt'te frontend ve backend OpenShift service
CA sertifikasıyla TLS sunar, Route `destinationCACertificate`'i taşır,
frontend backend'e https ile gider; edge'de render değişmez; pilot.md "Ön
koşullar"a güvenlik satırı (IPsec var mı). Kapsam dışı: Ingress'te uçtan uca
TLS, mTLS, cert-manager ile pod sertifikası.

Güncelleme (2026-11-04, #60): sınır cümlesi README "Dışarıya açma"da (TLS
router'da ya da Ingress controller'da biter, pod ağında düz HTTP, LDAP
parolası bu yoldan geçer, OVN-Kubernetes IPsec node'lar arası pod trafiğini
şifreler); docs/pilot.md güvenlik satırı ve kod bu maddede.

## E-posta konusunda kurulum kimliği

2026-10-29'da ertelendi (S28'in kapsam dışı parçası, sprint 4 PO kararı,
[sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)). S28
(#61) kurulum kimliğini (`UPTIME_INSTANCE_NAME`, boşsa `UPTIME_PUBLIC_URL`'ün
host'u) PagerDuty `dedup_key`'ine, `custom_details`'e ve webhook gövdesine
(`instance`), Slack ve Mattermost alt bilgisine, Teams ve Google Chat
kartına ve e-posta gövdesine koyar. E-posta konusu (`Subject()`,
`router.go:139`; `email.go:111`) bilerek değişmez: konuya göre kurulmuş
posta kuralları kırılmasın. Bu, BA varsayılanından (konunun başında
`[<kimlik>]`) bilinçli bir sapma. Tetikleyici: birden çok cluster'ı
e-postayla izleyen bir kullanıcının kurulumları konudan ayırt etme isteği.

## PagerDuty dedup anahtarını olay kaydında saklamak

2026-10-29'da ertelendi (S28'in kapsam dışı parçası, SRE'nin 1. koşulu;
sprint 4 PO kararı "anahtar kayda yazılmaz, migration yok",
[sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md); #61'in
incelemesiyle 2026-11-04'te kayda alındı). S28 (#61) `dedup_key`'i her
gönderimde kimlikten kurar; kimlik ya da `publicURL` değişince ya da
kimliksiz bir sürüme dönülünce eski anahtarla açık alarm Kubeat'ten
kapanmaz, README **Kurulum kimliği** > "Kimliği değiştirmek" açık olayları
önceden kapatmayı söyler. Anahtar olay kaydına (yavaşlık için monitor'e)
yazılırsa düzelme alarm hangi anahtarla açıldıysa onunla gider; migration
ister (S6). Tetikleyici: kimlik ya da `publicURL` değişikliğinden ya da
geri dönüşten sonra Kubeat'ten kapanmayan bir PagerDuty alarmı.

## PagerDuty özetinde ve öteki sohbet kanallarında kurulum kimliği

2026-10-29'da ertelendi (S28'in kapsam dışı parçası, sprint 4 PO kararı,
[sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md); #61'in
incelemesiyle 2026-11-04'te kayda alındı). S28 (#61) kimliği PagerDuty'de
yalnızca `dedup_key`'e ve `custom_details`'e koyar, `summary` değişmez;
sohbet kanallarında yalnızca Slack ve Mattermost alt bilgisine, Teams ve
Google Chat kart alanına girer; mesaj gövdeleri ile Telegram, Discord,
ntfy ve Gotify mesajları değişmez. Tetikleyici: birden çok cluster'ı
PagerDuty özetinden ya da bu dört kanaldan izleyen bir kullanıcının
kurulumları ayırt etme isteği.

## README'de kalan eski yükseltme notu paragrafları ve sürüm karşılaştırma cümleleri

2026-11-04'te ertelendi (sprint 4 kapanışı; #59 ve #62 developer
raporları, [sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)). #59 0.38–0.45 sürüm bölümlerini
`docs/yukseltme.md`'ye taşıdı; README'nin bölümleri içinde 0.37/0.38
döneminden kalan "**Yükseltme notu:**" paragrafları (SSO, oturum çerezi,
metrik anahtarı, Ingress TLS, header'lar, Thanos 9091) yerinde bırakıldı.
#62'nin "Reddetti" maddesindeki iki "0.44'te …" cümlesi ve "Alarm"daki
"0.45'te …" karşılaştırmaları da README'de; tamamlanma tanımına göre
README bugünkü davranışı anlatır, karşılaştırma `docs/yukseltme.md`'nin
işi. S boyut, yalnız belge. Tetikleyici: README'yi yeniden düzenleyen bir
sonraki belge maddesi (S41'in runbook'u ya da S12'nin devamı) ya da bir
okuyucunun eski sürüm cümlesine takılması.

## İzlenen namespace'lerin NetworkPolicy'sini chart'tan yönetmek ve keşifte izin ipucu

2026-11-04'te ertelendi (S51'in kapsam dışı parçaları, sprint 4 PO
kararı; #60, [sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)). #60 varsayılan-ret politikalı namespace için
kopyalanabilir `allow-from-kubeat` politikasını, proje şablonunu ve
AdminNetworkPolicy örneğini belgeledi. Dışarıda kalanlar: chart'ın izlenen
namespace'lere NetworkPolicy basması (başka namespace'e yazmak chart'ın
yetki modelini genişletir), hedef namespace'in politikalarını okuyup
"Kubeat'e izin yok" diye uyarmak, chart'ın kendi egress politikasını
üretmesi ve keşifte `connect_timeout` veren monitor için politika ipucu.
Tetikleyici: belge yoluna rağmen pilotta ya da filoya yaygınlaştırmada
varsayılan-ret namespace'te ilk gün yanlış kesinti, ya da "Kullanıcıda
kalanlar" provasının (devredilen 3) belgeyi yetersiz bulması.

## Alloy'un `prometheus.operator.servicemonitors` bileşeniyle chart'ın ServiceMonitor'ünü okutmak

2026-11-04'te ertelendi (#60 developer raporu, backlog adayı; [sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).
README'nin operator'süz yolu kuralları `mimir.rules.kubernetes` ile
taşıyor, metrikleri ise yalnız elle scrape'le (`job_name kubeat-backend`,
metrik anahtarı, `backendIngressFrom`) anlatıyor; Alloy'un
`prometheus.operator.servicemonitors` bileşeni chart'ın ServiceMonitor'ünü
doğrudan okuyabilir ama `job` ve `namespace` etiket semantiği resmî
belgeden doğrulanamadı ve kuralların beklediği etiketlerle eşleşmesi
görülmedi. Tetikleyici: Alloy provası (devredilen 1) sırasında elle
scrape'in etiketlerinin kurallarla tutmaması ya da Müşteri B'nin
ServiceMonitor yolunu istemesi.

## Kurulum kimliği anahtarının kalanları: Latin dışı harfler ve test bildirimi

2026-11-04'te ertelendi (#61 developer raporu, küçük; [sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).
`dedupInstance` Latin-1 Supplement ve Latin Extended-A harflerini
bağımlılıksız bir tabloyla ASCII'ye çevirir; Latin Extended-B (ör.
Vietnamca Ơ/Ư), Kiril ve CJK harfleri anahtarda tireye döner ve açılışta
WARN verir. Unicode normalizasyonuyla genişletme `golang.org/x/text`
ister (go.mod'da yalnız dolaylı). Test bildiriminin `kubeat-test`
anahtarı kimlik taşımaz (kabul ölçütünde yoktu). Tetikleyici: Latin dışı
bir kurulum adı ya da ortak PagerDuty servisinde test bildirimlerinin
kurulumlar arasında çakışması.

## Kullanıcı başına hedef kısıtı (SSRF/keşif) — yapıldı

2026-11-04'te ertelenmişti ([adr/0003-hedef-erisilebilirligi.md](adr/0003-hedef-erisilebilirligi.md), karar c; yetkili güvenlik değerlendirmesi); a (deny-CIDR
belgesi) ve b (`UPTIME_CHECK_SHOW_VALUE`) o turda yapıldı. **Sprint 5'te (0.47.0,
[#84](https://github.com/yusufceng/Kubeat/issues/84)) tamamlandı:** kapsamı
sınırlı bir kullanıcı cluster içi bir hedefe (monitor ya da kanal webhook'u)
yalnızca adından okunan namespace'i (`servis.namespace.svc`) kapsamındaysa
ulaşabilir; API sunucusu, başka ekiplerin Service'leri ve namespace'i
okunamayan çıplak ClusterIP/pod IP'leri reddedilir. Her namespace'i gören
kullanıcı ve cluster dışı hedefler değişmez (ADR-0003 c, README "Hedef
politikası", [sprints/2026-11-04-sprint-05.md](sprints/2026-11-04-sprint-05.md)).
Deny-CIDR derinlemesine savunma olarak kalır (cross-tenant'ı tek başına
ayıramaz, çünkü ClusterIP aralığı paylaşılır).

## Sınırlı kullanıcının çıplak ClusterIP ile cluster içi hedef izlemesi

2026-11-04'te bilerek kapatıldı (S64, [#84](https://github.com/yusufceng/Kubeat/issues/84)):
kapsamı sınırlı bir kullanıcı, adından namespace'i okunamayan bir cluster içi
hedefe (çıplak ClusterIP, pod IP'si) monitor ya da kanal webhook'u kuramaz,
çünkü kapsam denetimi hedefi bir namespace'e bağlayamaz. Gereksinim doğarsa
ileride sınırlı kullanıcı için de servis adıyla (ClusterIP yerine) izleme
açılabilir. Tetikleyici: çıplak ClusterIP ile cluster içi hedef izlemesi gereken
sınırlı bir kullanıcının isteği. (İlgili ayrı konu — iki etiketli kısa ad
`servis.namespace` kapsam denetimini atlıyordu, S64 takibi — 0.48.0'da çözüm
anında (dial-time) kapsam denetimiyle **kapatıldı**: S67,
[#95](https://github.com/yusufceng/Kubeat/issues/95),
[sprints/2026-11-05-sprint-06.md](sprints/2026-11-05-sprint-06.md); ADR-0003 c.)

## Giriş kilitlemesinin hizmet reddi riski (CWE-307)

2026-11-04'te ertelendi (yetkili güvenlik değerlendirmesi). Giriş kilitlemesi
kullanıcı başına (10 deneme / 15 dk); bir saldırgan geçerli bir kullanıcı adını
bildiği hesabı ~15 istekle kilitleyerek meşru sahibini (yönetici dahil) dışarıda
bırakabiliyor; tam da izlemeye en çok ihtiyaç duyulan anda. Düzeltme kaynak IP
başına kademeli throttle'a geçmek ve hesabı sert kilitlememektir; bu, auth'ta
güvenlik tavizi olan bir tasarım değişikliğidir (IP throttle dağıtık tahmine
daha zayıf, ama meşru kullanıcıyı kilitlemez) ve aceleye getirilmemeli. Bugünkü
risk sınırlı ve kendiliğinden geçen 15 dakikalık bir kilit.

Güncelleme (2026-11-05, sprint 6 planı; [sprints/2026-11-05-sprint-06.md](sprints/2026-11-05-sprint-06.md)):
sprint 6'ya **alındı** (S65, [#98](https://github.com/yusufceng/Kubeat/issues/98)),
gerçek bir DoS açığı olduğu ve aynı sprintte public GKE Ingress belgelenirken
(S3+S60) yöneticiyi dışarıda bırakan kilidi açık tutmak tutarsız olduğu için.
S63 (dizin erişilebilirlik metriği) aynı `auth.go`/Throttle yüzeyine dokunduğu
için S65'ten ayrıldı ve sprint 7'ye bırakıldı (rebase churn'ünü önlemek ve S63'ün
ara sinyali zaten var olduğu için). Bir ADR ile karar verilir (hesap-başına sert
kilitten IP-başına throttle'a geçiş, taviz ve XFF/vekil güveni). Ara önlem:
güçlü parolalar ve `login_failed` denetim kaydının izlenmesi.

Güncelleme (2026-11-05, 0.48): **çözüldü.** Kilit kaynak-adres bazlı throttle'a
döndü (birincil kaynak-başına + gevşek hesap tavanı `UPTIME_LOGIN_ACCOUNT_MAX_FAILURES`,
kaynak `UPTIME_TRUST_PROXY_HOPS` ile), karar [ADR-0004](adr/0004-giris-kilitleme-stratejisi.md).
Kalan sınırlar (ingress arkasında vekil-hop ayarı, dağıtık tahminin hesap
tavanını tetikleyebilmesi, tek-replica bellek) yol haritası "Bilinen sınırlar"da.

## Durum sayfası kilidi ve parola değiştirme throttle'larının CWE-307 benzeri yüzeyleri

2026-11-05'te ertelendi (S65, [#98](https://github.com/yusufceng/Kubeat/issues/98)
developer kararı). S65 giriş yolunu hesap-başına sert kilitten kaynak-adres bazlı
throttle'a çevirdi, ama durum sayfası kilidini açma (status-page unlock) ve parola
değiştirme akışlarının kendi throttle'ları aynı turda ele alınmadı; yalnız ortak
429 yanıtına `Retry-After` başlığı eklendi (davranış tutarlılığı). Bu yüzeyler de
giriş gibi kaynak yerine yalnız gizli değere göre kilitlenebiliyorsa benzer bir
hizmet reddi riski taşıyabilir. İş gerekirse ayrı bir madde: her yüzeyin
throttle'ını giriş gibi kaynak-adres bazlı yapmak ve ADR-0004 kararını bu yüzeylere
genişletmek. Tetikleyici: bu yüzeylerden birinde giriş-kilidi benzeri bir DoS
gözlemi ya da bir güvenlik incelemesi; herhangi bir `throttle.go`/`auth.go`
dokunuşuna katlanabilir.

Güncelleme (2026-11-06, sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
S76 olarak **yeniden açıldı** ve sprint 9 kuyruğuna girdi (sprint 7'de geri
çevrilmişti). Tetikleyicisi ("herhangi bir `throttle.go`/`auth.go` dokunuşu")
S78 ([#118](https://github.com/yusufceng/Kubeat/issues/118)) ile karşılanıyor.
Sprint 8 keşfi durum sayfası kilidinin ve parola değiştirmenin de arayüzden
`X-Forwarded-For`'suz geldiğini gösterdi; S78 başlığı bu iki çağrıda da
taşıyınca `sp:`/`pw:` anahtarlarına kaynak eklemek ilk kez işe yarar (S78
olmadan etkisiz). Sprint 8'de alınmadı: dörtlü sınır dolu ve aynı auth
yüzeyinde iki L var (S78, S79); ikisi merge edildikten sonra sprint 9'da. Açık
soru varsayılanı değişti: girişteki gibi gevşek bir slug/hesap tavanı olur
(`UPTIME_LOGIN_ACCOUNT_MAX_FAILURES` yeniden kullanılır, yeni ayar yok).

Güncelleme (2026-11-07, sprint 8 kapanışı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
0.50'de arayüz başlığı parola değiştirme ve durum sayfası kilidinde de taşıyor
(S78, [#118](https://github.com/yusufceng/Kubeat/issues/118)), backend `pw:`/`sp:` sayaçlarında onu henüz kullanmıyor. Sprint 8
provası durum sayfası kilidinin gerçek yolda sayfaya göre kaldığını gösterdi:
öteki kaynaktan girilen doğru parola da kilitli kaldı. Sprint 9 kuyruğunda.

Güncelleme (2026-11-07, sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
sprint 9'a **alındı** (S76, [#134](https://github.com/yusufceng/Kubeat/issues/134)). Aynı maddeye slot almadan iki belge
parçası katıldı: S75 (README "Durum sayfaları"nda "Sınırlar") ve S88'in belge
kısmı (README "LDAP" ve "Deneme sınırı"nda dizin kilidi etkileşimi, ADR-0004
notu). Kaynak başına, adlardan bağımsız giriş tavanı (S88 kod) sprint 10
kuyruğunda; sıra S76 → S88 → S85.

Güncelleme (2026-11-07, S76 [#134](https://github.com/yusufceng/Kubeat/issues/134), 0.51.0): **çözüldü.** Durum sayfası
kilidi ve parola değiştirme kaynak + kapsam anahtarına (`sp:`/`pw:`) geçti, gevşek
tavan girişinkiyle aynı değerde kapsam başına (`UPTIME_LOGIN_ACCOUNT_MAX_FAILURES`,
yeni ayar yok); karar ADR-0004 "Sonuçlar"da. Kalanlar: bu iki yüzeyin 429'ları
`kubeat_signin_throttled_total`'a girmez (yalnız throttle'ın dakikalık WARN'ı;
S85 kapsamında, sprint 10 adayı, sıra S76 → S88 → S85), gerçek tarayıcı yolunda
iki kaynaklı prova sürümden sonra (sandbox Route). IPv6 kaynakları ayrı maddede
("Throttle'da IPv6 kaynaklarını /64'e toplamak").

Güncelleme (2026-11-07, sprint 9 kapanışı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
iki kaynaklı prova sandbox Route'unda (0.51.0, hops=1) geçti: A kaynağı 10 yanlıştan
sonra kilitlendi, B doğru parolayla sayfayı açtı ve parolayı değiştirdi; ret satırları
A'nın adresini yazdı. Kalanlar değişmedi: 429'ların sayacı S85'te, kaynak başına
adlardan bağımsız tavan S88'de (ikisi sprint 10 önerisinde).

Güncelleme (2026-11-07, S88 [#145](https://github.com/yusufceng/Kubeat/issues/145), 0.52.0): kaynak başına, adlardan
bağımsız giriş tavanı geldi (`UPTIME_LOGIN_SOURCE_MAX_FAILURES`, varsayılan `0`
kapalı, `kind="source_total"`; ADR-0004 "Not: adlardan bağımsız kaynak tavanı").
Yalnız girişi kapsar; bu maddenin iki yüzeyi kaynak + kapsam sınırıyla kalır.

Güncelleme (2026-11-07, S85 dilim 1 [#146](https://github.com/yusufceng/Kubeat/issues/146), 0.52.0): bu iki yüzeyin
429'ları `kubeat_throttled_total{surface,kind}`'da sayılır (`source` ya da `account`);
`kubeat_signin_throttled_total`'ın anlamı değişmedi.

## Throttle'da IPv6 kaynaklarını /64'e toplamak

2026-11-07'de ertelendi (S76 incelemesi, [#134](https://github.com/yusufceng/Kubeat/issues/134)).
Throttle kaynağı tam adres olarak tutar (`/128`): tek bir `/64`'ü olan biri her
adresten ayrı deneme hakkı alır; girişte, parola değiştirmede ve durum sayfasında
onu yalnız gevşek tavan sınırlar (ADR-0004 "Maliyetler ve riskler"). Seçenekler:
IPv6 kaynağını `/64`'e toplamak (aynı `/64`'ü paylaşan kullanıcıları birleştirir)
ya da kaynak ve `/64` için iki katlı anahtar. Tetikleyici: IPv6'lı ilk Route ya da
Ingress.

Güncelleme (2026-11-07, S88 [#145](https://github.com/yusufceng/Kubeat/issues/145), 0.52.0): 0.52'nin kaynak tavanı
(`UPTIME_LOGIN_SOURCE_MAX_FAILURES`) da tam adrese anahtarlanır; tek bir `/64`'ten
yapılan sprey her adreste ayrı tavan alır. Toplama yapılırsa bu tavana da uygulanmalı.
Tetikleyici değişmedi.

## Çok kiracılı ve internete açık kurulum için tek sertleştirme kontrol listesi (S66 kalanı)

2026-11-04'te ertelendi (sprint 5 planı; [sprints/2026-11-04-sprint-05.md](sprints/2026-11-04-sprint-05.md)).
S66'nın yük taşıyan parçası (deny-CIDR cross-tenant'ı ayıramaz, çünkü API
sunucusu ve ekip Service'leri aynı ClusterIP aralığında; gerçek savunma c)
S64'ün belge kabul ölçütüne girdi (ADR-0003 "Sonuçlar" + README "Hedef
politikası"), bu yüzden bu sprint kapanıyor. Kalan konsolidasyon düşük
aciliyetli belge işi: README "Güvenlik"ten bağlı tek "Sıkı / çok kiracılı ve
internete açık kurulum" kontrol listesi (deny-CIDR, `UPTIME_CHECK_SHOW_VALUE=false`,
giriş yüzeyini kısıtlama ve giriş kilidinin bugünkü sınırı), pilot.md "Ön
koşullar"a deny-CIDR + `UPTIME_CHECK_SHOW_VALUE=false` satırı ve chart values
sertleştirme bloğunun örneklenmesi. 0.46.0 için geçici "bilinen-açık" satırı
yazılmaz: 0.46.1 + advisory kullanıcıda bekliyor (2026-11-07 kullanıcı kararıyla
düştü) ve 0.47.0 fixleri zaten taşıyacak. Tetikleyici: ilk gerçek internete
açık (GKE Ingress) çok kiracılı kurulum ya da bir güvenlik incelemesi.

Güncelleme (2026-11-05, sprint 6 planı; [sprints/2026-11-05-sprint-06.md](sprints/2026-11-05-sprint-06.md)):
tetikleyici büyük ölçüde karşılandı — Müşteri A'nın güvenlik ekibi incelemesi
ve 30 cluster / 60 ekip yaygınlaştırma kararı güvenlik incelemesi bağlamını
sağladı. Yine de liste S67 (iki etiketli ad + dial-time kapsam, #95) kapsam
sınırı metnini değiştireceği için **S67'den sonra** yazılmalı; kapasite gereği
bir sprint kaydı. **Güçlü sprint-7 adayı**, S67 merge sonrası alınır.

Güncelleme (2026-11-06, sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
sprint kuyruğundan backlog'a alındı; bağımlılık S78
([#118](https://github.com/yusufceng/Kubeat/issues/118)) ve S80 oldu. Üç rol de
listenin onlardan sonra yazılmasını istiyor: bugün yazılırsa arayüz yolunda
geçersiz olan hop satırlarını ve README "Hedef politikası"ndaki kurum ağı
cümlesini kalıcılaştırır. Liste, S78 ile S80'in merge edildiği sprintin en son
merge'ü olarak yazılır; hops>0 ise ağ politikası satırını (ve S79 teslim
edildiyse güvenilir eş ayarını, [#119](https://github.com/yusufceng/Kubeat/issues/119))
içerir. Tetikleyici: S78 ile S80'in merge edilmesi.

Güncelleme (2026-11-07, sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
tetikleyici genişledi: S80 ([#132](https://github.com/yusufceng/Kubeat/issues/132)), S86 ([#131](https://github.com/yusufceng/Kubeat/issues/131)), S87 ([#133](https://github.com/yusufceng/Kubeat/issues/133)) ve S88'in
kod kısmının merge edilmesi. Listenin yeni satırları bunlara bağlı: kurum
aralıkları (S80), kapsamın her bağlantıda uygulanması (S86, teslim edilmediyse
bilinen sınır olarak), doğru hop değeri ve negatif doğrulama adımı, host ağındaki
router ve GKE satırları (S87), LDAP/AD kilidi (S88). Sprint 11'in en son merge'ü
olarak yazılır.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
tetikleyici doldu. S66 sprint 11'de [#164](https://github.com/yusufceng/Kubeat/issues/164)
olarak, sprintin en son merge'ü. Kapsamı:

- Müşteri B'nin tek kiracılı kısa yolu;
- çok kiracılı ve internete açık kurulum listesi;
- denetim kaydını API'den çekme tarifi (S83'ün kalanı backlog'da kalır);
- `docs/yukseltme.md` 0.51'deki SSO çaresi;
- curl digest'i.

Çok kiracılı metni `docs/` altına taşımak 1.0 kararına bırakıldı (PO). Merge edilince bu
madde kapanır.

Kapandı (2026-11-08, S66 [#164](https://github.com/yusufceng/Kubeat/issues/164), 0.53):

- kontrol listesi README "Güvenlik"in başında ("Çok kiracılı ve internete açık kurulum");
  her satır bir ayar ya da adım ve bir bölüm bağlantısı, genel dille;
- tek kiracılı kısa yol hızlı başlangıcın altındaki "Küçük ekip, tek kiracılı kurulum"
  kutusunda;
- denetim kaydını API'den çekme tarifi README "Hesaplar ve giriş" → "Denetim kaydını
  SIEM'e almak"ta (S83'ün kalanı ve `since` parametresi kendi maddesinde);
- SSO çaresi `docs/yukseltme.md` "0.51'e yükseltme" → "Annotation monitor'leri her
  kurulumda"da.

Kalanlar, tetikleyicileriyle:

- Çok kiracılı metni `docs/` altına taşımak. README denetimleri bölüm adlarına bağlı.
  Tetikleyici: 1.0 kararı.
- 2026-11-04 kapsamındaki `docs/pilot.md` "Ön koşullar" satırı ve chart values'ta
  sertleştirme bloğu yapılmadı. values.yaml'ın `backend.extraEnv` yorumu
  `UPTIME_CHECK_SHOW_VALUE` ve `UPTIME_TARGET_DENY_CIDRS`'i zaten örnekliyor; pilot'un
  values bloğunu bu sprintte S98 ([#162](https://github.com/yusufceng/Kubeat/issues/162))
  değiştiriyor ve liste README'de tek yerde. Tetikleyici: pilot values'unda sertleştirme
  ayarını isteyen bir prova ya da kurulum.

## Kapsam/yetki dışı hedef reddini denetim kaydına ve metriğe yazmak (S68)

2026-11-05'te sprint 7 kuyruğuna alındı (sprint 6 planı; [sprints/2026-11-05-sprint-06.md](sprints/2026-11-05-sprint-06.md)).
SRE orta + Müşteri A önemli: `targetInScope` reddi (`monitors.go:738,806,990` ve
`alerting.go:450,550`) yalnız 403 ile `writeError`'dan çıkıyor; `h.audit` yalnız
başarıda çağrılıyor ve `metrics.go`'da kapsam/forbidden sayacı yok. Müşteri A:
"Bir ekibin başka ekibin Service'ine ya da API sunucusuna monitor/kanal kurma
GİRİŞİMİNİ 90 günlük denetim kaydında görmek ve SOC'a alarm bağlamak istiyorum."
`login_failed` denetleniyorken bu denetlenmiyor. İş: kapsam dışı hedef reddi (ve
ilgili CanEdit reddi) bir denetim satırı (`deny`/`target-scope`) üretir ve reason
etiketli bir sayaç (`kubeat_target_scope_denied_total{reason}`) eklenir; istemci
yanıtı yine 403. Gözlemlenebilirlik, savunmanın kendisi değil. Tetikleyici:
S67 merge sonrası (`targetscope.go`'ya S67 ağır dokunuyor ve iki etiketli ad S67
kapanana kadar denetime hiç girmiyor, metrik ancak S67'den sonra tam anlamlı);
sprint 7'de S67 yerleşince alınır.

## 0.47.0'ın güvenlik sürümü olduğunu ve eski minor'ların açık kaldığını belgelemek (S69)

2026-11-05'te sprint 7 kuyruğuna alındı (sprint 6 planı; [sprints/2026-11-05-sprint-06.md](sprints/2026-11-05-sprint-06.md)).
Ece yüksek, S boyutlu pure docs: "0.46.0'daki bir operatör olarak hangi
yükseltmenin güvenlik için acil olduğunu README/SECURITY/yükseltme notundan
anlamak istiyorum." 0.47.0 #77-81 Strix düzeltmelerini (palette sızıntısı, giriş
enum, iç IP maskesi, SSRF deny-CIDR) kamuya açıyor ama hiçbir belge 0.47.0'ı
güvenlik sürümü olarak adlandırmıyor (SECURITY.md genel "son minor'a yükseltin"
diyor, `docs/yukseltme.md` "0.47'ye yükseltme" güvenlik düzeltmesinden söz etmiyor).
İş: `docs/yukseltme.md` ve SECURITY.md'ye 0.47.0'ın güvenlik düzeltmelerini kamuya
açtığı, eski minor'ların açık olduğu ve 0.46.1'in yalnız 0.46 hattına geri taşıma
olduğu cümleleri. Dörtlü cap ve öncelik kuralı gereği gerçek DoS fix (S65) öne
geçti; 0.46.1 + advisory kullanıcıda (2026-11-07 kullanıcı kararıyla düştü) ve
advisory bunu kısmen duyuracak. Metin advisory çıkmadan da doğru olmalı.
Tetikleyici: sprint 7'de ilk doküman dokunuşuna katlanacak S boyutlu iş.

Güncelleme (2026-11-07, sprint 9 planı; kullanıcı kararı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
kapsam daraldı. 0.47–0.49'u geriye dönük güvenlik sürümü diye adlandırmak ve
0.46.1 cümlesi **yapılmayacak**: satılmış sürüm yok, eski minor'lara geri taşıma
ve advisory düştü. Kalan iki parça slot almadan sprint 9 sürüm PR'ında release
manager yazar, BA okur: SECURITY.md'nin "düzeltme Security Advisory ile
duyurulur" cümlesi (:18-19) kararlı sürüme (1.0) kadar bazı düzeltmelerin
advisory'siz, sürüm notunda ve `docs/yukseltme.md`'de "güvenlik sürümü" adıyla
çıktığını söyler; 0.51.0, S86 ([#131](https://github.com/yusufceng/Kubeat/issues/131)) çıktıysa ayrıntı vermeden "güvenlik
sürümü; namespace'le sınırlı kullanıcısı olan kurulumlar yükseltmeli" diye
adlandırılır.

## Kaldır+yeniden kurulumda kimlik Secret'larının sahiplenilmesini ve rotasyonun etkisizliğini belgelemek (S70)

2026-11-05'te ertelendi (sprint 6 planı; [sprints/2026-11-05-sprint-06.md](sprints/2026-11-05-sprint-06.md)).
SRE/Ece orta, yalnız belge: 0.47.0'da `keepOnUninstall` varsayılan `true`
(`values.yaml:70,287`) kaldırmada admin/API/metrik Secret'larını ve veri PVC'sini
bırakıyor; aynı adla yeniden kurulum eski parola/token'ları sessizce sahipleniyor.
SRE: "Yönetici parolası/token sızarsa kaldırıp yeniden kurarak temiz, döndürülmüş
kimliğe dönmek istiyorum" ama uninstall+install rotasyon sanılıyor, token/parola
değişmiyor; `yol-haritasi.md` "token rotasyonu" yalnız `UPTIME_SECRET_KEY`'i anıyor
(165-167). Ece: temiz sıfırlama beklerken eski kimlik/veri geliyor ve GKE'de iki PD
ücret yazıyor. İş yalnız belge (S39 keep davranışı değişmez, sprint 4 kararı):
README "Güvenlik"/rotasyon ve `docs/yedek-geri-yukleme.md`'ye "kimlik döndürmek
için ilgili Secret'ı elle silip/düzenleyip yeniden oluşturun; uninstall+install
rotasyon değildir", NOTES.txt'e temiz sıfırlama (`keepOnUninstall=false`) cümlesi.
Müşteriyi engellemiyor, aciliyeti düşük. Tetikleyici: gerçek yeniden kurulumda
sahiplenme provası (kullanıcıda) ya da bir sonraki rotasyon/belge turu; herhangi
bir yedek-geri-yukleme/NOTES dokunuşuna katlanabilir.

## Başarısız kontrol hatasında yalnız iç cluster adresini maskelemek, public hedef IP'sini açık bırakmak (S71)

2026-11-05'te ertelendi (sprint 6 planı; [sprints/2026-11-05-sprint-06.md](sprints/2026-11-05-sprint-06.md)).
Ece "hoş olur", Müşteri A ilgisiz: `addrmask.Mask` her IPv4/köşeli-IPv6 adresini
maskeliyor (`addrmask.go:15-22`, ayrım yok), oysa belgelenen amaç yalnız iç cluster
adresini gizlemek (README "Başarısız kontrolün ayrıntıları"); public hedefin
`dial tcp <public-ip>:443` hatası da `[address hidden]` oluyor, güvenlik kazancı
yok. Ece: "GKE'den kendi public SaaS URL'lerimi izliyorum; bir kontrol düşünce
bildirimdeki hatada çözülen IP'yi hızlıca görmek istiyorum." İş: maskeleme yalnız
`netguard.InCluster` olan (iç) adreslere uygulanır; public bir hedefin IP'si hata
metninde kalır; ADR-0003 (d) ve README "Başarısız kontrolün ayrıntıları" bugün
"çözülen IPv4/IPv6 adresleri de maskelenir" diyen ayrımsız metni düzeltir.
`diagnostics.address` (ip:port) belgelenmiş tanı alanı olarak açık kalır (ADR-0003 d).
Düşük öncelik, PO kararı; ADR-0003 (d) güncellemesi ister. Tetikleyici: herhangi
bir addrmask/ADR-0003 (d) dokunuşuna katlanabilir; bağımsız slot şişme olur.

## Giriş throttle için güvenilir-vekil IP allowlist (yalnız kaynak-IP'ye dayanmayan ayrım)

2026-11-05'te eklendi (sprint 7 planı; [sprints/2026-11-05-sprint-07.md](sprints/2026-11-05-sprint-07.md)).
S72'nin (giriş throttle vekil/NAT takibi, [#106](https://github.com/yusufceng/Kubeat/issues/106))
açık sorusunun ertelenen parçası: paylaşılan kurumsal NAT/vekil arkasında aynı
kaynak adresini paylaşan kullanıcıları gerçekten ayıran çözüm (güvenilir aralık
allowlist'i ya da kaynak-IP'ye ek bir ayrım) kod + ADR-0004 değişimi ister ve S72
kapasitesine sığmaz. S72 bu turda NAT durumunu ve `UPTIME_LOGIN_ACCOUNT_MAX_FAILURES`
tavanını ADR-0004/README'de açıklar ve yön olarak allowlist'i bırakır; iş buradan
izlenir. Tetikleyici: NAT'lı popülasyonda gözlenmiş kilitlenme ya da ayrı kapasite.

Güncelleme (2026-11-06, sprint 7 kapanışı; [sprints/2026-11-05-sprint-07.md](sprints/2026-11-05-sprint-07.md)):
tetikleyici büyük ölçüde karşılandı. Müşteri A (Deniz) tam o tetikleyici popülasyon:
60 uygulama ekibi tek kurumsal NAT/ileri vekil arkasından giriyor; `hops` doğru
ayarlansa bile herkes tek kaynak adres görünüyor ve bir ekibin hatalı denemeleri
diğerlerini (ve admin hesabını) yavaşlatabiliyor. S72'nin önerdiği tek çare
`UPTIME_LOGIN_ACCOUNT_MAX_FAILURES` tavanını yükseltmek, bu da kilitleme korumasını
gevşetiyor. A (önemli): "Kurumsal NAT'lı popülasyon tetikleyici sayılıp güvenilir-vekil
IP allowlist (ya da kaynak-IP'ye dayanmayan kimlik sinyali) bir sonraki sprinte
planlansın." Güçlü sprint-8 adayı; kod + ADR-0004 değişimi, L.

Güncelleme (2026-11-06, sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
ilk dilimi sprint 8'de S79 ([#119](https://github.com/yusufceng/Kubeat/issues/119))
alıyor: hops>0 iken backend `X-Forwarded-For`'a yalnız arayüzden (chart'ın
ürettiği paylaşılan sır başlığıyla tanınan eşten) gelen istekte güvenir; bu,
ADR-0004'te karar olur. Kaynak-IP'ye dayanmayan NAT ayrımı backlog'da kalır.
Tetikleyici: S79 merge edildikten sonra NAT'lı bir popülasyonda gözlenen
kilitlenme. Cluster dışındaki güvenilir vekiller için CIDR listesi ayrı maddede
("XFF güveni için cluster dışındaki güvenilir vekillerin CIDR listesi").

Güncelleme (2026-11-07, S79 teslimatı; [#119](https://github.com/yusufceng/Kubeat/issues/119)):
ilk dilim yapıldı. hops>0 iken backend `X-Forwarded-For`'u yalnız arayüzün
sırrını (`X-Frontend-Token`; `UPTIME_FRONTEND_TOKEN`/`FRONTEND_TOKEN`, chart
`<release>-frontend-token`) sunan istekte okur; arayüz Service'ine doğrudan
geleni isteğe bağlı `frontend.networkPolicy.enabled` kapatır. ADR-0004 "Güvenilir
eş: arayüz (0.50)". Bu madde artık yalnız kaynak-IP'ye dayanmayan NAT ayrımını
izler; tetikleyici değişmedi.

## Kapsam daralınca var olan monitor/kanal hedeflerini kapsama karşı yeniden denetlemek (S73)

2026-11-05'te sprint 8 kuyruğuna alındı (sprint 7 planı; [sprints/2026-11-05-sprint-07.md](sprints/2026-11-05-sprint-07.md)).
Sprint 6'da "backlog'da izlenir" denen Müşteri A bulgusu (önemli): kapsam yalnız
oluştur/değiştir anında denetleniyor; rol/LDAP grubu/takım değişimiyle kapsamı
daralan kullanıcı eski hedefini (artık göremediği ekibin Service'i) izlemeye devam
ediyor. `checker.go`'da kapsam referansı yok, README.md:653-656 ve ADR-0003:57-58
"kısıt yalnız oluştur/değiştirde uygulanır, var olan monitor'ler yeniden
denetlenmez" diyor. İş: kapsam daralınca (ya da düzenli turda) var olan monitor/kanal
hedefleri kapsama karşı yeniden değerlendirilir; kapsam dışına düşenler işaretlenir
ve denetime/metriğe düşer ("izleme asla durmaz": varsayılan işaretle+raporla,
duraklat/zorlama ayrı ADR kararı). S77 (çözüm-anı fail-open kapanışı,
[#107](https://github.com/yusufceng/Kubeat/issues/107)) ile aynı "kapsam tek
seferlik denetleniyor" kökünden olduğundan S77'nin defense-in-depth tamamlayıcısı.
L ve ADR-0003:57-58 bilinçli kararını açtığı için PO/ADR kararı gerektirir.
Tetikleyici: S77 merge sonrası ve gerçek çok kiracılı prova tanımlanınca sprint 8'in
kesin adayı.

Güncelleme (2026-11-06, sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
artık sprint 8'in kesin adayı değil, backlog'da (PO önceki kararı açıkça
değiştirdi). Müşteri A ve SRE, S80 (kurum aralıkları), S81 (vekilli NXDOMAIN,
[#121](https://github.com/yusufceng/Kubeat/issues/121)) ve S82
(`reason=resolve_error`, satır sınırı) düzelmeden yapılacak bir yeniden denetimin
bugünkü RFC1918 ve NXDOMAIN mantığıyla kurum ve vekilli hedeflerin hepsini kapsam
dışı işaretleyip denetim fırtınası çıkaracağını gösterdi. Kabul ölçütlerine üç şey
eklendi: S80 ve S81 kurallarının aynen uygulanması, çözüm hatasının
`resolve_error` olarak işaretlenip kapsam dışı sayılmaması ve tur başına satır
sınırı. Hikâye L ve ADR-0003:57-58'deki kararı açıyor. Tetikleyici: S80, S81 ve
S82'nin merge edilmesi. README'deki "var olan monitor'ler yeniden denetlenmez"
satırı artık README.md:727-730'da.

Güncelleme (2026-11-07, sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
kapsam kaydının biçimi ve yaşam döngüsü (kapsam değişince güncellenir mi) bu
sprintte S86'nın ([#131](https://github.com/yusufceng/Kubeat/issues/131)) ADR'sinde kararlaştırılır; tur ve işaretleme S73'te
kalır. Tetikleyici (S80, S81 ve S82'nin merge'ü) sprint 10 sonunda dolar;
uygulama sprint 11 adayı (L). ADR kapsamı canlı okuyan bir kayıt seçerse S73'ün
kapsamı yeniden değerlendirilir.

Güncelleme (2026-11-07, S86 [#131](https://github.com/yusufceng/Kubeat/issues/131)): ADR-0003 (c) "Her bağlantıda kapsam" bir
**anlık görüntü** seçti: monitor'ün `settings` JSON'unda, kontrolün nereye
bağlandığını en son belirleyen kullanıcının o anki namespace'leri (`scope`),
yazanın kimliği olmadan. Monitor'ler artık her bağlantıda bu kayda göre
denetleniyor; S73'ün işi kapsam daralınca (rol, grup ya da namespace
değişikliği) kayıtları kullanıcıların bugünkü kapsamıyla karşılaştırıp işaretlemek
ve raporlamak olarak kalır. Bunun için kayda yazanın kimliği göçsüz eklenir
(JSON alanı); kaydı olmayan ya da yazanı bilinmeyen monitor'ler için kural S73'ün
ADR'sinde. Kanallar ayrı maddede ("Kanal hedeflerinde hedef kapsamını teslimat
anında uygulamak").

Kayıt kuralı asimetrik (ADR-0003 c): kayıt daha geniş bir kapsama yalnız
kontrolün bağlandığı yeri adlandıran bir ayar değişince geçer; kaydın
namespace'lerinin hepsini tutmayan birinin kontrolün herhangi bir ayarını
değiştirmesi onu ortak namespace'lere daraltır. Formun bir seçeneği API'nin
sakladığından farklı yazması (API'yle kurulmuş bir monitor'de) bu yüzden
gereksiz bir daralma doğurabilir; bedeli erişilebilirliktir. Formla API'nin
seçenek yazışını eşitlemek bu sınırı kaldırır (frontend, küçük; kanıt gelirse).

Güncelleme (2026-11-07, sprint 10 planı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)):
S91 (kaydı olmayan monitor'lerde kapsam kaydının kendiliğinden genişlemesini bir
kereye indirmek ve kaydı yöneticiye göstermek) aynı kayıt yüzeyinde. Sprint 11'de S73
ile aynı ADR-0003 (c) turunda gelir, ki ADR iki ardışık sprintte iki kez değişmesin.
ADR'deki "Görüntü yazanı tutmaz" notunun yazan kimliği S73'ten S91'e geçti (Müşteri A,
sprint 10 keşfi): kayda yazanın kimliği göçsüz bir JSON alanında S91'le gelir.
Tetikleyici (S80, S81 ve S82'nin merge'ü) sprint 10 sonunda dolar; S73 sprint 11
taslağının ilk maddesi.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
PO iki kararı bilerek değiştirdi.

- **S73 sonraki sprintte.** Sprint 11 taslağının ilk maddesiydi. L; ADR-0003 (c)'yi
  değiştiriyor ve beş yazma yoluna (rol, grup eşlemesi, namespace, silme, dizin girişi)
  dokunuyor; sekiz PR'lık sprintte ilk kayan madde olurdu. Sonraki sprint kuyruğunda 1.
  sırada; 1.0 kararı "henüz değil" çıkmazsa 1.0 "Bilinen sınırlar"ına girer. Kayda yazanın
  kimliği (göçsüz JSON alanı) yeniden S73'ün ölçütünde.
- **S91 backlog'da.** Kapsam: kaydı olmayan monitor'lerde kapsam kaydının kendiliğinden
  genişlemesini bir kereye indirmek, kayda yazanı tutmak ve kaydı yöneticiye göstermek;
  dilim 2'deki arayüz onayı dahil.
  - Kaydı olmayan monitor yalnız 0.50 ya da öncesinden yükseltilen bir kurulumda oluşur
    (`backend/cmd/server/main.go:771-773`; ADR-0003 (c) "Kaydı olmayan monitor").
  - Kullanıcı kararı (a): hiçbir sürüm satılmadı, eski sürüm desteği yok. 0.53 ya da 1.0
    ile yeni kurulan bir ortamda bu monitor'ler oluşmaz.
  - Etkilenen tek kurulum sandbox (kaydı olmayan 9 monitor). `docs/yukseltme.md` "0.51'e
    yükseltme"deki "kaydetmeyin" uyarısı onu kapsıyor.
  - Hikâye yazılı (L); geri alma maliyeti düşük. Devreden prova ("kaydı olmayan yönetici
    monitor'ü 0.50 → 0.51") S91 ile burada kalır.

  Tetikleyici: 0.50 ya da öncesinden yükseltilen gerçek bir kurulum ya da 0.51 öncesi bir
  yedekten geri yükleme isteği.

## Yeni giriş/dizin/throttle metrikleri için chart PrometheusRule ve throttle sessiz-etkisizlik metriği (S63/S72 takibi)

2026-11-06'da sprint 7 kapanışında eklendi ([sprints/2026-11-05-sprint-07.md](sprints/2026-11-05-sprint-07.md)).
İki ertelenen parça/bulgu tek kökten (gözlem eklendi ama operatör alarma bağlayamıyor):
(a) S63 ([#108](https://github.com/yusufceng/Kubeat/issues/108)) `kubeat_signin_failures_total{reason}`
ve `kubeat_signin_throttled_total{kind}` metriklerini ekledi ama isteğe bağlı
`KubeatDirectoryUnreachable` (ve hesap-kilidi) PrometheusRule'u promtool lokalde
olmadığından eklenmedi; chart var olan 13 kuralı getiriyor, bu yeni seriler için
kural yok. (b) S72 ([#106](https://github.com/yusufceng/Kubeat/issues/106)) XFF
yanlış ayarını yalnız dakika-sınırlı log WARN'ıyla (`throttle.go` `proxyWarn`)
bildiriyor; `metrics.go`'da buna karşılık bir sayaç yok, yayımlanan DoS düzeltmesinin
sessizce etkisiz kaldığı metrikle görülemiyor. Müşteri B (Ece, GKE + Grafana Cloud,
log boru hattı yok): "Sessiz etkisizliği bir metrikle (ör. `kubeat_signin_throttle_degraded_total`)
görüp Grafana Cloud'dan Slack'e alarm kurmak istiyorum (önemli); yeni metrikler için
hazır alarmla 15 dakikada kurmak istiyorum (hoş olur)." İş: (a) opsiyonel
`KubeatDirectoryUnreachable`/hesap-kilidi PrometheusRule chart values ile; (b) throttle
sessiz-etkisizlik sayacı + opsiyonel alarm; ikisi de `deploy/helm/test-chart.sh`'ta
promtool ile doğrulanır. S72/S63 auth + `metrics.go` + chart yüzeyinin doğrudan
takibi. Tetikleyici: promtool'a katlanacak bir docs+chart turu ya da throttle/dizin
alarmı isteyen ilk kullanıcı (B tetikleyici popülasyon).

Güncelleme (2026-11-06, sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
S85 olarak hikâyeleşti ve sprint 9 kuyruğunda: üç kural (dizine ulaşılamıyor,
hesap tavanı kilidi `kind="account"`, kaynak ayrımı gerilemiş) ve kaynak-ayrımı
sayacı (`reason` kapalı küme, proxyWarn'ın dakika sınırından bağımsız). Sprint 8'e
alınmadı: sayaç ancak S78'den ([#118](https://github.com/yusufceng/Kubeat/issues/118))
sonra anlam kazanıyor, hop anlamı S78/S79 ile değişiyor ve auth zinciri bu sprint
iki L ile dolu. Sprint 7'nin "promtool lokalde yok" engeli geçersiz (CI'da kurulu).
Gerekirse dizin ve hesap tavanı kuralları M boyutlu ayrı bir maddeye bölünür.
README'deki örnek `kind="account"` alarmının arayüz yolunda gerçek admin kilidini
kaçırdığı (kilit `kind="source"`'ta oluyor) S85'in kabul ölçütünde.

Güncelleme (2026-11-07, sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
sprint 10 kuyruğunda 3. sırada. Müşteri A'nın isteğiyle iki dilim: önce sayaç ve
gerileme kuralı, sonra dizin ve hesap tavanı kuralları. 0.50 (S79) iki gerileme
yolu ekledi (`no_frontend_token`, `untrusted_peer`); sayacın `reason` kümesi
dörde çıktı. Bu sprintte `throttle.go`'ya S76 ([#134](https://github.com/yusufceng/Kubeat/issues/134)) dokunuyor; sıra S76 →
S88 → S85.

Güncelleme (2026-11-07, sprint 10 planı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)):
dilim 1 sprint 10'da (S85 [#146](https://github.com/yusufceng/Kubeat/issues/146), aynı
zincirde S88'den sonra): kaynak ayrımı gerileme sayacı (`reason` kümesine
`hops_too_low` eklendi, `surface` etiketi), durum sayfası ve parola değiştirme
429'larının yeni serisi ve "kaynak ayrımı gerilemiş" kuralı. Backlog'da kalan dilim 2:
"dizine ulaşılamıyor" (`reason="directory_unreachable"`) ve hesap tavanı kilidi
(`kind="account"`) kuralları, README "Aracın kendi sağlığı" → "Kopyalamak"taki kabuk
ve GitHub Actions örneklerinin kurulu değerlerle render etmesi. Adım 2 bu iki sinyal
için bugün de karşılanıyor: S63'ün `reason`/`kind` etiketleri onları ayırıyor.
Tetikleyici: dizin ya da hesap kilidi alarmı isteyen bir kurulum, ya da sprint 11'de
boş yer.

Güncelleme (2026-11-07, S85 dilim 1 [#146](https://github.com/yusufceng/Kubeat/issues/146), 0.52.0): (b) **çözüldü.**
`kubeat_signin_source_degraded_total{reason,surface}` (`hops_zero`, `no_frontend_token`,
`untrusted_peer`, `hops_too_high`, `hops_too_low`; her istek, adres yok), durum sayfası ve
parola değiştirme 429'ları için `kubeat_throttled_total{surface,kind}` ve chart'ın
`kubeat.signin` grubunda `KubeatSignInSourceDegraded` (`untrusted_peer` hariç;
`metrics.prometheusRule.signInSeverity`), promtool'da yanan ve yanmayan senaryoyla.
`hops_too_low` `UPTIME_PUBLIC_URL`'ün host'unu açılışta ve 5 dakikada bir çözer, CNAME'de ve
çözülemeyen host'ta susar; GKE'de ölçülmedi. Bu maddede kalan yalnız dilim 2: (a)'nın
dizine ulaşılamıyor ve hesap tavanı kuralları ve README "Kopyalamak" örneklerinin kurulu
değerlerle render etmesi. Tetikleyici değişmedi. Bilinen sınır: kural iki kurulumda doğru
değerde de sürekli yanar (arayüz Service'i vekilsiz dışarı açık, `hops_zero`; başlığı
yazmayan bir katman genel adresten bağlanıyor, `hops_too_low`) ve kuralı tek başına
kapatan bir values anahtarı yok; README "Sayaç ve kural" Alertmanager'da `alertname` +
`reason` + `namespace` ile susturmayı veriyor. Kural başına bir anahtar, susturmayı
yetersiz bulan ilk kurulumda dilim 2 ile birlikte değerlendirilir.

Güncelleme (2026-11-08, sprint 10 kapanışı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)):
dilim 2'ye kaynak tavanının kuralı da girer (`kubeat_signin_throttled_total{kind="source_total"}`;
S88 [#145](https://github.com/yusufceng/Kubeat/issues/145) erteledi). Kurulum notuna
`KubeatSignInSourceDegraded` satırı S95 ile gelir (sprint 11 önerisi). `hops_too_low`'un
F5/WAF önlü Route sınırı ayrı maddede ("`hops_too_low`'u önde başlık yazan katmanların
çıkış aralıklarına genişletmek"). Sandbox UWM'de `hops_zero` ile yandı (sprint 10 prova
(b)); artış yeni pod serisinin ilk örneklemesinden önceyse `increase()` onu görmez.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
kurulum notundaki `KubeatSignInSourceDegraded` satırı S95'in
([#161](https://github.com/yusufceng/Kubeat/issues/161)) kabul ölçütünde. Dilim 2'nin kendisi
(dizine ulaşılamıyor ve hesap tavanı kuralları, `kind="source_total"` kuralı, README
"Kopyalamak" örnekleri) sprint 11'e girmedi; tetikleyicisi değişmedi.

Güncelleme (2026-11-08, S95 [#161](https://github.com/yusufceng/Kubeat/issues/161)): kurulum
notunun satırı geldi. Kurallar açıkken (`metrics.prometheusRule.enabled`) giriş throttle'ı
bölümü `KubeatSignInSourceDegraded`'i anar. Dilim 2 değişmedi.

## Kapsam-dışı hedef reddi denetimini keşif-import/restore, bakım ve workload yollarına genişletmek + flood cap (S68 takibi)

2026-11-06'da sprint 7 kapanışında eklendi ([sprints/2026-11-05-sprint-07.md](sprints/2026-11-05-sprint-07.md)).
S68 ([#109](https://github.com/yusufceng/Kubeat/issues/109)) kapsam reddini denetim
satırına (`deny`/`target-scope`) ve `kubeat_target_scope_denied_total{reason}` sayacına
yazdı ama yalnız monitor create/clone/update ve kanal create/update red dallarını
kapsıyor. Müşteri A (Deniz, banka SOC, 90 gün denetim, önemli): "Cross-tenant hedef
kurma girişiminin HER yolunun denetimde iz bırakmasını istiyorum." Bugün
`discovery.go:192,306` (keşiften içe-aktarma/geri yükleme) ve `maintenance.go:182,215`
(bakım penceresi) `scopeError` redleri ile `workloadInScope` reddi `auditScopeDenied`'dan
geçmiyor; bu yollardan kapsam dışına çıkma denemesi 403 dönse de denetime/sayaca
yazılmıyor (SOC kör noktası). Ek olarak S68 red kaydında `login_failed`'deki gibi bir
flood cap yok. İş: aynı `deny`/`target-scope` kaydı ve sayacı bu yollarda da üretilir;
en azından kapsanmayan yollar SECURITY/denetim belgesinde "denetlenmeyen red yolları"
olarak açıkça sayılır; bir yüzeyde flood gözlemlenirse cap eklenir. S68 ile aynı
`targetscope.go`/`monitors.go` yüzeyi. Tetikleyici: S68 merge sonrası (karşılandı) ve
bir docs/targetscope turu; sprint 8 adayı, S77 fail-safe ayrımıyla birlikte
değerlendirilir.

Güncelleme (2026-11-06, sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
aşağıdaki "Kapsam çözüm-hatası reddini ..." maddesiyle birlikte S82 olarak
hikâyeleşti ve sprint 9 kuyruğunda (2. aday). Red yolları tek tek sayıldı:
`discovery.go:192` (içe aktarma) ve `:306` (geri yükleme), `maintenance.go:182` ve
`:215`, `monitors.go:734` ve `:992` (`workloadInScope`), `:840` (push token
yenileme), `:1136` (silme), `alerting.go:225` (test bildirimi), `notes.go:113` ve
`:170` (olay notu). Her biri ya `deny`/`target-scope` satırı yazar ya da README
"Kapsam ihlali denetimi"ndeki "denetlenmeyen red yolları" listesinde gerekçesiyle
yer alır (sprint 7 retro "değişecek 3"). Satır sınırı `login_failed`'in `auditCap`'i
ile gelir, yeni ayar yok. S80 ve S81
([#121](https://github.com/yusufceng/Kubeat/issues/121)) önce gelir: onlar
düzelmeden kapsamı genişletmek yanlış pozitifleri çoğaltır.

Güncelleme (2026-11-07, sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
S82 sprint 10 kuyruğunda 1. sırada. `targetscope.go` zinciri S80 ([#132](https://github.com/yusufceng/Kubeat/issues/132)) →
S86 ([#131](https://github.com/yusufceng/Kubeat/issues/131)) → S82; S86'nın kontrol anı reddi aynı etiket kümesini ve satır
sınırını kullanacak. Müşteri A'nın sprint 8 bulgusu 3'ü (ayarı açmadan bir dış
adın yerel çözüm sonucunu gösteren komut, pilot ön şart sınaması) S82'ye katıldı.

Güncelleme (2026-11-07, S86): kontrol anı reddi `reason="connection"` ile her
kontrolde sayılıyor; denetim satırı (`deny`/`target-scope`, kullanıcı `checker`)
monitor başına, bağlandığı yeri belirleyen ayarları değişene ya da backend yeniden
başlayana kadar bir kez yazılıyor. S82 bu yola da `resolve_error` ayrımını ve
`auditCap`'in dakika sınırını getirir: vekile giden kontrolde SERVFAIL ve zaman
aşımı, doğrudan bağlantıda ise kontrolün yöneldiği ama monitor'ün ayarlarında
yazılı olmayan bir adın her çözüm hatası (NXDOMAIN dahil; iç-adres oracle'ı
kapansın diye, oluşturmadaki kural) bugün `connection` sayılıyor. Ayrım
sayaçta ve denetimde kalmalı, sonuçta değil: sonuç cluster içine düşen bir addan
ayırt edilmemeli.

Güncelleme (2026-11-07, sprint 10 planı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)):
sprint 10'da S82'nin ikinci PR'ıyla karşılanıyor
([#143](https://github.com/yusufceng/Kubeat/issues/143)): satır sınırı (`auditCap`,
API'de ve kontrol anında) ve red yolları tek tek (`discovery.go:192`, `:311`;
`maintenance.go:182`, `:215`; `monitors.go:735`, `:1008`, `:856`, `:1222`;
`alerting.go:225`; `notes.go:113`, `:170`). PO kararı: push token yenileme, silme,
test bildirimi ve olay notu hedef seçimi değil yetki reddi; "denetlenmeyen red
yolları" listesinde gerekçesiyle yer alır. Madde #143 merge edilince kapanır; kapanış
notu listenin README'deki yerini (bölüm adı) yazar.

Kapandı (2026-11-07, S82 [#143](https://github.com/yusufceng/Kubeat/issues/143), 0.52):
`deny`/`target-scope` satırları ve `WARN`'ları dakika başına
`UPTIME_LOGIN_AUDIT_PER_MINUTE` ile sınırlı (API'nin ve kontrollerin redleri ayrı
bütçe, sayaç her reddi sayar). Keşiften içe aktarma ve geri yükleme, bakım
penceresinin kurulması ve silinmesi `namespace`, workload hedefi (oluşturma ve hedef
değişikliği) `target` ile satır yazar. Denetlenmeyen yolların listesi README "Hedef
politikası" → "Kapsam ihlali denetimi" → "Denetlenmeyen red yolları"nda: push token
yenileme, monitor silme, test bildirimi, olay notu.

## Kapsam çözüm-hatası reddini gerçek cross-tenant reddinden ayırmak (reason/WARN, S77 takibi)

2026-11-06'da sprint 7 kapanışında eklendi ([sprints/2026-11-05-sprint-07.md](sprints/2026-11-05-sprint-07.md)).
S77 ([#107](https://github.com/yusufceng/Kubeat/issues/107)) fail-open'ı fail-safe'e
çevirdi ama `targetscope.go:40` tek `scopeRefused` sabiti hem çözüm-hatası reddini hem
cluster içine çözülen gerçek cross-tenant reddini döndürüyor; çağrı noktaları
(`monitors.go:741/811/997`) ikisini de `metrics.ScopeDeniedTarget` (reason=target)
olarak sayıyor. Müşteri A (Deniz, hava boşluklu OpenShift, önemli): "deny/target-scope
sayacındaki bir sıçramanın bir SSRF saldırısı mı yoksa benim egress/CoreDNS arızam mı
olduğunu anlamak istiyorum." Bugün CoreDNS kesintisi/egress-reddinde meşru dış-hedef
create'leri reddedilince denetim ve sayaç tıpkı bir saldırı gibi görünüyor, operatöre
"yanlış-yapılandırma" sinyali yok. İş: çözüm-hatası reddi için ayrı bir `reason`/etiket
(ör. `reason=resolve_error`) ya da bir WARN üretilsin ki S72'deki sessiz/yanıltıcı
durumun benzeri olmasın; egress/DNS arızası bir saldırı dalgasından ayrılabilsin. S77/S68
ile aynı `targetscope.go`/`metrics.go` yüzeyi. Tetikleyici: S77 merge sonrası (karşılandı)
ve bir targetscope/gözlem turu; sprint 8 adayı, S68 kapsam genişletmesiyle birlikte.

Güncelleme (2026-11-06, sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
yukarıdaki "Kapsam-dışı hedef reddi denetimini ..." maddesiyle birlikte S82 olarak
hikâyeleşti ve sprint 9 kuyruğunda (2. aday). Kapsam: SERVFAIL, NXDOMAIN ve zaman
aşımı için `reason="resolve_error"`; istemci isteği iptal ettiyse denetim satırı
yazılmaz (SRE: çözüm zaman aşımı 5 sn, arayüzün backend zaman aşımı 3 sn, iptal
edilen istek sahte bir red satırı bırakıyor); `UPTIME_TARGET_RESOLVE_TIMEOUT`
varsayılanı arayüzünkünden küçük olur. Sprint 8'de alınmadı: Müşteri A'ya göre
S80 ve S81 önce gelmeli, yoksa yanlış pozitifler çoğalır; S81
([#121](https://github.com/yusufceng/Kubeat/issues/121)) vekilli topolojideki
NXDOMAIN gürültüsünü zaten azaltıyor. Aynı `targetscope.go`/`config.go` yüzeyi,
S81'den sonra sıralı.

Güncelleme (2026-11-07, sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
S82 sprint 10 kuyruğunda 1. sırada. `targetscope.go` zinciri S80 ([#132](https://github.com/yusufceng/Kubeat/issues/132)) →
S86 ([#131](https://github.com/yusufceng/Kubeat/issues/131)) → S82; S86'nın kontrol anı reddi aynı etiket kümesini ve satır
sınırını kullanacak. Müşteri A'nın sprint 8 bulgusu 3'ü (ayarı açmadan bir dış
adın yerel çözüm sonucunu gösteren komut, pilot ön şart sınaması) S82'ye katıldı.

Güncelleme (2026-11-07, sprint 10 planı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)):
sprint 10'da S82'nin ilk PR'ıyla karşılanıyor
([#142](https://github.com/yusufceng/Kubeat/issues/142)): `reason="resolve_error"`
API'de ve kontrolün açtığı her bağlantıda; iptal ve süre dolması red sayılmaz; kapalı
kümeli `source` etiketi (`user`, `annotation`, `unrecorded`).
`UPTIME_TARGET_RESOLVE_TIMEOUT` yalnız API yolunu sınırlar ve varsayılanı 5 sn'den 2
sn'ye iner; kontrol anındaki çözüm monitor'ün kendi zaman aşımına bağlanır (0.51'de
vekile giden kontrol de API'nin süresini kullanıyordu). Madde S82'nin iki PR'ı
([#142](https://github.com/yusufceng/Kubeat/issues/142),
[#143](https://github.com/yusufceng/Kubeat/issues/143)) merge edilince kapanır;
kapanış notu "denetlenmeyen red yolları" listesinin README'deki yerini yazar.

Kapandı (2026-11-07, S82 [#142](https://github.com/yusufceng/Kubeat/issues/142) ve
[#143](https://github.com/yusufceng/Kubeat/issues/143), 0.52): çözüm hatası sayaçta,
logda ve denetim satırında `resolve_error`; API reddi de bir `WARN` yazar, kontrol anı
`WARN`'ı namespace'i ve `source`'u taşır; vekilli kurulumun NXDOMAIN ön şartı ayarı
açmadan `server resolve` ile sınanır (README "Vekilli kurulum" → "Ön sınama",
`docs/pilot.md` "Vekilli kapsam"). Denetlenmeyen red yolları README "Hedef politikası"
→ "Kapsam ihlali denetimi" → "Denetlenmeyen red yolları"nda.

## Kapsam reddi satır sınırında insanın reddini kayıtta tutmak (S82 takibi)

2026-11-07'de ertelendi (S82 [#143](https://github.com/yusufceng/Kubeat/issues/143)
incelemesi; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)). 0.52'de
kontrollerin `deny`/`target-scope` satırları ve `WARN`'ları dakikada
`UPTIME_LOGIN_AUDIT_PER_MINUTE` ile sınırlı. Bir CoreDNS kesintisinde 60'tan fazla
annotation monitor'ü aynı dakikada ilk `resolve_error` reddini verirse, o dakika kaydı
bir kullanıcıya ait monitor'ün gerçek `connection` reddinin ilk satırı ve `WARN`'ı düşer
ve sonradan yazılmaz (README "Hedef politikası" → "Kapsam ihlali denetimi" → "Satır
sınırı"). Dakikanın uyarısı yalnız `dropped`, `minute` ve `limit` yazar, düşenlerin
`source` ve `reason` dağılımını vermez. Sayaç `source="user",reason="connection"`
sinyalini korur, bu yüzden 0.52'yi bekletmedi; merkezi loga bakan SOC ise o dakikadaki
insan kaynaklı reddi logdan göremez. Seçenekler: dakikanın uyarısına `source` ve
`reason` sayımı eklemek ya da `connection` redlerine `resolve_error`'dan önce bütçe
vermek. Tetikleyici: bir pilotta ya da provada kesinti dakikasında kaybolan bir
`source=user` satırı.

## `server resolve`'un vekil istisnasını da değerlendirmesi (S82 takibi)

2026-11-07'de ertelendi (S82 [#143](https://github.com/yusufceng/Kubeat/issues/143);
[sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)). 0.52'nin
`server resolve NAME...` komutu yalnız ad alır ve pod'un çözücüsünün kararını yazar;
`UPTIME_TARGET_SCOPE_TRUST_PROXY`'yi, `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`'yi okumaz.
Çıktı ve README "Vekilli kurulum" → "Ön sınama" NXDOMAIN satırını "vekile giden
HTTP(S) istekte" diye niteler; `NO_PROXY`'ye düşen bir ad için bu karar geçmez.
Seçenek: komutun URL de alıp isteğin vekile gidip gitmediğini ve ayar açıkken
kapsamın kararını yazması. Tetikleyici: vekilli bir kurulumda `NO_PROXY` listesinde
olan dış bir adın ön sınamada yanlış okunması.

Güncelleme (2026-11-10, S98 [#162](https://github.com/yusufceng/Kubeat/issues/162)):
`docs/pilot.md` "Vekilli kapsam"ın üçüncü onay kutusu ve `docs/openshift-dogrulama.md`
"Vekilli ve hava boşluklu OpenShift" 4. adım, NXDOMAIN satırının `NO_PROXY`'deki adlarda
geçmediğini ve komutun `NO_PROXY`'yi okumadığını yazıyor. Kod değişmedi; tetikleyici
değişmedi.

## Chart'ta Route/Ingress açıkken `UPTIME_TRUST_PROXY_HOPS` varsayılanını 1 yapmak

2026-11-06'da ertelendi (sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)).
Release manager gözlemi: `route.enabled=true` iken chart'ın hop değeri 0 kalıyor;
OpenShift Route ile kurulan her kurulum WARN alıyor ve throttle'ı router adresine
anahtarlıyor, operatör elle 1 yazmalı. Sprint 8'de varsayılan bilerek 0 bırakıldı,
çünkü 1 yapmak iki soruna yol açar: önünde WAF ya da yük dengeleyici olan bir
Route'ta throttle o katmanın adresine anahtarlanır ve WARN düşmeden sessizce
geriler; frontend NetworkPolicy'si kapalıyken de arayüz Service'inden uydurma XFF
yolu açılır. S78 ([#118](https://github.com/yusufceng/Kubeat/issues/118)) ile
hops=0 iken arayüz yolunda da WARN düşer ve NOTES, route ya da ingress açıkken
README'deki değeri önerir. Tetikleyici: frontend NetworkPolicy'sinin (S79,
[#119](https://github.com/yusufceng/Kubeat/issues/119)) sahada kullanılması ya da
yanlış hop ayarının tekrarlanan raporu.

Güncelleme (2026-11-07, S78 ve S79 teslimatı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
varsayılan 0.50'de de `0` kaldı. Developer notu: 0.50'de fazla değer artık güvenli
tarafa düşmüyor (başlığa ekleyen bir vekilin arkasında istemci kaynağını seçebilir;
sprint 8 provasında hops=2 ile gerçek yolda görüldü). Bu yüzden `1` varsayılanı,
önünde başlığı yazmayan bir katman olan kurulumda yalnız eksik (güvenli) tarafta
kalır; bu bilgi kararı kolaylaştırır. Sınır: router istemcinin başlığına dokunmuyorsa
(`forwardedHeaderPolicy` IfNone ya da Never; sprint 8 Müşteri A bulgusu 1) `1` de
fazladır. Tetikleyici değişmedi.

## Arayüzün kendi gördüğü eşi `X-Forwarded-For`'a eklemesi

2026-11-06'da ertelendi (sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)).
S78 ([#118](https://github.com/yusufceng/Kubeat/issues/118)) gelen başlığı aynen
taşır, çünkü server action soket eşini okuyamıyor; Next.js de başlık varken eşi
eklemiyor (`frontend/node_modules/next/dist/server/base-server.js:601`, `??=`).
Arayüz Service'ine doğrudan gelen uydurma XFF'i ağ politikası olmadan kapatmanın
tek yolu bu, ama özel bir sunucu gerektiriyor. Bugünkü önlem S79'un
([#119](https://github.com/yusufceng/Kubeat/issues/119)) opt-in frontend
NetworkPolicy'si; kapalıyken sınır README "Hesaplar ve giriş" ve ADR-0004'te
yazılı. Tetikleyici: opt-in frontend NetworkPolicy'sinin bir platformda
uygulanamaması.

Güncelleme (2026-11-07, S78 ve S79 teslimatı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)):
developer notuna göre bu ek iki şeyi daha çözer. Arayüz Service'i doğrudan dışarı
açıkken (bugün doğru değer `0`, tek paylaşılan kilit) kaynak-başına throttle
çalışır. Fazla hop değerinin atlatılabilir olması da kökten kapanır: son girdiyi
arayüz yazacağı için değer topolojiden bağımsız sabitlenebilir. Tetikleyiciye ek:
opt-in politikanın GKE'de varsayılan `from` ile uygulanamadığının (sprint 8
Müşteri B bulgusu 5) ya da router politikası IfNone/Never olan bir kurulumun
(Müşteri A bulgusu 1) sahada görülmesi.

## XFF güveni için cluster dışındaki güvenilir vekillerin CIDR listesi (S79'un isteğe bağlı parçası)

2026-11-06'da ertelendi (sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)).
S79 ([#119](https://github.com/yusufceng/Kubeat/issues/119)) arayüzü chart'ın
ürettiği paylaşılan bir sır başlığıyla tanır (pod adresleri tek bir pod ağı
CIDR'ından dağıtıldığı için bir CIDR listesi arayüz pod'unu saldırgan pod'dan
ayıramaz). Cluster dışındaki güvenilir vekiller için isteğe bağlı bir CIDR listesi
ise alınmadı: API bilerek Route/Ingress'ten açılmıyor, backend'e doğrudan bağlanan
meşru bir dış vekil kanıtı yok ve ayar sayısı küçük kalmalı. Tetikleyici:
backend'in önüne kendi vekilini koyan bir kurulum.

## Keşif eşitlemesinde bellek sınırı ve OOMKill döngüsü (S42)

2026-11-06'da sprint kuyruğundan backlog'a alındı (sprint 8 planı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)).
Sprint 5'ten beri "sonraki sprint" kuyruğundaydı; boyut L. SRE ve Müşteri A
(yüksek): annotation eşitlemesi her turda Service, Pod, EndpointSlice, Ingress,
Route ve workload listelerini sayfalamadan belleğe alıyor; varsayılan 256Mi limit
6–7 bin pod civarında aşılıyor ve OOMKill'den sonraki açılış yine eşitlemeyle
başladığı için döngü kuruluyor, kontroller duruyor. Kanıt yalnız yerel sentetik
ölçüm; gerçek büyük bir kurulum yok. Geçici yol [yol-haritasi.md](yol-haritasi.md)
"Bilinen sınırlar"da yazılı: `backend.resources.limits.memory` artırılır ya da
`discovery.namespaces` ile kapsam daraltılır. Aynı `k8s.go` `Candidates`
fonksiyonuna dokunan S43 (okunamayan namespace'i atlamak + eşitleme metriği, M)
sprint 9 kuyruğunda ve önce gelir: sprint 3'ün "S42 → S43" sırası tersine döndü,
çünkü S43'ün metriği S42 için ilk sinyali verir. İlk dilim önerisi: bellek aşımını
görünür kılan metrik ve rehber; sayfalama/akış ikinci dilim. Tetikleyici: S43
merge edildikten sonra 3 binden fazla pod'lu gerçek bir kurulum ya da bir OOMKill
gözlemi. Yol haritasının "Bilinen sınırlar" satırı bu maddeye bağlanır.

Güncelleme (2026-11-07, sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
S43 sprint 10 kuyruğunda (4. sıra); kabul ölçütüne chart kuralı ya da ayırıcı
etiket eklendi (`sprint-takimi.md` adım 2). S42'nin kararı ve tetikleyicisi
değişmedi.

Güncelleme (2026-11-07, sprint 10 planı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)):
SRE'nin `GOMEMLIMIT` önerisi S42'nin ilk dilimi olarak not edildi. Downward API
limitin %90'ını hesaplayamıyor (yalnız limitin kendisini verir); bu yüzden backend'de
bir kod parçası ve bir ayar gerekiyor, sprint 10'a alınmadı. 0.51'de ikinci bir bellek
tüketicisi çıktı (kapsama bağlı HTTP kontrollerinin boşta tuttuğu bağlantılar); onu
sprint 10'da S92 ([#144](https://github.com/yusufceng/Kubeat/issues/144)) kaldırıyor.
S43 ([#147](https://github.com/yusufceng/Kubeat/issues/147)) sprint 10'da. Tetikleyici
değişmedi: S43 merge edildikten sonra 3 binden fazla pod'lu gerçek bir kurulum ya da
bir OOMKill.

Güncelleme (2026-11-07, S43 [#147](https://github.com/yusufceng/Kubeat/issues/147)):
annotation eşitlemesinin okuma hataları artık sayılıyor
(`kubeat_discovery_sync_errors_total{reason}`); büyük bir listenin `UPTIME_KUBE_TIMEOUT`
içinde gelmemesi `reason="timeout"` olarak görünür ve turu hiçbir şeyi değiştirmeden
bitirir. OOMKill'in kendisi bu sayaçta görünmez (süreç ölür, sayaç sıfırlanır).
Tetikleyici değişmedi.

## Annotation eşitlemesinin cluster'ı hiç okuyamadığı tur için chart kuralı (S43 takibi)

2026-11-07'de ertelendi (S43 [#147](https://github.com/yusufceng/Kubeat/issues/147);
[sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)). 0.52'nin
`KubeatDiscoveryNamespaceSkipped` kuralı yalnız namespace sınırlı keşifte atlanan
namespace'lere bakar (`kubeat_discovery_namespaces_skipped`). Cluster'ın hiç okunamadığı
tur (yanıt alınamayan istek, reddedilen kimlik bilgisi, cluster geneli keşifte okuma
hatası) hiçbir şeyi değiştirmez; `kubeat_discovery_sync_errors_total{reason}` onu sayar
ve backend WARN yazar, ama chart kuralı yoktur. Bilerek eklenmedi: PO kararı tek kural
("namespace atlıyor"); sayaçtan "eşitleme sürekli düşüyor" diyen güvenilir bir ifade
için son turun sonucunu taşıyan bir gauge (ör. son başarılı turun zamanı) gerekir.
Cluster geneli keşifte API sunucusunun gidişi workload monitor'lerinde ve Keşif
sayfasında zaten görünür. Aynı maddede, kuralın 10 dakikasının Recreate'te baştan
başlaması da ele alınabilir (yeni pod ilk turundan önce 0 bildirir). Tetikleyici:
eşitlemenin uzun süre sessizce düştüğü bir kurulum ya da bu sinyali isteyen bir SRE
bulgusu.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
tetikleyici doldu (SRE, sprint 11 keşfi). Yanıt vermeyen bir namespace isteği bütün turu
bitiriyor ve `kubeat_discovery_namespaces_skipped`'i güncellemiyor (`backend/internal/k8s/k8s.go:273`,
`:285-293`). Madde S103 olarak hikâyeleşti: başarılı son tur gauge'u,
`KubeatDiscoveryStale` kuralı ve zaman aşımına uğrayan namespace'in atlanması. PO, dört
slot dolu olduğu için onu sonraki sprinte aldı (kuyrukta 7. sıra). Bugünkü sinyal sayaç ve
`WARN`; sorun izlemeyi değil eşitlemeyi durduruyor.

## Açıkça kapatılmış `UPTIME_TARGET_SCOPE_TRUST_PROXY` için açılış WARN'ını INFO'ya indirmek

2026-11-07'de ertelendi (S81 [#121](https://github.com/yusufceng/Kubeat/issues/121)
incelemesinin düşük bulgusu). 0.50'de `HTTP_PROXY`/`HTTPS_PROXY` verilmiş ve ayar
kapalıysa backend her açılışta bir `WARN` yazar; değişken açıkça `false`
yazıldığında da. Sınırlı kullanıcısı olmayan tek kiracılı kurulumda ya da vekili
cluster içinde olduğu için ayarı bilerek kapalı tutan kurulumda bu kalıcı bir
satırdır ve log tabanlı uyarıda gürültü yapar. Öneri: değişken açıkça verildiyse
(`os.LookupEnv`) satırı `INFO`'ya indirmek; verilmemişse `WARN` kalır (kabul
ölçütü, sessizce etkisiz kalan güvenlik ayarı olmasın diye). Ertelendi, çünkü
kabul ölçütü bugünkü davranışı istiyor ve gürültü bildiren bir kurulum yok.
Tetikleyici: log tabanlı uyarıda bu satırdan şikâyet eden bir kurulum.

## İlk kurulum (setup) çağrısında `X-Forwarded-For`'u taşımak

2026-11-07'de ertelendi (S78 [#118](https://github.com/yusufceng/Kubeat/issues/118), PO kararı; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)).
0.50'de arayüz başlığı giriş, parola değiştirme ve durum sayfası kilidinde taşır;
ilk kurulum çağrısı taşımaz. Çağrı tek seferlik ve throttle'sız olduğu için kaynak
adres bugün hiçbir karara girmiyor. Tetikleyici: setup yoluna throttle ya da kaynak
adresli bir denetim satırı eklenmesi.

## Vekil-hop rehberinin satırlarını arayüz yolunda ölçmek

2026-11-07'de ertelendi (S78 [#118](https://github.com/yusufceng/Kubeat/issues/118); [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)).
README "Vekil-hop rehberi" tablosunun altı satırı da 0.50'de "ölçülmedi" yazıyor.
Sprint 8 provası (sandbox, tarayıcı → Route → arayüz → backend) yalnız "Düz
OpenShift Route: 1" satırını ölçtü. Port-forward (`1`), WAF'lı Route (`2`) ve GKE
satırları (`ingress-nginx` `1`, L7 GCLB + `ingress-nginx` `3`, GCE Ingress `2`)
ölçülmedi. Bir satır ölçülünce README, ADR-0004, `values.yaml` ve
`docs/yukseltme.md`'de aynı anda "ölçüldü" yazılmalı; düz Route için bu yazım da
henüz yapılmadı. `ingress-nginx` satırı minikube'un ingress addon'uyla ajan-içi
ölçülebilir (sprint 8 Müşteri B bulgusu 6). Tetikleyici: sprint 9'un minikube
`ingress-nginx` provası ya da kullanıcının GKE ya da kurumsal provası.

Güncelleme (2026-11-07, sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
tetikleyici karşılanıyor. S87 ([#133](https://github.com/yusufceng/Kubeat/issues/133)) sprint 8'de ölçülen "Düz OpenShift
Route: 1" satırını README, ADR-0004, `values.yaml` ve `docs/yukseltme.md`'de
"ölçüldü (sandbox, 2026-11-07)" yapar. `ingress-nginx` satırı sprint 9 zorunlu
prova (a) ile Calico'lu ve ingress addon'lu minikube'da ölçülür ve ortamıyla
etiketlenir. Prova sonrası madde kapanır; gerçek GKE satırları, WAF'lı Route
ve port-forward satırı kullanıcıda kalır.

Güncelleme (S87, [#133](https://github.com/yusufceng/Kubeat/issues/133)): düz Route satırı README, ADR-0004, `values.yaml` ve
`docs/yukseltme.md`'de (0.49 ve 0.50 bölümleri) "ölçüldü (sandbox, 2026-11-07)"
oldu. Tabloya üç satır "ölçülmedi" ile eklendi: router `Replace`, router
`IfNone`/`Never` (Route annotation'ıyla `1`) ve GKE Gateway
(`gke-l7-global-external-managed`, `2`); WAF'lı Route satırına router `Append`
şartı yazıldı. Kurulum notunun
`ingress.className`'e göre önerdiği değerler (`nginx` `1`, `gce` `2`) prova
(a)'da ve kullanıcının GKE provasında ölçülür; ölçülmeyen satır "ölçülmedi"
kalır.

Güncelleme (2026-11-07, sprint 9 kapanışı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
**kapandı.** Prova (a) Calico'lu ve ingress eklentili minikube'da (controller v1.10.1)
`ingress-nginx` `1` satırını ölçtü; README, ADR-0004 ve `docs/yukseltme.md`'de
ortamıyla "ölçüldü" yazıldı. Kullanıcıda kalanlar (sprint dosyası "Sprint 10'a
devredenler"): GCE Ingress ve GKE Gateway `2`, `ingress-nginx`'in LoadBalancer +
`externalTrafficPolicy: Local` yolu, WAF ya da F5 önlü Route `2`, router `Replace`,
`IfNone`/`Never` ve port-forward satırları.

## Arayüz sırrı değişince pod'ları kendiliğinden yeniden başlatmak (checksum annotation)

2026-11-07'de ertelendi (S79 [#119](https://github.com/yusufceng/Kubeat/issues/119); [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)).
`<release>-frontend-token` Secret'ı değişince backend ve arayüz pod'ları
kendiliğinden yeniden başlamaz. Pod'lardan biri eski sırla kalırsa tarayıcı
girişleri arayüz pod'una anahtarlanır (tek paylaşılan kilit ve "güvenilmeyen eş"
WARN'ı). Elle yeniden başlatma README'de belgeli; öteki Secret'larda da checksum
annotation'ı yok. Yapılırsa bütün Secret'lar için aynı desende yapılır. Tetikleyici:
sır döndürme isteyen bir kurulum ya da bu pencerenin GitOps'ta gözlenmesi.

## Doğrulamalı yedek bloklarının kalanları (S84 takibi)

2026-11-07'de ertelendi (S84+S74 [#120](https://github.com/yusufceng/Kubeat/issues/120); [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)).
0.50'de `docs/yedek-geri-yukleme.md`'deki dışarı alma ve geri yükleme blokları
parçalı, sha256 ve `SQLite format 3` doğrulamalı; sprint 8'de minikube'da
(`kubectl`) ve sandbox'ta (`oc`) geçti. Kalanlar:

- **Blokları CI'da sınamak.** Belgedeki blokları olduğu gibi çıkarıp sahte bir
  `kubectl` ile koşan test. Developer'ın lokal düzeneği (52 senaryo) depoya
  alınmadı, çünkü PO kararı ayrı betik istemedi. Düzenek #123'ün kuralına uymalı
  (`KUBECONFIG` tuzağı, `command -v`) ve `KUBECTL`'i sahte betiğin mutlak yoluna
  ayarlamalı.
- **Yükleme yönünü parçalı yapmak.** Geri yükleme kopyayı tek akışla yükler; pod
  tarafındaki sha256 ve başlık denetimi kesik bir yüklemeyi `mv` etmeden reddeder.
  Tetikleyici: yükleme yönünde kesilmenin gözlenmesi.
- **Anahtar Secret'ının adını Deployment'tan okumak.** Bugün `SECRET=<ad>`
  parametresiyle ve belgedeki bir jsonpath komutuyla veriliyor. Blok kendisi okusaydı
  ek bir `get deploy` çağrısı ve RBAC varsayımı gerekirdi.
- **İnceleme düşük bulguları (BA).** `SECRET` verilmemiş bir `existingSecret`
  kurulumunda blok bütün parçaları indirdikten sonra Secret'ı okuyamayıp kopyayı
  siliyor; güvenli ama büyük veritabanında zaman kaybı, Secret önce denetlenebilir.
  `kubeat-restore` pod'u yoksa, hazır değilse ya da namespace yanlışsa yalnız
  `kubectl`'in hatası kalıyor ve belgede söz verilen `GERİ YÜKLENMEDİ` satırı
  çıkmıyor; `mv` yapılmadığı için güvenli.

Tetikleyici: bloklara dokunan bir sonraki madde. İmaja `tar` (S7) ayrı maddede
("Yedekleri cluster dışına taşıma belgesi ve backend imajına `tar`").

Güncelleme (2026-11-07, sprint 9 planı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
"Blokları CI'da sınamak" S90 olarak hikâyeleşti. PO sprint 8'in "ayrı betik yok"
kararını açıkça değiştirdi: iki müşterinin felaket yolu Linux istemci (Müşteri B
Cloud Shell ya da GitHub Actions ubuntu koşucusu, Müşteri A RHEL bastion) ve
`sprint-takimi.md` adım 7 gereği asıl yol ya CI'da koşar ya da zorunlu prova
olur. Kabul ölçütü özeti: test blokları belgeden olduğu gibi çıkarır ve CI'ın
Linux koşucusunda (GNU coreutils) koşar; sahte `kubectl` `KUBECTL`'in mutlak
yoluyla verilir, `KUBECONFIG` tuzaktır ve `kubectl`/`oc`/`helm` koruma betikleri
çağrılınca kayıt yazıp hata verir; doğru, değiştirilmiş, SQLite olmayan ve kesik
kopya ile `SECRET`'sız `existingSecret` senaryoları. Sprint 9'a alınmadı:
slotlar güvenlik ve kurulum işleriyle dolu. Ara önlem: plan dalında runbook'un
"Yeniden prova" bölümüne "GNU od/tr ile henüz sınanmadı" notu. Tetikleyici: S7
ile aynı belge turu (sprint 11 adayı) ya da bloklara dokunan ilk madde.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
S7 sprint 11'e girmediği için S90 da sonraki sprintte, S7 ile aynı belge turunda
(kuyrukta 5. sıra). Bu turda yeni müşteri kanıtı yok. Bloklar macOS'ta ve sandbox'ta `oc`
ile koşuldu; Linux istemci için "sınanmadı" notu kalır.

## ntfy kanal adresindeki host'u yönetici için de ASCII'ye zorlamak

2026-11-07'de ertelendi (S81 [#121](https://github.com/yusufceng/Kubeat/issues/121): incelemenin düşük bulgusu ve developer'ın
isteğe bağlı önerisi; [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md)). README "Hedeflerdeki ve kanal adreslerindeki host
adları ASCII ile yazılır" diyor, ama ntfy kanal ayarları ASCII denetiminden
geçmiyor: yönetici ya da `*` kapsamlı kullanıcı ASCII olmayan bir host'la 201
alıyor (2026-10-20'den beri). Güvenlik etkisi yok; kapsamı sınırlı kullanıcı
0.50'de kapsam denetimiyle reddediliyor. Yöneticiye 400 vermek tek kiracılı
kurulumda da davranışı değiştirir, bu yüzden kabul ölçütünün dışında kaldı.
Tetikleyici: kanal ayarlarına dokunan bir madde ya da ASCII olmayan bir ntfy
host'unun teslimatta sorun çıkardığı bir rapor.

## Kanal hedeflerinde hedef kapsamını teslimat anında uygulamak (sertleştirme)

2026-11-07'de ertelendi (S86 [#131](https://github.com/yusufceng/Kubeat/issues/131)'in kapsam dışı maddesi; sprint 9 planı;
[sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)).
S86 kapsamı sınırlı kullanıcının monitor'lerinde hedef kapsamını kontrol
sırasında yapılan her bağlantıda uygular. Kanal (webhook türü) hedefleri bu
sertleştirmenin dışında kaldı; kanalların kapsamı bugünkü gibi oluşturma ve
değiştirmede denetlenir. Kanıt gelirse ayrı madde olur. Tetikleyici: S86'nın SRE
incelemesindeki değerlendirme.

Güncelleme (2026-11-07, S86 teslimatı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
SRE incelemesinden sonra madde backlog'da kaldı. Yeni tetikleyici: kanal hedefleriyle
ilgili bir bulgu ya da S73'ün kanal kısmı.

## Kurum aralığı açılış korumasını node ağına ve cluster ağlarının tamamına genişletmek

2026-11-07'de ertelendi (S80 [#132](https://github.com/yusufceng/Kubeat/issues/132); [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)).
0.51'de `UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS` aralıklarından biri API sunucusunun
adresini (`KUBERNETES_SERVICE_HOST`) ya da backend'in kendi adresini içeriyorsa
backend başlamaz. Koruma yalnız bu iki örneğe, o açılıştaki hâlleriyle bakar:
aralık pod ya da Service ağının başka bir parçasını, node (makine) ağını ya da
API VIP'ini içeriyorsa backend başlar ve kapsamı sınırlı kullanıcı oralara
namespace'siz ulaşır (README "Hedef politikası", "Kurum ağı", "Şart"). Pod
ağının başka bir parçasını içeren aralık ise pod o parçaya taşınınca (drain,
yeniden zamanlama) backend'i durdurur; ağın tamamını okumak bunu ilk açılışta
yakalar. Router VIP'i on-prem Route'lar için bilerek eklenebildiğinden koruma
onu reddetmemeli.
Seçenekler: chart'ın Downward API ile `status.hostIP`'yi vermesi, operatörün
cluster ağlarını ayrı bir ayarla beyan etmesi ya da OpenShift'te
`network.config/cluster`'ı okumak (küme düzeyinde okuma yetkisi ister). Router
VIP'ini kendiliğinden bulmak S80'in kapsam dışıydı. Tetikleyici: pilotta ya da
provada aralığın node ağı veya API VIP'iyle çakıştığının görülmesi, ya da bunu
isteyen bir kurulum.

Güncelleme (2026-11-07, sprint 9 kapanışı; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)):
**tetikleyici doldu** ("bunu isteyen bir kurulum"; Müşteri A, sprint 9 bulgusu 2,
önemli). Müşteri ayarı 30 cluster'da GitOps'la cluster başına veriyor. OpenShift'te her
node pod ağından kendi alt ağını alıyor ve her cluster yükseltmesi node'ları sırayla
drain ediyor: pod ağının bir parçasını içeren aralık ilk açılışta geçip tam yükseltme
sırasında backend'i durdurur. Liste dışlama kabul etmediği için kurum ağı tek büyük bir
özel aralıksa cluster başına onlarca CIDR elle yazılıyor. Koruma gerçek bir pod'da
denenmedi (sprint 10'a devreden prova). Müşterinin isteği:

- Operatör cluster ağlarını chart'ta beyan eder; çakışma `helm template` ya da ArgoCD
  sync anında render hatası verir, CrashLoop değil.
- Çalışma anında yakalanan çakışmada backend durmaz: ayar yok sayılır (boş liste daha
  sıkı taraf), `ERROR` satırı ve chart alarmı verilir, izleme sürer.

Çalışma anı kısmı S80'in PO kararına ("çakışan aralık backend'i başlatmaz") aykırı;
gerekçe "İzleme asla durmaz" ilkesi ve boş listenin daha sıkı taraf olması, karar
PO'da. PMO önerisi: 1.0'dan önce, aynı kapsam yüzeyindeki S73 ile sıralı olarak sprint
11 adayı.

Güncelleme (2026-11-07, sprint 10 planı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)):
PO kararı: çalışma anı geri düşüşü (S96) kabul edildi ve S80'in "backend başlamaz"
kararı ortama bağlı denetimler (API sunucusunun adresi, pod'un kendi adresleri,
adreslerin okunamaması) için değişiyor. Backend başlar, ayar yok sayılır (boş liste
daha sıkı taraf), `ERROR` satırı, gauge ve chart kuralı verilir; biçim hataları yine
backend'i başlatmaz. Uygulama sprint 11'de, render anındaki cluster ağı beyanı ve S73
ile. Ek kanıt (Müşteri B, sprint 10 keşfi): beyan edilen cluster ağları kapsam
denetiminin cluster içi sınıflamasını da beslemeli. Bugünkü açılış korumasının gerçek
pod'daki davranışı sprint 10'un zorunlu provası (c)'de, çok node'lu minikube'da pod
drain ile taşınarak ölçülür; sonuç sprint 11'in girdisi.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
prova (c) CrashLoopBackOff'u ölçtü (iki node'lu minikube, izleme durdu). Madde sprint 11'de
iki sıralı PR olarak geliyor:

- S96 [#157](https://github.com/yusufceng/Kubeat/issues/157): ortama bağlı denetimlerde
  yalnız çakışan girdi yok sayılır (SRE önerisi, PO kararı); `ERROR`, gauge ve
  `KubeatScopeExternalRangesIgnored` verilir.
- [#158](https://github.com/yusufceng/Kubeat/issues/158): `targetScope.clusterNetworks`
  beyanı (`pods`, `services`, `nodes`), render hatası ve `UPTIME_TARGET_SCOPE_CLUSTER_CIDRS`.
  Render denetimi yalnız IPv4.

İkisi merge edilince madde kapanır. Ayrı kalanlar:

- beyan edilen ağların kapsamın cluster içi sınıflamasını beslemesi ("Beyan edilen cluster
  ağlarının kapsam denetiminin cluster içi sınıflamasını beslemesi (S96 takibi)");
- router VIP'ini kendiliğinden bulmak ve dışlama sözdizimi. Bunların tetikleyicisi
  değişmedi.

Güncelleme (sprint 11, [#157](https://github.com/yusufceng/Kubeat/issues/157) dalı): çalışma
anı kısmı yapıldı (0.53). Ortama bağlı denetimler (API sunucusunun adresi, pod'un kendi
adresleri, adreslerin okunamaması) backend'i durdurmuyor; yalnız çakışan girdi yok
sayılıyor, `ERROR` satırı, `kubeat_target_scope_external_ranges_ignored` ve
`KubeatScopeExternalRangesIgnored` veriliyor. Biçim hataları yine başlatmıyor. Madde
[#158](https://github.com/yusufceng/Kubeat/issues/158) (beyan ve render hatası) merge
edilince kapanır.

Kapandı (sprint 11, [#158](https://github.com/yusufceng/Kubeat/issues/158) dalı; 0.53):
operatör cluster ağlarını opt-in `targetScope.clusterNetworks` ile (pod, Service, node;
node ağı elle, `lookup` yok) beyan ediyor. Beyan edilen bir ağla çakışan kurum aralığı
`helm template`/`helm upgrade`'de render hatası veriyor (yalnız IPv4); beyan
`UPTIME_TARGET_SCOPE_CLUSTER_CIDRS` olarak backend'e de gidiyor ve açılış IPv6 dahil
ağların tamamına karşı denetleyip çakışan girdiyi yok sayıyor. Tek adres (`/32`, `/128`)
beyanla karşılaştırılmıyor: node ağındaki router VIP'i böyle, bilerek yazılıyor. Kalanlar
kendi maddelerinde: "Render anındaki cluster ağı denetimini IPv6'ya genişletmek (S96
takibi)", "API VIP'ini ve tek adresleri cluster ağı beyanıyla denetlemek (S96 takibi)",
"Beyan edilen cluster ağlarının kapsam denetiminin cluster içi sınıflamasını beslemesi (S96
takibi)"; router VIP'ini kendiliğinden bulmak ve dışlama sözdizimi yukarıdaki
tetikleyiciyle burada kalıyor.

## Kurulum notunda `frontend.networkPolicy.from` önerisini Ingress sınıfına göre vermek

2026-11-07'de ertelendi (S87 [#133](https://github.com/yusufceng/Kubeat/issues/133); [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)).
0.51'de kurulum notu hop değerini Ingress sınıfına göre öneriyor (`nginx` `1`, `gce`
`2`), ama arayüz politikası kapalıyken `frontend.networkPolicy.from` için eski genel
cümleyi veriyor; `gce` için yük dengeleyici aralıklarını söylemiyor. `gce-internal`
gibi öteki sınıflar için ayrı öneri yok, bugünkü üç seçenek sıralanıyor. Kabul ölçütü
yalnız hop önerisini istiyordu. İlgili müşteri bulguları (sprint 9, Müşteri B, önemli):
GCE'de verilen değer sınıfla çelişince kurulum notu bunu söylemiyor (bulgu 4) ve GKE
için kopyalanabilir bir `values-gke.yaml` yok (bulgu 5). Tetikleyici: o iki bulguyu
taşıyan madde (sprint 10 ya da sprint 11 belge turu adayı) ya da `gce-internal` ile
kurulan bir kurulum.

Güncelleme (2026-11-07, sprint 10 planı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)):
tetikleyici doldu. Madde S95'in kabul ölçütüne girdi (GCE Ingress yolu: kurulum
notunda hop ve TLS çelişki satırları, `from` öneri satırı, CI'da render edilen
`values-gke.yaml`); S95 sprint 11 adayı, S94 ile. Madde S95 merge edilince kapanır. Bu
sprintte yalnız ara önlem: README GKE bölümünün GCE paragrafı ManagedCertificate yolu
için üç değeri birlikte veriyor (`ingress.tls.enabled=false`,
`publicURL=https://<host>`, annotation; plan dalı).

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
S95 sprint 11'de [#161](https://github.com/yusufceng/Kubeat/issues/161) olarak, eşi S94 değil
S98 ([#162](https://github.com/yusufceng/Kubeat/issues/162)). Madde #161 merge edilince
kapanır; `gce-internal` için ayrı öneri bu bölümde kalır.

Güncelleme (2026-11-08, S95 [#161](https://github.com/yusufceng/Kubeat/issues/161)):
**çözüldü.** Kurulum notu sınıf `gce` iken politika kapalıysa `from` için iki aralığı verir,
açıksa ve `from` `35.191.0.0/16` ile `130.211.0.0/22`'yi birden içermiyorsa bir WARNING
yazar. Hop çelişkisi (`gce` iken `2`, `nginx` iken `1` değilse) ve ManagedCertificate'li
Ingress'te TLS çelişkisi de satır alır. `values-gke.yaml` `docs/gke.md`'de ve CI'da render
ediliyor. Kalanlar:

- `gce-internal` için ayrı öneri. Tetikleyici değişmedi: `gce-internal` ile kurulan bir
  kurulum.
- Önde L7 GCLB olan `ingress-nginx`'te doğru değer `3`'tür, ama sınıf `nginx` iken çelişki
  satırı yine çıkar. Bu, PO'nun kabul ölçütüdür ("`nginx` iken `1` değilse"). Satırı o
  kurulum için susturan bir işaret (ör. bir values anahtarı) yok. Önünde `X-Forwarded-For`
  yazan başka bir vekil (WAF ya da başka bir bulutun L7 yük dengeleyicisi) olan
  `ingress-nginx` de aynıdır: `use-forwarded-headers` ve `compute-full-forwarded-for`
  açıkken değer `1` artı o vekillerin yazdığı girdilerdir. Satır bunu söyler ve değeri
  değiştirmemeyi ister; README "GKE kontrol listesi" CI'da yalnız saymayı yazar.
  Tetikleyici: önde GCLB'li ya da başka bir L7 vekilli bir `ingress-nginx` kurulumunun
  README'deki CI tarifini kullanması.

## `test-chart.sh`'in README denetimini kurulum notunun gösterdiği bölüme daraltmak

2026-11-07'de ertelendi (S87 [#133](https://github.com/yusufceng/Kubeat/issues/133) BA incelemesinin düşük bulgusu; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)).
`sprint-takimi.md` adım 2, kurulum notunun önerdiği değerin notun gösterdiği README
bölümünde geçmesini kabul ölçütü sayıyor. `deploy/helm/test-chart.sh` metinleri bütün
README'de arıyor; bölüm başka yerde aynı metni taşıyınca denetim yanlışlıkla geçer.
İş: denetimi bölüm başlıkları arasında (`awk` ile, pilot örneğindeki gibi) yapmak.
Tetikleyici: kurulum notuna dokunan bir sonraki madde.

Güncelleme (2026-11-07, sprint 10 planı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)):
tetikleyici doldu; madde S95'in kabul ölçütünde (sprint 11 adayı) ve S95 merge
edilince kapanır.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
S95 sprint 11'de [#161](https://github.com/yusufceng/Kubeat/issues/161).

Güncelleme (2026-11-08, S95 [#161](https://github.com/yusufceng/Kubeat/issues/161)):
**çözüldü.** `test-chart.sh` metinleri notun gösterdiği README bölümünde arar: başlığın
altında, verilmişse o başlığın içindeki kalın açılışlı paragrafta. Daraltma ilk koşuda bir
açık buldu: notun `ingress-nginx` için önerdiği
`--set controller.service.externalTrafficPolicy=Local` "Hesaplar ve giriş"te değil yalnız
hızlı başlangıçta geçiyordu; "Kurulum notunun önerisi"ne eklendi.

## Keşiften içe aktarmayı monitor'ün kapsam kuralıyla hizalamak

2026-11-07'de ertelendi (S80 [#132](https://github.com/yusufceng/Kubeat/issues/132) ve S86 [#131](https://github.com/yusufceng/Kubeat/issues/131); [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)).
İçe aktarma hedefi çözmez, yalnız kullanıcının düzenleyebildiği namespace'e bakar
(README "Hedef politikası" → "Sınır: keşiften içe aktarma"). Ayar boşken ya da router
VIP'i listede değilken kapsamı sınırlı kullanıcı bir Route'u içe aktarabiliyor; aynı
monitor'ün klonu ve aynı host'a hedef değişikliği ise reddediliyor. 0.51'de kontrol her
bağlantıda kapsama tabi olduğundan bu monitor kontrolde `blocked_by_scope` ile DOWN
olur. İçe aktarma aynı kararı içe aktarma anında verirse kullanıcı erken ve açık bir
403 alır. Aynı `discovery.go` yolu S82'nin red yolları listesinde. Tetikleyici: S82
(sprint 10) ya da S73.

Güncelleme (2026-11-07, sprint 10 planı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)):
PO kararı: S82'ye girmedi, çünkü ayrı bir davranış değişikliği ve S82 zaten L. S82 içe
aktarmanın red yolunu (`discovery.go:192`) yalnız denetim ve sayaç açısından ele
alıyor ([#143](https://github.com/yusufceng/Kubeat/issues/143)). Tetikleyici S73
(sprint 11).

## Kapsam denetiminin porta bakması (node üstündeki router VIP'i)

2026-11-07'de ertelendi (S80 [#132](https://github.com/yusufceng/Kubeat/issues/132); [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)).
Router VIP'i bir node'un üstündeyse (bare metal'de keepalived) ve `/32` ile
`UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS`'e girdiyse, aynı adres o node'un host ağında
dinleyen portlarına da (ör. kubelet) çıkar; kapsam portlara bakmaz. 0.51'de yalnız
belgelendi (README "Hedef politikası" → "Kurum ağı" → "Şart"): bunu istemeyen VIP'i
listeye almaz, on-prem Route'ları yönetici kurar. Çözüm, aralıkla birlikte izinli
portları da okumak olabilir. Tetikleyici: VIP'i node üstünde olan
ve on-prem Route'ları kapsamı sınırlı editörlere açmak isteyen bir kurulum.

## Annotation monitor'leri için kurulum düzeyinde kapsam istisnası

2026-11-07'de ertelendi (S86 [#131](https://github.com/yusufceng/Kubeat/issues/131); [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)).
0.51'de annotation'la kurulan monitor her kurulumda nesnesinin namespace'ine bağlıdır,
çünkü hedefini nesneyi düzenleyen belirler ve Kubeat onun kim olduğunu bilmez. Kapsamı
sınırlı kullanıcısı olmayan tek kiracılı bir kurulum için bu kural gereğinden sıkı
olabilir. Kurulum düzeyinde bir istisna ayarı bilerek eklenmedi: kanıt yok ve güvenli
varsayılanı geri açar. Sprint 9'da Müşteri A (bulgu 1, SSO) istisna değil belge çaresi
ve `kubeat.io/path` istedi; Müşteri B (bulgu 6) tek kiracılı kurulum için yükseltme
notunda kısa yol istedi. Tetikleyici: belge çaresi ve `kubeat.io/path` yetmeyen bir
kurulum.

## README'deki negatif adımın imajını (`curlimages/curl:8.10.1`) digest'e sabitlemek ve `check-versions.sh` ile denetlemek

2026-11-07'de eklendi (sprint 10 planı;
[sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)). README "Hesaplar
ve giriş" → "Arayüzün ağ politikası" → "Politikanın uygulandığını doğrulamak" adımı
başka bir namespace'te `curlimages/curl:8.10.1` ile bir pod açar. İmaj yalnız etiketle
verilmiş ve etiketin var olduğu doğrulanmadı (sprint 9 "Sprint 10'a devredenler").
Müşteri A istedi; Müşteri A, Müşteri B ve SRE düşük önemde dedi. Etiketin varlığı tek
bir registry sorgusuyla doğrulanır (release manager; cluster gerekmez). Etiketin
yanına digest'i yazmak ve `scripts/check-versions.sh`'in bu satırı denetlemesi sprint
11 belge turunda yapılır. Tetikleyici: sprint 11 belge turu.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
etiket sprint 10'un "Sprint başında" adımında doğrulandı; liste digest'i
`sha256:d9b4541e214bcd85196d6e92e2753ac6d0ea699f0af5741f8c6cccbfcf00ef4b`. Sabitleme ve
denetim S66'nın ([#164](https://github.com/yusufceng/Kubeat/issues/164)) kabul ölçütünde;
madde #164 merge edilince kapanır.

Kapandı (2026-11-08, S66 [#164](https://github.com/yusufceng/Kubeat/issues/164), 0.53):
README'deki adım `curlimages/curl:8.10.1@sha256:d9b4…ef4b` ile koşar.
`scripts/check-versions.sh` (CI'ın chart işi ve yayımın ilk işi) belgelerde imajın
etiketli ya da digest'li her anışının betikteki `CURL_REF` olmasını ve README'de en az bir
anış bulunmasını ister; `scripts/test-check-versions.sh` sınar. Yeni bir etiketin
digest'i registry API'sinden okunur, betikte ve belgelerde birlikte değişir.

## Kaynak tavanının ilk reddi için denetim satırı

2026-11-07'de ertelendi (S88 [#145](https://github.com/yusufceng/Kubeat/issues/145)
SRE incelemesinin düşük bulgusu; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)).
Bir ad kaynak + ad sınırında kilitlenince denetim kaydına "sign-in locked after N failed
attempts" satırı yazılıyor; bir kaynak `UPTIME_LOGIN_SOURCE_MAX_FAILURES` tavanını
doldurduğunda denetim satırı yok. İz dakikalık throttle `WARN`'ı
(`throttle=source_total`) ve `kubeat_signin_throttled_total{kind="source_total"}`.
Güvenlik ekibi bir spreyin kilitlendiği anı denetim kaydında göremez. Öneri: tavanın
ilk reddinde tek bir satır (ör. `login_locked_source`), denetim satırı sınırıyla
(`UPTIME_LOGIN_AUDIT_PER_MINUTE`). Tetikleyici: denetim kaydını okuyan gerçek bir
güvenlik ekibi isteği ya da S83'ün kalanı.

## API'de birden çok adı sınayan istekte ilk çözüm hatası ve iki çözümün süresi (S82 takibi)

2026-11-08'de ertelendi (S82 [#142](https://github.com/yusufceng/Kubeat/issues/142)
incelemesi; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)). 0.52'de API bir isteği ilk reddiyle sayar: istek birden çok ad
sınıyorsa ve ilk ad çözülemiyorsa ret, ötekiler sınanmadan `resolve_error` sayılır ve
denetim satırı ilk adı yazar (README "Hedef politikası" → "Kapsam ihlali denetimi",
"Bilinen sınır"). İstek yine 403 alır, sızıntı yok; ama öteki ad cluster'a götürse de
`reason="target"` alarmı yanmaz. Kontrol anında bütün adlar sınanır. Seçenek: oluşturma,
klon ve değiştirmede bütün adların yazılışını çözümden önce sınamak; bu, 400/403 ile
namespace ve hedef reddinin öncelik sırasını değiştirir. Aynı yüzeyde ikinci parça: iki
çözüm ayrı 2 sn'lik bütçe kullandığından birden çok ad sınayan bir isteğin
oluşturması ~4 sn sürebilir ve arayüzün 3 sn'sini aşar; seçenek isteğe tek bir son tarih
(belge düzeltmesi seçildi, kod değişikliği testte gerçek saat ister). Tetikleyici: S73 ya
da S93'ün `targetscope.go` turu, ya da `target` yerine `resolve_error` sayılan gerçek bir
girişim.

Güncelleme (2026-11-08, S93 [#160](https://github.com/yusufceng/Kubeat/issues/160)): S93
`targetscope.go`'ya dokunmadı (kontrol anı reddinin açıklaması, yönetici alanı ve kural);
madde issue'nun kapsam dışında. Tetikleyici S73 ya da gerçek bir girişim olarak kalır.

## Kapsam reddi için chart'ta işletim kuralı ve README'deki PromQL örneklerinin promtool'la sınanması (S82 takibi)

2026-11-08'de ertelendi (S82 [#142](https://github.com/yusufceng/Kubeat/issues/142), PO
kararı: chart'a kapsam reddi alarmı kapsam dışı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)). 0.52
`resolve_error`'u sayaçta ayırıyor ama chart'ta kural yok. README "Kapsam ihlali
denetimi"ndeki iki PromQL örneği (`reason="target",source="user"` ve
`reason="resolve_error"`) promtool'la ya da gerçek bir Prometheus'ta denenmedi. Müşteri A
(sprint 10 bulgusu 1, önemli): `metrics.prometheusRule` altında
`resolve_error{source="annotation"}` artışı için promtool'la sınanan bir işletim kuralı;
DNS kesintisi böylece platform ekibine kapsam reddi diye değil işletim alarmı diye gider.
Tetikleyici: S93 (sprint 11 önerisinde S96'nın zincirinde).

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
S93 sprint 11'de [#160](https://github.com/yusufceng/Kubeat/issues/160) olarak, S89'un
zincirinde. İçeriği:

- yeni `kubeat.scope` grubunda `KubeatScopeResolveErrors` (`source="annotation"`,
  `for: 10m`, `warning`);
- README'deki iki PromQL'in `test-chart.sh`'te belgeden çıkarılıp promtool'la sınanması.

#160 merge edilince madde kapanır.

Kapandı (2026-11-08, S93 [#160](https://github.com/yusufceng/Kubeat/issues/160), 0.53):
`KubeatScopeResolveErrors` `kubeat.scope` grubunda; annotation monitor'lerinin
`resolve_error` redleri 10 dakika, en geç 5 dakikada bir sürünce yanar, tek bir geçici
SERVFAIL'de yanmaz (`prometheusrule-test.yaml`, iki cluster'lı senaryo
`prometheusrule-cluster-test.yaml`'da). README "Hedef politikası" → "Kapsam ihlali
denetimi"ndeki iki PromQL `test-chart.sh`'te bölümün ilk kod bloğundan olduğu gibi alınıp
`prometheusrule-readme-test.yaml`'ın senaryolarıyla promtool'da sınanıyor. İkinci ifade
artık `source!="annotation"` diyor; annotation monitor'lerininkini kural verir. Gerçek
bir Prometheus'ta yanıp çözülmesi sprint 11 prova (b)'de (kullanıcıda kalanlar).

## README "Metrikler" tablosunda `kubeat_target_scope_denied_total`

2026-11-08'de ertelendi (S82 [#142](https://github.com/yusufceng/Kubeat/issues/142);
[sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)). README "Aracın kendi sağlığı" → "Metrikler" tablosu sayacı listelemiyor; eksik bu
sürümden önce de vardı. Satır `reason` (`target`, `namespace`, `connection`,
`resolve_error`) ve `source` (`user`, `annotation`, `unrecorded`) etiketlerini ve "Kapsam
ihlali denetimi"ne bağlantıyı verir. Tetikleyici: sprint 11 belge turu.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
satır S93 [#160](https://github.com/yusufceng/Kubeat/issues/160) ile gelir, çünkü sayaç
kuralın serisi; madde #160 merge edilince kapanır. Tablonun eksik öteki dokuz serisi, kural
sayısını şablonla karşılaştıran CI denetimi ve README'yi kayıtlı serilerle eşleyen test S100
olarak hikâyeleşti (sonraki sprint, kuyrukta 9. sıra). S100'ün iki olgusal hatası plan
dalında düzeltildi: "Kopyalamak" artık 15 kural diyor ve `kubeat.discovery`'yi adlandırıyor;
Actions adımı `--set backup.enabled=true`'yu taşıyor.

Kapandı (2026-11-08, S93 [#160](https://github.com/yusufceng/Kubeat/issues/160), 0.53):
README "Aracın kendi sağlığı" → "Metrikler" tablosunda
`kubeat_target_scope_denied_total{reason,source}` satırı (`reason` ve `source` değerleri,
"Kapsam ihlali denetimi"ne bağlantı). Eksik öteki seriler S100'de kalır.

## Kapsam reddinde reddedilen host'u yöneticiye monitor ayrıntısında göstermek (S93 takibi)

2026-11-08'de ertelendi (S93 [#160](https://github.com/yusufceng/Kubeat/issues/160),
issue'nun kapsam dışı; ADR-0003 (d) maskesi, ayrı karar). 0.53'ten beri yönetici kontrol
anı reddinin nedenini (`resolve_error` ya da `connection`) monitor ayrıntısında, sonuçlarda
ve olayın zaman çizelgesinde görür; reddedilen host'u görmez. Host bugün yalnız yöneticiye
açık denetim kaydında (monitor ve neden başına ilk ret) ve backend log'unun `WARN`
satırındadır. Sonuçta host'u tutmak ADR-0003 (d)'nin "sonuç bir iç servisin yanıtını
okumanın yolu olmasın" kuralına ve S77'nin tek genel mesaj kararına karşı tartılır.
Tetikleyici: denetim kaydında ya da logda host'u bulmakta zorlanan bir yöneticinin geri
bildirimi ya da ADR-0003 (d)'ye dokunan bir sonraki madde.

## OpenShift'te EgressIP ve vekil ACL reçetesi (S82 takibi)

2026-11-08'de ertelendi (S82 [#143](https://github.com/yusufceng/Kubeat/issues/143); Müşteri
A'nın sprint 8 bulgusu 3'ünün ilk yarısı; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)). 0.52'nin `server resolve`'u vekilli
kurulumun NXDOMAIN ön şartını sınıyor. Çıkış adresi (EgressIP) ve vekil ACL'i için bir
OpenShift reçetesi, backend pod'undan node adresine ve API VIP'ine red bekleyen iki sınama
satırı yok. #143'ün kabul ölçütünde değildi, PO'ya bırakıldı. Tetikleyici: vekilli kurumsal
OpenShift provası ya da pilotun "Vekilli kapsam" adımını koşan bir kurulum.

## Kapsama bağlı monitor başına HTTP taşıyıcısını kaldırmak (S92 takibi)

2026-11-08'de ertelendi (S92 [#144](https://github.com/yusufceng/Kubeat/issues/144);
[sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)). 0.52'de her HTTP kontrolü bağlantılarını kapatıyor, monitor başına taşıyıcı yapısı
PO kararıyla kaldı. 2000 kapsama bağlı monitor'de ~8 MiB heap kalıyor (monitor başına ~4
KiB); taşıyıcıyı kontrol başına klonlamak bunu sıfırlar. S42 (keşif belleği) ile birlikte
tartılır. Tetikleyici: S42 ya da bellek limitine yaklaşan gerçek bir kurulum.

## Kaynak tavanının kalanları: öteki yüzeyler, açık değerin bildirimi ve ret satırının metni (S88 takibi)

2026-11-08'de ertelendi (S88 [#145](https://github.com/yusufceng/Kubeat/issues/145) ve
Müşteri A'nın #145 puan yorumu; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)).

- Tavan yalnız girişi kapsar; durum sayfası kilidi ve parola değiştirme kaynak + ad
  sınırında (S76) kalır (issue kapsam dışı).
- LDAP tanımlıyken açık bir değer (>0) açılışta bildirilmiyor; yalnız eksikken `WARN`,
  `0`'da `INFO` var.
- Tavan reddinin `WARN`'ı `throttle=source_total` alanını taşıyor ama metni "the name is
  locked" diyor (sandbox, sprint 10 prova (b)); tavan için kaynağı adlandıran bir metin.

Tavanın chart kuralı S85 dilim 2'de, denetim satırı "Kaynak tavanının ilk reddi için
denetim satırı"nda. Müşteri A NAT, VDI ve vekil arkasındaki kullanıcıları yüzünden tavanı
`0`'da tutacak. Tetikleyici: tavanı açan bir kurulum ya da `throttle.go`/`auth.go`'ya dokunan
bir sonraki madde (metin için).

## `hops_too_low`'u önde başlık yazan katmanların çıkış aralıklarına genişletmek (F5/WAF önlü Route; S85 takibi)

2026-11-08'de ertelendi (S85 dilim 1 [#146](https://github.com/yusufceng/Kubeat/issues/146);
Müşteri A, sprint 10 bulgusu 2, önemli; [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)). `hops_too_low` yalnız anahtarlanan kaynak
`UPTIME_PUBLIC_URL` host'unun çözüldüğü adreslerden biriyse sayılır. Router'ın önünde F5 ya
da WAF varsa katman router'a kendi çıkış adresinden (SNAT havuzu) bağlanır: değer `2` yerine
`1` olsa da sayaç, `WARN` ve kural susar ve throttle bütün tarayıcılar için tek kilide düşer.
LDAP'lı kurulumda bu, dizin hesap kilidine giden yol (README "Yanlış ayar sessizce etkisiz
kalmaz"). A kayıtlı bir CDN'in arkası da yakalanmaz. Öneri: opt-in bir ayar önde başlık
yazan katmanların bilinen çıkış aralıklarını (CIDR) alır ve kaynak bu aralıktaysa
`hops_too_low` sayılır. Ayar `X-Forwarded-For`'a güven vermez, yalnız sinyali besler ("XFF
güveni için cluster dışındaki güvenilir vekillerin CIDR listesi" maddesinden ayrı). Satır
pilot reçetesinde ve NOTES'ta F5/WAF satırının yanında yer alır. Ajan içinde minikube'da
`ingress-nginx` önüne başlık yazan ikinci bir nginx ile ölçülebilir; gerçek F5 kullanıcıda.
Tetikleyici: sprint 11'de boş bir slot (yedek) ya da F5/WAF önlü bir kurulumun pilotu.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
S101 olarak hikâyeleşti. GKE'deki `externalTrafficPolicy: Cluster` durumu da kapsama girdi
(Müşteri B, sprint 11 keşfi). Sprint 11'e girmedi, çünkü dört slot dolu; sonraki sprint
kuyruğunda 4. sırada. Ara önlemler sprint 11'de:

- `docs/pilot.md` "Hop değeri"nde uydurma XFF'li elle doğrulama (S98,
  [#162](https://github.com/yusufceng/Kubeat/issues/162));
- README GKE kontrol listesinde "`externalTrafficPolicy: Cluster`'da sinyal susar" cümlesi
  (S95, [#161](https://github.com/yusufceng/Kubeat/issues/161)).

Güncelleme (2026-11-08, S95 [#161](https://github.com/yusufceng/Kubeat/issues/161)): README
"GKE kontrol listesi"nin 4. maddesi cümleyi yazıyor; S101 gelince o madde güncellenir.
Tetikleyici değişmedi.

Güncelleme (2026-11-10, S98 [#162](https://github.com/yusufceng/Kubeat/issues/162)): ara
önlem yazıldı. `docs/pilot.md` "Hop değeri" F5/WAF önlü Route'ta eksik değerin alarmı
susturduğunu ve değerin başlıksız ve uydurma `X-Forwarded-For`'lu iki giriş satırıyla elle
doğrulandığını söylüyor; `docs/openshift-dogrulama.md` "Vekilli ve hava boşluklu OpenShift"
5. adım ve `docs/gke-dogrulama.md` 5. adım (`externalTrafficPolicy: Cluster`) aynı
doğrulamayı komut ve beklenen çıktıyla veriyor. S101 gelince üçü güncellenir. Tetikleyici
değişmedi.

## İki cluster'lı promtool senaryoları: `KubeatSignInSourceDegraded` ve `KubeatDiscoveryNamespaceSkipped`

2026-11-08'de ertelendi (S85 dilim 1 [#146](https://github.com/yusufceng/Kubeat/issues/146)
ve S43 [#147](https://github.com/yusufceng/Kubeat/issues/147); [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)).
`deploy/helm/prometheusrule-cluster-test.yaml`'da 0.52'nin iki yeni kuralı için iki
cluster'lı senaryo yok. `clusterLabel`'lı render `test-chart.sh`'ta `--lint-fatal` ile
denetleniyor. Tetikleyici: kurallara dokunan bir sonraki madde (S85 dilim 2, S93) ya da
`clusterLabel` kullanan bir kurulumda yanlış toplanan bir alarm.

Güncelleme (2026-11-08, sprint 11 planı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)):
tetikleyici doldu. İki senaryo S93'ün
([#160](https://github.com/yusufceng/Kubeat/issues/160)) kabul ölçütünde; madde #160 merge
edilince kapanır.

Kapandı (2026-11-08, S93 [#160](https://github.com/yusufceng/Kubeat/issues/160), 0.53):
`prometheusrule-cluster-test.yaml`'da `KubeatSignInSourceDegraded` (bir cluster'ın
`hops_zero`'su, tekilleştirilmemiş HA çiftinde tek alarm; öteki cluster'ın arayüzden
gelmeyen istekleri yakmaz), `KubeatDiscoveryNamespaceSkipped` ve `KubeatScopeResolveErrors`
için iki cluster'lı senaryolar.

## Kurum CA'sı reçetesinin kalanları: chart'ın ConfigMap'i üretmesi ya da `trustedCA` değeri, annotation ile monitor başına CA (S2 takibi)

2026-11-08'de ertelendi (S2 [#163](https://github.com/yusufceng/Kubeat/issues/163), PO
kararı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)). S2 0.53'te
yalnız reçete ve test olarak geliyor. Reçetede kurum CA'sı `backend.extraVolumes` ile
bağlanır ve `SSL_CERT_DIR` ile sistem demetine eklenir. OpenShift'te demet
`config.openshift.io/inject-trusted-cabundle` etiketli bir ConfigMap'ten gelir.

Ertelenenler:

- chart'ın ConfigMap'i bir değerle üretmesi (ör. `trustedCA`);
- annotation ile monitor başına CA.

Sprint 2 kararları korunur: init container yok, keşif sayfasından reçeteye bağlantı yok.
GitOps'ta ConfigMap tek bir nesne olduğundan chart değeri bugün kazanç getirmiyor (Müşteri
A: "kod gerekmiyor"). #163 merge'de ertelediği öteki parçaları buraya ekler. Tetikleyici:
reçeteyi yetersiz bulan bir kurulum, örneğin aynı cluster'da farklı CA'lara güvenmesi
gereken monitor'ler.

Güncelleme (2026-11-08, #163): reçete README "OpenShift" → "Kurum CA'sı"nda; Go davranışı
`backend/internal/checker/trustedca_test.go` alt süreç testinde, values'u
`deploy/helm/test-chart.sh`'ta. #163'ün ertelediği parçalar, tetikleyicileriyle:

- **Demetin yeniden başlatmadan okunması.** Go kökleri süreçte bir kez okur; ConfigMap
  değişince (CA eklenir ya da döner, OpenShift demeti yeniler) backend
  `rollout restart` ister. README bunu bilinen bir adım olarak yazar. Seçenekler: checker
  `SSL_CERT_DIR`'i kendisi izleyip havuzu yeniler, ya da ConfigMap'in özetini pod
  şablonuna annotation olarak koyan bir chart değeri (yalnız chart'ın ürettiği nesnede
  çalışır, bu yüzden "chart'ın ConfigMap'i üretmesi"yle birlikte düşünülür). Tetikleyici:
  yeniden başlatma unutulduğu için `tls_unknown_authority` ile düşen monitor'ler ya da
  CA'sı sık dönen bir kurulum.
- **mTLS istemci sertifikası** (#163'te kapsam dışı). Kontroller istemci sertifikası
  sunamaz; böyle bir hedef `tls_handshake_failed` verir. Tetikleyici: istemci
  sertifikası isteyen bir hedefi izlemek isteyen bir kurulum.

## Sprint 6 kuyruğundan backlog'a girmeden kalan maddeler: S23, S40, S44, S57, S61

2026-11-08'de eklendi (sprint 11 planı;
[sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md); sprint 10 retrosu
"değişecek 2"nin taraması). Sprint 6'nın "kuyruğun devamı" listesindeki
([sprints/2026-11-05-sprint-06.md](sprints/2026-11-05-sprint-06.md) "Yapılmayacaklar" ve
retro önerisi) şu hikâyeler sprint 7 kuyruğuna taşınmadı ve backlog'a da girmedi. Aynı
listeden S2 bu sprintte geri geldi (#163). S7 ve S34 zaten backlog'da, S42 kendi
bölümünde; S43 0.52'de yapıldı. Kod durumu 2026-11-08'de main `8bfbacc`'de yeniden okundu.

- **S23: `ldap://` ile StartTLS kapalı birleşimine uyarı, sonra ret.**
  - Önem: SRE orta, Müşteri A hoş olur (sprint 1–4).
  - Durum: values örneği `ldap://` ile `startTLS: false`'u yan yana veriyor
    (`deploy/helm/kubeat/values.yaml:339-342`). Backend bu birleşimde açılışta uyarmıyor;
    `backend/internal/ldapauth/ldapauth.go:91` yalnız şemayı denetliyor. Bu birleşimde
    parola dizine düz metin gider.
  - Sprint 2 kararı: önce açılış `WARN`'ı, NOTES satırı ve `ldaps` örneği; reddetmek
    sonraki minor'da, yükseltme notuyla. Pilot `ldaps` kullanıyor.
  - Tetikleyici: `config.go`'ya ya da `ldapauth`'a dokunan bir madde, ya da `ldap://` ile
    kurulan dizinli bir kurulum.
- **S40: yeniden başlatmada eski kesintinin gecikmeli ya da tekrarlı kanala duyurulması.**
  - Önem: Müşteri A ve SRE orta, B düşük (sprint 3–5).
  - Durum: kanal kesinti açılırken kapalı ya da kapsam dışıysa, yeniden başlatma ona eski
    kesintiyi yine duyuruyor. "Kesintiden önce var olan kanal" kuralı (`CreatedAt`) yalnız
    gecikmesiz ve tekrarsız kanallarda uygulanıyor
    (`backend/internal/alert/schedule.go:887-903`).
  - Sprint 2 PO kararı ("kapsamı sonradan genişleyen eski kanal alabilir") S40 alınınca
    tersine çevrilir. Bu, "S25'in incelemesinde kalan bildirim köşeleri"nin 2. köşesi.
  - Tetikleyici: `alert` paketine dokunan bir sonraki madde ya da pilotta yeniden
    başlatmadan sonra yanlış kanala giden bir kesinti.
- **S44: raporlarda takvim ayı.**
  - Önem: Müşteri A önemli, B düşük (sprint 3–5).
  - Durum: rapor yalnız son N günü, UTC'de ve o anki saatle biten bir dönemi veriyor
    (`backend/internal/api/report.go:91-105`). README "Raporlar" ise CSV'yi ekiplere giden
    aylık rapor için anlatıyor.
  - Karar taslağı (BA varsayılanı, sprint 3): API `from`/`to` alır, "Geçen ay"
    tarayıcının saat diliminde hesaplanır. Sınır yol haritası "Bilinen sınırlar"da yazılı.
  - Tetikleyici: aylık SLA raporunu Kubeat'ten alan bir kurulum ya da raporlara dokunan
    bir madde.
- **S57: tek seferlik sertifika uyarısının bir teslimat hatasında kaybolması.**
  - Önem: Müşteri B orta (sprint 4–5).
  - Durum: kanal almazsa sertifika, hatırlatma ve yavaşlık bildirimi bir kez daha
    denenmiyor (`backend/internal/alert/alert.go:162-164`); bir Slack kesintisi tek uyarıyı
    yutar.
  - Cluster içi yedek yol var: chart kuralları açıkken `KubeatCertificateExpiring`. `alert`
    paketinde sıra S40'tan sonra.
  - Tetikleyici: `alert` paketine dokunan bir sonraki madde ya da sertifika uyarısının
    kaybolduğu bir olay.
- **S61: kurulum notunun uyarılarını backend'in açılış log'una da yazmak.**
  - Önem: Müşteri A ve SRE önemli, B orta (sprint 4).
  - Durum: belge kısmı 0.46'da geldi (README "GitOps ile kurulum" → "Kurulum notları
    görünmez"). Kod kısmı yapılmadı: chart'tan yeni bir ortam değişkeniyle backend aynı
    uyarıları log'a yazacaktı.
  - 0.49–0.52'nin açılış `WARN`'ları (hop, vekil, LDAP tavanı) bir kısmını karşılıyor.
    fsGroup, heartbeat ve kural yokluğu, yedek ve Thanos 9091 uyarıları log'da yok.
  - "Kurulum notu uyarılarını Argo CD'de sync hatası olarak göstermek (S95 takibi)" ile
    birlikte tartılır.
  - Tetikleyici: o madde ya da NOTES'u görmeyen bir kurulumda kaçırılmış bir uyarı.

## Beyan edilen cluster ağlarının kapsam denetiminin cluster içi sınıflamasını beslemesi (S96 takibi)

2026-11-08'de ertelendi (S96 [#158](https://github.com/yusufceng/Kubeat/issues/158), PO
kararı; Müşteri B'nin sprint 10 keşfi;
[sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)). 0.53'ün
`targetScope.clusterNetworks` beyanı yalnız kurum aralıklarının cluster ağlarıyla
çakışmasını denetler (render anında ve açılışta). Müşteri B, beyan edilen ağların kapsam
denetiminin cluster içi sınıflamasını da beslemesini önerdi. Bu, kapsamın güvenlik anlamını
değiştirir ve ayrı bir ADR kararı ile güvenlik incelemesi ister. Ayrıntı herkese açık metne
yazılmaz (gizlilik); değerlendirme SRE'nin yerel kaydında. Tetikleyici: beyanı kullanan bir
kurulumda sınıflamanın yetersiz kaldığı bir vaka ya da ADR-0003'e dokunan bir sonraki madde.

## Render anındaki cluster ağı denetimini IPv6'ya genişletmek (S96 takibi)

Sprint 11'de ertelendi (S96 [#158](https://github.com/yusufceng/Kubeat/issues/158), PO
kararı; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)). 0.53'te
`targetScope.clusterNetworks` ile beyan edilen bir ağla çakışan kurum aralığını chart
yalnız IPv4'te render hatasıyla durduruyor: sprig'de CIDR fonksiyonu yok ve önek
aritmetiği `_helpers.tpl`'de 32 bit için yazıldı. IPv6 ağı ve aralığı render'da
karşılaştırılmıyor; backend açılışta karşılaştırıp çakışan girdiyi yok sayıyor (`ERROR`,
`kubeat_target_scope_external_ranges_ignored`, `KubeatScopeExternalRangesIgnored`), yani
çakışma Argo CD'de sync hatası değil çalışma anı alarmı olarak görünüyor (README "Kurum
ağı", "Cluster ağlarının beyanı", "Sınır"). 128 bitlik aritmetik şablonda grup grup
yazılabilir; bedeli şablonun karmaşıklığı. Tetikleyici: beyanı IPv6 ya da çift yığınlı
bir cluster'da kullanan bir kurulum, ya da IPv6 çakışmasının render'da durmasını isteyen
bir GitOps kullanıcısı.

## API VIP'ini ve tek adresleri cluster ağı beyanıyla denetlemek (S96 takibi)

Sprint 11'de ertelendi (S96 [#158](https://github.com/yusufceng/Kubeat/issues/158),
developer varsayımı, SRE incelemesine açık; [sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)).
0.53'ün `targetScope.clusterNetworks` beyanı pod, Service ve node ağlarını alıyor; API
VIP'i (cluster dışından API'ye ulaşılan adres) beyanda yok. Kurum aralıklarındaki tek
adres (`/32`, `/128`) beyanla karşılaştırılmıyor, çünkü README "Şart" node ağındaki router
VIP'ini böyle, bilerek yazdırıyor; yasaklamak on-prem Route yolunu kapatırdı. Bu yüzden
API VIP'i ya da node ağındaki bir node'un adresi `/32` olarak listeye girerse ne render ne
açılış yakalar (yalnız API sunucusunun ClusterIP'si ve pod'un kendi adresi yakalanıyor);
pod ya da Service ağındaki tek adres de öyle. Seçenekler: beyana bir `apiVIP` alanı;
tek adres istisnasını yalnız node ağına daraltmak (backend'in ağ türünü bilmesi, yani ayrı
bir ayar ya da değer sözdizimi ister); router VIP'i için açık bir değer. Tetikleyici: pilotta
ya da provada API VIP'inin ya da bir node adresinin `/32` olarak listeye girdiğinin
görülmesi, ya da SRE incelemesinin istisnayı daraltma kararı.

## Kurulum notu uyarılarını Argo CD'de sync hatası olarak göstermek: koşulları bir manifest şablonuna taşımak (S95 takibi)

2026-11-08'de ertelendi (S95 [#161](https://github.com/yusufceng/Kubeat/issues/161), PO
kararı; Müşteri A'nın sprint 11 keşfindeki yeni bulgu 5, hoş olur;
[sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)). Kurulum notları
(`NOTES.txt`) Argo CD'de ve OpenShift GitOps'ta render edilmez. Yalnız NOTES'ta duran bir
`fail`, örneğin bir `notes.failOnWarning` değeri, orada etkisizdir. 0.53 bunun yerine bir
CI tarifi verir: README "Kurulum notları görünmez"de `helm install --dry-run=client` ile
notları basıp WARNING'leri saymak.

Uyarıyı sync hatasına çevirmek, koşulları render anında `fail` eden bir manifest
şablonuna taşımayı ister. Bu, NOTES'un bugün yalnız uyardığı değerleri (fsGroup, hop, arayüz
politikası) render hatasına çevirir ve var olan kurulumları kırabilir; opt-in bir değer ve
ayrı karar ister. S61 (uyarıları backend log'una yazmak; "Sprint 6 kuyruğundan backlog'a
girmeden kalan maddeler") ile birlikte tartılır. Tetikleyici: CI tarifini yetersiz bulan bir
GitOps kurulumu.

Güncelleme (2026-11-08, S95 [#161](https://github.com/yusufceng/Kubeat/issues/161)): CI
tarifi geldi, ama `helm install --dry-run=client` ile değil. `helm install` `--api-versions`
almıyor (Helm 3.22.1 ve 4.3.1). Helm 3'te `--dry-run=client` cluster'a da bağlanıyor
(3.22.1'de `/version`'a gittiği bir dinleyiciyle ölçüldü). Tarif bunun yerine chart'ın bir
kopyasında `NOTES.txt`'i bir ConfigMap'e sarıp `helm template --api-versions` ile render
ediyor; `test-chart.sh` README'deki bloğu Helm 3 ve Helm 4 ile koşuyor. Tetikleyici
değişmedi.

## Cluster DNS kesintisinde Kubeat'in kendi kanal bildirimlerini gruplamak ya da bastırmak (S97 takibi)

2026-11-08'de eklendi (sprint 11 planı, PO;
[sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)). 0.52'den beri her HTTP
kontrolü bağlantısını kendisi açtığı için adı her kontrolde çözülüyor. Cluster DNS'i
kesilince her HTTP monitor'ü kendi zaman aşımında, aynı turda DOWN oluyor (sprint 10 prova
(a), minikube). Her ekibin kanalına ayrı bir kesinti bildirimi gidiyor; çok kiracılı
kurulumda bu 60 ekip demek.

S97 (sonraki sprint) nedene göre bir sayaç ve tek bir "cluster DNS" chart alarmı getirir;
Kubeat'in kendi kanal bildirimlerine dokunmaz. Bildirimleri bastırmak ya da gruplamak olay
doğruluğunu değiştirir: aynı anda gerçek bir kesinti de olabilir. "İzleme asla durmaz"
ilkesine karşı tartılması gereken ayrı bir karar. Ara yol, S97'nin kuralı sürerken
Alertmanager `inhibit_rules` ile `KubeatMonitorDown`'ı bastırmak; bu yalnız chart
kurallarını alan kurulumda işe yarar. Tetikleyici: S97 geldiğinde onun ölçütüne kesin
bağlanır, ya da DNS kesintisinde kanal fırtınası yaşayan bir kurulum.

## Sprint 11 kapanış bulguları: 1.0 metin turu (altı belge maddesi)

2026-11-11'de eklendi (sprint 11 kapanışı, PMO;
[sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md) "Sprint sonu" → "Yeni
müşteri bulguları"). Altısı da belge, kod yok; ayrıntı ve kanıt satırları sprint dosyasında.

- README "Güvenlik" → "Çok kiracılı ve internete açık kurulum" listesine aynı biçimde dört
  satır: backend `networkPolicy.enabled`, `UPTIME_TARGET_DENY_CIDRS` (derinlemesine savunma),
  imza doğrulamalı aynalama, `backup.enabled`. Müşteri A; S66'nın 60 satır sınırını ~8 satır
  aşar, PO kararı.
- `docs/openshift-dogrulama.md` "Vekilli ve hava boşluklu OpenShift" 6(b) adımı koştuğu
  cluster'da kapsamı geçici genişletiyor: "yalnız üretim dışı cluster'da ya da ayrı bir test
  release'inde" diye işaretlenir; üretim için 6(a) ve beyan çakışmasının lokal `helm
  template`'te durması; geri bildirim şablonunda "6 Drain" (a)/(b) ayrılır. Müşteri A.
- S75'in yükseltme penceresi README "Durum sayfaları" → "Sınırlar"da ve runbook madde 16'da
  nedene bağlı yazılır (imaj node'da hazırsa ~4 sn; çekiliyorsa ya da pod node değiştiriyorsa
  ~25 sn); madde 16 ölçümle kaydedilecek yan verileri sayar (`oc get events` Pulled süresi,
  eski ve yeni pod'un node'u, PVC bağlanma süresi). Müşteri A.
- README "Küçük ekip, tek kiracılı kurulum" kutusuna lisans satırı (ücretsiz katman yeter: bir
  durum sayfası; LDAP/OIDC, namespace sınırı, denetim kaydı ve çoklu sayfa lisanslı) ve
  betikler için "API" bağlantısı; "OpenShift" satırına "Kurum CA'sı reçetesi hariç, OpenShift
  dışında da geçer" notu. Müşteri B.
- README "Cluster ağlarının beyanı", `values.yaml` `targetScope.clusterNetworks` yorumu ve
  `docs/gke.md` "Değerlerin gerekçesi"ne GKE okuma komutları (`gcloud container clusters
  describe … --format='value(clusterIpv4Cidr,servicesIpv4Cidr)'`; node ağı için subnet'in
  `ipCidrRange`'i). Müşteri B.
- `docs/gke-dogrulama.md` adım 2'nin negatif testi curl imajının sabit referansını kendisi
  yazar; `scripts/check-versions.sh` (#180) onu da denetler. Müşteri B.

Tetikleyici: 1.0 kararı. Seçenek 1'de (1.0'ı 0.53.0'dan çıkarmak) metin turunun PR'ı; seçenek
2'de sprint 12 keşfinin girdisi. İki durumda da altı madde o turla kapanır ve bu bölüm kapanış
notu alır.

Kapanış (2026-11-11, release manager; PR [#184](https://github.com/yusufceng/Kubeat/pull/184),
BA ve SRE "yalnız bu diff" onayıyla): 1, 2, 4, 5 ve 6 kapandı. 3'ün iki parçası kapandı (madde 16
kaydedilecek yan verileri sayar; README "Sınırlar" ve madde 16 "farkın nedeni ölçülmedi" der ve
pencereyi uzatan etkenleri sayar); nedensel cümle ("imaj node'da hazırsa ~4 sn; çekiliyorsa ya da
pod node değiştiriyorsa ~25 sn") **ertelendi**: iki ölçümde imaj çekme süresi, pod'ların node'u ve
volume'un bağlanma süresi kaydedilmemişti, cümle kanıtsız olurdu. Tetikleyici: madde 16'nın yan
verileriyle kaydedilen ilk yükseltme (sandbox'ta bir sonraki sürüm ya da bir müşterinin geri
bildirimi); o zaman README "Durum sayfaları" → "Sınırlar" ve yol haritası "Bilinen sınırlar" ilk
girdisi nedene bağlı yazılır. Aynı PR'ın yol haritası "Bilinen sınırlar"a eklediği "ölçüm yok"
girdisinin tetikleyicisi yol haritası satır 32'dir (iki runbook'un koşulması).

Kapanış 2 (2026-11-11, sandbox'ın 0.53.1 yükseltmesi): tetikleyici aynı gün geldi, neden ölçüldü
ve yazıldı. Pencere ~25 sn (kötü örnek aralığı 25,4 sn, yoklama ~2 sn aralıklı; eski pod'un
`Killing`'inden yeni pod'un `Ready`'sine 28 sn); yeni pod başka bir node'a düştü, veri ve yedek
volume'ları 17 ve 21 sn'de bağlandı (`FailedAttachVolume … Multi-Attach error` beklemesi; container
ikincisini de bekledi), imaj 1,1 sn'de çekildi (170 MB), backend Started→Ready 4 sn. Müşterinin
cümlesi düzeltilmiş biçimiyle
yazıldı: fark imajın çekilmesinden değil pod'un node değiştirmesinden (volume'un yeniden
bağlanması) geliyor; aynı node ~4 sn, başka node ~25 sn. README "Durum sayfaları" → "Sınırlar",
runbook madde 16, yol haritası "Bilinen sınırlar" ve pilot satırı güncellendi; 3 tamamen kapandı.

## Annotation monitor'ünün geçmişi için silme öncesi bekleme süresi (S102)

2026-11-11'de eklendi (1.0 kararı; sprint 11 "Yapılmayacaklar" 6. sıra,
[sprints/2026-11-08-sprint-11.md](sprints/2026-11-08-sprint-11.md)). Annotation kaldırılınca ya da
nesne silinince monitor geçmişiyle birlikte hemen silinir (README "Keşif"); GitOps kazasında SLA
geçmişi gider. İş L: "annotation kalkınca monitor de kalkar" tasarım kararını değiştirir, yeni bir
durum (bekleyen silme), ayar ve arayüz ister. Yol haritası "Bilinen sınırlar" → "1.0'da
yapılmayanlar". Tetikleyici: GitOps kazasında geçmişi silinen bir kurulum ya da SLA geçmişinin
korunmasını isteyen ilk müşteri.

## Kontrol başına bağlantının kapasite hesabı belgesi (S99)

2026-11-11'de eklendi (1.0 kararı; sprint 11 "Yapılmayacaklar" 8. sıra). 0.52'den beri her HTTP
kontrolü kendi bağlantısını açar; CPU, DNS sorgu hızı ve vekil (`CONNECT`) yükünün monitor sayısı ve
aralığa göre hesabı tek belgede yok. GKE'nin Cloud NAT ve DNS notu README "Bağlantı yükü (GKE)"de,
ölçüm adımları iki runbook'ta, 0.51–0.52 ölçümü [yuk-testi.md](yuk-testi.md)'de. Tetikleyici:
worker doluluğu (`KubeatWorkersSaturated`), DNS ya da vekil yükü soran ilk kurulum.

## Port-forward girişinde kaynak ayrımı kuralının çaresi (S104)

2026-11-11'de eklendi (1.0 kararı; sprint 11 "Yapılmayacaklar" 10. sıra). Yalnız `port-forward` ile
erişilen bir kurulumda `KubeatSignInSourceDegraded` kuralı hop değerini düzeltmeye iter; doğru değer
README "Sayaç ve kural" → "Port-forward"ta. Kurulum notu ve kural metni bu durumu ayırmaz.
Tetikleyici: `NOTES.txt`'ye dokunan bir sonraki madde ya da yalnız port-forward'la erişilen bir
kurulumun bulgusu.
