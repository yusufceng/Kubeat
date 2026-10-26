# Ertelenen fikirler

Konuşulup bilerek sonraya bırakılan işler. Buradaki bir madde, plana alınana
kadar yapılacak iş değildir.

## Alertmanager'a doğrudan kanal

Kubeat'in olayları Alertmanager API'sine (`/api/v2/alerts`) kendisinin
göndermesi. 2026-10-12'de bunun yerine chart'a PrometheusRule eklendi:
cluster'ın Prometheus'u `/metrics`'ten `kubeat_monitor_incident_open`
serisini okuyup alarmı kendisi üretir; token, CA ve `resolve_timeout` için
tekrar gönderme döngüsü gerekmez. Doğrudan kanal yalnızca user workload
monitoring kapalı cluster'larda anlam kazanır; böyle bir ortam görülürse
Kubeat'in kanal türü olarak eklenir (OpenShift'te `monitoring-alertmanager-
edit` rolü ve kube-rbac-proxy üzerinden 9095 portu).

## Pod bazında sağlık

Service üzerinden tek kontrol, arkadaki pod'lardan birkaçı bozukken çoğunlukla
başarılı döner. İki olası çözüm konuşuldu (2026-10-12):

Güncelleme (2026-10-12): monitor sayfasına "Pod'lar" paneli eklendi. Panel
hazır pod sayısını, pod durumlarını ve restart'ları cluster API'sinden canlı
okur, CPU/belleği Prometheus'tan alır. Aşağıdaki iki madde hâlâ geçerli:
panel yalnızca gösterir, saklamaz ve alarm üretmez.

Güncelleme (2026-10-12, sonra): 1. madde `workload` monitor tipiyle
karşılandı: hazır pod sayısı istenenin altına düşünce olay açılır ve bildirim
gider. Restart sayıları için alarm yok; panelde görünür. 2. madde hâlâ açık.

1. **Hazır/istenen replika oranı ve restart sayıları.** Bu veri OCP'nin kendi
   Prometheus'unda (kube-state-metrics) zaten var ve hazır alarm kuralları ile
   geliyor (`KubeDeploymentReplicasMismatch`, `KubePodCrashLooping`). Kendimiz
   toplamak tekerleği yeniden icat etmek olur. Gerekirse plandaki Thanos
   entegrasyonu sırasında yalnızca okunup gösterilir.
2. **Pod'ları tek tek yoklamak.** EndpointSlice'tan pod IP'leri alınıp her
   birine gerçek iş yolundan istek atılır. "Ready görünüyor ama cevap vermiyor"
   durumunu yalnızca bu yakalar ve hazır araçlarda karşılığı yoktur.

   Güncelleme (2026-10-12, en son): `each_pod` seçeneğiyle yapıldı; agent ve
   monitor başına birden çok örnek gerekmedi, çünkü backend cluster içinde
   çalışır ve pod'lara doğrudan erişir; sonuç tek kayıttır, düşen pod'lar
   hata metninde adlarıyla yer alır. Pod başına ayrı geçmiş tutulmaz.

## Sohbet kanallarında kalan biçim kaçışlaması

2026-10-16'da Slack, Mattermost ve Google Chat'te `&`, `<`, `>` kaçışlandı,
Discord mesajı kimseyi etiketlemez oldu: push raporunun mesajı ya da bir
hedefin hata metni `<!channel>` ile herkesi çağıramıyor, etiketi adresini
gizleyen bağlantı kuramıyor. Kalanlar: Mattermost düz `@channel`, `@all`,
`@here` ve markdown `[etiket](adres)` yazımını da tanır; Teams'in TextBlock'u
ve Gotify'ın mesajı markdown işler. Konuşulan çözüm, dışarıdan gelen
değerleri satır içi kod olarak yazmak ya da `@` işaretini bozmaktır;
metinlerin okunurluğunu değiştirdiği için ayrıca karar verilmeli.

