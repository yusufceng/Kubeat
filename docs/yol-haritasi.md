# Yol haritası

[Proje planı](proje-plani.md) üzerine, MVP'den sonra kararlaştırılan sıra.
Ertelenen fikirler [backlog.md](backlog.md) dosyasındadır.

| # | Adım | Durum |
| --- | --- | --- |
| 0 | Temel: scheduler, HTTP/TCP kontrolleri, SQLite, olay ve webhook, arayüz, Helm chart | Tamamlandı |
| 1 | Keşif: Service/Ingress/Route'ları bulma, namespace görünümü | Tamamlandı |
| 2 | Arayüz derinliği: grafik, uzun dönem uptime, düzenleme/durdurma, HTTP seçenekleri, sertifika süresi | Tamamlandı |
| 3 | Alarm olgunluğu: Slack/Teams/e-posta, bakım pencereleri, susturma, hatırlatma | Tamamlandı |
| 4 | Aracın kendi sağlığı: `/metrics`, heartbeat | Tamamlandı |
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
| 19 | Cluster Alertmanager'ı ile alarm: chart'ta PrometheusRule | Tamamlandı |
| 20 | Olay bağlamı: monitor sayfasında pod'ların ve workload'un cluster olayları | Tamamlandı |
| 21 | Ekip raporları: namespace başına uptime ve kesinti özeti, CSV | Tamamlandı |

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

Gerçek bir OpenShift'te hiç çalıştırılmadı. 2026-10-12'den beri minikube
üzerinde bir benzetim var (`make minikube-openshift-sim`, ayrıntısı
README'de): Route ve ServiceMonitor CRD'leri, token ve TLS isteyen bir
thanos-querier taklidi, `cluster-monitoring-view`, kısıtlı Pod Security
Standard ve rastgele UID.

Benzetimle doğrulandı:

- Kısıtlı güvenlik bağlamı: rastgele UID, GID 0, salt okunur kök dosya
  sistemi, tüm capability'ler düşük; backend, frontend, ping ve DNS çalıştı
- Route keşfi ve Route üzerinden pod çözümleme (gerçek API sunucusu, CRD ile)
- Chart'ın Route ve ServiceMonitor şablonlarının kabulü
- Servis hesabı token'ı ve service CA ile metrik okuma; chart'ın
  `cluster-monitoring-view` bağlaması

Hâlâ gerçek OpenShift gerektirenler:

- Router'ın Route'u taşıması ve host ataması
- SCC admission'ın UID/fsGroup atamasıyla volume izinleri
- Service CA'nın pod'a kendiliğinden gelmesi (`service-ca.crt`)
- Thanos Querier'ın gerçek yetki kuralı
- İç registry, pull secret, OAuth proxy ile arayüz koruması

## Bilinen sınırlar

- Backend tek replica çalışır; güncelleme sırasında birkaç saniye kontrol
  yapılmaz.
- Giriş kilidi sayacı backend belleğindedir; yeniden başlatınca sıfırlanır.
  Tek replica olduğu için şimdilik sorun değildir.
- Gizli değerler `UPTIME_SECRET_KEY` ile şifrelenir; anahtar değiştirme
  (rotation) yoktur ve push token'ları şifrelenmez.
- Keşif sayfasından içe aktarma tek seferliktir; sürekli izleme için nesneye
  `kubeat.io/monitor` annotation'ı eklenir.
- Annotation eşitlemesi her turda tüm cluster'ın Service, Pod, Ingress, Route
  ve workload listesini çeker; binlerce pod'lu cluster'da aralık uzatılmalıdır.
- Bakım sırasındaki başarısız kontroller uptime'a dahildir.
- Bildirim sağlayıcıları gerçek servislere karşı değil, isteği kaydeden
  yerel sunuculara karşı denendi; gövdeler sağlayıcıların belgelerine göre
  yazıldı.
- Ping, çekirdeğin yetkisiz ICMP soketine izin vermesine bağlıdır. Kısıtlı
  güvenlik bağlamındaki pod'dan minikube'da çalıştı; RHCOS'ta CRI-O bu izni
  varsayılan olarak verir, ama denenmedi.
