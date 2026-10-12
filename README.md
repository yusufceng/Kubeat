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
make minikube-demo              # keşfi denemek için örnek namespace
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
| `PATCH` | `/api/v1/monitors/{id}` | Monitor'u düzenler; `{"enabled": false}` ile durdurur |
| `DELETE` | `/api/v1/monitors/{id}` | Monitor'u ve sonuçlarını siler |
| `GET` | `/api/v1/monitors/{id}/results?limit=100` | Son kontrol sonuçları, yeniden eskiye |
| `GET` | `/api/v1/monitors/{id}/series?range=24h` | Yanıt süresi ve erişilebilirlik, zaman dilimlerine bölünmüş |
| `GET` | `/api/v1/monitors/{id}/uptime?windows=24h,168h` | İstenen her dönem için uptime |
| `GET` | `/api/v1/discovery` | Keşfin kullanılabilir olup olmadığı |
| `GET` | `/api/v1/discovery/namespaces` | Cluster'daki namespace'ler |
| `GET` | `/api/v1/discovery/candidates?namespace=x` | Namespace'teki izlenebilir hedefler |
| `POST` | `/api/v1/discovery/import` | Seçilen hedefleri monitor'a çevirir |
| `GET` | `/api/v1/incidents?monitor_id=1&limit=100` | Olaylar, yeniden eskiye |
| `GET` | `/api/v1/channels` | Bildirim kanalları (URL'ler maskelenir) |
| `POST` | `/api/v1/channels` | Kanal ekler: `{"name", "type", "target"}` |
| `DELETE` | `/api/v1/channels/{id}` | Kanalı siler |
| `POST` | `/api/v1/channels/{id}/test` | Kanala test bildirimi gönderir |

```
curl -X POST localhost:8090/api/v1/monitors -d '{
  "name": "ornek", "type": "http", "target": "https://example.com",
  "interval_seconds": 30, "timeout_seconds": 5
}'
```

`type` değeri `http` ya da `tcp` (`host:port` bağlantısı kurulabiliyorsa
başarılı) olabilir. HTTP monitor'ları isteğe bağlı bir `http` nesnesi alır:

```json
"http": {
  "method": "GET",
  "expected_status": "200-299,401",
  "keyword": "\"status\":\"ok\"",
  "headers": { "Authorization": "Bearer ..." },
  "tls_skip_verify": false
}
```

- `method`: `GET` ya da `HEAD`.
- `expected_status`: başarılı sayılan kodlar ve aralıklar; varsayılan `200-399`.
- `keyword`: yanıt gövdesinde bulunması gereken metin.
- `headers`: isteğe eklenen header'lar. Değerler API yanıtlarında `********`
  olarak maskelenir; güncellemede bu değer gönderilirse kayıtlı değer korunur.
- `tls_skip_verify`: yalnızca bu monitor için sertifika doğrulamasını kapatır.

## Geçmiş verisi

Her kontrol sonucu `UPTIME_RETENTION` (varsayılan 7 gün) boyunca tek tek
saklanır. Ayrıca her monitor için saatlik özet tutulur (kontrol sayısı,
başarılı sayısı, başarılı kontrollerin yanıt süresi) ve bu özetler
`UPTIME_ROLLUP_RETENTION` (varsayılan 1 yıl) boyunca kalır. Kısa aralıklar
ham sonuçlardan, ham verinin ötesine geçen aralıklar özetlerden hesaplanır.

Yanıt süresi yalnızca başarılı kontrollerden hesaplanır; başarısız bir
kontrolün süresi çoğunlukla zaman aşımıdır ve yanıt süresi hakkında bir şey
söylemez.

## Keşif

Backend cluster içinde çalışırken pod'un servis hesabıyla cluster'a bağlanır
ve arayüzdeki **Keşif** sayfasında seçilen namespace'teki izlenebilir
hedefleri listeler. Seçilenler tek adımda monitor'a çevrilir ve ana sayfada
namespace'e göre gruplanır. Erişim yalnızca okuma amaçlıdır.

| Kaynak | Önerilen kontrol |
| --- | --- |
| Route (OpenShift) | Route adresine HTTP |
| Ingress | Her host ve yol için HTTP |
| Service, pod'unda HTTP probe var | Service üzerinden probe'un yoluna HTTP |
| Service, HTTP probe yok | Porta TCP bağlantısı |

Service için yol tahmin edilmez: uygulamanın kendi readiness (yoksa liveness)
probe'u hangi adresin "sağlıklı" anlamına geldiğini zaten söyler; rastgele bir
yol 404 dönüp yanlış alarm üretir.

Denemek için `make minikube-demo` örnek bir `shop-demo` namespace'i kurar.

Cluster dışında çalışırken keşif için hem `UPTIME_KUBECONFIG` hem
`UPTIME_KUBE_CONTEXT` verilmelidir; kubeconfig'in o anki bağlamı bilerek
kullanılmaz.

## Alarm

Bir monitor art arda `failure_threshold` kez başarısız olursa olay açılır;
art arda `UPTIME_RECOVERY_THRESHOLD` kez başarılı olunca kapanır. Her iki
durumda etkin tüm kanallara bir bildirim gider; olay açıkken tekrar bildirim
gönderilmez.

HTTPS hedeflerde her kontrol sertifikanın bitiş tarihini de kaydeder. Bitişe
`UPTIME_CERT_WARNING` (varsayılan 14 gün) kala ayrı bir "sertifika" olayı
açılır ve bildirim gider; sertifika yenilenince olay kapanır. Bu olay
kesintilerden bağımsızdır: hedef çalışırken de açık olabilir.

### Bildirim kanalları

| Tür | Hedef | Gönderilen |
| --- | --- | --- |
| `slack` | Slack incoming webhook URL'i | `{"text": "..."}` |
| `teams` | Teams workflow webhook URL'i | Adaptive Card içeren mesaj |
| `email` | Virgülle ayrılmış adresler | Düz metin e-posta |
| `webhook` | Herhangi bir URL | Aşağıdaki JSON gövdesi |

E-posta için bir SMTP sunucusu tanımlanmalıdır (`UPTIME_SMTP_*`); tanımlı
değilse e-posta kanalı eklenemez. Varsayılan bağlantı güvenliği `starttls`
olup sunucu STARTTLS sunmuyorsa gönderim reddedilir, şifresiz gönderilmez.

Webhook URL'leri API yanıtlarında maskelenir.

Genel webhook kanalına şu gövdeyle `POST` yapılır. `text` alanı insan tarafından
okunabilir özettir.

```json
{
  "event": "down",
  "text": "🔴 odeme-api erişilemiyor (https://...). Art arda 3 kontrol başarısız. Son hata: ...",
  "monitor": { "id": 1, "name": "odeme-api", "type": "http", "target": "https://..." },
  "incident": { "id": 7, "kind": "down", "started_at": "...", "resolved_at": null, "cause": "..." }
}
```

`event` değeri `down`, `up`, `cert_expiring`, `cert_renewed` ya da `test`
olur; `incident.kind` ise `down` ya da `cert`.

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
| `UPTIME_ROLLUP_INTERVAL` | `1m` | Saatlik özetlerin güncellenme sıklığı |
| `UPTIME_ROLLUP_RETENTION` | `8760h` | Saatlik özetlerin saklanma süresi; grafik ve uptime en fazla bu kadar geriye gider |
| `UPTIME_SERIES_POINTS` | `120` | Bir grafikteki yaklaşık nokta sayısı |
| `UPTIME_HTTP_INSECURE_SKIP_VERIFY` | `false` | Tüm HTTP kontrollerinde TLS sertifika doğrulamasını kapatır |
| `UPTIME_KEYWORD_BODY_LIMIT_BYTES` | `1048576` | Keyword aranırken yanıt gövdesinin okunan kısmı |
| `UPTIME_MIN_CHECK_INTERVAL` | `5s` | Bir monitor için izin verilen en kısa kontrol aralığı |
| `UPTIME_STATS_WINDOW` | `24h` | Uptime yüzdesinin hesaplandığı varsayılan süre |
| `UPTIME_MAX_RESULTS` | `1000` | Tek istekte dönen en fazla kontrol sonucu |
| `UPTIME_DEFAULT_FAILURE_THRESHOLD` | `3` | Monitor'da belirtilmezse, olay açmak için gereken art arda başarısız kontrol sayısı |
| `UPTIME_RECOVERY_THRESHOLD` | `1` | Olayı kapatmak için gereken art arda başarılı kontrol sayısı |
| `UPTIME_CERT_WARNING` | `336h` | Sertifika bitişine bu kadar kala uyarı olayı açılır |
| `UPTIME_NOTIFY_ATTEMPTS` | `3` | Kanal başına bildirim deneme sayısı |
| `UPTIME_NOTIFY_RETRY_DELAY` | `5s` | Bildirim denemeleri arasındaki bekleme |
| `UPTIME_NOTIFY_TIMEOUT` | `5s` | Tek bir bildirim denemesinin zaman aşımı |
| `UPTIME_NOTIFY_QUEUE_SIZE` | `100` | Gönderilmeyi bekleyebilecek en fazla bildirim sayısı |
| `UPTIME_DISCOVERY_ENABLED` | `true` | Cluster keşfini açar |
| `UPTIME_KUBECONFIG` | – | Cluster dışında çalışırken kullanılacak kubeconfig dosyası |
| `UPTIME_KUBE_CONTEXT` | – | Kubeconfig içindeki bağlam; kubeconfig verildiyse zorunlu |
| `UPTIME_KUBE_TIMEOUT` | `10s` | Cluster API istekleri için zaman aşımı |
| `UPTIME_SMTP_HOST` | – | E-posta kanalları için SMTP sunucusu |
| `UPTIME_SMTP_PORT` | `587` | SMTP portu |
| `UPTIME_SMTP_FROM` | – | Gönderen adresi, örn. `Kubeat <kubeat@ornek.com>` |
| `UPTIME_SMTP_SECURITY` | `starttls` | `starttls`, `tls` ya da `none` |
| `UPTIME_SMTP_USERNAME` | – | SMTP kullanıcı adı; boşsa kimlik doğrulama yapılmaz |
| `UPTIME_SMTP_PASSWORD` | – | SMTP parolası |
| `BACKEND_URL` | `http://localhost:8080` | Frontend'in backend'e eriştiği adres |
| `BACKEND_TIMEOUT_MS` | `3000` | Frontend'in backend isteği zaman aşımı (ms) |
| `REFRESH_INTERVAL_MS` | `5000` | Açık sayfaların veriyi yenileme aralığı (ms) |
| `HISTORY_LIMIT` | `40` | Geçmiş şeridinde gösterilen kontrol sayısı |
| `RESULTS_LIMIT` | `100` | Monitor sayfasında listelenen kontrol sayısı |
| `INCIDENTS_LIMIT` | `100` | Bir sayfada listelenen olay sayısı |
| `UPTIME_WINDOWS` | `24h,168h,720h` | Monitor sayfasında uptime'ı gösterilen dönemler |
| `CHART_RANGES` | `24h,1h,168h,720h` | Grafik için sunulan aralıklar; ilki varsayılandır |

Süreler Go biçimindedir (`500ms`, `5s`, `1m30s`).

## Katkı

Branch stratejisi ve commit kuralları: [CONTRIBUTING.md](CONTRIBUTING.md)
