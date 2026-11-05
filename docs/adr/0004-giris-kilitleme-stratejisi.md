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

Kaynak adresi `UPTIME_TRUST_PROXY_HOPS` ile belirlenir: önünüzdeki kendi
vekillerinizin sayısı. `0` (varsayılan) hiçbir `X-Forwarded-For` başlığına
güvenmez ve bağlanan eşi kullanır; `N` ise istemciyi `X-Forwarded-For`'un
sağdan N girdisini (vekillerinizin eklediği) güvenerek, solundaki (istemcinin
uydurabileceği) girdileri yok sayarak okur. Bir Ingress/yük dengeleyici
arkasında eş hep vekilin adresidir; o yüzden kaynakları ayırt etmek için bu
değer ayarlanmalıdır, yoksa throttle tek bir paylaşılan, ad-başına kilide
geriler (0.47.0 davranışı). Bu, mevcut vekil/`X-Forwarded-Proto` güveni
(frontend `cookieSecure`) kararıyla aynı mantıktadır: başlığa yalnızca
operatör açıkça güvenince güvenilir.

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
- **Vekil güveni.** `UPTIME_TRUST_PROXY_HOPS` gerçek vekil sayısından büyük
  ayarlanırsa istemci `X-Forwarded-For` uydurarak her istekte taze bir kaynak
  gibi görünüp throttle'ı atlatabilir; küçük ya da `0` ayarlanırsa (Ingress
  arkasında) tüm istemciler vekilin tek adresini paylaşıp ada-başına tek bir
  kilide geriler. Doğru değer tam olarak önünüzdeki kendi vekillerinizin
  sayısıdır.
- **Tek replica.** Sayaç bellektedir; backend yeniden başlayınca ya da başka
  replicada sıfırlanır. Çok replicalı dağıtık throttle yol haritasında bilinen
  sınırdır.
- **Geri alma.** Yalnız davranış/ayar değişikliğidir, şema değişmez (26);
  `helm rollback` ile 0.47.0'a (kullanıcı-adı bazlı kilit) dönülebilir. Yeni
  ayarlar kaldırılınca varsayılanlar devreye girer.
