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

**(a) ve (b) önce (0.46 turu, #80/#81); (c) ve (d) sprint 5'te (#84/#85);
(c)'nin kalan boşluğu çözüm anında denetimle sprint 6'da (0.48, #95).**

- **(a)** `UPTIME_TARGET_DENY_CIDRS`'in rolü belgelendi: bir monitor ya da
  kanal webhook'u politikanın izin verdiği her adrese ulaşır; hassas ya da
  çok kiracılı bir cluster yönetim ve iç servis aralıklarını engellemelidir
  (README "Hedef politikası", docs/pilot.md).
- **(b)** `UPTIME_CHECK_SHOW_VALUE` (varsayılan `true`) eklendi. `false`
  iken JSON kontrolü yalnızca path'in eşleşip eşleşmediğini bildirir, bulunan
  değeri sonuç ve hataya yazmaz.
- **(c)** Kullanıcı başına hedef kısıtı **yapıldı** (sprint 5, #84). Her
  namespace'i gören bir kullanıcı (yönetici, API token'ı, kapsamı `*` olan
  editör) her hedefe ulaşır; kapsamı sınırlı bir kullanıcı cluster içi bir
  hedefe (`netguard.InCluster`) yalnızca adından okunan namespace'i
  (`servis.namespace.svc`, pod adı; `netguard.Namespace`) kapsamındaysa
  (`CanRead`) ulaşabilir. Çıplak bir ClusterIP/pod IP'si ya da Kubeat'in kendi
  namespace'indeki kısa ad gibi namespace'i adından okunamayan cluster içi bir
  adres, sınırlı bir kullanıcı için reddedilir; cluster dışı hedefler eskisi
  gibi yalnızca hedef politikasına tabidir. Meşru cross-namespace izleme
  (kapsamı o namespace'i içeren kullanıcı için) bozulmaz; kısıt hedef oluşturma
  ve değiştirmede uygulanır, var olan monitor'ler yeniden denetlenmez. Monitor
  API'si ile kanal webhook'u yolunu kapsar (`backend/internal/api`,
  `backend/internal/netguard`).

  **Çözüm anında (dial-time) denetim (0.48, #95).** (c) hedefi 0.47'de yalnız
  adından (`netguard.InCluster`) sınıflıyordu; bir cluster son ekiyle bitmeyen
  iki etiketli kısa ad (`api.payments`, `kubernetes.default`) ya da iç adrese
  çözülen genel bir ad (`172-30-0-1.sslip.io`) "dış" sayılıp denetimi atlıyordu,
  oysa pod'un DNS arama alanı (`ndots`) onu iç Service'e ya da API sunucusuna
  çözüyordu (aşağıda "Kapsamın kalan boşluğu"). 0.48'de kapsam denetimi, adı dış
  görünen bir hedefi çözer (`netguard.ResolvesInCluster`, backend'in kendi
  çözücüsüyle, bir kez): çözülen adres cluster aralığındaysa, adında kapsama
  bağlanabilecek bir namespace taşımadığı için sınırlı kullanıcı için reddedilir.
  Yalnız ad-sınıflama yaması (iki etiketli adı `InCluster`'a eklemek) yetmezdi —
  iç adrese çözülen genel adı kaçırırdı. Çözümleme kontrol anında bir kez yapılır
  ve karar o yanıta dayanır; adı bu çözümle kontrol anı arasında değişen (DNS
  rebinding) bir hedef bu kapının dışındadır (TOCTOU). Bu kalan boşluk için
  deny-CIDR ve çok kiracılı prova derinlemesine savunma olarak kalır. Yük
  korumaları (`guardrails.External`, scheduler host tavanları) bilinçli olarak
  adı yazıldığı gibi sınıflamayı sürdürür: bunlar kiracı sınırı değil yük
  korumasıdır, çözümlemeyi zamanlayıcının sıcak yoluna taşımak sınıfı bir zone
  sahibinin eline bırakırdı ve iç bir adı dış saymak yalnızca daha kısıtlayıcıdır
  (daha uzun asgari aralık, host tavanı), cross-tenant ihlali değil. Kiracı
  sınırı kapsam denetimidir (`api.targetInScope`).

  **Çözüm hatasında güvenli taraf (0.49, #107).** 0.48'in çözüm-anı denetimi,
  ad çözümü hata ya da zaman aşımı verince (CoreDNS kesintisi, SERVFAIL,
  NXDOMAIN) adı sessizce "dış" varsayıp hedefi kabul ediyordu
  (`netguard.ResolvesInCluster` hata → `false` → `api.targetInScope` → `nil`):
  kapsamı sınırlı bir kullanıcı CoreDNS kesintisinde `api.payments` gibi dış
  görünen bir adla monitor kurabiliyor, kayıt kalıyor ve çözücü düzelince checker
  onu cluster içine bağlıyordu (var olan monitor'ler yeniden denetlenmez,
  `checker.go` değişmedi). 0.49'da bu fail-open kapatıldı: kapsamı sınırlı bir
  çağıran için adı dış görünen bir hedef yalnızca **ispatlanabilir biçimde
  cluster dışı bir adrese çözülürse** kabul edilir; çözüm hata ya da zaman aşımı
  verirse ad dış kanıtlanamadığı için güvenli tarafta reddedilir. Yönetici ve
  kapsamı `*` olan kullanıcı çözüm yapmadığı için etkilenmez. Çözüme ayrı bir
  zaman aşımı kondu (`UPTIME_TARGET_RESOLVE_TIMEOUT`, varsayılan 5 sn) ki yavaş
  ya da takılı bir çözücü bir oluşturma/değiştirmeyi sunucunun yazma zaman
  aşımına (`UPTIME_HTTP_WRITE_TIMEOUT`, 2 dk) kadar bekletmesin. Red mesajı tek
  genel bir metne indirildi (`api.scopeRefused`): önceki üç ayrı mesaj
  (namespace adını ya da "iç adrese çözülüyor"u söyleyen) sınırlı kullanıcıya bir
  adın iç adrese çözülüp çözülmediğini ya da hangi namespace'e düştüğünü açık
  eden bir iç-adres oracle'ıydı; makine-okur `target_scope` kodu ve iki dilli
  arayüz mesajı korunur.

  **Bilinçli taviz.** CoreDNS kesintisinde kapsamı sınırlı bir kullanıcı dış
  görünen yeni bir monitor ya da kanal webhook'u kuramaz (hedef gerçekte dış
  olsa da, kesinti boyunca); bu, kesinti anında sessiz bir cross-tenant
  kapsam-aşımına izin vermektense yeni kurulumu bekletmeyi yeğleyen bilinçli bir
  seçimdir (işaretle-ve-izin bypass'ı açık bırakacağı için seçilmedi). Var olan
  monitor'ler çalışmaya devam eder ("İzleme asla durmaz"); yalnız sınırlı
  kullanıcının yeni kapsam-dışı create'i reddedilir.

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
- **Cross-tenant savunma (c).** Deny-CIDR, API sunucusu ve ekip Service'leri
  meşru izlenenlerle aynı ClusterIP aralığını paylaştığı için cross-tenant
  erişimi ayıramaz; erişimi asıl sınırlayan (c)'deki kullanıcı başına
  kapsamdır (README "Hedef politikası"). Deny-CIDR ve
  `UPTIME_CHECK_SHOW_VALUE=false` derinlemesine savunma olarak kalır.
- **Kapsamın kalan boşluğu (0.47) — kapatıldı (0.48).** 0.47'de kapsam hedefi
  yalnız `netguard.InCluster` ile adından sınıflıyordu; bir cluster son ekiyle
  bitmeyen iki etiketli kısa ad (`api.odeme`, `kubernetes.default`) cluster dışı
  sayılıp denetimi atlıyordu, oysa backend pod'unun DNS arama alanı (`ndots`) onu
  cluster içindeki bir Service'e ya da API sunucusuna çözebiliyordu. 0.48'de
  (yukarıda "Çözüm anında denetim", #95) kapsam denetimi adı dış görünen hedefi
  çözer ve çözülen adres cluster aralığındaysa sınırlı kullanıcı için reddeder;
  iki etiketli kısa ad, iç adrese çözülen genel ad ve TOCTOU biçimleri negatif
  testlerle tek tek kapsanır (`backend/internal/netguard`,
  `backend/internal/api`). Kalan artık yalnızca DNS rebinding penceresidir
  (adın çözümle dial arasında değişmesi); bunun için deny-CIDR ve çok kiracılı
  prova derinlemesine savunmadır.
- **Çözüm hatasında kapalı (0.49).** 0.48'in çözüm-anı denetimi çözüm hata ya da
  zaman aşımı verince hedefi sessizce kabul ediyordu (fail-open); 0.49'da kapsam
  denetimi çözüm hatasında da kapalı kalır: kapsamı sınırlı bir kullanıcı CoreDNS
  kesintisinde dış görünen bir adla kapsam dışına monitor ya da kanal kuramaz,
  yalnız ispatlanabilir biçimde cluster dışına çözülen ad kabul edilir. Çözüme
  `UPTIME_TARGET_RESOLVE_TIMEOUT` (varsayılan 5 sn) sınırı kondu ve iç-adres reddi
  tek genel mesaja indirilerek iç-adres oracle'ı daraltıldı. Yönetici ve kapsamı
  `*` olan kullanıcı etkilenmez. Bilinçli taviz yukarıda (c), "Çözüm hatasında
  güvenli taraf".
