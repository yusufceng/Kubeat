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
| 10 | Etiketler, gruplar ve toplu işlemler | Sırada |
| 11 | Durum sayfaları ve rozetler | Planlandı |
| 12 | Tekrarlayan bakım pencereleri | Planlandı |
| 13 | Bildirim sağlayıcıları: Telegram, Discord, PagerDuty, Opsgenie, Mattermost, Google Chat, ntfy | Planlandı |
| 14 | Veritabanı ve kuyruk kontrolleri (PostgreSQL, MySQL, Redis, MongoDB, Kafka, RabbitMQ); Kubernetes workload tipi | Planlandı |
| 15 | Ayarlar ekranı, kişisel hesaplar ve roller, API anahtarları | Planlandı |

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
| Veritabanları (PostgreSQL, MySQL, SQL Server, MongoDB, Redis, Oracle), MQTT, Kafka, RabbitMQ, SMTP, SNMP, SFTP, NTP, RADIUS | Yok; ilgili olanlar 14. adımda |
| Docker container, PM2, sistem servisi | Yok; karşılığı Kubernetes workload tipi (14. adım) ve bugünkü pod paneli |
| Gerçek tarayıcı, oyun sunucuları, Steam, Tailscale, Globalping | Kapsam dışı |
| Kenar çubuğunda liste, arama ve filtre, toplu seçim | Liste, arama ve süzgeç var; toplu seçim 10. adımda |
| Monitor sayfası: kalp atışı çubuğu, yanıt süresi grafiği, 24 sa / 30 gün / 1 yıl uptime, sertifika bilgisi, olay tablosu | Var |
| Açık, koyu ve sistem teması | Var |
| Gruplar ve etiketler | Namespace gruplaması var; serbest grup ve etiket 10. adımda |
| Durum sayfaları (herkese açık, olay duyurusu, RSS), rozetler | 11. adım |
| Bakım: tek seferlik, tekrarlayan, cron | Tek seferlik var; kalanı 12. adımda |
| 110 bildirim sağlayıcısı | Webhook, Slack, Teams, e-posta var; yaygın olanlar 13. adımda |
| Kullanıcı, 2FA, API anahtarları, ayarlar ekranı | Tek yönetici parolası var; kalanı 15. adımda |
| Proxy, uzak tarayıcı, Cloudflare tüneli | Kapsam dışı |

Kuma'da olmayıp burada olanlar: cluster'dan keşif (Service, Ingress, Route),
monitor'un arkasındaki pod'lar ile CPU/bellek, Helm chart ve OpenShift
kısıtlarına uygun imajlar, `/metrics` ve heartbeat ile kendi kendini izleme.

## Gerçek OpenShift'te doğrulanması gerekenler

Geliştirme minikube'da yapıldı. Aşağıdakiler yalnızca birim testleriyle ya
da şablon render edilerek doğrulandı:

- Route keşfi ve Route üzerinden pod çözümleme
- Kısıtlı SCC altında çalışma (docker'da rastgele UID ile taklit edildi)
- Thanos Querier'a servis hesabı token'ı ve service CA ile erişim
- ServiceMonitor ve `cluster-monitoring-view` bağlaması
- Chart'taki Route şablonu

## Bilinen sınırlar

- Backend tek replica çalışır; güncelleme sırasında birkaç saniye kontrol
  yapılmaz.
- Giriş tek bir yönetici parolasıyladır: kişi bazında hesap, rol (salt
  okunur kullanıcı gibi) ve kimin neyi değiştirdiğinin kaydı yoktur.
- İstek header'ları ve webhook adresleri veritabanında şifresiz durur.
- Keşif tek seferliktir; sonradan eklenen servisler kendiliğinden izlenmez.
- Bakım pencereleri tek seferliktir ve bakım sırasındaki başarısız
  kontroller uptime'a dahildir.
- Slack ve Teams gönderimi gerçek servislere karşı denenmedi.
- Ping, çekirdeğin yetkisiz ICMP soketine izin vermesine bağlıdır. Minikube'da
  kısıtlı güvenlik bağlamındaki pod'dan çalıştı; OpenShift'te denenmedi.
