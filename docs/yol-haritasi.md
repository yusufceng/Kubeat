# Yol haritası

[Proje planı](proje-plani.md) üzerine, MVP'den sonra kararlaştırılan sıra.
Ertelenen fikirler [backlog.md](backlog.md) dosyasındadır.

| # | Adım | Durum |
| --- | --- | --- |
| 0 | Temel: scheduler, HTTP/TCP kontrolleri, SQLite, olay ve webhook, arayüz, Helm chart | Tamamlandı |
| 1 | Keşif: Service/Ingress/Route'ları bulma, namespace görünümü | Tamamlandı |
| 2 | Arayüz derinliği: grafik, uzun dönem uptime, düzenleme/durdurma, HTTP seçenekleri, sertifika süresi | Tamamlandı |
| 3 | Alarm olgunluğu: Slack/Teams/e-posta, bakım pencereleri, susturma, hatırlatma | Tamamlandı |
| 4 | Aracın kendi sağlığı: `/metrics`, heartbeat | Tamamlandı; sprint 1 (S9): düşen sonuç, başarısız bildirim, dolu worker, duran ve yaşlanan yedek, biten lisans PrometheusRule'da alarm (bkz. 19); sprint 2 (S32): heartbeat sonuçlar yazılamazken de kesilir (sonuç gelmeyen aralık sağlıklı sayılır), veri PVC'si için boyut kuralı README "Geçmiş verisi"nde; sprint 3 (S38, #50): heartbeat'in kararı `kubeat_self_healthy` gauge'unda ve heartbeat'siz kurulumda da `critical` yanan `KubeatUnhealthy` kuralında (bkz. 19), `UPTIME_NOTIFY_QUEUE_SIZE` varsayılanı 1000 ve etkin monitor sayısı kuyruğu aşınca bir kez WARN, heartbeat de kurallar da kapalıysa ve yedek kapalıysa kurulum notunda uyarı |
| 5 | Cluster metrikleri: monitor'un arkasındaki pod'lar, Prometheus/Thanos'tan CPU ve bellek | Tamamlandı |
| 6 | Agent ve çoklu cluster: her cluster'da hafif agent, merkeze push | Ertelendi: şimdilik tek cluster yeterli (2026-10-12) |
| 7 | Üretime hazırlık: API koruması, yük testi, yedekleme, yönetici parolasıyla giriş | Tamamlandı |
| 8 | Kontrol motorları: HTTP genişletmeleri, TLS'li TCP, ping, DNS, push, gRPC, WebSocket; tekrar aralığı, ters mod, kopyalama | Tamamlandı |
| 9 | Çalışma alanı arayüzü: kenar çubuğunda aranabilir monitor listesi, genel bakış, zengin monitor sayfası, tüm tipler için ekleme/düzenleme formu, tema | Tamamlandı |
| 10 | Etiketler, gruplar ve toplu işlemler | Tamamlandı |
| 11 | Durum sayfaları ve rozetler | Tamamlandı |
| 12 | Tekrarlayan bakım pencereleri | Tamamlandı |
| 13 | Bildirim sağlayıcıları: Telegram, Discord, PagerDuty, Mattermost, Google Chat, ntfy, Gotify; kanal düzenleme ve namespace'e göre yönlendirme | Tamamlandı |
| 14 | Veritabanı kontrolleri: PostgreSQL, MySQL/MariaDB, Redis | Tamamlandı |
| 14b | Kubernetes workload tipi: hazır pod sayısı, keşifte workload'lar | Tamamlandı |
| 14c | MongoDB, Kafka, RabbitMQ kontrolleri | Planlandı |
| 16 | Gizli değerlerin veritabanında şifrelenmesi | Tamamlandı |
| 15 | Kişisel hesaplar, LDAP girişi, namespace bazlı roller, değişiklik kaydı | Tamamlandı |
| 15b | Ayarlar ekranı, kişi başına API anahtarları | Planlandı |
| 17 | Annotation ile sürekli keşif: `kubeat.io/monitor` taşıyan nesneler kendiliğinden izlenir | Tamamlandı |
| 18 | Pod bazında kontrol: Service'in arkasındaki her hazır pod'a aynı kontrol (`each_pod`) | Tamamlandı |
| 19 | Cluster Alertmanager'ı ile alarm: chart'ta PrometheusRule | Tamamlandı; sprint 1 (S9) öz-sağlık kurallarını ekledi: `KubeatResultsDropped`, `KubeatNotificationsFailing`, `KubeatWorkersSaturated`, `KubeatBackupFailing`, `KubeatBackupStale`, `KubeatLicenseExpiring`; sprint 2 (S29): `KubeatDown` backend pod'u hiç yokken de yanar, `KubeatMonitorDown` ve `KubeatCertificateExpiring` 10 dakikaya kadar süren scrape boşluğunda çözülmez ve Recreate'te tek alarm kalır, `KubeatBackupFailing` yedek düzelene kadar açık kalır; kurallar promtool ile sınanır (`test-chart.sh`, CI); sprint 3 (S38, #50): `KubeatUnhealthy` (`critical`, `kubeat_self_healthy` 5 dakika 0); (S37, #50): `metrics.prometheusRule.clusterLabel` ile kurallar birden çok cluster'ın tek deposunda (Grafana Cloud/Mimir, global Thanos Ruler) cluster başına değerlendirilir, `KubeatDown` 24 saatlik yokluk penceresiyle; boşken 0.44'teki gibi; iki cluster'lı ikinci promtool senaryo dosyası; (S26, #49): `KubeatNotificationsFailing` kanal başına (`channel`, `channel_type`) ve `for: 5m` ile yanar, kanalın son denemesi başarısızken ya da almadığı bir kesinti yeniden gönderilmeyi beklerken açık kalır, scrape boşluğunda çözülmez (`kubeat_channel_last_delivery_ok`, `kubeat_channel_undelivered_outages`), `clusterLabel` verilince cluster ve kanal başına toplanır |
| 20 | Olay bağlamı: monitor sayfasında pod'ların ve workload'un cluster olayları | Tamamlandı |
| 21 | Ekip raporları: namespace başına uptime ve kesinti özeti, CSV | Tamamlandı |
| 22 | Alarm yönlendirme: kanalda etiket ve olay süzgeci, namespace başına gruplama, monitor sayfasında "alarm nereye gider"; workload monitor'da rollout toleransı | Tamamlandı |
| 23 | SSO: OpenID Connect ile giriş (PKCE), gruplardan rol | Tamamlandı |
| 24 | Yayım: sürüm etiketiyle GHCR'ye çok mimarili imajlar ve OCI chart; gerçek cluster kurulumunun ön koşulu | Tamamlandı |
| 25 | İki dil: arayüz (tarayıcıya göre, değiştirilebilir) ve bildirimler İngilizce/Türkçe | Tamamlandı |
| 26 | Namespace'le sınırlı keşif: cluster-admin olmadan kurulum (Role), Thanos'un namespace ucu | Tamamlandı |
| 27 | Open-core: Kubeat Enterprise lisansı, çevrimdışı imzalı lisans dosyası ([ADR-0002](adr/0002-open-core-lisans.md)) | Tamamlandı |
| 28 | Pilot: gerçek bir cluster'da üç hafta kullanım ([pilot.md](pilot.md)) | Developer Sandbox'ta (2026-10-27'den itibaren; 2026-10-28'de 0.43.0, doğrulanmış yedekten sonra 2026-10-29'dan beri 0.45.0; sprint 3'ün alarm provası (2) sandbox UWM'de geçti; pilot değerleri; karar 2026-11-27); şirket cluster'ı ve ekip olmadığından geri bildirim sprint ekibinin müşteri ve SRE ajanlarından |
| 29 | Sprint ekibi: müşteri, SRE, BA, PO, PMO ve developer ajanlarıyla düzenli sprintler ([agents/sprint-takimi.md](agents/sprint-takimi.md), [sprints/](sprints/)) | Sprint 1 (2026-10-27–28) tamamlandı, 0.43.0: bildirim teslimatı (S18, S17), yedek dayanıklılığı (S19), öz-sağlık alarmları (S9); puanlar 4,0/5 ve retro [sprints/2026-10-27-sprint-01.md](sprints/2026-10-27-sprint-01.md); sprint 2 (2026-10-28–29) tamamlandı, 0.44.0: Kubeat'in kendi arızası sessiz kalmaz ve kayıp yaratmaz (S29 öz-sağlık kuralları + CI'da promtool, S25 kesinti bildirimi kaybı ve ölü makine kanalı, S31 kaldırmada PVC koruması, S32 sonuç yazılamazken heartbeat); sandbox 0.44.0'da; gerçek ortam provaları (firing, kaldırma ve yeniden kurulum, SIGTERM, dolu volume) kullanıcıda; puanlar 3,5/5 ve retro [sprints/2026-10-28-sprint-02.md](sprints/2026-10-28-sprint-02.md); sprint 3 (2026-10-29) tamamlandı, 0.45.0: ilk imzalı ve SBOM'lu yayım (S1 #48), OpenShift dışında da yazılabilir volume'la kuran ve sürümü CI'da denetlenen README komutu (S10+S11 #47), 4xx'te duraklamayan ve bildirim alamayan kanalı adıyla alarmlayan teslimat (S26 #49), heartbeat'siz kurulumda da critical yanan ve tek depoda cluster başına değerlendirilen öz-sağlık kuralları (S38+S37 #50); imza ve fsGroup'lu kurulum provaları geçti, sandbox 0.45.0'da, alarm provası geçti; puanlar 3,75/5 ve retro [sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md); sprint 4 (2026-10-29) planı: doküman paketi (S12, S15, S30, S46, S51; #59, #60), PagerDuty ve webhook'ta kurulum kimliği ile kısa RETRY_MAX'ta açılış (S28+S36 #61), ulaşılamayan insan kanalının sayfaları geciktirmemesi (S55 #62), yeşil CI'a bağlı yayım ve CI'da imzalı aynalama (S52 #63); 0.46.0 ile [sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md) |
| 30 | Tedarik zinciri: cosign ile imzalı imaj ve chart, SBOM/provenance, SECURITY.md | Tamamlandı. Sprint 3'te S1 (#48) 0.45.0 ile yayımlandı: iki imajın ve chart'ın digest'i cosign v3 ile keyless imzalanır (GitHub OIDC; `id-token: write` yalnızca imzalayan işte), imajlar SPDX SBOM ve `mode=max` provenance taşır, `latest` itilmez; iş akışı imzaları README "Tedarik zinciri"ndeki komutlarla çevrimiçi ve ağsız doğrular, etiketin GitHub Release'i digest'leri ve SBOM'ları taşır; SECURITY.md (yalnızca son minor, kritik/yüksek açık için 7 günlük hedef), haftalık `govulncheck` ve `npm audit` (security.yml), govulncheck sabit sürümde; `make release` `release/v*` dalından da etiketler (yetenek, söz değil). Kuru deneme `kubeat-dry-run/` altındaki kendi paketlerine iter. İmza v0.45.0 etiketinde ve başka bir makinede doğrulandı ([sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md) "Provaların sonucu" 1). Açık kalanlar: yeşil CI'a bağlı yayım ve CI'da hava boşluklu aynalama sprint 4'te (S52, #63); etiket, Release ve registry etiketinin değişmezliği (S53) ile derleme girdilerinin sabitlenmesi (S54) sprint 5'te ([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)); destek penceresi taahhüdü yok ([backlog](backlog.md)) |
| 31 | Hızlı başlangıç ve kurulum belgeleri: güncel kurulum komutu, OpenShift dışı fsGroup notu, yükseltme notları docs/yukseltme.md, lisansın nasıl istendiği, operator'süz cluster için öz-sağlık kuralları (S30) | Sprint 3'te S10+S11 geldi (#47, 0.45.0 ile yayımlandı; fsGroup uygulayan bir CSI sürücüsünde kurulum ve geri yükleme provası geçti, GKE'de koşulmadı): kurulum komutunda ve geri yükleme pod'unda `fsGroup=1001` (OpenShift'te atlanır), OpenShift dışında `podSecurityContext` boşken NOTES uyarısı; README ve docs/pilot.md'deki kurulum ve aynalama komutlarının sürümünü `scripts/check-versions.sh` CI chart işinde ve yayımın imajlardan önceki ilk işinde denetler (etiket = Chart.yaml); Helm iki iş akışında v4.3.1'e sabit, CI chart işi ayrıca Helm 3 (v3.22.1) ile koşar; release.yml'in `workflow_dispatch` kuru denemesi yayım paketlerine ve etiketlere dokunmaz. Sprint 4'te #59 (S12; S34'ün README, S61'in belge kısmı): README'nin ilk ekranında hızlı başlangıç (Ingress `ingress.className` ve cert-manager ile tek komut, fsGroup ve heartbeat satırları), desteklenen sürüm satırı ve GKE zonal notu; 0.38–0.45 yükseltme notları [yukseltme.md](yukseltme.md)'de (her sürümde Yapılacak, Davranış değişikliği, Geri dönüş, Alarm kuralları; başta yükseltmeden önce yedek, atlamalı yol ve API'yle betiklenen ön kontroller), README'de bağlantı; "GitOps ile kurulum"da kurulum notlarının uyarıları ve kapatan değerleri; README "API"de cluster dışından erişim; `check-versions.sh` hızlı başlangıç komutunu da denetler. Kalanı #60 (S15, S30, S46, S51; S34 ile S56'nın belge kısımları; [sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)) |

Yük testi sonuçları: [yuk-testi.md](yuk-testi.md).

## Uptime Kuma ile karşılaştırma

8. adımdan itibaren hedef, [Uptime Kuma](https://github.com/louislam/uptime-kuma)'nın
işlevselliğini bu projenin amacına (OCP/Kubernetes servisleri) uyan kısmıyla
karşılamak. Kuma'nın kaynağı (3.0 beta, 2026-10) incelenerek çıkarıldı.

| Kuma'da | Kubeat'te |
| --- | --- |
| HTTP: metot, gövde, header, kabul edilen kodlar, yönlendirme sınırı, keyword (ve tersi), JSON sorgusu, basic/bearer | Var. NTLM, OAuth2 ve mTLS yok |
| TCP port, ping, DNS, push, gRPC, WebSocket | Var. gRPC'de keyword yerine standart sağlık protokolü |
| Tekrar sayısı ve tekrar aralığı, ters mod, sertifika süresi bildirimi | Var |
| Veritabanları (PostgreSQL, MySQL, SQL Server, MongoDB, Redis, Oracle), MQTT, Kafka, RabbitMQ, SMTP, SNMP, SFTP, NTP, RADIUS | PostgreSQL, MySQL/MariaDB ve Redis var. MongoDB, Kafka ve RabbitMQ 14c adımında; kalanı yok |
| Docker container, PM2, sistem servisi | Karşılığı var: Kubernetes workload tipi (hazır pod sayısı) ve pod paneli |
| Gerçek tarayıcı, oyun sunucuları, Steam, Tailscale, Globalping | Kapsam dışı |
| Kenar çubuğunda liste, arama ve filtre, toplu seçim | Var |
| Monitor sayfası: kalp atışı çubuğu, yanıt süresi grafiği, 24 sa / 30 gün / 1 yıl uptime, sertifika bilgisi, olay tablosu | Var |
| Açık, koyu ve sistem teması | Var |
| Gruplar ve etiketler | Var: namespace ya da serbest grup, renkli etiketler. Etiketlerin değeri ve iç içe gruplar yok |
| Durum sayfaları (herkese açık, olay duyurusu, RSS), rozetler | Var: sayfa, duyuru, günlük uptime geçmişi, son kesintiler, durum ve uptime rozetleri. RSS, özel CSS ve özel alan adı yok |
| Bakım: tek seferlik, tekrarlayan, cron | Tek seferlik ve günlük/haftalık/aylık tekrar var, saat dilimiyle. Cron ifadesi ve haftanın birden çok günü yok (her gün için ayrı pencere açılır) |
| 110 bildirim sağlayıcısı | 11 tanesi var: Slack, Teams, Telegram, Discord, Mattermost, Google Chat, PagerDuty, ntfy, Gotify, e-posta, webhook. Opsgenie eklenmedi: Atlassian ürünü sonlandırıyor |
| Monitor başına bildirim seçimi | Kanal başına namespace ya da grup kapsamı var; tek tek monitor seçimi yok |
| Kullanıcı, 2FA, API anahtarları, ayarlar ekranı | Hesaplar, LDAP ve namespace bazlı roller var (Kuma tek kullanıcılıdır). 2FA, kişi başına API anahtarı ve ayarlar ekranı yok |
| Proxy, uzak tarayıcı, Cloudflare tüneli | Kapsam dışı |

Kuma'da olmayıp burada olanlar: cluster'dan keşif (Service, Ingress, Route),
monitor'un arkasındaki pod'lar ile CPU/bellek, Helm chart ve OpenShift
kısıtlarına uygun imajlar, `/metrics` ve heartbeat ile kendi kendini izleme.

## OpenShift

2026-10-13'te gerçek bir OpenShift'te doğrulandı: Red Hat Developer Sandbox
(ROSA üzerinde OpenShift 4.21.30, Kubernetes 1.34), cluster-admin olmadan,
tek bir namespace'e kurulumla. Ondan önce yalnızca minikube üzerindeki
benzetim vardı (`make minikube-openshift-sim`).

Gerçek cluster'da doğrulananlar:

- `restricted-v2` SCC: rastgele UID (1010760000), GID 0, SCC'nin atadığı
  fsGroup ile PVC'ye yazma, salt okunur kök dosya sistemi
- Router'ın Route'u taşıması ve host ataması; edge TLS, HTTP→HTTPS
  yönlendirme, oturum çerezinin `Secure` gelmesi
- GHCR'den imaj çekme ve cluster'ın iç registry'sinden imaj çekme
- HTTP (Service ve Route üzerinden), TLS'li TCP, DNS ve ping kontrolleri;
  ping RHCOS'ta ek ayar gerektirmeden çalışıyor
- Namespace'le sınırlı keşif (ClusterRole olmadan): Route, Service ve
  workload adayları, annotation eşitlemesi
- Pod bazlı kontrol: dondurulan pod Ready kalırken adıyla yakalandı
- Workload monitor ve rolling update sırasında sessiz kalması
- Pod paneli: pod'lar, node'lar, cluster olayları
- Pod CPU/belleği: Thanos Querier'ın namespace ucu (9092), servis hesabı
  token'ı ve pod'a kendiliğinden gelen service CA ile
- User workload monitoring'in ServiceMonitor'ü toplaması ve PrometheusRule
  ile `KubeatMonitorDown` alarmının firing olması

Gerçek cluster'ın gösterdiği ve düzeltilen: OpenShift `namespace` etiketini
zorla scrape edilen namespace yapıyor (`honorLabels` etkisiz); monitor'un
namespace'i artık `monitor_namespace` etiketinde.

Hâlâ doğrulanmayanlar (cluster-admin gerektirir):

- Cluster çapında keşif (ClusterRole) ve `cluster-monitoring-view` ile
  Thanos'un 9091 ucu
- NetworkPolicy'nin backend'i gerçekten yalıtması (Sandbox'ın kendi
  "aynı namespace'e izin ver" kuralı sonucu gizliyor)
- Alarmın Alertmanager alıcısına (e-posta, Slack) kadar gitmesi
- OIDC/LDAP girişinin kurumsal bir sağlayıcıyla denenmesi

## Bilinen sınırlar

- Backend tek replica çalışır; güncelleme sırasında kontroller durur
  (gerçek OpenShift'te ölçülen boşluk 23–27 sn).
- Veri PVC'si attach'i uygulayan blok depolama ister. NFS, CephFS, EFS,
  Azure Files gibi ağ dosya sistemlerinde iki pod aynı anda yazıp SQLite'ı
  bozabilir. Node çökünce backend pod'u Terminating'de kalabilir ve yeni pod
  volume'u bağlayamayabilir (Multi-Attach); Kubeat kendiliğinden dönmeyebilir
  ve `KubeatDown` yanar. Adımlar S41'de (sprint 5'in korumalı 1. maddesi,
  [sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).
- Veri volume'u dolunca kontrol sonuçları yazılamaz ve kaybolur (bellekte
  tutulmaz): olay açılmaz, bildirim gitmez. Kubeat bunu kendi bildirim
  kanallarından duyuramaz. 0.45'ten beri `kubeat_self_healthy` 0 olur ve
  chart'ın kuralları açıksa `KubeatUnhealthy` 5 dakikada `critical` yanar,
  heartbeat olmadan da; heartbeat varsa o da kesilir. İkisi de kapalıysa
  kimse duymaz; kurulum notu bunu uyarır. Volume kendiliğinden büyümez;
  boyut kuralı README "Geçmiş verisi"nde ([backlog](backlog.md) "Dolan veri
  volume'u: büyütme, kotaya göre saklama, yazılamayan sonuçlar").
- Giriş kilidi sayacı backend belleğindedir; yeniden başlatınca sıfırlanır.
  Tek replica olduğu için şimdilik sorun değildir. Kilit kullanıcı adına
  göredir: adını bilen biri bir hesabı `UPTIME_LOGIN_MAX_FAILURES` yanlış
  denemeyle `UPTIME_LOGIN_LOCKOUT` süresince kilitleyebilir; IP bazlı sınır
  yoktur. Kilitli bir hesap için API anahtarı, SSO ya da backend'i yeniden
  başlatmak kalır (README, "Kilitli hesaba girmek").
- `/metrics` monitor adlarını, namespace'lerini, sertifika bitişlerini ve
  bildirim kanallarının adlarını verir ve kullanıcı rollerinin namespace
  sınırını tanımaz; bu yüzden
  `UPTIME_METRICS_TOKEN` ile ayrı bir anahtar ister (chart onu varsayılan
  olarak üretir). Anahtar ayarlanmazsa açıktır ve backend'e kimlerin
  ulaşabileceğini NetworkPolicy belirler. NetworkPolicy `/metrics`'i push ve
  API'den ayıramaz: üçü aynı porttadır.
- Yayımlanmış bir durum sayfasının her görüntülenmesi tüm monitor'ların
  durumunu okur; önbellek yoktur. Çok ziyaret edilen bir sayfa için önüne
  bir önbellek konmalıdır. Okuma havuzunda bağlantı sınırı da yoktur: çok
  sayıda eşzamanlı istek backend'i bellek limitine itebilir (S21,
  [backlog](backlog.md) "Okuma havuzuna bağlantı sınırı ve durum sayfasına
  eşzamanlılık sınırı").
- Gizli değerler `UPTIME_SECRET_KEY` ile şifrelenir; anahtar değiştirme
  (rotation) yoktur ve push token'ları şifrelenmez (S58, [backlog](backlog.md)
  "Şifreleme anahtarını döndürmek ve anahtar sızınca izlenecek yol").
- TLS router'da ya da Ingress controller'da biter: router → frontend ve
  frontend → backend trafiği pod ağında düz HTTP'dir. LDAP parolası bu
  yoldan geçer; OVN-Kubernetes IPsec açık değilse pod ağında şifresizdir.
  Route yalnızca edge'dir, reencrypt seçeneği yoktur (S59,
  [backlog](backlog.md) "Route'ta reencrypt ve arayüz ile backend arasında
  cluster içi TLS").
- Hedef politikası yalnızca loopback, link-local (bulut metadata servisi) ve
  multicast adreslerini kapatır; özel ağlar bilerek açıktır, yani monitor ya
  da kanal düzenleyebilen biri cluster içindeki her servise istek
  gönderebilir. Bunu `UPTIME_TARGET_DENY_CIDRS` daraltır. Vekil
  (`HTTP_PROXY`) üzerinden giden istekte hedef backend'de çözülüp denetlenir,
  ama vekil adı yeniden çözer; vekilin nereye ulaştığı onun ağına bağlıdır.
- Keşif sayfasından içe aktarma tek seferliktir; sürekli izleme için nesneye
  `kubeat.io/monitor` annotation'ı eklenir.
- Annotation eşitlemesi her turda Service, Pod, EndpointSlice, Ingress,
  Route ve workload listelerini (cluster geneli keşifte tüm cluster'ınkini)
  sayfalamadan belleğe alır. SRE'nin yerel sentetik ölçümüne göre
  varsayılan 256Mi bellek limiti 6–7 bin pod civarında aşılabilir;
  OOMKill'den sonraki açılış yine eşitlemeyle başladığı için döngü kurulur
  ve kontroller durur. Aralığı uzatmak belleği değiştirmez: büyük cluster'da
  `backend.resources.limits.memory` artırılmalı ya da keşif
  `discovery.namespaces` ile sınırlanmalıdır (S42, sprint 5 kuyruğu).
- Annotation eşitlemesinin hatası yalnızca WARN olarak loglanır; metriği ve
  alarmı yoktur. Namespace sınırlı keşifte listedeki tek bir namespace
  okunamazsa (silinmiş ya da 403) bütün tur bırakılır: öteki
  namespace'lerde yeni annotation'lar izlenmez, kaldırılanların monitor'leri
  yerinde kalır (S43, sprint 5 kuyruğu).
- Gateway API HTTPRoute okunmaz; keşif adayı ya da annotation hedefi
  değildir. Ingress'ten HTTPRoute'a geçerken Ingress silinince monitor
  geçmişiyle birlikte kalkar; geçişten önce elle monitor oluşturun (S49,
  [backlog](backlog.md) "Gateway API HTTPRoute keşfi ve annotation
  hedefi").
- Bakım sırasındaki başarısız kontroller durum sayfasında ve raporda
  uptime'a dahildir. Bakımda olay açılmadığından rapor CSV'sinde aynı
  kontroller `uptime_percent`'e girer, `downtime_seconds`'a girmez; SLA için
  `downtime_seconds` kullanılmalıdır (S45, [backlog](backlog.md) "Duyurulan
  bakımı durum sayfası ve rapor uptime'ından ayırmak").
- Raporlar son N günü UTC'de ve o anki saatle biten bir dönem için verir;
  takvim ayı alınamaz (S44, sprint 5 kuyruğu).
- Cluster API'ye monitor'ün başarısızlık eşiği kadar kontrol boyunca
  ulaşılamazsa `workload` ve `each_pod` monitor'leri kesinti açar ve
  bildirim gönderir; API kesintisi hedefin kendi hatasından ayrılmaz (S20,
  [backlog](backlog.md) "Cluster API'ye ulaşılamayan kontrolü hedef hatası
  saymamak"). Zonal bir GKE cluster'ında control plane yükseltmesi buna yol
  açabilir; önlemi README "Cluster'a kurulum"daki GKE notunda.
- Bildirim kanalları birbirinden bağımsız teslim edilir; başarısız kesinti
  bildirimi olay kapanana kadar yeniden denenir, düzelme (`up`) bildirimi
  en çok `UPTIME_NOTIFY_RETRY_FOR` boyunca. Bekleme hatanın sınıfına
  bağlıdır (README "Alarm", hata sınıfları): ulaşılamayan kanal 1, 2, 4…
  dk (üst sınır `UPTIME_NOTIFY_RETRY_MAX`), reddeden (4xx) kanal doğrudan
  `UPTIME_NOTIFY_RETRY_MAX`, meşgul (429) kanal `Retry-After` kadar
  bekletilir; meşgul ve reddeden alıcıya kısa tekrar yapılmaz. 4xx dönen
  kanal canlıdır ve duraklamaz: öteki bildirimler beklemeden gider; kanal
  düzeltilince reddedilmiş bildirim sıradaki denemesini (en çok
  `UPTIME_NOTIFY_RETRY_MAX`) bekler, düzenleme denemeyi öne almaz. Bunun
  iki bedeli var: geçici bir 4xx (alıcı yeniden yayımlanırken 404) de
  reddetme sayılır ve o bildirimi `UPTIME_NOTIFY_RETRY_MAX` kadar
  geciktirir; her şeyi reddeden bir makine kanalı (süresi dolmuş token,
  kaldırılmış alıcı) bekleyen her bildirim için `UPTIME_NOTIFY_RETRY_MAX`'ta
  bir istek alır. Birlikte reddedilenler tek geçişte sırayla gider ve
  teslimat döngüsünü istek sayısı × yanıt süresi kadar tutar (500 kesinti,
  300 ms: her 15 dakikada ~2,5 dk); bu sırada kuyruk okunmaz. Çare kanalı
  düzeltmek ya da kapatmaktır, hangi kanal olduğunu
  `KubeatNotificationsFailing` söyler ([backlog](backlog.md) "Teslimat hata
  sınıflarının kalanları").
  Ulaşılamayan ya da meşgul bir makine kanalı (webhook, PagerDuty) yeni
  bildirimleri kendi yeniden deneme zamanına bekletir; tek bir monitor'un
  bildirimlerine sürekli 5xx dönen bir alıcı kanalı her geçişte yeniden
  duraklatır, yeni bildirimler en çok `UPTIME_NOTIFY_RETRY_MAX` gecikir.
  `Retry-After` yalnızca başlıktan okunur (saniye ya da HTTP tarihi);
  gövdedeki `retry_after` alanları okunmaz, sağlayıcı başına istemci
  tarafı hız sınırlayıcı yoktur. SMTP'nin kalıcı yanıtları (5xx SMTP
  kodları) ulaşılamadı sayılır. İnsan kanalları ise gruplama kapalıyken
  (`UPTIME_NOTIFY_GROUP_WAIT=0`) her bildirimde yine denenir.
  Deneme listesi bellektedir: yeniden
  başlatmada açık kesintininki, hiç denenmemiş olanlar (kuyrukta ya da grup
  beklemesinde kalanlar) dahil, bildirim kaydından kurulur (kaydı
  `UPTIME_DELIVERY_RETENTION` ile silinmiş açık kesinti bir kez daha
  gider); bekleyen düzelme bildirimi düşer, kanal almadan yeni bir kesinti
  açılırsa da düşer.
  Kapanışta insan kanallarının grupları kuyruktan önce gider ve kapanışın
  tümü `UPTIME_SHUTDOWN_TIMEOUT` ile sınırlıdır; yetişmeyen düzelme
  bildirimi kaybolur. Teslim edilemeyen bildirimlerin ayrı bir görünümü
  (dead-letter) yoktur; bildirim kaydı,
  kanal başına `kubeat_channel_last_delivery_ok` ve
  `kubeat_channel_undelivered_outages` (chart'ın kanal başına
  `KubeatNotificationsFailing` alarmı bunlara bakar),
  `kubeat_notification_attempts_total{result="failed"}` ve
  `kubeat_notifications_total{result="failed"}` gösterir ([backlog](backlog.md)
  "Teslim edilemeyen bildirimler için dead-letter görünümü ve kalıcı
  yeniden deneme kuyruğu").
- Bildirim sağlayıcıları gerçek servislere karşı değil, isteği kaydeden
  yerel sunuculara karşı denendi; gövdeler sağlayıcıların belgelerine göre
  yazıldı.
- Aynı PagerDuty servisine bağlanan birden çok kurulumda olay numaraları
  çakışır: `dedup_key` kurulum kimliği taşımaz (`kubeat-incident-<n>`), bir
  cluster'ın kesintisi diğerinin açık alarmına eklenir ve ilk düzelme
  ikisini de kapatır. Her kuruluma ayrı bir PagerDuty servisi verin (S28,
  sprint 4, #61; [sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)).
- Chart'ın alarmları backend'i cluster'ın Prometheus'u scrape edebildiği
  sürece görür: Kubeat 10 dakikadan uzun düşerse kesinti, sertifika ve yedek
  alarmları çözülür. README'deki susturma kuralı, alarmları alan
  Alertmanager'a eklenirse (OpenShift'in ve kube-prometheus-stack'in
  varsayılanında yoktur), bunları `KubeatDown` yanarken susturur; bedeli,
  Kubeat 25 dakikadan uzun düşükken biten bir kesintinin düzelmesinin
  Alertmanager'dan gitmeyebilmesidir (Kubeat'in kendi kanalları bildirir).
  Cluster'ın tamamı düşerse yalnızca dışarıdaki heartbeat haber verir
  (README, "Aracın kendi sağlığı").
- Ping, çekirdeğin yetkisiz ICMP soketine izin vermesine bağlıdır; minikube'da
  ve gerçek RHCOS'ta ek ayar gerektirmeden çalıştı. Bir Service adına ping
  atılamaz: ClusterIP sanal bir adrestir ve ICMP'ye yanıt vermez; pod IP'si
  ya da cluster dışı bir adres verilmelidir.
