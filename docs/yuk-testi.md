# Yük testi

Tarih: 2026-10-12 · Ortam: geliştirici dizüstü bilgisayarı (macOS, Apple
Silicon), backend tek süreç, SQLite yerel diskte.

Hedef, proje planındaki ölçektir: **500 monitor, 30 saniyede bir kontrol,
7 gün ham veri**. Test veritabanı bu ölçekte üretildi: 500 monitor
(10 namespace), 10,08 milyon kontrol sonucu, 648 MB.

## Sonuçlar

| Ölçüm | Değer |
| --- | --- |
| Hedef yükte kontrol hızı (beklenen 16,7/sn) | 16,6/sn, kayıp 0 |
| 6 kat yük (5 sn aralık, beklenen 100/sn) | 92/sn, kayıp 0 |
| 30 kat yük (1 sn aralık, beklenen 500/sn) | 514/sn, kayıp 0 |
| Backend bellek kullanımı | 40-46 MB |
| Açılışta 10 milyon satırın saatlik özeti | yaklaşık 17 sn (arka planda, API bu sırada yanıt veriyor) |
| `GET /monitors` (500 monitor) | 12-29 ms |
| `GET /monitors?namespace=...&history=40` (50 monitor) | 19 ms |
| `GET /monitors/{id}/series?range=24h` | 74 ms |
| `GET /monitors/{id}/uptime` (24 sa, 7 gün, 30 gün) | 2 ms |
| `GET /metrics` (500 monitor) | 39 ms |
| Ana sayfa (500 monitor) | 46 ms, 77 KB |
| Namespace sayfası (50 monitor, 2000 geçmiş çubuğu) | 39 ms, 367 KB |

Kontrol hedefi aynı makinede çalışan hızlı bir HTTP ucuydu (ortalama kontrol
süresi 1-2 ms). Gerçek hedefler daha yavaş yanıt verir; kapasiteyi o zaman
worker sayısı (`UPTIME_WORKERS`) belirler:
kabaca `worker sayısı / ortalama kontrol süresi` kontrol/sn.

## Testin bulduğu sorunlar

Üçü de düzeltildi; sayılar düzeltmeden sonrasına aittir.

1. **Monitor listesi 5-6 saniye sürüyordu.** 24 saatlik istatistik her
   istekte 1,4 milyon ham satırdan hesaplanıyordu. Artık saatlik özetlerden
   okunuyor (10 ms). Sonucu: liste ve ana sayfadaki uptime/ortalama yanıt,
   özetlerin yenilenme aralığı (`UPTIME_ROLLUP_INTERVAL`, 1 dk) kadar geriden
   gelir ve pencere saat başına yuvarlanır.
2. **Scheduler saniyede en fazla worker sayısı kadar kontrol başlatıyordu.**
   Kuyruk dolunca o tick'te vazgeçiyordu; kontroller 2 ms sürse bile sınır
   20/sn idi, yani hedef yük sınırdaydı. Artık tick süresince boşalan
   worker'ları bekliyor. Ayrıca açılışta tüm monitor'lar ilk 5 saniyeye
   yığılıyordu; şimdi kendi aralıklarına yayılıyor.
