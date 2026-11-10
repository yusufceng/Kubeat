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
(c)'nin kalan boşluğu çözüm anında denetimle sprint 6'da (0.48, #95); çözüm
hatasında güvenli taraf sprint 7'de (0.49, #107), vekilli topolojideki
daraltması sprint 8'de (0.50, #121); kurum aralıkları ve kontrolün her
bağlantısında uygulanması sprint 9'da (0.51, #132, #131).**

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
  (kapsamı o namespace'i içeren kullanıcı için) bozulmaz. Kısıt hedef
  oluşturma ve değiştirmede uygulanır; monitor'de 0.51'den beri kontrolün
  yaptığı her bağlantıda da (aşağıda "Her bağlantıda kapsam"), kanalda yalnız
  oluşturma ve değiştirmede. Monitor API'si ile kanal webhook'u yolunu kapsar
  (`backend/internal/api`, `backend/internal/netguard`).

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
  kullanıcının yeni kapsam-dışı create'i reddedilir. Bu taviz bir **kesinti
  anı** için tartıldı; kalıcı vekilli topoloji için aşağıdaki daraltma geçerlidir.

  **Vekilli topolojide NXDOMAIN (0.50, #121).** Hava boşluklu ya da egress'i
  kısıtlı bir kurumda pod'lar dış adları kalıcı olarak çözemez; çözümü ve
  bağlantıyı cluster dışındaki kurumsal vekil yapar (`HTTPS_PROXY`, `NO_PROXY`).
  0.49'un fail-safe'i bu topolojide kapsamı sınırlı kullanıcının bütün dış HTTP
  monitor ve webhook'larını yerel NXDOMAIN'e bakarak reddediyor, her deneme de
  sahte bir cross-tenant denetim satırı (`deny`/`target-scope`, `reason=target`)
  bırakıyordu. PO kararı S77'nin kararını açıkça daraltır: **vekile gidecek bir
  hedefte yerel NXDOMAIN bir kapsam sinyali değildir**, çünkü isteği backend
  değil vekil çözer ve bağlar. 0.50'de opt-in `UPTIME_TARGET_SCOPE_TRUST_PROXY`
  (varsayılan `false`) açıkken, kapsam çözümü yalnız NXDOMAIN verirse ve istek
  vekile gidiyorsa hedef reddedilmez. Vekile gidip gitmediği checker'ın ve
  kanalların kullandığı `http.ProxyFromEnvironment` kararıyla aynıdır
  (`netguard.Proxied`, `NO_PROXY` dahil); yalnız HTTP/HTTPS (anahtar kelime ve
  JSON dahil), WebSocket monitor'leri ve webhook türü kanallar vekile gider.
  Çözüm yine yapılır ve karar o yanıta dayanır:
  - cluster içine çözülen ad (iki etiketli `api.payments`, `kubernetes.default`),
    SERVFAIL, REFUSED ve zaman aşımı yine reddedilir; fail-safe korunur. İç
    adrese çözülen genel ad yalnız backend onu çözebiliyorsa reddedilir;
    vekilli topolojide backend genel adları çözemez, ad NXDOMAIN alır ve vekile
    bırakılır (aşağıda "Şart");
  - `NO_PROXY`'ye uyan ad ve doğrudan bağlanan türler (TCP, DNS, gRPC, ping,
    veritabanı, `each_pod`) istisna almaz;
  - adından cluster içi olan biçimler (çıplak ClusterIP, kısa ad, `.svc`/FQDN,
    API sunucusu adı) zaten çözüme gelmeden ad-sınıflamayla karar bulur;
  - host sınıflanmadan önce vekilin okuduğu gibi okunur: küçük harfe çevrilir ve
    sondaki noktaların hepsi atılır. Rakamlardan oluşan ama adres gibi
    yazılmamış host (`172.30.1`, `0xac.0x1e.0.1`) sınırlı kullanıcı için
    reddedilir, çünkü vekil ve glibc onu bir adres okur, Go'nun çözücüsü ise
    yok sayar ve okuyucular bazı biçimlerde (`010`) anlaşamaz. ASCII olmayan
    host sınırlı kullanıcı için reddedilir; uluslararası ad `xn--` biçiminde
    yazılır. Vekil ve `NO_PROXY` eşlemesi host'u önce IDNA ile okur (yumuşak
    tire U+00AD ve sıfır genişlikli boşluk U+200B silinir, U+3002, U+FF0E ve
    U+FF61 nokta olur), Go'nun çözücüsü ise böyle bir adı sorgusuz NXDOMAIN
    sayar; HTTP/WebSocket monitor'leri ve öteki webhook türleri zaten ASCII
    ister, ntfy kanalının URL'si istemiyordu. Sonunda nokta olan bir ad istisna
    almaz: `NO_PROXY` adı yazıldığı gibi eşler. İnceleme turlarında
    `api.payments.svc..`, `172.30.0.1..` ve `172.30.1` gibi yazımlar ve ntfy
    kanalında yumuşak tireli ya da Unicode noktalı `kubernetes.default` Go
    çözücüsünden sorgusuz bir NXDOMAIN alıp `NO_PROXY`'yi atladığı için
    istisnadan geçiyordu (inceleme bulguları); bu okuma onları kapatır.

  Çözümü tamamen atlamak (Müşteri A'nın önerisi) seçilmedi: var olan
  monitor'ler yeniden denetlenmediği için (S73) `NO_PROXY` ya da vekil sonradan
  değişirse kayıtlı hedef ileride cluster içine çözülmeye başlayabilir. Ayarın
  otomatik açılması da seçilmedi: vekilin cluster dışında olduğunu yalnız
  operatör bilir. **Şart: vekil cluster dışında olmalı**, cluster'ın adlarını
  çözememeli ve ClusterIP/pod adreslerine ulaşamamalı. Cluster içindeki bir vekil
  cluster DNS'ini ve kendi arama alanlarını kullanır: backend'de bugün NXDOMAIN
  alan bir ad (Service'i sonradan oluşturulan `api.payments`, vekilin kendi
  namespace'ine göre çözülen bir ad) vekilde ClusterIP'ye çözülüp oraya
  bağlanabilir; böyle bir kurulumda ayar kapalı kalır. Ayar açıkken kapsamı
  sınırlı kullanıcı, vekilin ulaştığı her adrese (node IP'leri/NodePort, router,
  API ve LoadBalancer VIP'leri, kurumun özel aralıkları) bir adla ulaşır: iç
  adrese işaret eden genel bir ad backend'de NXDOMAIN alır ve vekile bırakılır.
  Vekilin ACL'i bunları kapatmalı, yoksa ayar kapalı kalmalı; ayar açıkken
  kapsamın sınırı vekilin ulaşabildiği yerdir. Vekil verilmiş ve ayar
  kapalıyken backend açılışta bir kez `WARN` yazar. Kalan sınırlar: cluster DNS'i
  dış adlara NXDOMAIN değil SERVFAIL ya da zaman aşımıyla yanıt veriyorsa ayar
  yardım etmez; 0.52'den beri bu red sayaçta, logda ve denetim satırında
  `resolve_error` ile gerçek bir cross-tenant reddinden ayrılır ve operatör
  backend'in yanıtını ayarı açmadan backend pod'undan sınar (`server resolve`,
  README "Vekilli kurulum", "Ön sınama"; S82, #142, #143); gerçek hava boşluklu
  cluster provası kullanıcıdadır.

  **Kurum ağı aralıkları (0.51, #132).** (c)'nin "cluster dışı hedefler yalnızca
  hedef politikasına tabidir" kuralında "cluster dışı" adresle okunur:
  `netguard.InClusterAddr` her özel adresi cluster içi sayar. Kurumun kendi
  ağı (on-prem bağımlılıklar, router VIP'ine çözülen Route adları) de özel
  aralıktadır; kapsamı sınırlı kullanıcı için 0.47'den (çıplak IP) ve 0.48'den
  (çözülen ad) beri reddediliyordu ve bu hedefleri yönetici kuruyordu. PO
  kararıyla operatör bu aralıkları opt-in `UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS`
  (varsayılan boş; boşken 0.50'nin davranışı) ile verir:
  - kural tek yerde, `netguard.ExternalRanges`'tadır: bir adres, kapsam için,
    `InClusterAddr` öyle diyorsa ve listedeki aralıkların hiçbiri onu
    içermiyorsa cluster içidir. `api.targetInScope` bunu çıplak adrese ve bir
    adın çözüldüğü her adrese sorar; kapsamı soran her denetim aynı değeri
    sormalıdır, iki ayrı sınıflama olmasın;
  - çıplak kurum IP'si de çözülen adresle aynı sınıftadır (PO kararı);
  - yalnız adresler böyle okunur. Adından cluster içi olan biçimler (`.svc`
    ve pod adı, kısa ad) adına göre karar bulur; yanıtlarından biri cluster
    içinde olan ad reddedilir;
  - hedef politikası ve yük korumaları ayarı okumaz; bunlar kiracı sınırı
    değildir;
  - aralıklardan biri API sunucusunun adresini (`KUBERNETES_SERVICE_HOST`) ya da
    backend'in kendi adresini içeriyorsa 0.51 ve 0.52'de backend başlamıyordu
    (S80 PO kararı: WARN değil hata, çünkü çakışma Service ya da pod ağını
    namespace'siz açar). Koruma yalnız bu iki örneğe, bu açılıştaki hâlleriyle
    bakar: pod ağının başka bir parçasını içeren aralık bugün geçer ve pod
    oraya taşınınca backend'i durduruyordu (sprint 10 prova (c): drain'de
    CrashLoopBackOff, izleme durdu). 0.53'te (S96,
    [#157](https://github.com/yusufceng/Kubeat/issues/157)) bu ortama bağlı
    denetimler için karar değişti ("İzleme asla durmaz"): backend başlar,
    yalnız çakışan girdi yok sayılır; backend kendi adreslerini okuyamazsa
    denetlenemeyen her girdi de. Listenin geri kalanı geçerlidir. Yok sayılan
    aralık ayarda hiç yokmuş gibi okunur, daha sıkı taraf: kapsam zayıflamaz,
    yalnız o aralığa giden kapsamı sınırlı monitor'ler `blocked_by_scope` ile
    DOWN olur. Açılışta `ERROR` satırı, `kubeat_target_scope_external_ranges_ignored`
    ve `KubeatScopeExternalRangesIgnored` verilir; bütün listeyi yok saymak
    seçilmedi, çünkü kurum hedefli bütün kapsamı sınırlı monitor'leri düşürürdü
    (SRE). Biçim hataları backend'i yine başlatmaz: aynı okuyucu
    (`netguard.ParseExternalRanges`) IPv6 biçiminde yazılmış IPv4 aralığını
    reddeder; koruma ve kapsam denetimi bir yazımda ayrışmaz;
  - listeye ne girer (README "Kurum ağı", "Şart"): pod, Service ve node ağı
    ile API VIP'i hiç girmez; korumanın yakalamadığı çakışmayı dışarıda tutmak
    operatörün sorumluluğundadır. Router VIP'i
    yalnız on-prem Route'lar için bilerek (node ağındaysa `/32` ile) girer;
    o zaman o VIP'teki her Route, başka ekiplerinkiler de, kapsamı sınırlı
    kullanıcıya açılır;
  - keşiften içe aktarma hedefi çözmez, yalnız namespace'e bakar: aralık
    Route'un VIP'ini içeriyorsa içe aktarma, klon ve hedef değişikliği aynı
    sonucu verir; içermiyorsa içe aktarma kabul eder, klon ve hedef değişikliği
    reddeder (README "Hedef politikası", "Kurum ağı").
  Listedeki her adrese kapsamı sınırlı kullanıcı bir namespace'e bağlanmadan,
  Kubeat'in pod'undan ulaşır; ayarın sınırı operatörün listesidir.

  **Her bağlantıda kapsam (0.51, #131).** 0.50'ye kadar kapsam yalnız hedef
  belirlenirken denetleniyordu: monitor kaydı onu kuranın kapsamını taşımıyor,
  checker da kapsama bakmıyordu. Kontrol hedefin kendisinden başka
  bağlantılar da açabildiği için, kayıt anında kapsamda görünen bir monitor
  kontrol sırasında kapsamın dışına ulaşabiliyordu (yolların ayrıntısı
  güvenlik gereği burada yazılmaz). PO kararıyla:

  - **Kural.** Kapsamı sınırlı bir kullanıcının kurduğu monitor'ün kontrolü,
    yaptığı her bağlantıda o kullanıcının kapsamına tabidir. Her bağlantı
    oluşturmadaki kuralla okunur; kural tek yerdedir (`netguard.ScopeRules`)
    ve API de onu sorar: adından cluster içi olan host yalnız kapsamdaki
    namespace'te, çıplak cluster adresi ve kısa ad hiç, adı dış görünen host
    yalnız cluster dışına düşüyorsa kabul edilir; kurum aralıkları (S80) ve
    vekil kararı (S81) aynen geçerlidir. Doğrudan bağlantıda karar bağlanılan
    adrese verilir (`netguard.Dialer.In`), yanıtı kayıttan sonra değişen bir ad
    da (DNS rebinding) böylece yakalanır. Vekile verilen istekte backend
    bağlanmadığı için vekilin istendiği her host, verilmeden önce oluşturmadaki
    gibi çözülüp denetlenir (`netguard.Scope.Proxy`). Kontrolün yöneldiği,
    monitor'ün ayarlarında yazılı olmayan dış görünen bir ad çözülemezse
    (NXDOMAIN, SERVFAIL, zaman aşımı) bağlantı da oluşturmadaki kuralla
    reddedilir: çözülemeyen ad cluster dışında olduğu kanıtlanamaz ve ayrı bir
    hatası, cluster içine düşen addan ayırt edilerek hangi adların orada
    bulunduğunu söylerdi (iç-adres oracle'ı; yukarıda tek red mesajı). Ayarlarda
    yazılı adlar (hedef ve öteki uç noktalar) yazılırken denetlendiği için
    çözülemezse kendi hatasıyla düşer. Kontrolün bağlandığı uç noktalar oluşturma ve
    değiştirmede de denetlenir.
  - **Red.** Kapsamın reddettiği bağlantı kontrolü DOWN yapar. Sonucun hatası
    tek genel bir metindir, adresi ve nedeni söylemez ((d) maskesi; ayrıntı
    yalnız backend log'unda); açıklaması `blocked_by_scope`, arayüzde ve
    bildirimde iki dilde. Ters monitor bunu, hedef politikasının reddi
    (`ErrBlocked`) gibi, beklediği başarısızlık saymaz. Her ret
    `kubeat_target_scope_denied_total{reason="connection"}`'ı artırır; denetim
    kaydına (`deny`/`target-scope`, kullanıcı `checker`) monitor başına bir kez
    yazılır, monitor'ün bağlandığı yeri belirleyen ayarları değişince, monitor
    duraklatılıp açılınca ya da backend yeniden başlayınca bir kez daha. Satır
    sayısı böylece monitor'lerin ve ayar değişikliklerinin sayısıyla
    sınırlıdır; dakika sınırı S82'de `auditCap`'le birleşir. Checker'ın monitor
    başına tuttuğu durum (kendi bağlantı havuzu, son ret) monitor kapsamdan
    çıkınca, silinince ya da duraklatılınca bırakılır.
  - **Kaydın yeri.** Karşılaştırılan seçenekler:
    1. Var olan `settings` JSON sütunu: göç yok, şema 26 kalır, 0.50'ye
       `helm rollback` mümkün. 0.50 alanı okur ama monitor'ü kaydederken
       düşürür (testle sınandı); o monitor kaydı olmayan kuralına düşer.
    2. Yeni sütun: sorgulanabilir ve açık, ama şema göçü; `docs/backlog.md`
       S6 kuralı gereği önce açılışta otomatik yedek gelir ve 0.50'ye rollback
       biter.
    3. Monitor'ün namespace alanından türetmek: göçsüz ve kayıtsız, ama
       yöneticinin bir namespace'e koyduğu monitor'ü kapsamı sınırlı
       kullanıcınınkinden ayıramaz; yönetici monitor'leri de namespace'e
       bağlanırdı.

    (1) seçildi (PO şartı: göç yok). Kayıt
    `"scope": {"namespaces": [...], "discovered": true}` biçimindedir;
    `["*"]` her namespace'tir. Kaydı, kontrolün bağlandığı yeri en son
    adlandıran yazar: oluşturan, klonlayan, içe aktaran ya da hedefi veya
    türün host adı taşıyan bir seçeneğini değiştiren
    (`store.SameConnections`). Kontrolün öteki seçenekleri (ne gönderdiği,
    yanıtı nasıl okuduğu, her pod seçeneği) host adlandırmaz; bağlanılan yeri
    hedef ve onun nesnesi belirler, onları yazanın kapsamı da kayıttadır.
    Kural asimetriktir (`api.scopeRecord`):
    - **Genişletme dar.** Mevcut bir kaydı daha geniş bir kapsamla değiştirmek
      yalnız bu ayarlar değişince olur. Kaydın namespace'lerini tutan biri
      (yönetici, daha çok namespace'i olan editör) kontrolün öteki
      seçeneklerini değiştirince ya da formu kaydedince kayıt olduğu gibi
      kalır; yoksa bir yöneticinin sıradan bir form kaydı kapsamı sınırlı
      kullanıcının monitor'ünden kapsamı kaldırırdı. İstisna kaydı olmayan
      (0.51 öncesi) monitor'dür: kayıt yokken yazanın kim olduğu bilinmez,
      her namespace'i gören birinin her kaydı her namespace'in kaydını yazar
      (aşağıda "Kaydı olmayan monitor"); bedeli yükseltme notunda yazılı.
    - **Daraltma geniş.** Kaydın namespace'lerinin hepsini tutmayan biri
      kontrolün herhangi bir seçeneğini değiştirince (`store.SameCheck`)
      kayıt ikisinin ortak namespace'lerine daralır (`TargetScope.Within`):
      kapsamı sınırlı kullanıcı, yöneticinin monitor'ünün gönderdiğini ya da
      okuduğunu değiştirirse kontrol onun kapsamına bağlanır. Formun bir
      seçeneği saklanandan farklı yazması da değişiklik sayılır; yanlış bir
      daraltmanın bedeli erişilebilirliktir (monitor `blocked_by_scope` ile
      DOWN olur, yönetici hedefi ya da host adı taşıyan bir seçeneği
      değiştirerek ya da kopyalayarak geri alır), kapsam değil.
    - Kontrolü değiştirmeyen bir kayıt (duraklatmak; ad, etiket ya da aralık
      değiştirmek) kaydı korur.

    Seçenekler: (a) kontrolün her değişikliğinde kaydı değiştirene vermek:
    yöneticinin sıradan bir kaydı kapsamı kaldırır; (b) form ile API'nin bir
    seçeneği yazışını alan alan eşitlemek: kural, formla API'nin her seçeneği
    her zaman aynı yazmasına dayanır ve her yeni alanda yeniden sınanmalıdır;
    (c) asimetrik kural: genişletme bağlanılan yeri adlandıran birkaç ayara,
    daraltma her değişikliğe bağlıdır. (c) seçildi. Geri alma maliyeti
    küçük: kural tek yerdedir (`api.scopeRecord`, `store.SameConnections`),
    göç yoktur; listeye bir ayar eklemek genişletmeyi kolaylaştırır, bu yüzden
    yalnız host adlandıran bir ayar eklenir.
    Hedef ya da seçenek gönderen her değişiklik hedefi değiştirenin
    kapsamıyla denetlenir, böylece kapsamı sınırlı kullanıcı hedefi kapsamının
    dışındaki bir monitor'ün kontrolünü üstlenemez. `discovered`, hedefin
    keşiften (içe aktarma ya da annotation) geldiğini söyler: monitor'ün
    kendi host'unun yalnız çözümü denetlenmez, içe aktarmadaki gibi
    namespace'iyle kapsamdadır (router VIP'ine çözülen on-prem Route adı
    reddedilmez). Nesnenin host'unu nesnenin sahibi istediği gibi yazar;
    adından reddedilen bir host (başka namespace'in Service adı, API
    sunucusu, kısa ad) bu yüzden keşiften gelse de reddedilir. Kontrolün başka
    her bağlantısı kayıttaki namespace'lere tabidir.
  - **Annotation monitor'leri.** Annotation'la kurulan monitor'ün hedefini
    nesneyi düzenleyen belirler; bu kişi cluster'ın RBAC'ındadır ve Kubeat
    onun kim olduğunu, Kubeat'te hesabı olup olmadığını bilmez. Kubeat'te
    kapsamı sınırlı kullanıcı olmaması cluster'da namespace'lerine sınırlı
    ekip olmadığı anlamına gelmez. Bu yüzden kaydı **her kurulumda**
    nesnenin namespace'idir (`store.ObjectScope`): eşitleme oluştururken ve
    güncellerken yazar, başka bir kayda dönmüşse bir sonraki turda geri yazar;
    eşitleme yönettiği sürece API'deki değişiklikler kaydı değiştirmez;
    açılış kaydı olmayan annotation monitor'lerine de bunu yazar. Seçenekler:
    (a) her kurulumda namespace'e bağlamak; (b) yalnız kapsamı sınırlı
    kullanıcısı olan kurulumda bağlamak: Kubeat hesaplarına bakar, kapıyı
    tek-kiracılı görünen ama çok ekipli bir cluster'da açık bırakır ve kural
    açılıştaki kararla eşitlemenin kararı arasında ikiye ayrılır. (a)
    seçildi; Müşteri B'nin "yalnız kapsamı sınırlı kullanıcıya" şartından
    bilerek sapılır, çünkü annotation'ın sahibi o kullanıcıdır. Etki: kontrolü
    kendi host'undan başka bir namespace'e ya da cluster içine, kurum aralığı
    yoksa özel adresli bir kurum hostuna giden annotation monitor'ü DOWN olur;
    çare kurum aralığı (S80; on-prem router VIP'i `/32` dahil) ya da monitor'ü
    elle kurmaktır. Vekilli kurulumda (S81 ortamı: backend'in çözücüsü genel
    adları çözemez) kontrolün ulaştığı, backend'in çözemediği başka bir dış
    host da reddedilir ve annotation monitor'ü DOWN olur; çare
    `UPTIME_TARGET_SCOPE_TRUST_PROXY`'dir: yalnız NXDOMAIN'i vekile bırakır,
    SERVFAIL ve zaman aşımı yine red sayılır (S82). Geri alma maliyeti
    küçük: kural tek yerde (`store.ObjectScope`, açılış ve eşitleme), göç
    yok.
  - **Anlık görüntü mü, canlı okuma mı (S73).** Kayıt, yazanın o anki
    kapsamının anlık görüntüsüdür. Canlı okuma (kaydın kullanıcıyı
    göstermesi, kontrolün kullanıcının bugünkü kapsamını okuması) kapsam
    daralınca etkisini hemen gösterir ve S73'ün işinin çoğunu yapardı; ama
    silinen ya da devre dışı bırakılan kullanıcının, servis token'ının, rolü
    düşürülen yöneticinin monitor'leri birden DOWN olur, dizin grubundan gelen
    kapsam yalnız girişte yenilenir ve kontrolün sıcak yolunda kullanıcı
    okunurdu. Anlık görüntü seçildi: kapsam daralınca var olan monitor'ler
    eski görüntüyle çalışmaya devam eder; S73 bu görüntüleri kullanıcıların
    bugünkü kapsamıyla karşılaştırıp işaretler. Görüntü yazanı tutmaz; S73 onu
    kayda göçsüz ekleyebilir (JSON alanı).
  - **Kaydı olmayan monitor.** 0.51'den önce kurulan ya da 0.50'de yeniden
    kaydedilen monitor'ün kaydı yoktur. Seçenekler: (a) değişiklik kaydından
    oluşturanı bulmak: denetim kaydı 90 gün saklanır, `update` satırı hedefin
    değişip değişmediğini söylemez ve rol bugünkü roldür, eski monitor'lerin
    çoğu için cevap çıkmaz; (b) namespace'ten türetmek: monitor'ü her zaman
    namespace'ini düzenleyebilen biri yazdı (`CanEdit`), namespace'siz
    monitor'ü yalnız her namespace'i gören yazabilir; (c) sınırsız saymak:
    yükseltmeden önce kurulmuş kapsam dışı monitor'ler açık kalır. (b) seçildi
    (PO): namespace'siz monitor hiçbir şeye tabi değildir, ötekiler kendi
    namespace'lerine, keşiften gelenler kendi host'larıyla namespace'lerine
    bağlanır (`store.Monitor.TargetScope`). Bir yöneticinin bir namespace'e
    koyup başka yere bağlanan monitor'ü de bu kurala düşer. Sessizce DOWN
    olmasın diye:
    - annotation monitor'lerine açılışta, her kurulumda, nesnenin
      namespace'inin kaydı yazılır (yukarıda "Annotation monitor'leri");
    - ötekiler için, açılışta kapsamı sınırlı bir editör yoksa (yerel hesap,
      dizin grup eşlemesi, LDAP ya da OIDC varsayılan rolü) kimse bir
      monitor'ü namespace'inin dışına yöneltemezdi: kaydı olmayan monitor'lere
      her namespace'in kaydı yazılır (keşiften gelenlere `discovered` ile) ve
      bunlarda değişen bir şey olmaz; tek kiracılı kurulumda değişen yalnız
      annotation monitor'leridir;
    - varsa sayıları açılışta bir `WARN` satırında yazılır, yükseltmeden
      önceki ön kontrol onları listeler (`docs/yukseltme.md`) ve bir
      yöneticinin monitor'ü kaydetmesi, değiştirmeden de olsa, kaydını yazar;
      kapsamı sınırlı bir kullanıcının, hedefi ya da host adı taşıyan bir
      seçeneği değiştirmeyen kaydı yazmaz (monitor namespace'ine bağlı
      kalır);
    - sınır: yalnız bugünkü editörlere bakılır; kapsamı sınırlı editörü
      silinmiş ya da genişletilmiş bir kurulumda onun kurduğu kaydı olmayan
      monitor'ler de her namespace'in kaydını alır.
  - **HTTP kontrolünün bağlantıları (0.52, #144).** Her HTTP kontrolü yeni
    bağlantı açar ve bitince bağlantılarını kapatır; bir kontrolün
    bağlantısını sonraki kontrol yeniden kullanmaz. Kural kapsama bağlı olsun
    olmasın her HTTP monitor'üne uygulanır (PO kararı), böylece aynı hedefe
    elle ve annotation'la kurulmuş iki monitor aynı yanıt süresini ölçer:
    ad çözme, bağlantı ve TLS el sıkışması dahil. Kontrolün kendi içinde
    aynı sunucuya giden yönlendirme bağlantıyı yeniden kullanır. Kapsama
    bağlı monitor'ün kendi taşıyıcısı kalır ama kontroller arasında bağlantı
    tutmaz; taşıyıcının kendisini bugünkü `Prune` temizliği bırakır, bağlantı
    yaşı için ayar yoktur. 0.51'de bu taşıyıcı bağlantısını kontroller arasında
    tutuyordu: boşta bağlantılar monitor sayısıyla büyüyor, yenilenen
    sertifika aynı bağlantıda görünmüyordu
    ([yuk-testi.md](../yuk-testi.md) "HTTP kontrolünün bağlantıları").
    Seçenekler: (a) yalnız kapsama bağlı kontrolde kapatmak: ölçüm semantiği
    monitor'ün nasıl kurulduğuna göre ikiye ayrılır; (b) her HTTP
    kontrolünde kapatmak. (b) seçildi; bedeli her kontrolde bir el sıkışma
    (500 monitor ve 30 sn aralıkta saniyede yaklaşık 17). Geri alma maliyeti
    düşük: kural tek yerde (`checkHTTPAt`), göç yok.
  - **Yönetici ve `*`.** Kaydı her namespace olan monitor (yönetici, servis
    token'ı, kapsamı `*` olan editör, namespace'siz monitor) hiçbir şeye tabi
    değildir; kontrolü 0.50'deki gibi çalışır.
  - **Kalan sınırlar.** Kanallar (webhook türü) yalnız oluşturma ve
    değiştirmede denetlenir (backlog). Vekile verilen istekte karar backend'in
    çözümüne dayanır, vekil adı yeniden çözer; kontrolün yöneldiği ve
    çözülemeyen bir ad (vekilli istekte SERVFAIL ve zaman aşımı; doğrudan
    bağlantıda ayarlarda yazılı olmayan her ad) oluşturmadaki gibi güvenli
    tarafta red sayılır. CoreDNS kesintisinde kapsamı sınırlı ve annotation'la
    kurulan monitor'lerin bu kontrolleri bu yüzden DOWN olur ve bildirim DNS
    kesintisini kapsam reddi diye söyler; 0.52'den beri bu red sayaçta, logda
    ve denetim satırında `resolve_error` ile gerçek bir reddinden ayrılır,
    kontrolün zaman aşımının ya da kapanışın yarıda kestiği çözüm red sayılmaz
    (S82, #142). Denetim (0.52, S82, #143):
    - `deny`/`target-scope` satırları ve log satırları dakika başına
      `UPTIME_LOGIN_AUDIT_PER_MINUTE` ile sınırlıdır; API'nin ve kontrollerin
      redleri ayrı bütçe kullanır, sayaç her reddi sayar. Sınıra takılan bir
      kontrolün ilk reddi sonradan yazılmaz; dakikanın uyarısı sayısını
      söyler.
    - Push token yenileme, monitor silme, test bildirimi ve olay notu redleri
      hedef seçimi değil yetki reddidir ve denetlenmez (README "Kapsam ihlali
      denetimi", "Denetlenmeyen red yolları").
    - Kayıtlar API ve arayüzden, log satırı merkezi logdan okunur; SIEM'e
      kendiliğinden akmaz (denetim kaydının tamamını stdout'a yazmak backlog'da).

    Kapsam daralması var olan kayıtları değiştirmez (S73). Yönetici, kaydı
    başka birinin olan bir monitor'ü kendine ancak hedefi ya da host adı
    taşıyan bir seçeneği değiştirerek ya da monitor'ü kopyalayarak alır;
    formun farklı yazdığı bir seçenekle daralmış bir monitor de böyle geri
    alınır.

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
  prova derinlemesine savunmadır. 0.51'den beri kapsamı sınırlı monitor'lerin
  doğrudan bağlantılarında bu pencere de kapandı: kontrol bağlandığı adresi
  denetler (yukarıda (c), "Her bağlantıda kapsam").
- **Çözüm hatasında kapalı (0.49).** 0.48'in çözüm-anı denetimi çözüm hata ya da
  zaman aşımı verince hedefi sessizce kabul ediyordu (fail-open); 0.49'da kapsam
  denetimi çözüm hatasında da kapalı kalır: kapsamı sınırlı bir kullanıcı CoreDNS
  kesintisinde dış görünen bir adla kapsam dışına monitor ya da kanal kuramaz,
  yalnız ispatlanabilir biçimde cluster dışına çözülen ad kabul edilir. Çözüme
  `UPTIME_TARGET_RESOLVE_TIMEOUT` (varsayılan 5 sn) sınırı kondu ve iç-adres reddi
  tek genel mesaja indirilerek iç-adres oracle'ı daraltıldı. Yönetici ve kapsamı
  `*` olan kullanıcı etkilenmez. Bilinçli taviz yukarıda (c), "Çözüm hatasında
  güvenli taraf".
- **Vekilli topolojide daraltma (0.50).** Cluster dışındaki bir vekile giden
  HTTP isteğinde yerel NXDOMAIN, opt-in `UPTIME_TARGET_SCOPE_TRUST_PROXY` ile
  kapsam reddi sayılmaz; cluster içine çözülen ad, SERVFAIL ve zaman aşımı yine
  reddedilir (yukarıda (c), "Vekilli topolojide NXDOMAIN").
- **Her bağlantıda kapsam (0.51).** Kapsamı sınırlı kullanıcının monitor'leri
  kontrolün her bağlantısında onun kapsamına tabidir; kayıt `settings`
  JSON'unda, şema 26, 0.50'ye rollback mümkün. Yönetici ve `*` etkilenmez;
  kaydı olmayan monitor namespace'ine bağlanır, kapsamı sınırlı editör yoksa
  her namespace'in kaydını alır; annotation monitor'ü her kurulumda nesnesinin
  namespace'ine bağlıdır. Kanallar ve kapsam daralması sonraki işlerdir
  (yukarıda (c), "Her bağlantıda kapsam").
