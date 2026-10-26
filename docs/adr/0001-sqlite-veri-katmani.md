# ADR-0001: Veri katmanı olarak SQLite

- Durum: Öneri
- Tarih: 2026-10-12

## Bağlam

Araç; monitor tanımlarını ve check sonuçlarını saklamak zorunda. Hedef ölçekte
(500 endpoint, 30 saniyede bir kontrol) günde yaklaşık 1,4 milyon check sonucu
oluşur. Kurulumun basit olması, air-gapped OCP ortamlarında ek bir veritabanı
servisi gerektirmemesi ve container'ın root olmadan, minimal bir image ile
çalışması isteniyor.

## Seçenekler

1. **SQLite** (tek dosya, PVC üzerinde): ek servis yok, operasyonu basit; tek
   yazıcı ve tek replica ile sınırlı.
2. **PostgreSQL**: yatay ölçek ve HA mümkün; ama kurulum, yedekleme ve işletme
   yükü getirir, MVP için fazla.
3. **Sonuçları Prometheus'a yazmak**: zaman serisi için uygun; ama monitor,
   olay ve alarm kuralı gibi ilişkisel veriler için ayrıca bir depo yine gerekir.

## Karar

MVP için SQLite kullanılır.

- **Sürücü:** `modernc.org/sqlite`. CGO gerektirmez; statik binary ve UBI
  tabanlı minimal image üretmek kolaylaşır.
- **Eşzamanlılık:** WAL modu. Yazmalar tek bağlantılı ayrı bir havuzdan
  (`_txlock=immediate`), okumalar `query_only` açık ayrı bir havuzdan geçer.
  Böylece `SQLITE_BUSY` hataları uygulama seviyesinde sıraya dönüşür.
- **Yazma biçimi:** check sonuçları tek tek değil, batch halinde tek
  transaction ile yazılır (`InsertCheckResults`).
- **Soyutlama:** kodun geri kalanı `store.Store` interface'ine bağımlıdır;
  ileride PostgreSQL'e geçiş bu interface'in ikinci bir implementasyonudur.
- **Şema:** zamanlar Unix milisaniye (INTEGER, UTC) olarak saklanır.
  Migration'lar binary'ye gömülüdür ve dosya adı sırasıyla uygulanır.
  Uygulananlar dosya adıyla `schema_migrations` tablosuna yazılır; böylece
  iki dalda eklenip ters sırayla birleşen bir migration da çalışır.
  Uygulanan sayı ayrıca `PRAGMA user_version` içinde tutulur: eski
  binary'ler yeni bir veritabanını yine reddeder, yedek doğrulaması da bu
  sayıya bakar. Tablodan önceki veritabanlarında ilk `user_version` kadar
  dosya bir kez adıyla kaydedilir (0.41.1).

## Sonuçlar

- Backend tek replica çalışır; HA bu kararla mümkün değildir. Aracın kendi
  çökmesi dead man's switch ile fark edilecektir (ayrı ADR).
- Retention zorunludur. `DeleteCheckResultsBefore` ham veriyi siler; dakikalık
  ve saatlik özet tabloları henüz yoktur ve ayrı bir işte eklenecektir.
- Yedekleme (Litestream benzeri) henüz ele alınmadı.
- Olay, alarm kuralı ve bildirim kanalı tabloları alarm motoruyla birlikte
  sonraki migration'larda eklenecektir.
- Hedef ölçekteki yazma yükü henüz ölçülmedi; yük testi ayrı bir spike'tır.
