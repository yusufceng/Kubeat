# Kubeat

OCP/Kubernetes ortamlarındaki servisler için uptime monitoring aracı.
Go + Next.js + SQLite.

Proje planı: [docs/proje-plani.md](docs/proje-plani.md)

## Yapı

```
backend/        Go API, scheduler, checker, alarm motoru, agent
frontend/       Next.js (App Router) arayüzü
deploy/helm/    Helm chart
docs/adr/       Mimari karar kayıtları
```

## Lokal çalıştırma

```
make dev        # ikisi birlikte; arayüz -> http://localhost:3000
make backend    # API  -> http://localhost:8090
make frontend   # UI   -> http://localhost:3000
make test
```

## Cluster'a kurulum

Chart [deploy/helm/kubeat](deploy/helm/kubeat) altındadır. Minikube için:

```
minikube start --keep-context   # kubectl'in mevcut bağlamını değiştirmez
make minikube-deploy            # imajları derler, yükler, chart'ı kurar
make minikube-open              # arayüz -> http://localhost:3000
```

Backend cluster içinde çalıştığı için Service'ler DNS adlarıyla izlenebilir,
örneğin `http://servis.namespace.svc:8080/healthz`.

- Backend tek replica çalışır (SQLite tek yazıcıya izin verir) ve verisini bir
  PVC'de tutar.
- İmajlar UBI tabanlıdır; container'lar root olmadan, rastgele bir UID ile ve
  salt okunur kök dosya sistemiyle çalışabilir.
- OpenShift'te `podSecurityContext` boş bırakılır ve `route.enabled=true`
  verilir. Chart henüz bir OpenShift cluster'ında denenmedi.
- Ayarlar `backend.env` ve `frontend.env` altında ortam değişkeni olarak
  verilir; bkz. [values.yaml](deploy/helm/kubeat/values.yaml).

## API

