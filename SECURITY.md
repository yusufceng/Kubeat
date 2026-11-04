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

Düzeltme yayımlanınca açık, depoda bir GitHub güvenlik duyurusuyla
(Security Advisory) duyurulur.

## Desteklenen sürümler

Güvenlik düzeltmesini yalnızca son minor sürüm alır: en son yayımlanan
`0.Y` hattının son yaması. Düzeltme o hatta yeni bir yama sürümü olarak
çıkar ve eski minor'lara geri taşınmaz. Daha eski bir minor'daysanız
düzeltmeyi almak için son minor'a yükseltin
([docs/yukseltme.md](docs/yukseltme.md)'deki yükseltme notları).

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
