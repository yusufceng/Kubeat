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
