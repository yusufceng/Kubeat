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
| 7 | Üretime hazırlık: API koruması, yük testi, yedekleme, kullanıcı girişi | API koruması, yük testi ve yedekleme tamamlandı; kullanıcı girişi bekliyor |

Yük testi sonuçları: [yuk-testi.md](yuk-testi.md).

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
- Backend API'si ortak bir anahtarla korunur, ama arayüzde kullanıcı girişi
  yoktur: arayüze erişen herkes her şeyi değiştirebilir.
- İstek header'ları ve webhook adresleri veritabanında şifresiz durur.
- Keşif tek seferliktir; sonradan eklenen servisler kendiliğinden izlenmez.
- Bakım pencereleri tek seferliktir ve bakım sırasındaki başarısız
  kontroller uptime'a dahildir.
- Slack ve Teams gönderimi gerçek servislere karşı denenmedi.
