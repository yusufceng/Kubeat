# ADR-0004: Giriş kilitlemesini kaynak IP-başına throttle'a çevirmek

- Durum: Kabul
- Tarih: 2026-11-05

## Bağlam

0.47.0'a kadar giriş deneme sınırı yalnızca kullanıcı adına göre tutuluyordu:
bir ad için `UPTIME_LOGIN_LOCKOUT` (15 dk) içinde `UPTIME_LOGIN_MAX_FAILURES`
(10) hatalı denemeden sonra o ad kilitleniyor ve süre dolana kadar doğru parola
da reddediliyordu (`backend/internal/api/auth.go`, `signInKey` kullanıcı adına
göre).

Bu, internete açık bir girişte bir hizmet reddi (DoS) açığıdır (CWE-307).
Girişi public bir GKE Ingress'te (`kubeat.example.com`) açıkta olan bir
operatör (Müşteri B/Ece) şunu gösterdi: adını herkesin bildiği `admin`
hesabını, ~`UPTIME_LOGIN_MAX_FAILURES` kadar istekle, tam bir kesinti anında
15 dakika boyunca dışarıda bırakmak mümkün. Kilit kendiliğinden kalkıyor olsa
da, izlemeye en çok ihtiyaç duyulan anda yöneticiyi kendi aracından çıkarmak
kabul edilemez. Backlog bu yönü (kaynak IP başına kademeli throttle, hesabı
sert kilitlememek) sprint 6'ya bağladı (`docs/backlog.md`).

Kısıtlar:

- İzleme asla durmamalı; kilit yalnızca girişi etkiler, kontrolleri değil.
- İstemciye bir kullanıcının var olup olmadığı sızdırılmamalı (#78 ile
  tutarlı): yanıt genel kalmalı, kilit 429 + `Retry-After` ile verilmeli.
- Sayaç tek replica için backend belleğindedir; çok replicalı dağıtık durum
  bu ADR'nin kapsamı dışıdır (yol haritası "Bilinen sınırlar").
- Kaynak adresi doğru okunmalı. Backend çoğu kurulumda bir Ingress/Route ya da
  yük dengeleyici arkasındadır; o zaman TCP eşi (peer) hep aynı vekil adresidir.
  Gerçek istemci ancak güvenilen vekillerin eklediği `X-Forwarded-For`'dan
  okunabilir ve bu başlık, vekil yoksa istemci tarafından uydurulabilir.

## Seçenekler

1. **Olduğu gibi bırak** (kullanıcı-adı bazlı sert kilit). CWE-307 açık kalır;
   kabul edilemez.
2. **Hesap-başına sert kilidi tümüyle kaldır, yalnız kaynak IP-başına
   throttle.** Meşru kullanıcı asla kilitlenmez; ama dağıtık bir tahmin (çok
   sayıda IP) hiçbir hesap sınırına çarpmadan sürebilir.
3. **Kaynak IP-başına throttle birincil, hesap-başına gevşek ve çok yüksek bir
   tavan ikincil.** Tek bir kaynak birincil throttle'la düşük tutulur; hesap
   tavanı yalnızca tüm kaynaklardaki toplam başarısızlık için ve meşru sahibin
   kendi hatalarının asla ulaşamayacağı kadar yüksek bir sınırdır.

## Karar

**Seçenek 3.** Birincil throttle artık kaynak adresi + kullanıcı adına göre
anahtarlanır (`backend/internal/api/throttle.go`, `signInIPKey`): bir adın bir
kaynaktan gelen hatalı denemeleri yalnızca o kaynağı sınırlar, adı herkes için
değil. Böylece saldırganın kaynağı kilitlense bile meşru sahip başka bir
adresten doğru parolayla girebilir (CWE-307 kapanır).

Hesap-başına sert kilit tümüyle kaldırılmadı (açık soru, varsayılan: hayır).
İkincil, çok yüksek bir tavan (`UPTIME_LOGIN_ACCOUNT_MAX_FAILURES`, varsayılan
50; `0` kapatır) bir adın tüm kaynaklardaki başarısızlıklarını sınırlar, ki
dağıtık bir tahmin sonsuza dek süremesin. Birincil throttle (varsayılan 10) tek
bir kaynağı tavanın çok altında tuttuğundan, meşru sahibin kendi hataları bu
tavana ulaşmaz.