| Metot | Yol | Açıklama |
| --- | --- | --- |
| `GET` | `/api/v1/health` | Backend sağlık durumu |
| `GET` | `/api/v1/monitors?window=24h` | Monitor'lar; son durum ve uptime ile |
| `POST` | `/api/v1/monitors` | Monitor ekler |
| `GET` | `/api/v1/monitors/{id}?window=24h` | Tek monitor |
| `DELETE` | `/api/v1/monitors/{id}` | Monitor'u ve sonuçlarını siler |
| `GET` | `/api/v1/monitors/{id}/results?limit=100` | Son kontrol sonuçları, yeniden eskiye |
| `GET` | `/api/v1/incidents?monitor_id=1&limit=100` | Olaylar, yeniden eskiye |
| `GET` | `/api/v1/channels` | Bildirim kanalları (URL'ler maskelenir) |
| `POST` | `/api/v1/channels` | Webhook kanalı ekler |
| `DELETE` | `/api/v1/channels/{id}` | Kanalı siler |
| `POST` | `/api/v1/channels/{id}/test` | Kanala test bildirimi gönderir |

```
curl -X POST localhost:8090/api/v1/monitors -d '{
  "name": "ornek", "type": "http", "target": "https://example.com",
  "interval_seconds": 30, "timeout_seconds": 5
}'
```

`type` değeri `http` (2xx/3xx yanıt başarılı sayılır) ya da `tcp`
(`host:port` bağlantısı kurulabiliyorsa başarılı) olabilir.

## Alarm

Bir monitor art arda `failure_threshold` kez başarısız olursa olay açılır;
art arda `UPTIME_RECOVERY_THRESHOLD` kez başarılı olunca kapanır. Her iki
durumda etkin tüm kanallara bir bildirim gider; olay açıkken tekrar bildirim
gönderilmez.

Webhook kanalına şu gövdeyle `POST` yapılır. `text` alanı insan tarafından
okunabilir özettir.

```json
{
  "event": "down",
  "text": "🔴 odeme-api erişilemiyor (https://...). Art arda 3 kontrol başarısız. Son hata: ...",
  "monitor": { "id": 1, "name": "odeme-api", "type": "http", "target": "https://..." },
  "incident": { "id": 7, "started_at": "...", "resolved_at": null, "cause": "..." }
}
```

`event` değeri `down`, `up` ya da `test` olur.

## Yapılandırma

Ayarlar koda gömülmez; hepsi ortam değişkeniyle verilir. Örnek değerler
[.env.example](.env.example) dosyasındadır.

| Değişken | Varsayılan | Açıklama |
| --- | --- | --- |
| `UPTIME_ADDR` | `:8080` | Backend'in dinlediği adres |
| `UPTIME_DB_PATH` | `uptime.db` | SQLite dosyasının yolu |
| `UPTIME_DB_BUSY_TIMEOUT` | `5s` | Kilitli veritabanı için bekleme süresi |
| `UPTIME_READ_HEADER_TIMEOUT` | `5s` | HTTP istek başlığı okuma süresi |
| `UPTIME_SHUTDOWN_TIMEOUT` | `10s` | Kapanışta açık isteklerin bitmesi için süre |
| `UPTIME_SCHEDULER_TICK` | `1s` | Zamanı gelen monitor'ların ve monitor değişikliklerinin kontrol sıklığı |
| `UPTIME_MAX_JITTER` | `5s` | İlk kontrolden önceki rastgele gecikmenin üst sınırı |
| `UPTIME_WORKERS` | `20` | Aynı anda çalışabilecek kontrol sayısı |
| `UPTIME_RESULT_BATCH_SIZE` | `100` | Tek seferde yazılan en fazla sonuç sayısı |
| `UPTIME_RESULT_FLUSH_INTERVAL` | `1s` | Dolmamış batch'in yazılma aralığı |
| `UPTIME_RETENTION` | `168h` | Ham kontrol sonuçlarının saklanma süresi |
| `UPTIME_RETENTION_INTERVAL` | `1h` | Eski sonuçların silinme sıklığı |
| `UPTIME_HTTP_INSECURE_SKIP_VERIFY` | `false` | HTTP kontrollerinde TLS sertifika doğrulamasını kapatır |
| `UPTIME_MIN_CHECK_INTERVAL` | `5s` | Bir monitor için izin verilen en kısa kontrol aralığı |
| `UPTIME_STATS_WINDOW` | `24h` | Uptime yüzdesinin hesaplandığı varsayılan süre |
| `UPTIME_MAX_RESULTS` | `1000` | Tek istekte dönen en fazla kontrol sonucu |
| `UPTIME_DEFAULT_FAILURE_THRESHOLD` | `3` | Monitor'da belirtilmezse, olay açmak için gereken art arda başarısız kontrol sayısı |
| `UPTIME_RECOVERY_THRESHOLD` | `1` | Olayı kapatmak için gereken art arda başarılı kontrol sayısı |
| `UPTIME_NOTIFY_ATTEMPTS` | `3` | Kanal başına bildirim deneme sayısı |
| `UPTIME_NOTIFY_RETRY_DELAY` | `5s` | Bildirim denemeleri arasındaki bekleme |
| `UPTIME_NOTIFY_TIMEOUT` | `5s` | Tek bir bildirim denemesinin zaman aşımı |
| `UPTIME_NOTIFY_QUEUE_SIZE` | `100` | Gönderilmeyi bekleyebilecek en fazla bildirim sayısı |
| `BACKEND_URL` | `http://localhost:8080` | Frontend'in backend'e eriştiği adres |
| `BACKEND_TIMEOUT_MS` | `3000` | Frontend'in backend isteği zaman aşımı (ms) |
| `REFRESH_INTERVAL_MS` | `5000` | Açık sayfaların veriyi yenileme aralığı (ms) |
| `HISTORY_LIMIT` | `40` | Geçmiş şeridinde gösterilen kontrol sayısı |
| `RESULTS_LIMIT` | `100` | Monitor sayfasında listelenen kontrol sayısı |
| `INCIDENTS_LIMIT` | `100` | Bir sayfada listelenen olay sayısı |

Süreler Go biçimindedir (`500ms`, `5s`, `1m30s`).

## Katkı

Branch stratejisi ve commit kuralları: [CONTRIBUTING.md](CONTRIBUTING.md)
