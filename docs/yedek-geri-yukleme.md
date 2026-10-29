# Yedekten geri yükleme: hedefler ve prova kaydı

Adımlar README'nin "Yedekleme ve geri yükleme" bölümündedir. Bu belge ne
kadar veri kaybedilebileceğini, geri dönüşün ne kadar sürdüğünü ve bunun
nasıl denendiğini kaydeder.

## Hedefler

| | Değer | Neye bağlı |
|---|---|---|
| **RPO** (kaybedilebilecek en uzun süre) | `UPTIME_BACKUP_INTERVAL`, varsayılan 24 saat | Kopya bu aralıkla alınır; son kopyadan sonraki monitor, kanal ve ayar değişiklikleri ile kontrol sonuçları kaybolur. Daha kısa RPO için aralığı düşürün (kopya çalışırken alınır, kontroller durmaz). |
| **RTO** (geri dönüş süresi) | Aynı kurulumda birkaç dakika; yeni kurulumda kurulum süresi + birkaç dakika | Provada yeni bir namespace'e kurulum dahil 76 saniye sürdü (imajlar önbellekteydi). Büyük veritabanında kopyanın taşınma süresi eklenir. |
| **Cluster kaybı** | Kopyalar ve anahtar dışarıda saklanmışsa geri dönülebilir | Yedek PVC'si aynı cluster'dadır. Kopyaları ve `kubeat-secret-key` içeriğini dışarıya taşımak kurulumu yapanın işidir. |

Geri yüklemenin geri getirmedikleri:

- Kopyadan sonraki kontrol sonuçları ve olaylar. Bu sürede çıkan kesintiler
  geçmişte görünmez.
- Gönderilmiş bildirimler. Kopyadan sonra açılıp kapanmış bir olay yeniden
  bildirilmez; hâlâ süren bir kesinti kontroller başlayınca yeniden bulunur.
- Anahtarsız bir kopya. Gizli değerler (token'lar, parolalar, webhook
  adresleri) anahtar olmadan okunamaz. Yanlış anahtarla backend açılışta
  durur ve nedenini yazar.

## Prova: 2026-10-26, minikube, 0.41.0

Senaryo: namespace'in tamamı kaybolur. Elde yalnızca dışarı alınmış son
kopya ve anahtar vardır. Geri yükleme sıfırdan yeni bir kuruluma yapılır.

1. `kubeat-rh-a` namespace'ine `backup.enabled=true` ve
   `backup.interval=2m` ile kuruldu. API üzerinden 3 HTTP monitor ve
   adresi şifreli saklanan bir webhook kanalı eklendi.
2. Son kopya `kubectl exec ... cat` ile, anahtar Secret'tan bilgisayara
   alındı. Kopya `PRAGMA integrity_check` = ok, şema 26, 3 monitor ve 9
   kontrol sonucu içeriyordu.
3. Release kaldırıldı ve namespace silindi. Bununla veri ve yedek PVC'leri
   de gitti.
4. `kubeat-rh-b` namespace'ine saklanan anahtarla
   (`secretKey.existingSecret`) yeni kurulum yapıldı. Backend sıfıra
   ölçeklendi, yardımcı pod ile kopya `kubeat-data`'ya yazıldı, backend
   yeniden başlatıldı.
5. Sonuç: 3 monitor ve webhook kanalı geri geldi. Kanalın şifreli adresi
   anahtarla çözüldü. Dosya izni açılışta `0600`'a çekildi.

Toplam süre, namespace oluşturmadan backend'in hazır olmasına kadar
**76 saniye**.

Provada görülenler, README'ye işlendi:

- Backend imajında `tar` yok, bu yüzden `kubectl cp` çalışmaz. Dosyalar
  `kubectl exec ... cat` ile taşınır.
- Backend düzgün kapandığında `-wal` ve `-shm` dosyası kalmaz. Bunları
  silmek yalnızca backend çökmüşse gerekir.
- Yardımcı pod backend imajıyla çalışabilir, ek bir imaj gerekmez. OpenShift
  `restricted-v2` SCC altında kullanıcıyı namespace aralığından alır ve aynı
  namespace'teki backend ile aynı UID'yi kullanır.

