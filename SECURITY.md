# Güvenlik politikası

## Açık bildirmek

Kubeat'te bir güvenlik açığı bulduysanız herkese açık bir issue açmayın.
GitHub'ın özel bildirim yolunu kullanın: deponun **Security** sekmesinde
**Report a vulnerability**
([doğrudan bağlantı](https://github.com/yusufceng/Kubeat/security/advisories/new)).
Bildirimi yalnızca bakımcılar görür. Türkçe ya da İngilizce yazabilirsiniz.

Bildirimde şunlar işe yarar:

- Etkilenen sürüm ve bileşen: backend imajı, frontend imajı ya da Helm
  chart.
- Açığı yeniden üretme adımları.
- Etkisi: ne okunabiliyor, ne değiştirilebiliyor, kimin yetkisiyle.

Düzeltme yayımlanınca dışarıdan bildirilen açık, depoda bir GitHub güvenlik
duyurusuyla (Security Advisory; CVE kimliği GitHub'dan istenir) duyurulur;
duyuru etkilenen ve düzelten sürümleri yazar. Kendi incelememizde ya da
taramamızda bulduğumuz bir açığın düzeltmesi duyuru açılmadan çıkar, 1.0'dan
sonra da: [docs/yukseltme.md](docs/yukseltme.md)'deki yükseltme notu sürümü
"güvenlik sürümü" diye adlandırır, hangi kurulumların yükseltmesi gerektiğini
yazar ve açığın ayrıntısını yazmaz; sürüm aşağıdaki "Güvenlik sürümleri"
tablosuna etkilenen sürüm aralığıyla girer. Bir bağımlılığın kendi duyurusu
olan açığında (Go, npm paketi, taban imaj) duyuru o kimliktir; tablo onu
yazar.

## Desteklenen sürümler

Güvenlik düzeltmesini yalnızca son minor sürüm alır. Düzeltme en son
yayımlanan `0.Y` hattında bir yama sürümüyle ya da bir sonraki minor sürümle
çıkar; eski minor'lara geri taşınmaz. Daha eski bir minor'daysanız
düzeltmeyi almak için son minor'a yükseltin
([docs/yukseltme.md](docs/yukseltme.md)'deki yükseltme notları). Bu kural
1.0'dan sonra da geçerlidir (karar 2026-11-11): bir önceki minor'a yama veren
bir destek penceresi yoktur; daha uzun bir pencere ancak bir sözleşmeyle ve o
sözleşmenin kapsamında verilir.

## Güvenlik sürümleri

Yükseltme notunda "güvenlik sürümü" diye adlandırılan sürümler ve bir
bağımlılığın açığı için çıkan yamalar; "etkilenen sürümler" yükseltmesi gereken
sürümlerdir. Duyurusu olmayan satırlar GHSA ve CVE'de görünmez: tarayıcı ve
envanter araçları için kaynak bu tablodur.

| Sürüm | Tarih | Etkilenen sürümler | Ne | Kimlik | Duyuru |
| --- | --- | --- | --- | --- | --- |
| 0.53.1 | 2026-11-11 | 0.53.0 ve öncesi (hepsi go1.26.10 ya da daha eski Go ile derlendi) | Go `net/http`: `MaxBytesReader` sınırı aşılırken başlık yarışı, HTTP/1 sunucu çökmesi; Kubeat kodu değişmedi, Go 1.26.11 ile yeniden derlendi | [GO-2026-6859](https://pkg.go.dev/vuln/GO-2026-6859), CVE-2026-97054 | Go'nun duyurusu; yükseltme notu "0.53.1'e yükseltme" |
| 0.51.0 | 2026-11-07 | 0.47.0–0.50.0 (namespace'le sınırlı kullanıcısı olan kurulumlar) | Kapsamı sınırlı kullanıcının monitor'ünde kapsam yalnız hedef belirlenirken denetleniyordu; 0.51'den beri kontrolün açtığı her bağlantıda | — (kendi bulgumuz) | Yükseltme notu "0.51'e yükseltme" |
| 0.50.0 | 2026-11-07 | 0.48.0–0.49.0 (Route ya da Ingress arkasındaki kurulumlar) | Kaynak-başına giriş throttle'ı (CWE-307 düzeltmesi) arayüz yolunda etkisizdi: arayüz istemci adresini backend'e taşımıyordu | — (kendi bulgumuz) | Yükseltme notu "0.50'ye yükseltme" |

Tablo 0.50.0'dan beri tutulur. 0.47.0–0.49.0'daki güvenlik iyileştirmeleri (iç
adreslerin hata metninden maskelenmesi, kullanıcı başına hedef kapsamı, hesap
kilidinin kaynak adrese göre throttle'a dönmesi) yükseltme notlarında
anlatılır; o sürümler "güvenlik sürümü" diye adlandırılmadı.

## Hedef süre

Kubeat'i etkileyen kritik ya da yüksek önemdeki bir açık için hedef, düzeltilmiş
sürümü **7 gün** içinde yayımlamaktır. Süre, açık bize ulaştığında (özel
bildirim, haftalık tarama ya da bağımlılığın kendi duyurusu) ve Kubeat'i
etkilediği doğrulandığında başlar. Önem derecesi açığın kendi duyurusundaki
derecedir. Orta ve düşük önemdeki açıklar olağan sürümlerle düzeltilir;
onlar için süre hedefi yoktur. Bu bir hedeftir, sözleşmeye bağlı bir hizmet
düzeyi değildir.

Bağımlılıklardaki bilinen açıklar her hafta taranır: backend için
`govulncheck`, frontend'in çalışma zamanı bağımlılıkları için `npm audit`
([security.yml](.github/workflows/security.yml)). Taban imajların işletim
sistemi paketleri (UBI) ve frontend imajındaki Node.js çalışma zamanı bu
taramaya girmez; onların açıkları Red Hat'in ve Node.js'in duyurularından ve
Dependabot'un haftalık taban imaj PR'larından izlenir. Yayımlanan imajları
bir imaj tarayıcısıyla taramak backlog'dadır.

## Taban imajlar

İmajlar yalnızca sürüm etiketinde derlenir ve yayımlanmış bir sürümün imajı
yeniden itilmez: digest'i ve imzası değişmez. Taban imajda (UBI) Kubeat
imajlarını etkileyen kritik ya da yüksek önemdeki bir açık düzeltilince imajlar
yeniden derlenir ve yeni bir yama sürümü olarak yayımlanır. Bu durumda da
hedef süre 7 gündür.

## Yayımı doğrulamak

0.45.0'dan beri imajlar ve chart imzalıdır; imajlar SBOM ve provenance
taşır. 0.44.0 ve öncesi imzasızdır. İmzanın nasıl
doğrulanacağı, imzalarla aynalama ve hava boşluklu ortamda doğrulama:
[README, "Tedarik zinciri"](README.md#tedarik-zinciri).