Kaynak adresi `UPTIME_TRUST_PROXY_HOPS` ile belirlenir: arayüzün önündeki
vekillerin `X-Forwarded-For`'a yazdığı girdi sayısı, hiçbiri yazmıyorsa `1`
(aşağıda "Arayüz yolu (0.50)"; 0.48–0.49'da tanım "önünüzdeki kendi
vekillerinizin sayısı" idi). `0` (varsayılan) hiçbir `X-Forwarded-For`
başlığına güvenmez ve bağlanan eşi kullanır; `N` ise istemciyi
`X-Forwarded-For`'un sağdan N girdisini (vekillerinizin eklediği) güvenerek, solundaki (istemcinin
uydurabileceği) girdileri yok sayarak okur. Bir Ingress/yük dengeleyici
arkasında eş hep vekilin adresidir; o yüzden kaynakları ayırt etmek için bu
değer ayarlanmalıdır, yoksa throttle tek bir paylaşılan, ad-başına kilide
geriler (0.47.0 davranışı). Bu, mevcut vekil/`X-Forwarded-Proto` güveni
(frontend `cookieSecure`) kararıyla aynı mantıktadır: başlığa yalnızca
operatör açıkça güvenince güvenilir.

**Sessiz etkisizliğe karşı uyarı ve güvenli-geriye düşüş (0.49).** Yayımlanmış
bir DoS düzeltmesi operatör ayarı bilmediğinde sessizce etkisiz kalmamalıdır.
0.49 iki durumu çalışma anında (dakikada en fazla bir kez, `clientIP`) loga
WARN olarak yazar: (a) `X-Forwarded-For` geldiği hâlde `UPTIME_TRUST_PROXY_HOPS=0`
— başlık varsa önde bir vekil vardır ama ona güvenilmiyor, throttle tek
paylaşılan ada-başına kilide gerilemiştir; (b) `X-Forwarded-For` zinciri
`UPTIME_TRUST_PROXY_HOPS`'tan kısa gelir — güvenilen hop sayısı gerçek girdi
sayısından büyük ya da bir istek vekili atlıyordur. (b) durumunda görülen kaynak
artık istemcinin ayarlayabileceği en soldaki girdiye (`chain[0]`) değil, hiçbir
istemcinin denetlemediği eşe (peer) düşer: uydurma bir `X-Forwarded-For` ile ne
her istekte taze bir kaynak gibi görünüp throttle atlanabilir ne de bir kurban
kilitlenebilir. Yanıt hâlâ değişmez (genel 401/429).

**Arayüz yolu (0.50).** Tarayıcı backend'e doğrudan gelmez: Route ve Ingress
yalnız arayüze gider ve backend'in eşi her istemci için arayüz pod'udur. 0.48 ve
0.49'da arayüz giriş, parola değiştirme ve durum sayfası kilidi isteklerini
backend'e `X-Forwarded-For` olmadan iletiyordu; bu yüzden kaynak-başına throttle
tarayıcı yolunda, hop değeri ne olursa olsun, ad-başına tek paylaşılan kilide
geriliyor ve yukarıdaki (a) uyarısı bu yolda hiç düşmüyordu (bilinen sınır).
0.50'de arayüz bu üç çağrıda gelen başlığı **aynen** taşır (girdi eklemez,
silmez; başlık yoksa eklemez). Next.js başlığı, gelen istekte yoksa (önde onu
yazan bir vekil de istemci de yazmadıysa) kendi eşiyle doldurur ve var olana
eklemez. Server action soket eşini
okuyamadığı için arayüzün kendi gördüğü eşi eklemesi seçilmedi (backlog). Arayüz
böylece backend'e en yakın vekilin yerini alır; değer, arayüzün önündeki
vekillerin `X-Forwarded-For`'a yazdığı girdi sayısıdır, hiçbiri yazmıyorsa `1`.
Next.js istemcinin gönderdiği başlığa da dokunmadığı için `1` (vekilsiz) yalnız
arayüze başlığı yazan ya da değiştiren bir vekilden geçmeden hiçbir güvenilmeyen
istemci ulaşamıyorsa (port-forward) geçerlidir; arayüz Service'i doğrudan
dışarı açıksa (LoadBalancer, NodePort) değer `0` kalır, ki yanlış ayarın
maliyeti bypass değil tek paylaşılan kilit olsun.