## Prova: 2026-10-29, minikube, 0.44.0 (kazayla kaldırma)

Senaryo: yanlış release'i kaldırmak ya da takılı bir release'i
"kaldır ve yeniden kur" ile düzeltmek. Kurulum yayımlanan 0.44.0 chart'ı ve
imajlarıyla yapıldı, `backup.enabled=true`, varsayılan
`keepOnUninstall=true`.

1. Kurulum, iki kanal ve üç monitor. Kurulum Secret'ındaki yönetici
   parolasıyla giriş: 200.
2. `helm uninstall`: Helm "These resources were kept due to the resource
   policy" altında `kubeat-data`, `kubeat-backup` ve `kubeat-secret-key`'i
   saydı; üçü de kaldı.
3. Aynı ad ve namespace'le `helm install`: hatasız bitti, iki PVC aynı
   volume'larla yeni release'e bağlandı. Kanallar ve monitor'lar geri geldi.
   Bir döngü daha aynı sonucu verdi.
4. Giriş: **eski** yönetici parolası geçti (200). Yeniden üretilen
   `kubeat-admin` Secret'ındaki, yani kurulum notlarının gösterdiği parola
   geçmedi (401). API ve metrik anahtarları da yeniden üretildi; backend
   bunları ortamdan okuduğu için yenileri geçerli.
5. Notlardaki `kubectl -n <namespace> delete pvc kubeat-data kubeat-backup`
   yalnızca bu iki PVC'yi sildi. Release kaldırıldıktan sonra notlar
   `helm get notes` ile okunamıyor; komut README'de de yazılı.

Görülenler: veri ve yedek kaldırmadan sağ çıkıyor (RPO etkilenmiyor),
yeniden kurulum bir dakikadan kısa sürüyor. Açık kalan: yeniden kurulumda
kurulum notları geçmeyen bir parola gösteriyor (backlog, "Aynı adla yeniden
kurulumda yeniden üretilen Secret'lar"; tetikleyicisi bu provaydı).

## Prova: 2026-10-29, minikube CSI (fsGroupPolicy: File), 0.45.0

Senaryo: fsGroup'u gerçekten uygulayan bir depolama sürücüsünde (minikube
`csi-hostpath-driver`, `hostpath.csi.k8s.io`, `fsGroupPolicy: File`) README
"Cluster'a kurulum" komutuyla kurulum ve README'deki yardımcı pod'la geri
yükleme. Varsayılan minikube hostpath sağlayıcısı fsGroup uygulamadığı için
önceki provalar bunu sınamıyordu. Yayımlanan 0.45.0 chart'ı ve imajları,
`backup.interval=1m`.

1. Kurulum `--set podSecurityContext.fsGroup=1001` ile: kurulum notlarında
   fsGroup uyarısı yok. Backend UID 1001, ek grup 1001; `/data` ve `/backup`
   `root:1001` sahipliğinde ve setgid'li geldi, veritabanı yazıldı.
2. Bir monitor eklendi, dakikalık yedek alındı; yedekten sonra bir monitor
   daha eklendi.
3. Negatif: pod düzeyinde `securityContext` olmayan yardımcı pod
   kopyalayamadı (`cp: cannot create regular file '/data/uptime.db.restore':
   Permission denied`).
4. README'deki yardımcı pod (`securityContext: { fsGroup: 1001 }`) ile
   adımlar: backend sıfıra, kopya `/data/uptime.db`'ye, `-wal`/`-shm`
   silindi, backend bire. Geri yüklemeden sonra yalnızca yedekteki monitor
   vardı; yedekten sonra eklenen gitti.
5. Negatif: aynı sürücüde fsGroup'suz kurulum. Kurulum notları fsGroup
   uyarısını gösterdi; backend uyarının söylediği `open database failed`
   (`unable to open database file (14)`) ile CrashLoopBackOff'a düştü.

Görülenler: README'deki fsGroup satırı hem kurulum hem geri yükleme için
gerekli ve yeterli; kurulum notlarının uyarısı doğru durumda çıkıyor.

## Yeniden prova

Sürüm şemayı değiştirdiğinde ya da yedekleme kodu değiştiğinde prova
tekrarlanır. Bu belgeye tarih, sürüm ve süre eklenir.
