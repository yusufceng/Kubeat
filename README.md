# Uptime

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
make backend    # API  -> http://localhost:8090
make frontend   # UI   -> http://localhost:3000
make test
```

## API

| Metot | Yol | Açıklama |
| --- | --- | --- |
| `GET` | `/api/v1/health` | Backend sağlık durumu |
| `GET` | `/api/v1/monitors?window=24h` | Monitor'lar; son durum ve uptime ile |
| `POST` | `/api/v1/monitors` | Monitor ekler |
| `GET` | `/api/v1/monitors/{id}?window=24h` | Tek monitor |
| `DELETE` | `/api/v1/monitors/{id}` | Monitor'u ve sonuçlarını siler |
| `GET` | `/api/v1/monitors/{id}/results?limit=100` | Son kontrol sonuçları, yeniden eskiye |

```
curl -X POST localhost:8090/api/v1/monitors -d '{
  "name": "ornek", "type": "http", "target": "https://example.com",
  "interval_seconds": 30, "timeout_seconds": 5
}'
```

`type` değeri `http` (2xx/3xx yanıt başarılı sayılır) ya da `tcp`
(`host:port` bağlantısı kurulabiliyorsa başarılı) olabilir.

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
| `BACKEND_URL` | `http://localhost:8080` | Frontend'in backend'e eriştiği adres |
| `BACKEND_TIMEOUT_MS` | `3000` | Frontend'in backend isteği zaman aşımı (ms) |

Süreler Go biçimindedir (`500ms`, `5s`, `1m30s`).

## Katkı

Branch stratejisi ve commit kuralları: [CONTRIBUTING.md](CONTRIBUTING.md)