**Somut hop örnekleri.** Arayüz yoluna göre türetildi; hiçbiri henüz bu yolda
ölçülmedi (README "Hesaplar ve giriş", "Vekil-hop rehberi"):

- Vekilsiz, arayüze `kubectl port-forward`: `1` (girdiyi Next.js yazar).
  Ölçülmedi.
- Düz OpenShift Route (HAProxy router, varsayılan `forwardedHeaderPolicy:
  Append`): `1`. Ölçülmedi.
- Önünde başlığı yazan bir WAF ya da L7 yük dengeleyici olan Route: `2`.
  Ölçülmedi.
- GKE `ingress-nginx`, L4 LoadBalancer Service, `externalTrafficPolicy: Local`,
  varsayılan ayarlar: `1` (controller gelen başlığı kendi eşiyle değiştirir).
  Ölçülmedi.
- Önde L7 Google Cloud yük dengeleyici (GCLB) + `ingress-nginx`
  (`use-forwarded-headers` ve `compute-full-forwarded-for` açık): `3` (GCLB
  istemciyi ve yük dengeleyicinin adresini, `ingress-nginx` GCLB vekilini
  yazar). Ölçülmedi.
- GKE'nin kendi Ingress'i (GCE): `2` (GCLB istemciyi ve yük dengeleyicinin
  adresini yazar). Ölçülmedi.

Başlığı yazmayan bir katman (L4 yük dengeleyici, TCP geçişli WAF) sayılmaz; o
zaman throttle o katmanın adresine anahtarlanır ve uyarı düşmez. 0.49'a kadar
burada yazan değerler (Route `1`, WAF'lı Route `2`, GCLB + `ingress-nginx` `2`,
yalnız GCLB `1`) arayüz yolunu hesaba katmıyordu; son ikisi GCLB'nin iki girdi
yazdığını ve `ingress-nginx`'in varsayılan ayarlarla başlığı değiştirdiğini de
saymıyordu.

**Paylaşılan kurumsal NAT/vekil.** `X-Forwarded-For` doğru okunsa bile, aynı
kurumsal NAT ya da ileri vekil arkasındaki kullanıcılar aynı kaynak adresten
görünür; kaynak-başına throttle onları ayırt edemez ve biri çok yanlış denerse
o NAT'ın arkasındaki herkesin o ada girişi birlikte yavaşlar. Bu popülasyonda
hesap tavanı (`UPTIME_LOGIN_ACCOUNT_MAX_FAILURES`) NAT'lı kullanıcı sayısına
göre ayarlanmalıdır (ya da `0` ile kapatılıp yalnız kaynak-başına throttle'a
bırakılmalıdır). Kaynak-IP'ye dayanmayan gerçek bir ayrım (güvenilir-vekil IP
allowlist) kod ve bu ADR'de değişim ister; şimdilik yön olarak backlog'da
izlenir (`docs/backlog.md`, "Giriş throttle için güvenilir-vekil IP allowlist").

Görülen kaynak adresi, ayar doğruyken de, her başarısız girişin
`sign-in refused` log satırındaki `source` alanından doğrulanır (0.50); 0.49'da
doğrulama yalnız yanlış ayarda düşen WARN satırına bağlıydı. `source`
istemcinin adresiyse `UPTIME_TRUST_PROXY_HOPS` doğrudur; bir vekilin adresiyse
değer eksik, arayüz pod'unun adresiyse `0` ya da fazladır. Satır yalnız operatör
logudur: denetim kaydı ve istemci yanıtı kaynak adresini taşımaz, enum
sızdırmamak için.

