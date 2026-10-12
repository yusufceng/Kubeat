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

## Ölçülmeyenler

- Uzun süreli çalışma (günler): veritabanı büyümesi, retention'ın
  10 milyon satırda silme süresi ve WAL boyutu gözlenmedi.
- Yavaş ya da zaman aşımına uğrayan çok sayıda hedef.
- Cluster içindeki kaynak limitleriyle (chart varsayılanı 256 Mi) davranış.
- Çok sayıda eşzamanlı arayüz kullanıcısı.
