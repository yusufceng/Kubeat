# ADR-0002: Open-core ve çevrimdışı lisans dosyası

- Durum: Kabul
- Tarih: 2026-10-26

## Bağlam

Kubeat hem şirket içinde kullanılacak hem de ileride satılabilecek. Kod
Apache-2.0 ile yayınlandı ve henüz dış katkı yok. Hedef ortamlar çoğunlukla
internete kapalı OCP cluster'ları. Çok cluster'lı müşteriler her cluster'a
ayrı kurulum yapıyor, merkezi bir Kubeat yok.

## Seçenekler

1. Tamamen açık kaynak, yalnızca destek satmak.
2. Open-core: çekirdek ücretsiz, kurumsal özellikler lisansla açılır.
   Lisans çevrimdışı imzalı bir dosyadır.
3. Open-core, ama lisans bir lisans sunucusundan çevrimiçi doğrulanır.
4. Tamamen kapalı, ücretli ürün.

## Karar

2. seçenek seçildi. Çekirdek izleme her zaman ücretsiz kalır: monitor'ler,
keşif, alarmlar, bildirimler, raporlar ve bir durum sayfası. Lisansla
açılanlar kurumların istediği yönetim özellikleridir: `sso` (LDAP, OIDC),
`scoped_access` (namespace'le sınırlı kullanıcılar), `audit` (denetim
kaydını görmek) ve `status_pages` (birden fazla durum sayfası).

Lisans, Ed25519 ile imzalanmış bir dosyadır. İçinde müşteri, açtığı
özellikler ve bitiş tarihi yazar. Açık anahtar binary'ye gömülüdür, gizli
anahtar çevrimdışı tutulur ve imzalama aracı ayrı, özel bir depodadır.
Dosya Secret olarak verilir ve değiştiğinde yeniden okunur.

İlkeler:

- **İzleme asla durmaz.** Lisans biterse yalnızca açtığı özellikler kapanır.
- **Bitiş erişimi genişletmez.** Bir kısıt (namespace sınırı) lisansla
  konduysa lisans bitince de kalır. Daraltılabilir ya da kaldırılabilir,
  genişletilemez.
- **Hiçbir yere bağlanılmaz.** Lisans sunucusu, telemetri ya da phone-home
  yoktur.
- Bitişten 30 gün önce uyarılır; ücretsiz sınırlar ve uyarı süresi ürün
  kararıdır, ayar değildir.

## Sonuçlar

- Air-gapped kurulumlar lisansla çalışır. Lisans cluster'a bağlanmaz, çünkü
  sandbox gibi ortamlarda servis hesabı `kube-system`'i okuyamıyor; kurulum
  sayısı sözleşmeyle sınırlanır.
- Kod Apache-2.0 olduğu sürece kontrolü koddan çıkarmak yasal olarak
  mümkündür. Lisans dosyası sözleşmenin parçasıdır, teknik bir kilit
  değildir. Kurumsal kodun ayrı bir lisansa taşınması ertelendi
  (docs/backlog.md, "Kurumsal kodun lisansı").
- İmzalama anahtarı kaybolursa yeni lisans verilemez. Çevrimdışı bir yedeği
  olmalıdır.

## Not: denetim satırının log satırı (2026-11-07, 0.52)

`audit` özelliğinin kapısı denetim kaydını **görmektir**: arayüz, API ve CSV.
Kayıt lisanstan bağımsız yazılır (README "Kubeat Enterprise lisansı": "Kayıt
yine yazılır"). Başarılı bir denetim yazımından sonra stdout'a yazılan log
satırı bu kapının dışındadır; lisanssız kurulumda da yazılır, çünkü operatörün
kendi logudur, görme yüzeyi değildir (`docs/backlog.md` "Denetim kaydı: stdout
log satırı ..." kararı). 0.52'de bu, kapsam reddinin `deny`/`target-scope`
satırlarının `WARN`'ına uygulandı (S82, [#143](https://github.com/yusufceng/Kubeat/issues/143)):
API reddi kullanıcıyı, namespace'i, nedeni ve hedefi; kontrol anı reddi
monitor'ü, namespace'i ve kaynağı yazar (yazım başarısız olsa da yazılır,
yanında `audit entry lost` hatası durur). Satırlar ve log satırları dakika
başına `UPTIME_LOGIN_AUDIT_PER_MINUTE` ile sınırlıdır. Denetim kaydının tamamını
stdout'a yazmak, süzgeçler ve CSV backlog'da kalır; karar değişmez.
