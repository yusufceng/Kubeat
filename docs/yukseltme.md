# Yükseltme notları

Kubeat'i bir sürümden ötekine yükseltirken yapılacaklar; en yeni sürüm
üstte. Yeni kurulum ve `helm upgrade`'in ayarlarla nasıl çalıştırılacağı
(`-f values.yaml` ya da `--reset-then-reuse-values`)
[README](../README.md)'de, "Hızlı başlangıç" ve "Cluster'a kurulum"
bölümlerindedir.

Her sürümün bölümü dört alt başlık taşır:

- **Yapılacak**: yükseltmeden önce ya da yükseltirken sizin
  yapacaklarınız. Denetimler betiğe konabilir (aşağıda "Ön kontrolleri
  betiklemek").
- **Davranış değişikliği**: hiçbir şey yapmasanız da değişenler.
- **Geri dönüş**: önceki sürüme nasıl dönülür, dönüşte ne olur.
- **Alarm kuralları: değişti** ya da **değişmedi**: chart'ın
  PrometheusRule'u (`metrics.prometheusRule.enabled`). Kuralları chart'tan
  render edip başka bir depoya (Grafana Cloud, Mimir, global Thanos Ruler)
  kopyalıyorsanız "değişti" yazan her sürümde yeniden kopyalayın: kopya
  chart'la yükselmez.

## Yükseltmeden önce yedek

Her yükseltmeden önce en yeni yedek kopyasını ve şifreleme anahtarını
cluster dışına alın ve kopyanın sağlama toplamını pod'daki dosyanınkiyle
karşılaştırın: [pilot.md "Yükseltmeden önce yedek"](pilot.md#yükseltmeden-önce-yedek).
Blok `exec … cat` ve sağlama toplamıyla çalışır, backend imajında `tar`
gerekmez; "yedek doğrulandı" demiyorsa yükseltmeyin.

- Veritabanı şemasını yükselten bir sürümden (0.38, 0.39, 0.40) dönüşün tek
  yolu bu kopyadır; 0.40'tan beri şema 26'dır ve sonraki sürümler
  `helm rollback` ile geri alınır.
- Yedekleme kapalıysa (`backup.enabled=false`, varsayılan) volume'u kendi
  aracınızla (Velero, OADP) yedekleyin ya da önce yedeklemeyi açın: ilk
  kopya açılışta alınır. Geri yükleme README "Yedekleme ve geri
  yükleme"dedir.

## Atlamalı yol

Ara sürümlere tek tek yükseltmeniz gerekmez: tek bir `helm upgrade` hedef
sürüme geçer. Aradaki her sürümün "Yapılacak" adımları yine yapılır ve
geri dönüş başladığınız sürüme olur.

### 0.42'den 0.45'e

1. [Yükseltmeden önce yedek](#yükseltmeden-önce-yedek).
2. 0.43'ün ön kontrolleri ("0.43'e yükseltme", Yapılacak): son gönderimi
   başarısız kanalları düzeltin ya da kapatın; özel bildirim ayarlarınız
   varsa `UPTIME_NOTIFY_RETRY_MAX`'ın açılış doğrulamasından geçtiğini
   denetleyin, geçmeyen değerle 0.43–0.45 backend'i açmaz; aynı
   PagerDuty servisine bağlı birden çok kurulumu ayırın.
3. 0.44'ün adımları ("0.44'e yükseltme", Yapılacak): yedeği kapatmayı ya
   da `existingClaim`'e geçişi bu yükseltmeyle aynı adımda yapmayın; açık
   kesintilere bakın; `KubeatDown` susturma kuralını ekleyin;
   `backend.terminationGracePeriodSeconds`'ı `UPTIME_SHUTDOWN_TIMEOUT`'tan
   en az 5 sn uzun tutun.
4. 0.45'in adımları ("0.45'e yükseltme", Yapılacak): `latest` etiketinden
   bir sürüm etiketine geçin; `KubeatNotificationsFailing`'in
   yönlendirmesini kanal etiketlerine göre ve `KubeatDown` susturma
   listesini güncelleyin.
5. Alarm kuralları 0.43, 0.44 ve 0.45'te değişti: yeni `kubeat.self`
   alarmlarını ve `KubeatUnhealthy`'yi Alertmanager yönlendirmesine
   ekleyin; kuralları kopyalıyorsanız 0.45'inkileri kopyalayın.
6. Yükseltme (ayarlar bir values dosyasında değilse `-f values.yaml`
   yerine `--reset-then-reuse-values`, Helm 3.14+):

   ```sh
   helm -n kubeat upgrade kubeat oci://ghcr.io/yusufceng/charts/kubeat --version 0.45.0 -f values.yaml
   ```

7. Geri dönüş: şema aynı (26), `helm rollback` ile 0.42.2'ye. Dönüşte
   0.43–0.45'in yeniden denemesi, alarmları ve metrikleri kalkar; 0.42'nin
   chart'ı PVC'lere kaldırma korumasını basmaz, `helm uninstall` onları
   yine siler.

### 0.41'den 0.45'e

1. 0.42'nin adımı ("0.42'ye yükseltme", Yapılacak): LDAP ya da OIDC ile
   giriş, namespace'le sınırlı kullanıcılar ya da dizin grupları, denetim
   kaydı ya da birden çok durum sayfası kullanıyorsanız lisansı
   yükseltmeden önce verin.
2. Sonra "0.42'den 0.45'e"deki adımlar.
3. Geri dönüş: şema aynı (26), `helm rollback` ile 0.41.1'e.

0.43 ya da 0.44'ten gelirken başladığınız sürümden sonraki bölümleri
eskiden yeniye okuyun. 0.40 ya da öncesinden gelirken de öyle; şemayı
yükselten 0.38, 0.39 ve 0.40'tan dönüş yalnızca yedekle olur.

## Ön kontrolleri betiklemek

"Yapılacak"taki denetimler cluster'a erişebilen bir makineden ya da
CI'dan API'yle yapılır. API Route'tan ya da Ingress'ten geçmez: backend
Service'ine port-forward ve API anahtarıyla erişilir
([README "API"](../README.md#api)). Komutlar `kubectl`, `helm`, `curl` ve
`jq` ister:

```sh
NS=kubeat REL=kubeat        # namespace ve release adı
kubectl -n "$NS" port-forward "svc/$REL-backend" 8090:8080 >/dev/null &
PF=$!; sleep 3
# apiToken.existingSecret verdiyseniz Secret'ın adı odur.
TOKEN=$(kubectl -n "$NS" get secret "$REL-api-token" -o jsonpath='{.data.token}' | base64 -d)
api() { p=$1; shift; curl -fsS -H "Authorization: Bearer $TOKEN" "$@" "http://localhost:8090/api/v1/$p"; }
```

- **Son gönderimi başarısız kanallar** (0.43, 0.45). Çıktı boşsa her
  etkin kanalın son denemesi başarılıdır ya da kanal hiç denenmemiştir:

  ```sh
  api channels | jq -r '.[] | select(.enabled and .last_delivery.delivered == false)
    | "\(.id)\t\(.type)\t\(.name)\t\(.last_delivery.error)"'
  ```

  Düzelttiğiniz kanalı test edin (`{"delivered": true}` dönmeli):
  `api channels/<id>/test -X POST`.
- **Açık kesintiler** (0.44):

  ```sh
  api 'incidents?state=open&kind=down' | jq -r '.[] | "\(.id)\t\(.monitor_namespace)/\(.monitor_name)\t\(.started_at)"'
  ```

- **Bildirim ayarları** (0.43). Çıktı boşsa varsayılanlar geçerlidir ve
  açılış doğrulamasından geçer. Ayarlar backend Deployment'ından okunur,
  böylece kurulumu Helm de yapmış olsa Argo CD ya da OpenShift GitOps da
  (onlarda Helm release'i yoktur) aynı komut çalışır:

  ```sh
  kubectl -n "$NS" get deploy "$REL-backend" -o jsonpath='{range .spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}' \
    | grep -E '^UPTIME_NOTIFY_(RETRY_MAX|ATTEMPTS|TIMEOUT|RETRY_DELAY)='
  ```

- **Lisans isteyen özellikler** (0.42). LDAP ya da OIDC adresi, namespace'le
  sınırlı kullanıcılar ve dizin grupları, durum sayfası sayısı:

  ```sh
  kubectl -n "$NS" get deploy "$REL-backend" -o jsonpath='{range .spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}' \
    | grep -E '^UPTIME_(LDAP_URL|OIDC_ISSUER)='
  api users | jq -r '.[] | select(.role != "admin" and ((.namespaces // []) | index("*") | not)) | .username'
  api ldap/groups | jq -r '.[] | select(.role != "admin" and ((.namespaces // []) | index("*") | not)) | .group_dn'
  api status-pages | jq length
  ```

- **30 saniyeden sık monitor'lar ve kotalar** (0.41). Hedefi cluster
  dışında olanlar 30 saniye kuralına girer:

  ```sh
  api monitors | jq -r '.[] | select(.interval_seconds < 30) | "\(.id)\t\(.interval_seconds)s\t\(.target)"'
  api monitors | jq '{toplam: length, namespace_200_ustu: [group_by(.namespace)[] | select(length > 200) | .[0].namespace]}'
  ```

Bitince port-forward'u kapatın: `kill $PF`.

## 0.45'e yükseltme

0.45 ilk imzalı sürümdür; bildirim teslimatında hata sınıflarını ayırır ve
Kubeat'in körleşmesini dış heartbeat olmadan da duyurur. Şema değişmez
(26).

### Yapılacak

- İmajları `latest` etiketiyle kullanıyorsanız bir sürüm etiketine ya da
  digest'e geçin: `latest` artık itilmez ve 0.44.0'da kalır. Denetim:
  `kubectl -n "$NS" get deploy -o jsonpath='{..image}'`.
- Son gönderimi başarısız kanalları yükseltmeden önce ya da sonra test
  edin ya da kapatın (Bildirimler sayfasında **Son gönderim**'i hatalı
  olanlar; betik: "Son gönderimi başarısız kanallar"): yükseltmeden 5
  dakika sonra son denemesi başarısız kalmış her etkin kanal için, eski
  bir kanal testi dahil, `KubeatNotificationsFailing` yanar.
- `KubeatNotificationsFailing`'e bağlı Alertmanager yönlendirmelerini ve
  susturmalarını kanal etiketlerine (`channel`, `channel_type`) göre
  güncelleyin. README "Aracın kendi sağlığı"ndaki `KubeatDown` susturma
  kuralını eklediyseniz hedef listesine `KubeatNotificationsFailing`'i
  ekleyin.
- Yeni `critical` alarm `KubeatUnhealthy`'yi yönlendirin. Scheduler
  durunca `KubeatSchedulerStalled` ile birlikte yanar; tek sayfa isteyen
  README "Aracın kendi sağlığı"ndaki isteğe bağlı susturma kuralını ekler.

### Davranış değişikliği

- **İmzalı yayım, `latest` yok.** İmajlar ve chart cosign ile imzalı
  yayımlanır, imajlar SBOM ve provenance taşır ve her sürümün GitHub
  Release'i digest'leri ve SBOM'ları yazar (README "Tedarik zinciri").
  `latest` etiketi artık itilmez ve 0.44.0'da kalır.
- **Reddeden makine kanalı duraklamaz.** Webhook ya da PagerDuty kanalı bir
  bildirimi 429, 407 ve 408 dışında bir 4xx ile reddederse kanal artık
  duraklamaz, aynı kanaldaki öteki bildirimler beklemeden gider. Reddedilen
  bildirim kısa tekrar almaz ve 1, 2, 4… dk yerine doğrudan
  `UPTIME_NOTIFY_RETRY_MAX` (varsayılan 15 dk) sonra yeniden denenir; geçici
  bir 4xx (alıcı yeniden yayımlanırken 404, token yenilenirken 401) sayfayı
  bu kadar geciktirebilir. Her şeyi reddeden bir makine kanalı bekleyen her
  bildirim için bu aralıkla bir istek alır. 429'da ve `Retry-After`'lı 503'te
  kısa tekrar yapılmaz, `Retry-After`'a uyulur.
- **`KubeatNotificationsFailing` kanal başına.** Alarm `channel` ve
  `channel_type` etiketleriyle ve `for: 5m` ile yanar; kanalın son
  denemesine bakar ve kanal bir bildirimi alana ya da başarılı bir testten
  geçene kadar açık kalır, 0.44'teki gibi 15 dakikada kendiliğinden sönmez.
  Kanal hâlâ teslim edilmemiş bir açık kesinti taşıyorsa başarılı test
  alarmı söndürmez; alarm kesinti teslim edilene ya da kapanana kadar açık
  kalır. Kuyrukta düşen bildirim aynı adla, kanalsız ve 5 dakika sonra
  yanar; `KubeatMetricsIncomplete` kanallar okunamadığında da yanar.
  `/metrics` artık kanal adlarını da verir.
- **Körleşme heartbeat olmadan da duyulur.** Yeni critical alarm
  `KubeatUnhealthy`, `kubeat_self_healthy` 5 dakika 0 kalınca yanar:
  scheduler ilerlemiyor, kontrol sonuçlarının hiçbiri yazılamıyor ya da bir
  yazım takıldı. Nedeni, karar değişince backend'in bir kez yazdığı
  `self-health failing` log satırındadır.
- **Bildirim kuyruğu 1000.** `UPTIME_NOTIFY_QUEUE_SIZE` varsayılanı 100'den
  1000'e çıktı; etkin monitor sayısı kuyruğu aşınca backend bir kez WARN
  loglar. İsteğe bağlı `metrics.prometheusRule.clusterLabel` boşken kurallar
  0.44'teki gibi kalır.
- **Kurulum notlarında fsGroup uyarısı.** OpenShift dışında
  `podSecurityContext`'i boş kurulumların `helm upgrade` notlarında yeni bir
  fsGroup uyarısı çıkar. Render edilen manifest aynıdır; backend bugün
  volume'a yazabiliyorsa uyarı yok sayılabilir. Yeni kurulumlar README'deki
  kurulum komutundaki `--set podSecurityContext.fsGroup=1001` ile yapılır;
  OpenShift'te bu satır atlanır, UID ve fsGroup'u SCC atar.

### Geri dönüş

Şema 26: `helm rollback` ile 0.44.0'a dönülür. Dönüşte
`KubeatNotificationsFailing` eski ifadesine ve etiketlerine döner, yani
yönlendirmeler yine güncellenir; `KubeatUnhealthy` ve yeni metrikler
kalkar, `metrics.prometheusRule.clusterLabel` yok sayılır, açıkça
verilmemişse kuyruk varsayılanı 100'e döner. Yayımlanmış imzalar kalır.

### Alarm kuralları: değişti

- Yeni `KubeatUnhealthy` (`critical`): `kubeat_self_healthy` 5 dakika 0.
- `KubeatNotificationsFailing` kanal başına (`channel`, `channel_type`),
  `for: 5m` ile; kuyrukta düşen bildirim için kanalsız ikinci ifade.
- `KubeatMetricsIncomplete` kanallar okunamadığında da yanar.
- İsteğe bağlı `metrics.prometheusRule.clusterLabel`; boşken öteki
  kuralların ifadesi 0.44'tekiyle aynıdır.
- Chart'ın şablonundaki kural sayısı 12'den 13'e çıktı.

## 0.44'e yükseltme

0.44 Kubeat'in kendi arızalarını sessiz bırakmaz ve kayıp yaratmaz. Şema
değişmez (26).

### Yapılacak

- 0.43 ya da öncesinden gelirken yedeği kapatmayı (`backup.enabled=false`)
  ya da `existingClaim`'e geçişi yükseltmeyle aynı adımda yapmayın: Helm
  canlı nesneye baktığından eski sürümün yedek PVC'si o adımda yine
  silinir. Önce aynı values ile 0.44'e yükseltin, değişikliği sonraki
  yükseltmede yapın (README "GitOps ile kurulum" ve "Yedekleme ve geri
  yükleme").
- Yükseltmeden önce açık kesintilere bakın ("Açık kesintiler"): 0.44'ün
  ilk açılışı bildirim kaydında hiç denemesi olmayan açık kesintileri de
  gönderir (aşağıda "Davranış değişikliği").
- Alarmları alan Alertmanager'a `KubeatDown` susturma kuralını ekleyin;
  yoksa Kubeat 10 dakikadan uzun düşükken kesinti, sertifika ve yedek
  alarmları "resolved" gider (README "Aracın kendi sağlığı").
- ServiceMonitor kapalıysa `up{job="<release>-backend",
  namespace="<namespace>"}` serisini bu etiketlerle toplayan bir scrape
  olsun; yoksa `KubeatDown` sürekli yanar.
- Log alarmlarınız `check result dropped` satırına bakıyorsa onları
  `batch insert failed` satırına taşıyın.
- Yeni chart değeri `backend.terminationGracePeriodSeconds` (varsayılan 30)
  `UPTIME_SHUTDOWN_TIMEOUT`'tan en az 5 sn uzun olmalıdır; kapanış süresini
  değiştirdiyseniz ikisini birlikte verin.

### Davranış değişikliği

- **Kaldırmada PVC'ler kalır.** `helm uninstall`, Argo CD 2.7 ve sonrasında
  uygulamayı silmek de, veri ve yedek PVC'lerini bırakır; eski davranış için
  `backend.persistence.keepOnUninstall=false` ve
  `backup.persistence.keepOnUninstall=false`.
- **Öz-sağlık alarmları sorun sürdükçe açık kalır.** `KubeatDown` backend
  pod'u hiç yokken de yanar; `up{job="<release>-backend",
  namespace="<namespace>"}` serisine bakar. `KubeatMonitorDown` ve
  `KubeatCertificateExpiring` 10 dakikaya kadar süren scrape boşluğunda açık
  kalır; `KubeatBackupFailing` yeni
  `kubeat_backup_last_failure_timestamp_seconds` metriğine bakar ve bir
  kopya başarılı olana kadar açık kalır. Bu dört alarmın etiketleri
  değiştiği için yükseltmede (ve 0.43'e dönüşte) açık olanlar bir kez
  "resolved" olur ve yeni etiketlerle yeniden yanar.
- **Heartbeat yazılamayan sonuçta kesilir.** `UPTIME_HEARTBEAT_URL` açıksa
  heartbeat, kontrol sonuçları veritabanına yazılamazken de kesilir;
  yükseltme anında veri volume'u zaten doluysa dış switch hemen alarm verir.
  Reddedilen toplu yazım artık satır başına `check result dropped` yerine
  yazım başına tek `batch insert failed` satırı loglar. Veri PVC'sinin boyut
  kuralı README "Geçmiş verisi"ndedir.
- **Bildirim yeniden başlatmada ve ölü makine kanalında kaybolmaz.**
  Yeniden başlatmada kuyrukta ya da grup beklemesinde kalmış açık
  kesintiler, kesintiden önce var olan kanallara gönderilir. Bildirim
  kaydında hiç denemesi olmayan açık kesintiler de her yeniden başlatmada,
  kesintiden önce var olan ve onu kapsayan kanallara gider; 0.44'ün ilk
  açılışında bu, 0.43'te kuyrukta kaybolmuş ya da kanal kapalı veya kapsam
  dışıyken açılmış kesintileri de kapsar. Bildirim almayan bir webhook ya
  da PagerDuty kanalına yeni bildirim denenmez, kanalın yeniden deneme
  zamanında gider; bir bildirimi sürekli reddeden (ör. 409) kanalda diğer
  bildirimler en çok `UPTIME_NOTIFY_RETRY_MAX` (bir bildirimin ilk
  reddedildiği geçişte bir geçiş daha) gecikir ve bu sürede aynı monitor
  için bekleyen hatırlatmanın ve yavaşlık bildiriminin yerini yenisi alır.
  Kapanışta insan kanallarının bekleyen grupları kuyruktan önce gider ve
  kapanış `UPTIME_SHUTDOWN_TIMEOUT` ile sınırlıdır (README "Alarm").

### Geri dönüş

Şema 26: `helm rollback` ile 0.43.0'a dönülür. Dönüşte `KubeatDown`,
`KubeatMonitorDown`, `KubeatCertificateExpiring` ve `KubeatBackupFailing`'in
açık alarmları, etiketleri değiştiği için bir kez "resolved" olur ve eski
etiketlerle yeniden yanar. 0.43'ün chart'ı PVC'lere kaldırma korumasını
basmaz: dönüşten sonra `helm uninstall` veri ve yedek PVC'lerini yine
siler.

### Alarm kuralları: değişti

- `KubeatDown` backend pod'u hiç yokken de yanar
  (`up{job="<release>-backend", namespace="<namespace>"}` ve `absent`).
- `KubeatMonitorDown` ve `KubeatCertificateExpiring` 10 dakikaya kadar
  süren scrape boşluğunda son değeri tutar.
- `KubeatBackupFailing` yeni `kubeat_backup_last_failure_timestamp_seconds`
  metriğine bakar ve bir kopya başarılı olana kadar açık kalır.
- Bu dört alarm `pod`, `instance`, `endpoint`, `container` ve `service`
  etiketlerini taşımaz. Kural sayısı aynıdır (12).

## 0.43'e yükseltme

0.43 bildirim teslimatını, yedeklemeyi ve aracın kendi alarmlarını
değiştirir. Şema değişmez (26).

### Yapılacak

- **Son gönderimi başarısız kanallar.** Yükseltmeden önce düzeltin ya da
  kapatın ("Son gönderimi başarısız kanallar"): 0.43 kanalın almadığı
  kesinti bildirimini olay kapanana kadar yeniden dener ve başarısız her
  deneme yeni `KubeatNotificationsFailing` alarmını yakar (son başarısız
  denemeden 15 dakika sonra söner).
- **Bildirim ayarları.** 0.43, 0.44 ve 0.45 `UPTIME_NOTIFY_RETRY_MAX`'ı
  açılışta doğrular: `0` değilse en az 1 dakika ve bir denemenin süresi
  (`UPTIME_NOTIFY_ATTEMPTS × (UPTIME_NOTIFY_TIMEOUT + UPTIME_NOTIFY_RETRY_DELAY)`,
  varsayılanlarla 30 sn) kadar olmalıdır; daha kısası reddedilir ve
  backend açılmaz. Varsayılan 15 dakikadır: bir denemesi 15 dakikayı aşan
  özel bir bildirim ayarınız varsa `UPTIME_NOTIFY_RETRY_MAX`'ı bu süreden
  uzun verin, denemeyi kısaltın ya da `0` ile yeniden denemeyi kapatın
  ("Bildirim ayarları").
- **PagerDuty.** Birden çok kurulumu aynı PagerDuty servisine bağlamayın:
  olay numaraları her kurulumda 1'den başlar, iki kurulumun aynı numaralı
  olayları tek alarmda birleşir ve birinin düzelmesi ötekini de kapatır.
  Her kuruluma ayrı bir servis verin.
- **Alertmanager.** Kurallar açıksa yeni alarmları (aşağıda "Alarm
  kuralları") Alertmanager yönlendirme ve susturma kurallarınıza ekleyin.

### Davranış değişikliği

- **Bildirim yeniden denenir.** Kanalın almadığı kesinti bildirimi olay
  kapanana kadar artan aralıkla (1, 2, 4… dk; en çok
  `UPTIME_NOTIFY_RETRY_MAX`, varsayılan 15 dk) yeniden gönderilir; düzelme
  bildirimi ilk başarısız denemeden itibaren `UPTIME_NOTIFY_RETRY_FOR`
  (varsayılan 1 sa) boyunca. Yeniden başlatmada bekleyen kesinti bildirimleri
  bildirim kaydından toparlanır. Eski davranış (tek deneme) için
  `UPTIME_NOTIFY_RETRY_MAX=0`. Kanallar artık birbirini bekletmez: yanıt
  vermeyen bir webhook Slack'i ya da e-postayı geciktirmez (README "Alarm").
- **Toparlanmanın 0.43'teki sınırı.** Yeniden başlatmada yalnızca en az
  bir kez denenmiş ve kanala ulaşamamış kesinti bildirimleri toparlanır.
  Gecikmesi ve kendi tekrarı (`delay_seconds`, `repeat_seconds`) olmayan
  bir kanala gidecek, yeniden başlatma anında kuyrukta ya da grup
  beklemesinde kalmış, hiç denenmemiş bildirim gitmez. 0.44 bunları da
  gönderir ("0.44'e yükseltme").
- **Yedek silme sırası.** Eski kopyalar yeni kopya yazılmadan önce `keep-1`'e
  indirilir; volume yine de doluysa en eski kopya silinip yazım bir kez
  yinelenir. `keep ≥ 2` ile son kopya hiç silinmez, `keep=1` ile tek kopya
  gidebilir; dolu kalan volume bir aralık sonra yeniden denenir (README
  "Yedekleme ve geri yükleme").
- **Yeni alarmlar ve metrik.** `metrics.prometheusRule.enabled` ile
  `kubeat.self` grubuna yeni alarmlar gelir (aşağıda "Alarm kuralları").
  `kubeat_notifications_total` artık bildirimin son sonucunu sayar;
  denemeler yeni `kubeat_notification_attempts_total` metriğindedir (README
  "Aracın kendi sağlığı").

### Geri dönüş

Şema aynı (26): `helm rollback` ile 0.42.2'ye dönülür. Dönüşte bildirim
yine tek denemeyle gider, yedek silme sırası eskisine döner, 0.43'ün
alarmları ve `kubeat_notification_attempts_total` kalkar.

### Alarm kuralları: değişti

`kubeat.self` grubuna yedi kural geldi: `KubeatResultsDropped`,
`KubeatNotificationsFailing`, `KubeatWorkersSaturated`,
`KubeatBackupFailing`, `KubeatBackupStale` (yalnızca `backup.enabled=true`
iken render edilir) ve `KubeatLicenseExpiring` (`warning` ve `critical`).
Önceki beş kural değişmedi; chart'ın şablonundaki kural sayısı 5'ten 12'ye
çıktı.

## 0.42'ye yükseltme

0.42 Kubeat Enterprise lisansını getirir (README "Kubeat Enterprise
lisansı"). Şema değişmez (26).

### Yapılacak

Aşağıdakilerden birini kullanıyorsanız **yükseltmeden önce** lisansı Secret
olarak verin (`license.existingSecret`); denetim için "Lisans isteyen
özellikler":

- LDAP ya da OIDC ile giriş;
- namespace'le sınırlı kullanıcılar ya da dizin grupları;
- denetim kaydı;
- birden fazla durum sayfası.

### Davranış değişikliği

Lisans verilmezse yükseltme yine çalışır ama:

- LDAP ve OIDC ile giriş kapanır; yalnızca yerel hesaplar girebilir.
- Namespace'le sınırlı kullanıcılar ve dizin grupları **sınırlı kalır**;
  yeni bir sınır konamaz, mevcut sınır daraltılabilir ya da kaldırılabilir
  ama genişletilemez.
- Denetim kaydı görünmez; yazılmaya devam eder.
- Birden fazla durum sayfası varsa hepsi yayında kalır ve düzenlenebilir,
  yalnızca yenisi oluşturulamaz.

İzleme, alarmlar ve bildirimler lisanstan etkilenmez.

### Geri dönüş

Şema aynı (26): `helm rollback` ile 0.41.1'e dönülür. 0.41 lisans okumaz;
bu özellikler orada lisanssız çalışır.

### Alarm kuralları: değişmedi

Kurallar 0.41'dekiyle aynıdır; `KubeatLicenseExpiring` 0.43 ile gelir.

## 0.41'e yükseltme

0.41 monitor'ların başka sistemlere yük bindirmesine karşı sınırlar getirir
(README "Hedef yükü"). Şema değişmez (26).

### Yapılacak

Denetim için "30 saniyeden sık monitor'lar ve kotalar".

- Cluster dışındaki bir hedefi 30 saniyeden sık kontrol eden bir monitor'un
  hedefini, aralığını ya da tekrar aralığını değiştirmeden ya da onu
  kopyalamadan önce aralığı 30 saniyeye (ya da üstüne) çekin ya da
  `UPTIME_MIN_CHECK_INTERVAL_EXTERNAL` ile sınırı kurulumunuza göre
  ayarlayın (`0` ayrı kuralı kaldırır).
- Monitor sayısı kotaların üstündeyse (1000 toplam, 200 namespace başına;
  5 dış host başına) kotayı `UPTIME_MAX_MONITORS*` ile yükseltin ya da `0`
  yapın.
- Monitor'ları toplu oluşturan betikler 429'da `Retry-After` başlığına
  uysun ya da `UPTIME_MONITOR_CREATE_PER_MINUTE`'i yükseltin.
- Sayıdan ibaret host'lu (`http://2130706433/`, `0x7f000001`, `127.1`)
  monitor'ların adresini adres olarak yazın.
- Kontrolleri `User-Agent`'a göre süzen bir WAF kuralı varsa güncelleyin ya
  da `UPTIME_USER_AGENT` verin.

### Davranış değişikliği

- Cluster dışındaki bir hedefi 30 saniyeden sık kontrol eden mevcut
  monitor'lar **çalışmaya devam eder** ve hedefi, aralığı ve tekrar aralığı
  değişmedikçe **düzenlenebilir**: adını, etiketlerini, açıklamasını
  değiştirmek, duraklatıp sürdürmek, formu olduğu gibi kaydetmek çalışır.
  Hedefi (yolu bile), aralığı ya da tekrar aralığını değiştiren bir
  `PATCH` ise yeni kurala tabidir ve 30 saniyenin altında
  `interval_seconds must be at least 30 for a target outside the cluster`
  ile reddedilir. Böyle bir monitor **olduğu gibi kopyalanamaz**: kopya yeni
  bir monitor'dur ve aynı hatayla reddedilir. Şirket ağındaki ama cluster
  dışındaki hedefler de "dış" sayılır; cluster içi adlar (`*.svc`, özel
  IP'ler) etkilenmez. Annotation ile yönetilen monitor'lar da bu kurala
  tabidir: 30 saniyeden kısa `kubeat.io/interval` taşıyan dış bir Ingress ya
  da Route için **yeni** monitor oluşturulmaz, keşif durumunda nedeniyle
  atlanmış görünür. Yükseltmeden önce oluşturulmuş böyle bir monitor ise
  **silinmez**: geçmişiyle birlikte, aralığına dokunulmadan çalışmaya devam
  eder ve atlananlar listesinde "the monitor made before the rule is kept
  at 10s" notuyla görünür; annotation kaldırılınca her zamanki gibi
  kaldırılır. `UPTIME_DISCOVERY_DEFAULT_INTERVAL` 30 saniyenin altındaysa
  aynısı geçerlidir: kendi aralığı olmayan mevcut dış Ingress ve Route
  monitor'ları kalır, yenileri atlanır ve sunucu açılışta bunu uyararak
  söyler.
- Bir dış host adına 60'tan, bir cluster içi host adına 600'den ya da bir IP
  adresine (hangi adla ulaşılırsa ulaşılsın) 600'den çok kontrol/dakika
  düşen kurulumlarda bazı kontroller ertelenir; monitor sayfasında "Hedef hız
  sınırında bekliyor" notu ve `kubeat_target_throttled_total` metriği bunu
  gösterir. Tek bir ingress ya da router adresinin arkasındaki yüzlerce host
  için README "Hedef yükü"ndeki paylaşılan adres paragrafı. Gerçekten
  gerekiyorsa `UPTIME_TARGET_MAX_RATE` / `UPTIME_CLUSTER_TARGET_MAX_RATE` /
  `UPTIME_TARGET_ADDRESS_MAX_RATE` değerlerini yükseltin.
- Mevcut monitor sayısı kotaların üstündeyse hiçbir şey durmaz, yalnızca
  yeni monitor oluşturulamaz ve dolu bir namespace'e monitor taşınamaz;
  annotation ile yönetilen monitor'lar da kotaya ve host sınırına tabidir
  (sığmayanlar keşif durumunda atlanmış görünür).
- Monitor'ları toplu oluşturan betikler son altmış saniyede 30'dan sonra 429
  alır.
- Sayıdan ibaret host'lar artık reddedilir.
- Kontrollerin `User-Agent`'ı `kubeat/<sürüm> (+https://github.com/yusufceng/Kubeat; <UPTIME_PUBLIC_URL>)`
  oldu.

### Geri dönüş

Şema aynı (26): `helm rollback` ile 0.40.1'e dönülür; sınırlar kalkar.

### Alarm kuralları: değişmedi

Kurallar 0.40'takiyle aynıdır.

## 0.40'a yükseltme

0.40 veritabanı şemasını 26'ya çıkarır (durum sayfası bölümleri, kuralları,
güncellemeleri ve ayarları; kanal gecikme ve tekrarı; yavaş yanıt eşiği).

### Yapılacak

Yükseltmeden önce yedek alın
([Yükseltmeden önce yedek](#yükseltmeden-önce-yedek)): geri dönüşün tek
yolu odur. Başka bir şey yapmak gerekmez.

### Davranış değişikliği

Mevcut durum sayfaları tek bir adsız bölümle, kanallar gecikmesiz ve genel
hatırlatma aralığıyla, monitor'lar yavaş yanıt eşiği kapalı olarak sürer.

### Geri dönüş

0.39 ve öncesi yükseltilmiş veritabanını açmaz ("database is at version 26,
newer than this binary"): `helm rollback` backend'i yeniden başlatma
döngüsünde bırakır. Geri dönmek gerekirse yükseltmeden önce alınmış yedeği
geri yükleyin (README "Yedekleme ve geri yükleme"); o yedekten sonraki
değişiklikler yeniden girilir ve 0.40'ın aldığı yedekler eski sürümlere
yüklenemez.

### Alarm kuralları: değişmedi

Kurallar 0.39'dakiyle aynıdır.

## 0.38'e yükseltme

0.38 güvenliği sıkılaştırır ve bazı varsayılanları değiştirir. Veritabanı
şeması 22'ye çıkar.

### Yapılacak

0.37 ya da öncesinden yükseltirken:

1. **Önce yedek alın** ([Yükseltmeden önce yedek](#yükseltmeden-önce-yedek)):
   geri dönüşün tek yolu odur (aşağıda "Geri dönüş").
2. **Şifreleme anahtarı.** Chart anahtarı kendisi üretir. Chart dışı
   kurulumlar `UPTIME_SECRET_KEY`'i (ya da `UPTIME_SECRET_KEY_FILE`'ı)
   vermeli ya da şifrelemeden bilerek `UPTIME_ALLOW_PLAINTEXT_SECRETS=true`
   ile vazgeçmelidir.
3. **Hedef politikası.** Loopback, link-local ya da metadata aralığını
   gerçekten kontrol eden monitor'lar (NodeLocal DNSCache'in
   `169.254.20.10`'u, Azure DNS'in `168.63.129.16`'sı) için
   `UPTIME_TARGET_ALLOW_CIDRS` verin (README "Hedef politikası"). Başka bir
   host'a ya da porta yönlenen ve kimlik bilgisi isteyen monitor'ların
   hedefini son adrese çevirin.
4. **Metrik anahtarı.** Elle yapılandırılmış bir Prometheus anahtarı
   göndermeli ya da `metrics.auth.enabled=false` verilmelidir (README
   "Metrik anahtarı").
5. **Ingress TLS.** Ingress'i düz HTTP ile kullanan kurulumlar TLS
   Secret'ını oluşturmalı ya da `ingress.tls.enabled=false` vermelidir;
   SSO ise Ingress controller'ın önünde HTTPS ister (README "Dışarıya açma:
   Route ve Ingress").
6. **SSO dönüş adresi.** `publicURL` Route ya da Ingress host'undan
   farklıysa yeni dönüş adresini (`<publicURL>/auth/callback`) yükseltmeden
   önce kimlik sağlayıcıya kaydedin ya da eskisini `oidc.redirectURL` ile
   sabitleyin; yolu olan bir `publicURL`'de
   (`https://portal.example.com/kubeat`)
   `oidc.redirectURL=https://<route ya da ingress host'u>/auth/callback`
   verin.
7. **Push token'larını yenileyin.** İzleyicilerin rapor gönderememesi
   gereken monitor'larda token'ı monitor sayfasındaki **Adresi yenile**
   düğmesiyle (`POST /api/v1/monitors/{id}/push-token`) yenileyin ve
   job'lara yeni adresi verin.
8. **Thanos'un 9091 portu.** Cluster çapındaki keşifle 9091'i kullanan
   kurulumlar `prometheus.openshiftMonitoringView=true` vermelidir; yoksa
   pod'ların CPU ve bellek değerleri boş kalır (README "Pod'lar ve kaynak
   kullanımı").
9. **`backend.env` ve `frontend.env`.** Ayarları `extraEnv`'e taşıyın.
   `--reuse-values` eski değeri geri getirir; `--reset-then-reuse-values`
   (Helm 3.14+) ile `--set backend.env=null --set backend.extraEnv.<AD>=<değer>`
   verin ya da ayarları taşınmış bir values dosyasıyla `--reuse-values`
   olmadan yükseltin. Denetim: `helm -n "$NS" get values "$REL" -o json |
   jq '{backend: .backend.env, frontend: .frontend.env}'` ikisi için de
   `null` vermeli.

### Davranış değişikliği

- **Şifreleme anahtarı zorunlu.** Backend `UPTIME_SECRET_KEY` (ya da
  `UPTIME_SECRET_KEY_FILE`) olmadan başlamaz. Anahtarlı ilk açılış
  veritabanını sıkıştırır ve büyük bir veritabanında uzun sürer (README
  "Gizli değerlerin şifrelenmesi").
- **Hedef politikası.** Monitor'lar ve bildirim kanalları loopback'e,
  link-local adreslere ve bulutların metadata servislerine bağlanmaz.
  Yönlendirmeler monitor'un header'larını ve `auth` bilgisini yalnızca aynı
  sunucuda kaldıklarında taşır.
- **Metrikler anahtar ister.** Chart'la kurulan backend'in `/metrics`'i
  bearer token ister. ServiceMonitor onu gönderir.
- **Ingress TLS ile sunulur.** `ingress.tls.enabled` varsayılan olarak
  açıktır ve chart adresi `https://<ingress.host>` olarak türetir.
- **Herkes bir kez yeniden girer.** HTTPS ile sunulan kurulumlarda oturum
  çerezinin adı `__Host-kubeat_session` oldu.
- **SSO dönüş adresi.** `publicURL` Route ya da Ingress host'undan
  farklıysa dönüş adresi `<publicURL>/auth/callback` olur; 0.37.0'a kadar
  SSO Route ya da Ingress host'unu kullanıyordu. Backend'in reddedeceği bir
  dönüş adresiyle chart kurulmaz ve hangi değerin düzeltileceğini söyler.
- **Push adresleri izleyicilere gösterilmez.** Önceki sürümler push
  monitor'larının rapor adreslerini izleyicilere de gösteriyordu.
- **`backend.env` ve `frontend.env` okunmaz.** Boş değillerse chart
  kurulmaz.
- Gerisi bir şey yapmayı gerektirmez: izleyiciler HTTP monitor'larının
  istek gövdesini görmez; ters çevrilmiş monitor'lar politikanın reddini
  beklenen hata saymaz; kullanılamayan bir SSO ayarı backend'i durdurmaz,
  yalnızca SSO'yu kapatır; `helm upgrade --reuse-values` 0.37.0'dan da
  çalışır.

### Geri dönüş

0.37.0 ve öncesi yükseltilmiş veritabanını açmaz ("database is at version
22, newer than this binary"), `helm rollback` backend'i yeniden başlatma
döngüsünde bırakır. Geri dönmek gerekirse yükseltmeden önce alınmış yedeği
geri yükleyin (README "Yedekleme ve geri yükleme"); o yedekten sonraki
değişiklikler yeniden girilir ve bu sürümün aldığı yedekler eski sürümlere
yüklenemez.

### Alarm kuralları: değişmedi

Kurallar 0.37'dekiyle aynıdır, yalnızca bir yorum satırı değişti. Ama
metrikler artık anahtar ister: anahtarı göndermeyen elle yapılandırılmış
bir Prometheus'ta seriler gelmez (Yapılacak 4).
