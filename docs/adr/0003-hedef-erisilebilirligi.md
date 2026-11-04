# ADR-0003: Hedef erişilebilirliği ve iç keşif yüzeyi

- Durum: Kabul
- Tarih: 2026-11-04

## Bağlam

Yetkili bir güvenlik değerlendirmesi (kendi uygulamamıza karşı koşan otonom
bir sızma testi aracı) giriş yapmış bir kullanıcının bir monitor ya da kanal
webhook'uyla iç cluster servislerine (ör. `kubeat-backend:8080`)
ulaşabildiğini gösterdi. JSON-path kontrolü başarısız olunca bulduğu kısa
değeri hata mesajına yazdığı için, bu aynı zamanda sınırlı bir veri sızdırma
oracle'ı oluşturuyor.

Kubeat cluster-native bir izleme aracıdır: işi tam da cluster içindeki
servisleri yoklamaktır. `netguard` paketi loopback'i, link-local'i (bulut
metadata servisleri dahil) ve multicast'i yerleşik olarak engeller; ama özel
ağ (RFC1918) adreslerini bilerek açık bırakır, çünkü izlenen hedefler orada
yaşar. Operatör `UPTIME_TARGET_DENY_CIDRS` ile ek aralıkları kapatabilir.

Yani "iç servise erişim" büyük ölçüde ürünün doğasıdır; soru, bir kullanıcının
bu yeteneği istenmeyen bir keşif ya da sızdırma aracına çevirmesini nasıl
sınırlayacağımızdır.

## Seçenekler

1. RFC1918'i de varsayılan olarak engelle. Cluster içi izlemeyi bozar; kabul
   edilemez.
2. Olduğu gibi bırak; operatör hassas aralıkları `UPTIME_TARGET_DENY_CIDRS`
   ile kapatsın ve bu belgelensin.
3. (2) + JSON kontrolünün bulduğu değeri hataya yazmasını opsiyonel yap, böylece
   yanıttan veri okuma oracle'ı daraltılabilir.
4. (3) + kullanıcı başına hedef kısıtı: bir kullanıcının yalnızca kendi
   namespace'lerine ya da rolüne uygun adreslere monitor kurabilmesi.

## Karar

**Şimdi (3), (4) sonraya.**

- **(a)** `UPTIME_TARGET_DENY_CIDRS`'in rolü belgelendi: bir monitor ya da
  kanal webhook'u politikanın izin verdiği her adrese ulaşır; hassas ya da
  çok kiracılı bir cluster yönetim ve iç servis aralıklarını engellemelidir
  (README "Hedef politikası", docs/pilot.md).
- **(b)** `UPTIME_CHECK_SHOW_VALUE` (varsayılan `true`) eklendi. `false`
  iken JSON kontrolü yalnızca path'in eşleşip eşleşmediğini bildirir, bulunan
  değeri sonuç ve hataya yazmaz.
- **(c)** Kullanıcı başına hedef kısıtı sprint 5'e ertelendi. Yetki modelinin
  her okuyucusuna (`CanRead`/`CanEdit`, namespace süzgeci) dokunur ve
  L boyutludur; cluster-native izlemenin meşru kullanımını (başka bir
  namespace'teki bir Service'i bilerek izlemek) bozmadan yapılması tasarım
  ister.

- **(d)** Monitor kontrol sonucunun hatasındaki (`result.Error`) çözülen
  IPv4/IPv6 adresleri de maskelenir (`[address hidden]`), çünkü bu hata bir
  monitor'ü okuyabilen herkese döner ve aksi hâlde bir iç cluster adresini
  sızdırır (Go'nun ağ hataları bağlandıkları adresi yazar; #77/#79'un kanal
  yolunda kapattığı sızıntının aynı sınıfı, farklı yüzeyde). Hata sınıfı ve
  host adı kalır; maskeleme kanal yoluyla aynı ortak yardımcıdan gelir
  (`backend/internal/addrmask`). İki durum adresi bilerek saklar: `netguard`'ın
  reddi yasakladığı adresi ve aralığı söyler (operatör onu açabilsin) ve DNS
  kontrolü doğası gereği adresleri raporlar (bulduğu kayıtlar). Başarısız bir
  kontrolün `diagnostics` ayrıntısındaki bağlanılan **adres** (`ip:port`) ise
  belgelenmiş bir tanı alanı olarak bilerek açık kalır (README "Başarısız
  kontrolün ayrıntıları").

## Sonuçlar

- **İzleme asla durmaz.** RFC1918 varsayılan açık kalır; cluster içi hedefler
  çalışır.
- **Operatör sorumluluğu.** Sıkı ortamlarda koruma, deny-CIDR ve
  `UPTIME_CHECK_SHOW_VALUE=false` ile operatöre bırakılır; varsayılanlar
  izlemeyi ve hata ayıklamayı kolay tutar.
- **Sızdırma sınırlı.** JSON değeri zaten en çok 100 karakterdir, bir
  kontrolde bir alan taşınır ve bir monitor hızında gider; (b) ile tümüyle
  kapatılabilir.
- **Sonuç hatası adres taşımaz.** (d) ile başarısız bir kontrolün hatası iç
  cluster adresini (çözülen IP) artık sızdırmaz; tanı için gereken bağlanılan
  adres yalnızca belgelenmiş `diagnostics.address` alanında kalır.
- **Kalan risk sprint 5'te.** (c) gelene kadar, aynı cluster'da birbirine
  güvenmeyen kullanıcılar varsa deny-CIDR tek savunma hattıdır.