3. **Ana sayfa 500 monitor'da açılmıyordu.** Her monitor için ayrı geçmiş
   isteği atılıyordu (500 paralel istek, 3 sn'de zaman aşımı). Geçmiş artık
   tek istekte geliyor; `DASHBOARD_FLAT_LIMIT` (50) üzerindeki kurulumlarda
   ana sayfa sorunlu monitor'ları ve namespace özetini gösteriyor, her
   namespace'in kendi sayfası var.

4. **Açılıştan sonraki 16 saniye hiç sonuç yazılmıyordu.** Saatlik
   özetlerin ilk hesaplaması tek bir ifadeyle 10 milyon satırı okuyor ve
   veritabanının tek yazma bağlantısını tutuyordu; kontroller de bu sırada
   bekliyordu. Hesaplama artık saat saat yapılıyor, retention silmesi de
   20.000 satırlık parçalarla; arada sonuçlar yazılabiliyor. Bekleyen
   monitor'lar aynı ana yığılmasın diye geciken bir monitor döngüdeki
   yerini koruyor.

## Yedekleme

600 MB'lık veritabanının kopyası kontroller sürerken 8-10 saniyede alındı
(580 MB). Bu sırada sonuç yazımı kesilmedi. Kopyadan başlatılan sunucu 500
monitor'u ve 10 milyon sonucu sorunsuz okudu.

## Satır boyutları

2026-10-29, şema 26 (S32): boş bir veritabanına 100 monitor, her birine
7 gün boyunca 60 sn aralıkla sonuç, scheduler gibi zamana göre karışık ve
100'lük toplu yazımla; boyut, WAL boşaltıldıktan sonra
`page_count × page_size` farkı. İndeksler dahildir.

| Satır | Boyut |
| --- | --- |
| HTTP sonucu (%99 başarılı, %1 hata metniyle) | 65 B |
| Aynısı, HTTPS sertifika bitişiyle | 71 B |
| Başarısız HTTPS sonucu, ayrıntılarıyla (zaman aşımı, süre dağılımı, TLS) | 550 B |
| Saatlik özet (monitor × saat; bir yıl, saat saat yazılmış) | 41 B |
| Bildirim kaydı (bir teslimat denemesi) | 93 B |

Veri volume'unun boyut kuralı bunlardan çıkar: README "Geçmiş verisi".

## HTTP kontrolünün bağlantıları (0.52)

2026-11-07, S92 ([#144](https://github.com/yusufceng/Kubeat/issues/144)):
**ölçüldü (ajan içi, httptest)**. Ortam: geliştirici dizüstü bilgisayarı
(macOS, Apple Silicon), Go 1.26.10. Checker paketi doğrudan sürüldü,
scheduler ve veritabanı yoktu. 2000 annotation monitor'ü vardı; her biri
nesnesinin namespace'ine, yani kapsama bağlıydı. 20 worker'la
(`UPTIME_WORKERS` varsayılanı) her monitor iki kez kontrol edildi. Hedef ayrı
bir süreçte çalışan tek bir httptest HTTPS sunucusuydu (HTTP/1.1, loopback).
Değerler yalnız istemci tarafının farkıdır: ölçümden önceye göre, iki GC'den
sonra `HeapInuse`, `StackInuse` ve goroutine sayısı; bağlantılar istemcide açık
kalanlardır. Aralıklar tekrarlanan koşulardandır (0.51.0 dört, 0.52 üç koşu).

| Ölçüm (2000 annotation monitor'ü) | 0.51.0 | 0.52 |
| --- | --- | --- |
| Kontrollerden sonra açık bağlantı | 2000 | 0 |
| Heap | +56–58 MiB | +8,5 MiB |
| Stack | +16 MiB | +2–2,5 MiB |
| Goroutine | +4000 | 0 |
| Kontrol süresi, ikinci tur ortalaması | 0,30 ms (önceki kontrolün bağlantısında) | 6,5–7,6 ms (yeni bağlantı ve TLS el sıkışması) |

- **Kalan heap.** 0.52'de kalan, monitor başına tutulan ve bağlantı taşımayan
  HTTP taşıyıcısıdır (monitor başına yaklaşık 4 KiB). Monitor silinince,
  duraklatılınca ya da kapsamdan çıkınca bırakılır. Kapsama bağlı olmayan 2000
  monitor'de aynı ölçüm 0 bağlantı ve +0,4 MiB heap verdi; 0.51.0'ın paylaşılan
  havuzu aynı hedefe en çok 2 bağlantı tutuyordu.
- **Kontrol süresi.** Loopback'te ve 20 eşzamanlı el sıkışmayla ölçüldü.
  Gerçek ağda her kontrole ad çözme ve bağlantı kurmanın gidiş-dönüşleri
  eklenir; eşik için README "Yavaş yanıt".
- **Sertifika.** Sunucu sertifikasını değiştirince 0.51.0'da sonraki kontrol
  eski bağlantıdan eski sertifikanın bitişini raporladı; 0.52'de bir sonraki
  kontrol yenisini raporlar ve sertifika olayı kapanır (checker testi
  `TestHTTPCheckSeesRenewedCertificate`, kapsama bağlı ve bağlı olmayan
  monitor). Sahadaki etki sunucunun keep-alive politikasına bağlıdır.
- **CI'da.** `TestHTTPCheckClosesItsConnections` 100 kapsama bağlı monitor ve
  4 worker'la, HTTP/1.1 ve HTTP/2'de, kontrollerden sonra açık bağlantının 0,
  aynı anda açık bağlantının en çok worker sayısı olduğunu sınar; süreye bağlı
  bir eşiği yoktur.
- **Ölçülmedi.** Gerçek cluster'da, chart'ın 256 Mi bellek limitiyle ve
  gerçek hedeflerle (aşağıda).

## Ölçülmeyenler

- Uzun süreli çalışma (günler): veritabanı büyümesi, retention'ın
  10 milyon satırda silme süresi ve WAL boyutu gözlenmedi.
- Yavaş ya da zaman aşımına uğrayan çok sayıda hedef.
- Cluster içindeki kaynak limitleriyle (chart varsayılanı 256 Mi) davranış.
- Çok sayıda eşzamanlı arayüz kullanıcısı.
