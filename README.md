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

## Katkı

Branch stratejisi ve commit kuralları: [CONTRIBUTING.md](CONTRIBUTING.md)
