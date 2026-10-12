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
| `BACKEND_URL` | `http://localhost:8080` | Frontend'in backend'e eriştiği adres |
| `BACKEND_TIMEOUT_MS` | `3000` | Frontend'in backend isteği zaman aşımı (ms) |

Süreler Go biçimindedir (`500ms`, `5s`, `1m30s`).

## Katkı

Branch stratejisi ve commit kuralları: [CONTRIBUTING.md](CONTRIBUTING.md)