Güncelleme (2026-10-16, sonra): Mattermost için yapıldı. Ona giden metinde
her `@` ve `]` işaretinden sonra görünmez bir boşluk (U+200B) var: hiçbir
bahsetme kimseyi çağırmıyor, `[etiket](adres)` bağlantı ya da resim
olmuyor ve metin aynı okunuyor. Teams ve Gotify hâlâ açık.

## Başarısız kontrollerin ayrıntılarında kalanlar

2026-10-16'da başarısız kontroller nedenlerini saklamaya başladı (README,
"Başarısız kontrolün ayrıntıları"). Bilerek sonraya bırakılanlar:

- Webhook gövdesinde açıklamanın kodu: genel webhook kanalı şimdilik
  yalnızca hata metnini (`incident.cause`) alır; ayrıntı alanı olan
  kanallarda (PagerDuty dahil) "Neden" alanı açıklamayı taşır.
- TCP, gRPC ve veritabanı kontrollerinde zamanlama, adres ve sertifika
  ayrıntıları; bu tiplerde yalnızca açıklama saklanır.
- Olayın kendisinde açıklama: bugün yalnızca kesinti bildirimi taşır; olay
  sayfası gelince açılıştaki başarısızlığın açıklaması olaya yazılabilir.
- Monitor listesindeki kısa geçmişte (`history`) ve ızgaranın en sık
  hatalarında açıklama kodu.

## UptimeRobot karşılaştırmasından ertelenenler

2026-10-20'de UptimeRobot (canlı hesap ve dokümanlar) Kubeat ile
karşılaştırıldı. Konum: UptimeRobot dışarıdan bakan bir SaaS, Kubeat
cluster'ın içinden bakar; rakipten çok tamamlayıcıdır. Plana alınanlar
(durum sayfası bölümleri, kurallarla üyelik, adımlı duyurular, erişim
ayarları, servis detay sayfası, NOC modu; kanal başına gecikme ve tekrar,
monitor'dan test bildirimi, yavaş yanıt eşiği) 0.40 turunda yapılıyor.
Bilerek sonraya bırakılanlar:

- **Durum sayfasına e-postayla abonelik.** SMTP zaten var; abone listesi,
  doğrulama ve abonelikten çıkma akışı gerekir. Duyurular oturunca
  değerlendirilir.
- **Üçüncü taraf servislerin durumu** (AWS, GitHub, quay.io, registry.redhat.io
  gibi tedarikçilerin durum sayfaları; çoğu Atlassian Statuspage JSON'u).
  Monitor tipi olarak değil, olay sayfasında "aynı anda tedarikçi X de
  bozukluk bildiriyordu" bağlamı olarak düşünülecek.
- **SLO hedefi ve hata bütçesi.** Hedefin nerede tutulacağı (annotation mı,
  monitor alanı mı) ve raporlardaki yeri ayrı bir karar.
- **Durum sayfası markalaması** (logo, yazı tipi, renkler) yapılmayacaklar
  listesinde kalıyor; yalnızca sıkışık yerleşim ve bölümler yapılıyor.

## TypeScript 7 ve ESLint 10

Dependabot'un iki major güncellemesi 2026-10-26'da denendi ve bekletildi.
TypeScript 7.0.2 ile `next build` tip kontrolü geçiyor, ama
typescript-eslint 8.72 TypeScript 7'yi desteklemediği için `npm run lint`
açılışta çöküyor. ESLint 10.13 ile lint geçiyor, ama eslint-config-next
16.4.3'ün içindeki `eslint-plugin-import`, `eslint-plugin-jsx-a11y` ve
`eslint-plugin-react` eklentileri peer olarak `eslint ^9` istiyor. İkisi de
bu eklentiler destek verdiğinde yeniden denenir. O zamana kadar
Dependabot'un PR'ları (#12, #13) kapatılabilir.
