# Logo

Kubeat'in logosu kelimenin kendisidir: Mona Sans'la (genişlik 118, ağırlık
680) yazılmış, harf aralıkları ölçülerek ayarlanmış ve çizgiye çevrilmiş
`kubeat`. İşaret ayrı bir sembol değil, kelimenin k'sidir.

k'nin gövdesi kendi kolunun çizgisinden kesilir: yükselen kenar. Üst kenarı
soldan sağa okunduğunda bir sinyal izidir (yüksel, düş, yüksel, bekle); her
kontrolün attığı tik. k vurgu mavisiyle ve 720 ağırlıktan çizilir, çünkü
mavi mürekkepten daha açık okunur; kelimenin geri kalanı mürekkep rengidir.

| Dosya | Nerede |
| --- | --- |
| `kubeat-lockup.svg` | Açık zeminde logo (README, belgeler) |
| `kubeat-lockup-dark.svg` | Koyu zeminde logo |
| `kubeat-mark.svg` | Yalnız k: chart ikonu ve sekme; sistem koyu temadaysa koyu kesime geçer |

Uygulamadaki kopyalar: `frontend/app/icon.svg` (sekme),
`frontend/app/favicon.ico` (16 piksel elle ayarlanmış katman, 32 ve 48),
`frontend/app/apple-icon.png` (ana ekran, 180) ve `frontend/app/ui/logo.tsx`
(menüdeki karo, giriş ekranı, telefon başlığı, durum sayfasının alt satırı).

## Çizim

- k 32 birimlik ızgarada çizildi: taban çizgisi 28, x yüksekliği 10, üst
  uzantı 2, gövde 6 birim. Gövdenin üstündeki kesim kola paraleldir, ikisi
  de 45°'dir; bacağın iki kenarı 3:4 eğimdedir. Bütün yatay ve dikey
  kenarlar çift birimdedir: 16 ve 32 pikselde tam piksele oturur.
- İşaretin gövdesi kelimedeki mavi k'nin gövdesiyle aynı orandadır (6/26 =
  168/729); küçük boyutlar için kol 45°'ye alınmıştır. Aynı k'nin iki optik
  boyutudur.
- Koyu kesim: açık renkli k koyu zeminde yayılır. Bu yüzden k'nin açık
  renkte durduğu yerlerde (koyu temada, mavi karonun üstündeki beyaz k'de)
  kol ve bacak çeyrek birim incedir; dosyalar ve bileşen bunu kendisi seçer.

## Kurallar

- **Renk**: yalnız vurgu mavisi (açıkta #2F65DB, koyuda #6294FA) ve mürekkep
  (#151B23, koyuda #EAEDF1). Yeşil, kırmızı, sarı ya da bakım grisi
  kullanılmaz: logo hiçbir zaman bir durum gibi okunmamalı.
- **Boyut**: k işareti 16 pikselden küçük kullanılmaz; en keskin hâli 16,
  32, 48 ve 64 pikseldir. Menüdeki karo 32 pikseldir. Logo en az 12 piksel
  yüksekliğindedir; metin içinde metnin kendi boyutunda (0,75 em) durur.
- **Boşluk**: logonun çevresinde kelimenin x yüksekliği kadar boşluk
  bırakılır. Logonun yanına ayrıca k işareti konmaz.
- **Yapılmayacaklar**: kelimeyi yazı tipiyle yeniden yazmak, k'nin kesimini
  kaldırmak ya da yumuşatmak, döndürmek, sündürmek, gölge ya da gradyan
  eklemek.