Yanıt değişmez: kilit 429 + `Retry-After` döner, enum sızdırmaz; başarısız
denemeler ve adı kilitleyen ret yine denetim kaydına ve loga yazılır.

## Sonuçlar

**Kolaylıklar.** Public bir girişte adını bilen biri bir hesabı artık herkes
için kilitleyemez; meşru sahip başka bir kaynaktan girmeye devam eder. Sayaç
bir IP seli altında bellek sınırını (`UPTIME_LOGIN_THROTTLE_MAX_KEYS`) aynı
kurallarla korur (kilitli anahtarlar tutulur, uydurma adlar birbirini iter).

**Maliyetler ve riskler.**

- **Dağıtık tahmine karşı daha zayıf.** Kaynak-başına throttle, çok sayıda
  farklı IP'den gelen bir tahmini tek bir ada karşı yavaşlatmaz; yalnızca
  hesap tavanı (varsayılan 50) sınırlar, ki bu da tüm kaynaklar toplandığında
  tetiklenir. Tavan tetiklenince hesap, meşru sahip dahil, kilit süresince
  kilitlenir — eski sert kilidin artığıdır, ama tek bir IP'den ~15 istekle
  değil, en az `ceil(50/10)=5` ayrı kaynaktan 50 istekle. Tavanı düşürmek bu
  maliyeti düşürür ve meşru sahibin kilitlenme riskini artırır; `0` tavanı
  tümüyle kaldırıp dağıtık tahmini sınırsız bırakır.
- **Vekil güveni.** Doğru değer tam olarak arayüzün önündeki vekillerin
  `X-Forwarded-For`'a yazdığı girdi sayısıdır, hiçbiri yazmıyorsa `1`
  (0.48–0.49'da "önünüzdeki kendi vekillerinizin sayısı"). `0` ayarlanırsa
  (Ingress arkasında) tüm istemciler vekilin tek adresini paylaşıp ada-başına
  tek bir kilide geriler; 0.49 bu durumu, başlık geldiği hâlde
  güvenilmediğinde, çalışma anında WARN'lar (yukarıda). Değer bu sayıdan
  büyükse, 0.47–0.48'de istemci `X-Forwarded-For` uydurarak her istekte taze
  bir kaynak gibi görünüp throttle'ı atlatabiliyor ya da bir kurbanı
  kilitleyebiliyordu; 0.49'da zincir hop'tan kısa gelince
  görülen kaynak istemci-kontrollü en soldaki girdiye değil eşe (peer) düşer ve
  WARN verilir, bu yüzden uydurma başlık ne throttle'ı atlatır ne de kurbanı
  kilitler (yalnızca o NAT/eş paylaşılan bir kilide geriler). Yanlış ayarın
  maliyeti böylece "bypass" değil "tek paylaşılan kilit"tir.
- **Cluster içinden uydurma başlık.** Değer `0`'dan büyükken backend
  `X-Forwarded-For`'a bağlanan eşe bakmadan güvenir: cluster içindeki bir pod
  backend'e doğrudan ya da 0.50'den beri arayüz Service'i üzerinden (arayüz
  başlığı aynen taşır) kendi yazdığı bir başlıkla bağlanıp kaynağını seçebilir;
  arayüz `NodePort` ise (ör. GCE Ingress için) node portuna yük dengeleyiciyi
  atlayıp node üzerinden gelen bir istemci de. Backend'e doğrudan geleni
  `networkPolicy.enabled=true` kapatır; arayüze doğrudan gelen bugün açıktır.
  İkisi S79'da ([#119](https://github.com/yusufceng/Kubeat/issues/119)) ele
  alınır.
- **Tek replica.** Sayaç bellektedir; backend yeniden başlayınca ya da başka
  replicada sıfırlanır. Çok replicalı dağıtık throttle yol haritasında bilinen
  sınırdır.
- **Geri alma.** Yalnız davranış/ayar değişikliğidir, şema değişmez (26);
  `helm rollback` ile 0.47.0'a (kullanıcı-adı bazlı kilit) dönülebilir. Yeni
  ayarlar kaldırılınca varsayılanlar devreye girer.
