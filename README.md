<h1>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/kubeat-lockup-dark.svg">
    <img alt="Kubeat" src="docs/assets/kubeat-lockup.svg" height="44">
  </picture>
</h1>

OCP/Kubernetes ortamlarındaki servisler için uptime monitoring aracı.
Go + Next.js + SQLite.

Proje planı: [docs/proje-plani.md](docs/proje-plani.md) · Yol haritası ve bilinen sınırlar: [docs/yol-haritasi.md](docs/yol-haritasi.md) · Şirket cluster'ında pilot: [docs/pilot.md](docs/pilot.md)

## Yapı

```
backend/        Go API, scheduler, checker, alarm motoru, agent
frontend/       Next.js (App Router) arayüzü
deploy/helm/    Helm chart
docs/adr/       Mimari karar kayıtları
```

## Lokal çalıştırma

```
make dev        # ikisi birlikte; arayüz -> http://localhost:3000
make backend    # API  -> http://localhost:8090
make frontend   # UI   -> http://localhost:3000
make test
```

İlk çalıştırmada hesap yoktur; arayüz `/setup` sayfasında ilk yöneticiyi
oluşturtur (`make dev ADMIN_PASSWORD=...` verilirse backend `admin` hesabını o
parolayla açar). Veritabanındaki gizli değerleri şifreleyen anahtar ilk
çalıştırmada `.dev/secret.key` dosyasına bir kez üretilir ve git'e girmez;
silinirse `backend/uptime.db` de silinmelidir. Arayüz yalnızca bu makineden
erişilsin diye `127.0.0.1`'de dinler; ağdaki başka bir cihazdan denemek için
`make dev FRONTEND_HOST=0.0.0.0`.

## Cluster'a kurulum

Her sürüm etiketi (`v0.28.0` gibi) imajları `ghcr.io/yusufceng/kubeat-backend`
ve `ghcr.io/yusufceng/kubeat-frontend` adlarıyla (amd64 ve arm64), chart'ı da
`oci://ghcr.io/yusufceng/charts/kubeat` olarak yayımlar
([.github/workflows/release.yml](.github/workflows/release.yml)). Herhangi bir
cluster'a kurulum:

```
helm upgrade --install kubeat oci://ghcr.io/yusufceng/charts/kubeat --version 0.42.2 \
  -n kubeat --create-namespace
```

İmajları kendi registry'nize aynalıyorsanız `backend.image.repository`,
`frontend.image.repository` ve gerekirse `imagePullSecrets` değerlerini
verin. Chart kaynaktan da kurulabilir: [deploy/helm/kubeat](deploy/helm/kubeat).
Minikube için (imajlar yerelde derlenir, registry gerekmez):

```
minikube start --keep-context   # kubectl'in mevcut bağlamını değiştirmez
make minikube-deploy            # imajları derler, yükler, chart'ı kurar
make minikube-open              # arayüz -> http://localhost:3000
make minikube-password          # giriş parolasını yazdırır
make minikube-demo              # keşfi denemek için örnek namespace
```

Backend cluster içinde çalıştığı için Service'ler DNS adlarıyla izlenebilir,
örneğin `http://servis.namespace.svc:8080/healthz`.

- Backend tek replica çalışır (SQLite tek yazıcıya izin verir) ve verisini bir
  PVC'de tutar.
- İmajlar UBI tabanlıdır; container'lar root olmadan, rastgele bir UID ile ve
  salt okunur kök dosya sistemiyle çalışabilir.
- OpenShift'te `podSecurityContext` boş bırakılır ve `route.enabled=true`
  verilir; gerçek bir OpenShift 4.21'de doğrulandı.
- Chart'ın ayrı değeri olmayan her ayar `backend.extraEnv` ve
  `frontend.extraEnv` altında ortam değişkeni olarak verilir — alarm
  davranışı da böyle ayarlanır (`UPTIME_REMINDER_INTERVAL`,
  `UPTIME_NOTIFY_GROUP_WAIT`, `UPTIME_RECOVERY_THRESHOLD`, ...); bkz.
  [values.yaml](deploy/helm/kubeat/values.yaml). Eski values dosyalarındaki
  `backend.env` ve `frontend.env` okunmaz: boş değillerse chart kurulmaz ve
  oradaki ayarların `extraEnv`'e taşınmasını ister.
- Yükseltirken ayarları bir values dosyasında tutup `-f` ile verin ya da
  `--reset-then-reuse-values` (Helm 3.14+) kullanın. `--reuse-values` eski
  chart'ın varsayılanlarını da taşır: imaj etiketi bir kez `--set` ile
  sabitlendiyse `null` ile silmek bile yetmez, pod'lar eski sürümde kalır.
  Eldeki ayarlar `helm get values kubeat -o yaml` ile alınır.

### 0.38'e yükseltme

0.38 güvenliği sıkılaştırır ve bazı varsayılanları değiştirir. 0.37 ya da
öncesinden yükseltirken:

1. **Önce yedek alın.** Veritabanı şeması 22'ye çıkar. 0.37.0 ve öncesi
   yükseltilmiş veritabanını açmaz ("database is at version 22, newer than
   this binary"), `helm rollback` backend'i yeniden başlatma döngüsünde
   bırakır. Geri dönmek gerekirse yükseltmeden önce alınmış yedeği geri
   yükleyin (bkz. "Yedekleme ve geri yükleme"); o yedekten sonraki
   değişiklikler yeniden girilir ve bu sürümün aldığı yedekler eski
   sürümlere yüklenemez.
2. **Şifreleme anahtarı zorunlu.** Backend `UPTIME_SECRET_KEY` (ya da
   `UPTIME_SECRET_KEY_FILE`) olmadan başlamaz; chart anahtarı kendisi
   üretir. Chart dışı kurulumlar anahtarı vermeli ya da şifrelemeden bilerek
   `UPTIME_ALLOW_PLAINTEXT_SECRETS=true` ile vazgeçmelidir. Anahtarlı ilk
   açılış veritabanını sıkıştırır ve büyük bir veritabanında uzun sürer
   (bkz. "Gizli değerlerin şifrelenmesi").
3. **Hedef politikası.** Monitor'lar ve bildirim kanalları loopback'e,
   link-local adreslere ve bulutların metadata servislerine bağlanmaz. Bu
   aralıkları gerçekten kontrol eden monitor'lar (NodeLocal DNSCache'in
   `169.254.20.10`'u, Azure DNS'in `168.63.129.16`'sı) için
   `UPTIME_TARGET_ALLOW_CIDRS` verin (bkz. "Hedef politikası").
   Yönlendirmeler monitor'un header'larını ve `auth` bilgisini yalnızca aynı
   sunucuda kaldıklarında taşır; başka bir host'a ya da porta yönlenen ve
   kimlik bilgisi isteyen monitor'ların hedefini son adrese çevirin.
4. **Metrikler anahtar ister.** Chart'la kurulan backend'in `/metrics`'i
   bearer token ister. ServiceMonitor onu gönderir; elle yapılandırılmış bir
   Prometheus da göndermeli ya da `metrics.auth.enabled=false` verilmelidir
   (bkz. "Metrik anahtarı").
5. **Ingress TLS ile sunulur.** Ingress'i düz HTTP ile kullanan kurulumlar
   TLS Secret'ını oluşturmalı ya da `ingress.tls.enabled=false`
   vermelidir; SSO ise Ingress controller'ın önünde HTTPS ister (bkz.
   "Dışarıya açma: Route ve Ingress").
6. **Herkes bir kez yeniden girer.** HTTPS ile sunulan kurulumlarda oturum
   çerezinin adı `__Host-kubeat_session` oldu.
7. **SSO dönüş adresi.** `publicURL` Route ya da Ingress host'undan
   farklıysa dönüş adresi `<publicURL>/auth/callback` olur: yükseltmeden
   önce kimlik sağlayıcıya kaydedin ya da eskisini `oidc.redirectURL` ile
   sabitleyin. 0.37.0'a kadar SSO Route ya da Ingress host'unu kullanıyordu;
   yolu olan bir `publicURL`'de (`https://portal.example.com/kubeat`)
   `oidc.redirectURL=https://<route ya da ingress host'u>/auth/callback`
   verin. Backend'in reddedeceği bir dönüş adresiyle chart kurulmaz ve
   hangi değerin düzeltileceğini söyler.
8. **Push token'larını yenileyin.** Önceki sürümler push monitor'larının
   rapor adreslerini izleyicilere de gösteriyordu. İzleyicilerin rapor
   gönderememesi gereken monitor'larda token'ı monitor sayfasındaki
   **Adresi yenile** düğmesiyle (`POST /api/v1/monitors/{id}/push-token`)
   yenileyin ve job'lara yeni adresi verin.
9. **Thanos'un 9091 portu.** Cluster çapındaki keşifle 9091'i kullanan
   kurulumlar `prometheus.openshiftMonitoringView=true` vermelidir; yoksa
   pod'ların CPU ve bellek değerleri boş kalır (bkz. "Pod'lar ve kaynak
   kullanımı").
10. **`backend.env` ve `frontend.env` okunmaz.** Boş değillerse chart
    kurulmaz: ayarları `extraEnv`'e taşıyın. `--reuse-values` eski değeri
    geri getirir; `--reset-then-reuse-values` (Helm 3.14+) ile
    `--set backend.env=null --set backend.extraEnv.<AD>=<değer>` verin ya
    da ayarları taşınmış bir values dosyasıyla `--reuse-values` olmadan
    yükseltin.

Gerisi bir şey yapmayı gerektirmez: izleyiciler HTTP monitor'larının istek
gövdesini görmez; ters çevrilmiş monitor'lar politikanın reddini beklenen
hata saymaz; kullanılamayan bir SSO ayarı backend'i durdurmaz, yalnızca
SSO'yu kapatır; `helm upgrade --reuse-values` 0.37.0'dan da çalışır.

### 0.40'a yükseltme

0.40 veritabanı şemasını 26'ya çıkarır (durum sayfası bölümleri, kuralları,
güncellemeleri ve ayarları; kanal gecikme ve tekrarı; yavaş yanıt eşiği).
Yükseltmeden önce yedek alın: 0.39 ve öncesi yükseltilmiş veritabanını
açmaz. Başka bir şey yapmak gerekmez; mevcut durum sayfaları tek bir adsız
bölümle, kanallar gecikmesiz ve genel hatırlatma aralığıyla, monitor'lar
yavaş yanıt eşiği kapalı olarak sürer.

### 0.41'e yükseltme

0.41 monitor'ların başka sistemlere yük bindirmesine karşı sınırlar getirir
(bkz. "Hedef yükü"). Şema değişmez. Yükseltince:

- Cluster dışındaki bir hedefi 30 saniyeden sık kontrol eden mevcut
  monitor'lar **çalışmaya devam eder** ve hedefi, aralığı ve tekrar aralığı
  değişmedikçe **düzenlenebilir**: adını, etiketlerini, açıklamasını
  değiştirmek, duraklatıp sürdürmek, formu olduğu gibi kaydetmek çalışır.
  Hedefi (yolu bile), aralığı ya da tekrar aralığını değiştiren bir
  `PATCH` ise yeni kurala tabidir ve 30 saniyenin altında
  `interval_seconds must be at least 30 for a target outside the cluster`
  ile reddedilir. Böyle bir monitor **olduğu gibi kopyalanamaz**: kopya yeni
  bir monitor'dur ve aynı hatayla reddedilir; önce aralığı 30 saniyeye (ya
  da üstüne) çekin ya da `UPTIME_MIN_CHECK_INTERVAL_EXTERNAL` ile sınırı
  kurulumunuza göre ayarlayın (`0` ayrı kuralı kaldırır). Şirket ağındaki
  ama cluster dışındaki hedefler de "dış" sayılır; cluster içi adlar
  (`*.svc`, özel IP'ler) etkilenmez. Annotation ile yönetilen monitor'lar da
  bu kurala tabidir: 30 saniyeden kısa `kubeat.io/interval` taşıyan dış bir
  Ingress ya da Route için **yeni** monitor oluşturulmaz, keşif durumunda
  nedeniyle atlanmış görünür. Yükseltmeden önce oluşturulmuş böyle bir
  monitor ise **silinmez**: geçmişiyle birlikte, aralığına dokunulmadan
  çalışmaya devam eder ve atlananlar listesinde "the monitor made before
  the rule is kept at 10s" notuyla görünür; annotation kaldırılınca her
  zamanki gibi kaldırılır. `UPTIME_DISCOVERY_DEFAULT_INTERVAL` 30 saniyenin
  altındaysa aynısı geçerlidir: kendi aralığı olmayan mevcut dış Ingress ve
  Route monitor'ları kalır, yenileri atlanır ve sunucu açılışta bunu
  uyararak söyler.
- Bir dış host adına 60'tan, bir cluster içi host adına 600'den ya da bir IP
  adresine (hangi adla ulaşılırsa ulaşılsın) 600'den çok kontrol/dakika
  düşen kurulumlarda bazı kontroller ertelenir; monitor sayfasında "Hedef hız
  sınırında bekliyor" notu ve `kubeat_target_throttled_total` metriği bunu
  gösterir. Tek bir ingress ya da router adresinin arkasındaki yüzlerce host
  için bkz. "Hedef yükü"ndeki paylaşılan adres paragrafı. Gerçekten
  gerekiyorsa `UPTIME_TARGET_MAX_RATE` / `UPTIME_CLUSTER_TARGET_MAX_RATE` /
  `UPTIME_TARGET_ADDRESS_MAX_RATE` değerlerini yükseltin.
- Mevcut monitor sayısı kotaların üstündeyse (1000 toplam, 200 namespace
  başına; 5 dış host başına) hiçbir şey durmaz, yalnızca yeni monitor
  oluşturulamaz ve dolu bir namespace'e monitor taşınamaz; annotation ile
  yönetilen monitor'lar da kotaya ve host sınırına tabidir (sığmayanlar
  keşif durumunda atlanmış görünür). Kotayı `UPTIME_MAX_MONITORS*` ile
  yükseltin ya da `0` yapın.
- Monitor'ları toplu oluşturan betikler son altmış saniyede 30'dan sonra 429
  alır; `Retry-After` başlığına uyun ya da `UPTIME_MONITOR_CREATE_PER_MINUTE`'i
  yükseltin.
- Sayıdan ibaret host'lar (`http://2130706433/`, `0x7f000001`, `127.1`)
  artık reddedilir; adresi adres olarak yazın.
- Kontrollerin `User-Agent`'ı `kubeat/<sürüm> (+https://github.com/yusufceng/Kubeat; <UPTIME_PUBLIC_URL>)`
  oldu; eskisine göre süzen bir WAF kuralı varsa güncelleyin ya da
  `UPTIME_USER_AGENT` verin.

### 0.42'ye yükseltme

0.42 Kubeat Enterprise lisansını getirir (bkz. "Kubeat Enterprise
lisansı"). Şema değişmez. Aşağıdakilerden birini kullanıyorsanız
**yükseltmeden önce** lisansı Secret olarak verin
(`license.existingSecret`); vermezseniz yükseltme yine çalışır ama:

- LDAP ve OIDC ile giriş kapanır; yalnızca yerel hesaplar girebilir.
- Namespace'le sınırlı kullanıcılar ve dizin grupları **sınırlı kalır**;
  yeni bir sınır konamaz, mevcut sınır daraltılabilir ya da kaldırılabilir
  ama genişletilemez.
- Denetim kaydı görünmez; yazılmaya devam eder.
- Birden fazla durum sayfası varsa hepsi yayında kalır ve düzenlenebilir,
  yalnızca yenisi oluşturulamaz.

İzleme, alarmlar ve bildirimler lisanstan etkilenmez.

### GitOps ile kurulum (Argo CD, OpenShift GitOps)

Chart dört Secret'ı ilk kurulumda rastgele üretir ve sonraki güncellemelerde
cluster'dan geri okuyarak korur:

| Secret (anahtar) | İçerik | Kendi Secret'ınız için |
| --- | --- | --- |
| `<release>-secret-key` (`key`) | Veritabanındaki gizli değerleri şifreleyen anahtar | `secretKey.existingSecret` |
| `<release>-admin` (`password`) | İlk yöneticinin parolası | `auth.existingSecret` |
| `<release>-api-token` (`token`) | API anahtarı | `apiToken.existingSecret` |
| `<release>-metrics-token` (`token`) | Metrik anahtarı | `metrics.auth.existingSecret` |

Cluster'a erişmeden şablon üreten araçlar (`helm template`, Argo CD,
OpenShift GitOps) bu Secret'ları geri okuyamaz: her üretimde yeni değerler
çıkar ve her eşitleme Secret'ların üzerine yazar. En ağır sonuç şifreleme
anahtarındadır: backend bir sonraki açılışta yeni anahtarı reddeder ve eski
anahtarla şifrelenmiş değerler geri gelmez. API anahtarı da değişir; metrik
anahtarı backend yeniden başlayana kadar ServiceMonitor'ün gönderdiğiyle
uyuşmaz ve scrape'ler `401` alır; yönetici Secret'ı da artık gerçek parolayı
göstermez.

Bu yüzden GitOps ile kurarken dört Secret'ı kendiniz oluşturun (Sealed
Secrets, External Secrets Operator, ...) ve adlarını yukarıdaki
`existingSecret` değerleriyle verin; chart o zaman bunları üretmez.

### OpenShift

OpenShift'te chart şu değerlerle kurulur; `podSecurityContext` boş bırakılır,
UID ve fsGroup'u SCC atar:

```
helm upgrade --install kubeat oci://ghcr.io/yusufceng/charts/kubeat --version 0.42.2 \
  -n kubeat --create-namespace \
  --set route.enabled=true --set route.host=kubeat.apps.<cluster-domain> \
  --set metrics.serviceMonitor.enabled=true --set metrics.prometheusRule.enabled=true \
  --set prometheus.url=https://thanos-querier.openshift-monitoring.svc:9091 \
  --set prometheus.tokenFile=/var/run/secrets/kubernetes.io/serviceaccount/token \
  --set prometheus.caFile=/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt \
  --set prometheus.openshiftMonitoringView=true
```

İmajlar UBI tabanlıdır, kök olmayan ve rastgele bir UID ile, salt okunur
kök dosya sistemiyle ve tüm capability'ler düşürülmüş olarak çalışır
(`restricted-v2` SCC'nin istedikleri). Backend yalnızca `/data`, `/backup` ve
`/tmp` dizinlerine yazar.

**Benzetim.** Elimizde gerçek bir OpenShift olmadığı için minikube,
Kubeat'in gözünde OpenShift'e benzetilir:

```
make minikube-demo
make minikube-openshift-sim
```

Bu komut `deploy/demo/openshift-sim/install.sh` ile şunları kurar ve chart'ı
yukarıdaki değerlerle (`deploy/demo/openshift-sim/values.yaml`) ayrı bir
`kubeat-ocp` namespace'ine kurar:

- Route, ServiceMonitor ve PrometheusRule CRD'leri (OpenShift ve
  prometheus-operator'ın kendi tanımları).
- `openshift-monitoring` namespace'inde bir **thanos-querier**: demo
  Prometheus'un önünde, benzetilmiş bir "service CA" ile TLS konuşan ve
  gelen token'ı TokenReview, yetkiyi SubjectAccessReview ile denetleyen
  kube-rbac-proxy. `cluster-monitoring-view` ClusterRole'ü de oluşturulur;
  chart'ın bağlaması gerçekte olduğu gibi ona yapılır.
- Her namespace'te bulunan `openshift-service-ca.crt` ConfigMap'inin bir
  kopyası; benzetimde CA bu ConfigMap'ten `backend.extraVolumes` ile
  bağlanır (OpenShift'te pod'a kendiliğinden gelir).
- `kubeat-ocp` namespace'inde `restricted` Pod Security Standard;
  `restricted-v2` SCC'nin en yakın karşılığı. Pod'lar SCC'nin atayacağı
  türden rastgele bir UID (1000680000) ile çalışır.
- `shop-demo`'da iki Route: biri cluster içinden çözülen bir host ile
  (kontrolü router olmadan da geçer), diğeri gerçek bir Route gibi apps alan
  adı ve edge TLS ile (host çözülmediği için kontrolü başarısız kalır).

Benzetimle doğrulananlar: kısıtlı güvenlik bağlamı altında çalışma, Route ve
ServiceMonitor şablonlarının API tarafından kabulü, Route keşfi ve Route
üzerinden pod çözümleme, servis hesabı token'ı ve service CA ile metrik
okuma, `cluster-monitoring-view` bağlaması, yetkisiz ICMP ile ping.

Benzetimin yapamadıkları: Route'un gerçekten trafik taşıması (router yok),
SCC admission'ın UID'yi kendisinin ataması, service CA'nın pod'a
kendiliğinden gelmesi, Thanos Querier'ın gerçek yetki kuralı, iç registry ve
pull secret'lar, OAuth proxy. Bunlar ilk gerçek kurulumda denenmelidir.

Benzetimi kaldırmak: `helm -n kubeat-ocp uninstall kubeat`; CRD'ler ve
`openshift-monitoring` namespace'i kalır.

## Güvenlik

Backend API'si her istekte ya giriş yapmış bir kullanıcının oturumunu ya da
`UPTIME_API_TOKEN` anahtarını (`Authorization: Bearer ...`) ister; arayüz
oturumla, betikler anahtarla konuşur (bkz. "Hesaplar ve giriş"). Anahtar
ayarlanmazsa API yalnızca oturumla kullanılabilir. Sağlık kontrolü
(`/api/v1/health`), yayımlanmış durum sayfaları ve rozetler kimlik istemez;
hiçbiri bir şey değiştirmez. `/metrics`, `UPTIME_METRICS_TOKEN` ayarlıysa
yalnızca o anahtarı sunan scrape'lere yanıt verir (bkz. "Metrik anahtarı").
Push raporları (`/api/push/{token}`) da anahtar istemez: adresteki token o
monitor'un sırrıdır ve yalnızca o monitor'a rapor yazabilir. Token'ı (ve
rapor adresini) yalnızca monitor'u değiştirebilenler görür; izleyicilere
gösterilmez, çünkü onunla monitor'u düşmüş ya da çalışıyor gösterebilirler.
Sızan bir token monitor sayfasından yenilenebilir (bkz. `push`).

Cluster'a ya da metrik sunucusuna yapılan bir istek başarısız olduğunda
hatanın kendisini (Kubeat'in servis hesabı, API sunucusunun ve metrik
sunucusunun adresi, sertifika ayrıntıları, kubeconfig bağlamı) yalnızca
yöneticiler görür. Diğerlerine hatanın türü söylenir: servis hesabının hangi
namespace'te neyi yapamadığı, kimliğinin kabul edilmediği, sunucuya
ulaşılamadığı ya da sunucunun sertifikasına güvenilmediği; türü belli olmayan
hatalarda ayrıntının logda olduğu yazar. Hatanın tam metni her zaman backend
logundadır. Workload monitor'larının ve her pod'u ayrı kontrol eden
monitor'ların kontrolleri de cluster'a sorar; bu kontrollerin sonucu, olay
nedeni ve bildirimi monitor'u okuyabilen herkese ve bildirim kanallarına
gittiği için yöneticilere de yalnızca hatanın türünü yazar (forbidden,
unauthorized, not found, zaman aşımı, ulaşılamıyor, güvenilmeyen sertifika);
tam metin logdadır.

Bir hedefin protokol dışı sözleri de sonuca yazılmaz: HTTP ile yanıt
vermeyen bir servisin ilk satırı (bir SSH ya da SMTP karşılaması), Redis'in
hata yanıtı ve `PING`'e verilen beklenmedik yanıt sonuçta yalnızca türüyle
görünür (`the server did not answer with HTTP`, `the server answered with an
error (WRONGPASS)`, `unexpected answer to PING`); kendisi backend
logundadır. Böylece hedefi seçemeyen izleyiciler bir iç servisin
karşılamasını sonuçlardan okuyamaz. Diğer hatalar hedefin söylediğini
taşımaya devam eder: sertifikadaki adlar, JSON kontrolünün karşılaştırdığı
değer, PostgreSQL ve MySQL'in hata mesajları ve yönlendirme adresleri. Bir
sonucun hata metninin ilk 300 baytı saklanır; uzun bir hedef adresi hatada
kısaltılır, ki hatanın nedeni kesilmesin.

Helm chart API anahtarını ilk kurulumda rastgele üretir, bir Secret'ta
saklar ve güncellemelerde korur; Argo CD gibi araçlarla kurarken bkz.
"GitOps ile kurulum".

**Ağ erişimi.** `networkPolicy.enabled=true` (varsayılan kapalı) backend'e
yalnızca arayüz pod'larının, `metrics.serviceMonitor.enabled` açıksa
`networkPolicy.metricsFrom`'daki Prometheus'un ve
`networkPolicy.backendIngressFrom`'da sayılanların bağlanmasına izin verir;
NetworkPolicy uygulayan bir ağ eklentisi gerekir. `metricsFrom` varsayılan
olarak OpenShift'in user workload monitoring'ini
(`openshift-user-workload-monitoring` namespace'i) seçer; platformun kendi
Prometheus'u için `openshift-monitoring`, başka cluster'larda Prometheus'un
namespace'i yazılır. Politika yalnızca backend'e gelen bağlantıları
kısıtlar, backend'in kendi bağlantılarını değil: kontroller her servise,
pod'a ve adrese ulaşabilmelidir. Politika açıkken başka namespace'lerdeki
işler push raporlarını backend'e değil arayüze gönderir
(`http://<release>-frontend.<namespace>.svc:3000/api/push/<token>`); arayüz
raporu backend'e iletir. Chart `PUSH_INTERNAL_URL`'i bu adrese çevirir,
monitor sayfası da cluster içi adres olarak bunu gösterir. API anahtarıyla
backend'e doğrudan bağlanan betikler `networkPolicy.backendIngressFrom` ile
izin almalıdır.

### Hedef politikası

Monitor hedeflerini ve bildirim kanallarının adreslerini, onları
düzenleyebilen her kullanıcı belirler; backend bu adreslere cluster'ın
içinden bağlanır. Böyle bir adres backend'in kendisine (loopback) ya da
bulut sağlayıcısının metadata servisine (`169.254.169.254`) götürmesin diye
kontroller ve bildirimler her bağlantıyı **hedef politikasına** göre açar.
Politika bağlantı anında, ad çözüldükten sonra bağlanılacak adrese bakar:
zararsız görünen bir alan adı, iki sorgu arasında değişen bir DNS yanıtı ve
yönlendirmenin (redirect) her adımı aynı kurala takılır. Yöneticinin
ortamda verdiği vekil, SMTP, Telegram ve PagerDuty adresleri ise tam
adresleriyle güvenilir (bkz. aşağıda "Yöneticinin uç noktaları").

Yerleşik olarak yasak olan aralıklar:

| Aralık | Neden |
| --- | --- |
| `127.0.0.0/8`, `::1/128` | Loopback: backend pod'unun kendisi |
| `0.0.0.0/8`, `::/128` | "Bu makine"; `0.0.0.0` loopback'e ulaşır |
| `169.254.0.0/16`, `fe80::/10` | Link-local; bulut metadata servisi `169.254.169.254`'tedir |
| `fd00:ec2::254/128` | AWS metadata servisinin IPv6 adresi |
| `fd20:ce::254/128` | Google Cloud metadata servisinin IPv6 adresi |
| `168.63.129.16/32` | Azure'un host ajanı (WireServer); aynı zamanda Azure'un DNS sunucusu |
| `100.100.100.200/32` | Alibaba Cloud metadata servisi |
| `224.0.0.0/4`, `ff00::/8`, `255.255.255.255/32` | Multicast ve broadcast |

Özel ağlar (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `100.64.0.0/10`,
`fd00::/8`) bilerek yasak **değildir**: Kubeat'in işi cluster içindeki
servisleri kontrol etmektir ve ClusterIP'ler (OpenShift'te `172.30.0.0/16`),
pod IP'leri ve `*.svc` adları bu aralıklardadır. IPv6 biçiminde yazılmış
IPv4 adresleri (`::ffff:127.0.0.1`) ve NAT64 adresleri (`64:ff9b::/96`),
taşıdıkları IPv4 adresine göre denetlenir. Yalnızca bu iyi bilinen NAT64
öneki tanınır: ağınız kendi önekini kullanıyorsa (örneğin `64:ff9b:1::/48`
içinden bir `/96`), IPv4 adresinin nereye yazıldığı ağa bağlıdır ve yasak
aralıkların o önekteki karşılıkları `UPTIME_TARGET_DENY_CIDRS`'e ayrıca
eklenmelidir; `64:ff9b:1:1::/96` için metadata ve loopback:
`64:ff9b:1:1::a9fe:0/112,64:ff9b:1:1::7f00:0/104`.

Azure'da `168.63.129.16` platformun DNS sunucusudur da: onu çözücü olarak
soran bir DNS monitor'u için istisna gerekir
(`UPTIME_TARGET_ALLOW_CIDRS=168.63.129.16/32`). Pod'un kendi ad çözmesi
bundan etkilenmez.

Yasak bir adrese giden kontrol ya da kanal testi, adresi ve aralığı söyleyen
bir hatayla başarısız olur: `169.254.169.254 is blocked by the target policy
(169.254.0.0/16); UPTIME_TARGET_ALLOW_CIDRS can allow it`. Ters çevrilmiş
(`inverted`) bir monitor bunu beklediği başarısızlık saymaz. İki ayar,
virgülle ayrılmış CIDR listesi alır; tek bir adres `/32` (IPv6'da `/128`)
ile yazılır, geçersiz bir girdi backend'i başlatmaz:

- `UPTIME_TARGET_DENY_CIDRS`: yerleşik listeye eklenen yasaklar.
- `UPTIME_TARGET_ALLOW_CIDRS`: istisnalar; yerleşik ya da eklenmiş her
  yasağın önüne geçer.

```
# Aynı node'daki bir ajanın 127.0.0.1'deki ucunu kontrol etmek
UPTIME_TARGET_ALLOW_CIDRS=127.0.0.1/32
# DNS monitor'unda çözücü olarak NodeLocal DNSCache (169.254.20.10)
UPTIME_TARGET_ALLOW_CIDRS=169.254.20.10/32
# Yalnızca cluster dışını kontrol eden sıkı bir kurulum: pod ve Service ağı da yasak
UPTIME_TARGET_DENY_CIDRS=10.0.0.0/8
```

Açılışta `target policy in effect` log satırı eklenen yasakları,
istisnaları ve güvenilen vekil adreslerini (`trusted`) yazar. Çözücüsü
verilmemiş DNS monitor'ları ve ad çözme,
pod'un kendi çözücüsünü (CoreDNS) kullanır; politika ona uygulanmaz.
Backend'e `HTTP_PROXY`/`HTTPS_PROXY` verildiyse vekil üzerinden giden bir
isteğin hedefi önce backend'de çözülüp denetlenir. Rakamlardan oluşan bir
host (`2852039166`, `0xa9.0xfe.0xa9.0xfe`, `127.1`) burada çözülmez ama
vekil onu bir IPv4 adresi olarak okuyabilir: böyle bir host, okunduğu adres
(`169.254.169.254`, `127.0.0.1`) olarak denetlenir, geçerli bir adres
değilse reddedilir. Host'un sonundaki noktalar (`169.254.169.254..`,
`localhost..`) bir şey değiştirmez: vekil hepsini atar, backend de host'u
onlarsız denetler. Düz `http://` bir istekte vekil, URL'deki host'a değil
istek satırına yazılan `Host` header'ına gider; monitor'un `Host`
header'ı da bu yüzden aynı biçimde denetlenir. Burada çözülemeyen bir ad
ise vekile bırakılır; vekil adı yeniden çözer ve nereye ulaşacağı onun
ağına bağlıdır.
Lokal geliştirmede `make backend` loopback'i istisna tutar
(`TARGET_ALLOW_CIDRS`), böylece makinedeki servisler kontrol edilebilir.

Kontrollerin sonuçları da hedefin yanıtını taşımaz: başarısız bir kontrol
nedenini anlatırken yanıt gövdesini saklamaz, başlıklardan yalnızca
birkaçını kısaltarak saklar (bkz. "Başarısız kontrolün ayrıntıları"); JSON
değeri arayan kontrol de bulduğu değerin en fazla ilk 100 karakterini
gösterir.

**Yöneticinin uç noktaları.** `HTTP_PROXY`/`HTTPS_PROXY` vekilleri, SMTP
sunucusu (`UPTIME_SMTP_HOST` ve `UPTIME_SMTP_PORT`),
`UPTIME_TELEGRAM_API_URL` ve `UPTIME_PAGERDUTY_EVENTS_URL` kullanıcıların
değil yöneticinin ayarlarıdır. Backend bunlara, yazıldıkları `host:port`
ile tam olarak eşleşen bağlantılarda politikaya bakmadan bağlanır;
karşılaştırma, ad çözülmeden önce istenen adres üzerinde yapılır. Böylece
pod'un loopback'inde dinleyen bir yan konteyner (vekil, SMTP aktarıcısı,
Telegram Bot API sunucusu) istisna gerektirmez ve loopback'in geri kalanı
kapalı kalır. SMTP, Telegram ve PagerDuty adreslerine yalnızca kendi
gönderimleri güvenir: aynı adrese yöneltilmiş bir kanal ya da monitor yine
reddedilir. Vekilin adresine ise her kontrol ve kanal güvenir, çünkü
istekler ona gider; bir monitor vekilin kendisine de bağlanabilir.

`UPTIME_TARGET_ALLOW_CIDRS`'e eklenen bir loopback aralığı (örneğin
`127.0.0.1/32`) ise loopback'in **her portunu her monitor'a ve kanala**
açar: pod'daki bir servis ağının (service mesh) yönetim portu da buna
dahildir. Yöneticinin uç noktaları için buna gerek yoktur; yalnızca bir
monitor'un gerçekten loopback'teki bir şeyi kontrol etmesi gerekiyorsa
verin.

Backend'in HTTP sunucusu bir isteğin tamamını okumaya `UPTIME_HTTP_READ_TIMEOUT`
(30 sn), yanıtını yazmaya `UPTIME_HTTP_WRITE_TIMEOUT` (2 dk) süre tanır, boşta
bekleyen bağlantıyı `UPTIME_HTTP_IDLE_TIMEOUT` (2 dk) sonra kapatır ve en fazla
`UPTIME_HTTP_MAX_HEADER_BYTES` (1 MiB) başlık kabul eder. Gövdesini yavaş
gönderen ya da bağlantısını boşta tutan bir istemci bağlantıları süresiz
meşgul edemez.

### Hedef yükü

Hedef politikası kontrollerin *nereye* gidebileceğini sınırlar; bu bölüm
*ne kadar* gidebileceğini. Monitor oluşturabilen her kullanıcı backend'e
başka bir sisteme istek attırır. Bir editör, ya da ele geçirilmiş bir editör
hesabı, tek bir dış siteye en kısa aralıkla binlerce monitor tanımlarsa
Kubeat o siteye yük bindiren bir araca döner; yanlışlıkla da olabilir, bir
betiğin döngüsü yüzlerce monitor açabilir. Aşağıdaki sınırlar bunun önüne
geçer. Hepsi ortam değişkenidir ve `0` ilgili sınırı kapatır.

**Cluster içi ve dış hedef.** Her sınır hedefin nerede olduğuna bakar.
Hedefin host'u özel bir adresteyse (`10.0.0.0/8`, `172.16.0.0/12`,
`192.168.0.0/16`, `100.64.0.0/10`, `fc00::/7`; loopback ve link-local da
sayılır, onları zaten politika reddeder), `.svc`, `.svc.cluster.local` ya da
`.cluster.local` ile bitiyorsa ya da noktasız bir adsa (aynı namespace'teki
bir Service'in kısa adı) hedef **cluster içidir**; geri kalan her şey, şirket
ağındaki bir sunucu da dahil, **dıştır**. Karar yazılan ada göre verilir, ad
çözülmez: özel bir adrese çözülen genel bir alan adı (`intranet.corp`)
bilerek dış sayılır, çünkü çözüm iki sorgu arasında değişebilir ve bir
bölgeyi yöneten kişi adını ucuz sınıfa sokabilirdi. `push` ve `workload`
monitor'ları dışarıya istek atmaz; hiçbir sınıra tabi değildir.

**Dış hedefler için en kısa aralık.** `UPTIME_MIN_CHECK_INTERVAL_EXTERNAL`
(30 sn) dış hedefli bir monitor'un kontrol aralığının ve tekrar aralığının
alt sınırıdır; `UPTIME_MIN_CHECK_INTERVAL`'dan (5 sn) kısa olamaz. Daha kısa
bir aralık oluşturmada, kopyalamada ve hedefi, aralığı ya da tekrar aralığını
değiştiren güncellemede 400 ile reddedilir (`interval_seconds must be at
least 30 for a target outside the cluster
(UPTIME_MIN_CHECK_INTERVAL_EXTERNAL)`); kuraldan önce oluşturulmuş daha hızlı
bir monitor bu üçüne dokunulmadıkça düzenlenebilir, ama olduğu gibi
kopyalanamaz. Monitor formu hedef dış göründüğünde bunu önceden söyler.
Keşfin varsayılan aralığı (`UPTIME_DISCOVERY_DEFAULT_INTERVAL`) cluster içi
hedefler içindir ve değişmez; dış bir Route ya da Ingress hedefi içe
aktarmada bu kurala takılırsa atlananlar listesinde nedeniyle görünür, ve
annotation eşitlemesi de aynı kuralı yeni monitor'lara uygular
(`kubeat.io/interval: must be at least 30s for a target outside the cluster
…` nedeniyle atlanır). Kuraldan önce oluşturulmuş, annotation ile yönetilen
bir monitor ise API'deki gibi olduğu gibi kalır: silinmez, aralığı
değiştirilmez, atlananlar listesinde "the monitor made before the rule is
kept at 10s" notuyla görünür. Varsayılan aralık dış sınırın altındaysa
sunucu açılışta uyarır.

**Host başına kontrol tavanı.** Bir host'a tüm monitor'ların birlikte
yapabileceği kontrol sayısı dakikada `UPTIME_TARGET_MAX_RATE` (60, dış
host) ve `UPTIME_CLUSTER_TARGET_MAX_RATE` (600, cluster içi host) ile
sınırlıdır. Tavan iki anahtarla tutulur: hedefin yazıldığı **ad** (küçük
harfe çevrilmiş ad ya da tek biçime getirilmiş IP; port sayılmaz, bir IP'nin
her yazımı — `2001:db8::1`, `2001:0db8::1`, `::ffff:203.0.113.1` — tek
anahtardır) ve kontrolün **ulaştığı IP adresi**. Ad uydurmak bedavadır,
adres değildir: aynı adrese çözülen `a1.example`, `a2.example`, … adlarının
hepsi o adresin tek kovasını paylaşır. Adres kovasının tavanı kendi ayarıdır,
`UPTIME_TARGET_ADDRESS_MAX_RATE` (600), adresin sınıfından bağımsız (özel
de olsa genel de olsa aynı); `0` adres kovasını kapatır. Her motor ulaştığı
adresi bildirir: HTTP, WebSocket ve TCP bağlandığı adresi, gRPC, PostgreSQL,
MySQL, Redis ve DNS (kendi çözücüsü verilmişse) kurduğu her bağlantının
adresini, ping ise çözdüğü adresi. Scheduler bir monitor'un son kontrolünün
uğradığı her host adını ve adresi hatırlar ve bir sonraki kontrolü
**göndermeden önce** hepsinin kovasından birer token alır; biri boşsa
kontrol bekler ve alınanların tümü geri verilir. Böylece kontrolleri başka
bir host'a yönlendirilen (redirect) bir monitor o host'un kovasına, o
host'un kendi monitor'larıyla eşit koşullarda bağlıdır: kurbanın kovası
boşken bekler, kurbanın kendi monitor'u aç kalmaz, kurbana düşen istek
sayısı yönlendirenin değil kurbanın tavanıyla sınırlıdır. Kalan boşluk
yalnızca şudur: bir adın uğradığı host'lar ve adresler ilk kontrolünde
öğrenilir, dolayısıyla yeni bir adın **ilk** kontrolü (ve backend yeniden
başladıktan sonraki ilk kontrol, çünkü bu bilgi bellektedir) yalnızca kendi
ad kovasından düşer; sonrakiler uğradığı her anahtara bağlıdır.
Yönlendirmeyle gelen istekler kurbanın kovasını kurbanın kendi
monitor'larıyla eşit paylaştığı için, kovası zaten dolu bir host'a
yönlendiren üçüncü bir taraf o host'un kendi kontrol sıklığını yarıya
kadar düşürebilir; yük sınırı yine de tutar. DNS monitor'larında ad kovası
sorulan ada aittir, çözücünün yükü yalnızca adres kovasıyla sınırlıdır. Scheduler her
anahtar için bir token kovası tutar: tavanı aşan bir kontrol başarısız
sayılmaz, sonuç yazılmaz, yalnızca sonraki tick'e ertelenir ve token gelince
en uzun bekleyen önce çalışır. Bu sırada monitor'un API durumunda
`throttled_since` dolu gelir, arayüzde listede ve monitor sayfasında "Hedef
hız sınırında bekliyor" notu görünür, `kubeat_target_throttled_total{host}`
metriği (etiket monitor'un kendi host'udur) her başlayan bekleme için bir
kez artar (bekleme kaç tick sürerse sürsün; etiket sayısı sınırlıdır: ilk
100 host'tan sonrakiler `other` altında sayılır). Her pod'u ayrı kontrol
eden bir monitor pod başına bir istek harcar; izlenen yönlendirmelerin
(redirect) her adımı da bir istek sayılır ve gittiği host'un (başka bir host
ise onun) ve ulaştığı adresin kovasından düşer. Bir host'un kovası, son
monitor'u silinse ya da duraklatılsa bile bir dakika daha yaşar: monitor'u
silip yeniden oluşturmak dolu bir kova vermez.

**Paylaşılan ingress ve router adresleri.** Bir cluster'ın Ingress ve Route
host'larının hepsi genellikle tek bir yük dengeleyici ya da router IP'sine
çözülür; o adresin kovasını hepsi paylaşır. Adres tavanı 600/dakika
olduğundan tek adresin arkasına 30 saniye aralıkla **300**, 60 saniyeyle
**600**, 5 dakikayla **3000** monitor sığar; ad tavanı ise host başınadır
(dış ad için 60/dakika: tek bir `shop.example.com` host'una 30 saniyeyle 30
monitor). Daha büyük bir kurulumda ertelenen kontroller `throttled_since`
ve metrikle görünür; `UPTIME_TARGET_ADDRESS_MAX_RATE` değerini yükseltin ya
da `0` ile adres kovasını kapatın. Adres kovası uydurma adların hilesini
engellemek içindir: kapatılırsa aynı adrese çözülen her yeni ad kendi 60
kontrol/dakikasını alır; açıkken bir kurulumun tek bir dış adrese
yapabileceği istek en çok 600/dakikadır (saniyede 10), aynı tavan
cluster içindeki bir router adresi için de geçerlidir (tek router
arkasında 30 saniyeyle 300'den çok Route varsa yükseltin).

**Host başına monitor sınırı.** Dış bir host'u en fazla
`UPTIME_MAX_MONITORS_PER_HOST` (5) monitor kontrol edebilir (host anahtarı
tavandaki ad anahtarıyla aynıdır); oluşturmada, kopyalamada, hedef değişen
güncellemede, içe aktarmada ve annotation eşitlemesinde denetlenir, fazlası
400 ile host'u ve sınırı söyleyerek reddedilir (`at most 5 monitors may
check the host example.com (UPTIME_MAX_MONITORS_PER_HOST); it has 5
already`; eşitleme aynı cümleyle atlar). Cluster içi host'lar için sınır
yoktur.

**Kotalar.** `UPTIME_MAX_MONITORS` (1000) toplam, `UPTIME_MAX_MONITORS_PER_NAMESPACE`
(200) namespace ya da grup başına monitor sayısını sınırlar. Oluşturmada,
kopyalamada, içe aktarmada ve annotation eşitlemesinde denetlenir; fazlası
422 ile reddedilir, içe aktarma ve eşitleme atlanan adayları nedeniyle
listeler (eşitleme adayları sıralı aldığından her çalışmada aynı adaylar
atlanır). Bir monitor'u başka bir namespace'e taşımak o namespace'in
kotasına tabidir. Kotalar yalnızca yeni monitor'ları bağlar: sınırın üstünde
kalan mevcut monitor'lar çalışmaya ve düzenlenmeye devam eder.

**Hesap başına oluşturma hızı.** Bir hesap son altmış saniyede en fazla
`UPTIME_MONITOR_CREATE_PER_MINUTE` (30) monitor oluşturabilir; pencere
kayar, saatin dakika sınırına bağlı değildir. API anahtarı tek hesap
sayılır, içe aktarmadaki her monitor bir oluşturmadır; annotation
eşitlemesi bir hesap olmadığı için bu kurala tabi değildir. Fazlası
`Retry-After` başlığıyla 429 alır ve bir patlamanın ilk reddinde kayda
(`monitor_create_throttled`) sayıyı söyleyen tek bir girdi düşer; yönetici
bunu kayıt sayfasında görür. Reddedilen ya da geçersiz bir istek sayılmaz,
adı alınmış olduğu için 409 alan bir istek de sayılmaz.

**User-Agent.** Kontroller ve bildirimler kendilerini
`kubeat/<sürüm> (+https://github.com/yusufceng/Kubeat; <UPTIME_PUBLIC_URL>)`
olarak tanıtır (adres yalnızca `UPTIME_PUBLIC_URL` ayarlıysa); fazla istek
gören bir sitenin işletmecisi hem yazılımı hem kurulumu bulabilir.
`UPTIME_USER_AGENT` bunun yerine geçer.

Açılışta `target guardrails in effect` log satırı yürürlükteki değerleri
yazar.

### Gizli değerlerin şifrelenmesi

Monitor'ların header değerleri, istek gövdeleri, parola ve token'ları ile
bildirim kanallarının adresleri ve token'ları veritabanında durur. Bunlar
`UPTIME_SECRET_KEY` anahtarıyla AES-256-GCM ile şifrelenerek yazılır;
veritabanı dosyası ya da yedeği başkasının eline geçse bile bu değerler
okunamaz.

- **Anahtar zorunludur**: ayarlı değilse backend başlamaz ve nedenini söyler.
  Anahtar en az 32 karakterlik rastgele bir metin olmalıdır
  (`openssl rand -base64 36`). Ortam değişkeni yerine bir dosyadan da
  okunabilir: `UPTIME_SECRET_KEY_FILE` (bağlanmış bir Secret ya da Docker
  secret). İkisi birden verilemez. Bu yol chart dışı kurulumlar içindir:
  Helm chart anahtarı Secret'tan `UPTIME_SECRET_KEY` olarak verir,
  `backend.extraEnv` ile ayrıca `UPTIME_SECRET_KEY_FILE` verilirse backend
  başlamaz.
- Anahtarın her baytı sayılır: dosya da ortam değişkeni gibi olduğu gibi
  kullanılır, böylece aynı Secret değişken olarak da dosya olarak da aynı
  anahtarı verir. Sonunda satır sonu olan bir dosyanın (`openssl rand -base64
  36 > key`, `echo ... > key`) satır sonu da anahtara dahildir; aynı metnin
  satır sonlu ve satır sonsuz hâli iki ayrı anahtardır ve backend yanlış
  anahtarla başlamaz. Dosyayı satır sonu olmadan yazın
  (`openssl rand -base64 36 | tr -d '\n' > key`); Secret'ı bu dosyadan
  (`--from-file=key=key`) ya da `--from-literal=key="$(openssl rand -base64
  36)"` ile oluşturun.
- Şifrelemeden bilerek vazgeçmek için `UPTIME_ALLOW_PLAINTEXT_SECRETS=true`
  verilir: değerler şifresiz yazılır ve backend bunu her açılışta uyarı
  olarak loglar. Veritabanında şifreli değer varken anahtar verilmezse ya da
  yanlış anahtar verilirse backend bu ayarla da başlamaz.
- Anahtar sonradan ayarlanırsa, açılışta daha önce şifresiz yazılmış değerler
  şifrelenir ve veritabanı sıkıştırılır (`VACUUM`), ki şifresiz kopyaları
  dosyanın boş alanında ve WAL dosyasında kalmasın. Bu, veritabanı kadar boş
  disk ister ve büyük bir veritabanında o açılışı uzatır; chart backend'e
  açılması için `backend.startupProbe` kadar süre tanır (varsayılan 10
  dakika). Açılış sıkıştırma bitmeden kesilir ya da sıkıştırma başarısız olursa
  (örneğin disk dolarsa) bu veritabanına not edilir: sonraki açılış bunu
  uyarı olarak loglar ve sıkıştırmayı yeniden dener. Üst üste üç açılış
  sıkıştırmayı bitiremezse (sıkıştırma açılışa tanınan süreden uzun
  sürüyorsa her açılış kesilir) dördüncüsü onu atlar: ne yapılacağını hata
  olarak loglar ve sıkıştırmadan açılır. Sonraki açılış bir kez daha
  dener; o da bitiremezse ondan sonraki yine atlar, yani bir yeniden
  başlatma en fazla bir kesilen açılışa mal olur. O zaman ya açılışa daha çok süre tanıyın (chart'ta
  `backend.startupProbe.failureThreshold`) ve veritabanı kadar boş disk
  bırakın, ya da sıkıştırmayı backend dururken elle yapın: veritabanı
  volume'unu `sqlite3` bulunan bir pod'a bağlayıp
  `sqlite3 -bail uptime.db 'VACUUM; DELETE FROM compaction_pending;'`
  çalıştırın (ikinci komut sıkıştırmanın yapıldığını backend'e bildirir).
  Anahtardan önce alınmış yedekler şifresizdir; yenileri onları silene kadar
  (`UPTIME_BACKUP_KEEP`) öyle kalır, isterseniz elle silin.
- Silinen ya da üzerine yazılan değerler dosyada sıfırlarla ezilir
  (`secure_delete`). Veritabanı, WAL dosyası ve yedekler yalnızca backend'in
  kendi kullanıcısının okuyabileceği izinlerle (`0600`) oluşturulur; daha önce
  oluşturulmuş veritabanı dosyasının izni açılışta `0600`'a çekilir. Yedekleri
  dışarı taşıyan bir iş bu yüzden backend'le aynı kullanıcıyla çalışmalıdır.
- **Anahtar kaybedilirse şifreli değerler geri getirilemez**; monitor ve
  kanalların bu alanlarının yeniden girilmesi gerekir. Yedekle birlikte
  anahtarın da bir kopyasını saklayın.
- Push monitor'larının token'ı şifrelenmez: gelen her rapor ona göre
  aranır. Bu token yalnızca o monitor'a rapor yazmaya yarar.
- Anahtar değiştirme (rotation) yoktur.

Helm chart anahtarı ilk kurulumda üretir (`<release>-secret-key` Secret'ı),
güncellemelerde korur ve release kaldırılsa bile silmez. Cluster'a erişmeden
şablon üreten araçlar (Argo CD, OpenShift GitOps, `helm template`) onu ve
chart'ın ürettiği diğer Secret'ları koruyamaz; böyle kurarken hangi
Secret'ları kendiniz vereceğiniz "GitOps ile kurulum" bölümündedir. Okumak
için:

```
kubectl -n kubeat get secret kubeat-secret-key -o jsonpath='{.data.key}' | base64 -d
```

### Hesaplar ve giriş

Arayüze hesapla girilir. Hesaplar ve roller Kubeat'in içindedir; isteğe
bağlı olarak kurumsal dizin (LDAP) hesaplarıyla da giriş yapılır.

**İlk hesap.** Hiç hesap yokken iki yol vardır: Helm chart ilk kurulumda
rastgele bir parola üretip backend'e `UPTIME_ADMIN_PASSWORD` olarak verir ve
backend açılışta `admin` adlı yöneticiyi oluşturur; chart dışı kurulumlarda
arayüz ilk ziyarette `/setup` sayfasında ilk yöneticiyi oluşturtur.
Parolayı okumak için:

```
kubectl -n kubeat get secret kubeat-admin -o jsonpath='{.data.password}' | base64 -d
```

Hesap yokken `/setup`'a ilk ulaşan yönetici olur: chart dışı bir kurulumu ilk
hesap oluşturulmadan ağa açmayın. Hesap tek adımda oluşturulur; aynı anda
gönderilen iki kurulumdan yalnızca biri yönetici açar, diğeri "zaten bir
hesap var" yanıtını (409) alır.

`UPTIME_ADMIN_PASSWORD` yalnızca hesap yokken işe yarar. Yönetici parolası
unutulursa `UPTIME_ADMIN_PASSWORD_RESET=true` ile bir kez başlatılır:
`admin` hesabının parolası `UPTIME_ADMIN_PASSWORD` değerine çekilir ve
oturumları kapatılır; sonra ayar kaldırılır. Değer, kullanıldığında (hesap
yokken ya da sıfırlamada) parola kurallarına (aşağıda) uymazsa backend
başlamaz ve nedenini `UPTIME_ADMIN_PASSWORD: ...` diye loglar; zayıf bir
parolayla açılmak ya da `/setup`'ı ilk gelene açık bırakmak yerine durur.

**Roller.** Her hesabın bir rolü, yönetici dışındakilerin bir de kapsamı
(namespace ve grup listesi, ya da hepsi için `*`) vardır:

| Rol | Görür | Değiştirir |
| --- | --- | --- |
| Yönetici (`admin`) | Her şeyi | Her şeyi; kullanıcıları, LDAP eşlemelerini, kapsamsız kanalları |
| Editör (`editor`) | Kapsamındaki monitor'ları, olayları, bakım pencerelerini; yalnızca kapsamıyla sınırlı kanalları ve durum sayfalarını | Kapsamında monitor ekler, düzenler, siler, susturur, keşiften içe aktarır, olaylara not ekler; kapsamıyla sınırlı kanal, bakım penceresi ve durum sayfası açar |
| İzleyici (`viewer`) | Kapsamındaki monitor'ları (push monitor'larının rapor adresi ve HTTP monitor'larının istek gövdesi hariç) ve olayları; kapsamını ilgilendiren bakım pencerelerini | Hiçbir şeyi |

Elle eklenen bir monitor'un "grubu" kapsam açısından namespace gibidir: `shop`
editörü grubu `shop` olan monitor'lar açabilir, grupsuz monitor açamaz.
Grupsuz monitor'ları yalnızca kapsamı `*` olanlar ve yöneticiler görür.
Keşif sayfası herkese yalnızca kapsamındaki namespace'leri gösterir; annotation
eşitlemesinin sayıları ve atlanan nesneleri de kapsamı `*` olmayanlar için
yalnızca bu namespace'lerden gelir.

Editör bir durum sayfasını, sayfadaki her monitor'u değiştirebiliyorsa
yönetir. Monitor'u olmayan bir sayfa (böyle oluşturulmuş ya da monitor'ları
silinince boşalmış) hiçbir namespace'e ait değildir: onu yalnızca yöneticiler
görür ve değiştirir. Editör monitor'suz sayfa oluşturamaz, kendi sayfasını da
boşaltamaz.

Yetki backend'de denetlenir; arayüz yalnızca yapılamayanı gizler.

**Oturum.** Giriş yapınca backend rastgele bir oturum token'ı üretir
(veritabanında hash'i tutulur) ve arayüz onu HttpOnly, SameSite=Lax (HTTPS
üzerinden Secure ve `__Host-` önekli; bkz. "Tarayıcı güvenliği") bir
çerezde saklar. Oturum `UPTIME_SESSION_TTL`
(varsayılan 12 saat) sonra biter; parola değişince diğer oturumlar kapanır;
hesap devre dışı bırakılınca anında kapanır.

**Parola kuralları.** Parolalar bcrypt ile saklanır. Bir parola en az
`UPTIME_PASSWORD_MIN_LENGTH` (8) karakter ve en fazla 72 bayt olur (bcrypt
fazlasını okumaz), kullanıcı adıyla aynı olamaz ve ilk denenenlerden biri
olamaz: `admin`, `administrator`, `password`, `changeme`, `kubeat`,
`12345678` (büyük-küçük harf fark etmez). Kurallar her yerde aynıdır: ilk
hesapta, kullanıcı oluştururken, parola sıfırlarken ya da değiştirirken ve
`UPTIME_ADMIN_PASSWORD`'da.

**Deneme sınırı.** Bir kullanıcı adı için `UPTIME_LOGIN_LOCKOUT` (15 dk)
içinde `UPTIME_LOGIN_MAX_FAILURES` (10) hatalı denemeden sonra giriş o ad
için kapanır; süre dolana kadar doğru parola da reddedilir (429). Deneme,
parola denetlenmeden önce sayılır: aynı anda gönderilen istekler de sınırı
aşamaz. Dizine ulaşılamaması ya da devre dışı bir hesabın doğru parolası
deneme sayılmaz; dizindeki parolası doğru olup burada rolü olmayan birinin
girişi ise sayılır. Parola değiştirirken sorulan mevcut parola da aynı sınırla,
ayrı bir sayaçla korunur. Sayaç backend belleğindedir, yeniden başlatınca
sıfırlanır ve kilitli olmayan en fazla `UPTIME_LOGIN_THROTTLE_MAX_KEYS`
(100000) ad tutar; son denemesi kilit süresinden eski adlar her kilit
süresinde bir silinir. Sınır dolunca (uydurma adlarla bir saldırı) yeni bir
ad reddedilmez, kendine yer açar: önce son denemesi kilit süresinden eski
adlar unutulur, sonra kilit süresi içinde en az denenmiş adlardan en uzun
süredir denenmeyeni. Kilitli bir ad unutulmaz ve sınıra sayılmaz: sayaç,
kilitli adlar kadar sınırın üstüne çıkabilir. Uydurma
adlar böylece birbirini iter: kimse kilitlenmez, bir kilit süresi dolmadan
kalkmaz ve k hatası olan bir adı unutturmak için önce sınır kadar başka ada
k'şar deneme yapmak gerekir; sınırı düşürmek bunu ucuzlatır. Backend yer
açmak için unutulan adları, sınırın üstünde tutulan adları ve kilitli
adlara gelen denemeleri dakikada en fazla bir kez uyarı olarak loglar.

**Parola denetimleri.** Bir bcrypt karşılaştırması bir çekirdeği çeyrek
saniye kadar meşgul eder. Aynı anda en fazla `UPTIME_LOGIN_CONCURRENCY`
(varsayılan: CPU sayısının yarısı, en az 1; LDAP açıkken en az 2)
karşılaştırma çalışır; sırasını `UPTIME_LOGIN_QUEUE_WAIT` (3 sn) içinde
bulamayan giriş 503 ile geri çevrilir. Böylece bir giriş seli kontrollerin
CPU'sunu tüketemez. Dizine
bağlanmak CPU harcamaz, dizini bekler; dizin girişlerinin bu yüzden ayrı bir
sırası vardır: aynı anda en fazla 16 tanesi çalışır, sıra yine
`UPTIME_LOGIN_QUEUE_WAIT` kadar beklenir. Yavaş ya da yanıt vermeyen bir
dizin yalnızca dizin girişlerini bekletir; yerel hesaplar, dizin çökünce
başvurulacak `admin` dahil, girmeye devam eder. İstemcisi vazgeçen bir
dizin girişi dizin bağlantısını hemen kapatır ve sırasını bırakır.

Hesabı olmayan bir adın reddi de bir parola denetimi kadar sürer. LDAP
açıkken yerel hesabı olmayan her ad, dizine sorulurken aynı maliyette bir
karşılaştırmadan da geçer ve yanıt ikisi de bitince döner. Dizin bir bcrypt
karşılaştırmasından yavaş yanıt vermedikçe yanıtın süresi hangi adların
yerel hesabı olduğunu söylemez; bunun bedeli, dizin kullanıcılarının
girişinin de çeyrek saniye kadar sürmesidir. Her dizin girişi, başarılı
olanlar da, böylece bir bcrypt karşılaştırması harcar ve yerel hesaplarla
aynı karşılaştırma sırasını bekler; LDAP açıkken `UPTIME_LOGIN_CONCURRENCY`
`0` ise bu yüzden aynı anda en az 2 karşılaştırma çalışır. Çok sayıda dizin
kullanıcısı aynı anda giriyorsa (mesai başı gibi) `UPTIME_LOGIN_CONCURRENCY`
değerini ya da backend'in CPU sınırını yükseltin. Frontend backend'i
`BACKEND_TIMEOUT_MS` (3 sn) kadar beklediğinden, sırada bu kadar bekleyen
giriş arayüzde "backend'e ulaşılamadı" olarak da görünebilir.

**Kayıt.** Kim ne zaman neyi oluşturdu, değiştirdi, sildi; girişler ve
başarısız denemeler **Kayıt** sayfasında (yalnızca yöneticiler) listelenir
ve `UPTIME_AUDIT_RETENTION` (90 gün) boyunca tutulur. Bir adın kilit süresi
içindeki başarısız girişlerinden (`login_failed`) ve rolü olmayan bir dizin
kullanıcısı olarak reddinden (`login_denied`) kayda her birinin ilki ve adı
kilitleyen ret yazılır, aradakiler yazılmaz: parolasını önce yanlış yazıp
sonra rolü olmadığı için reddedilen bir dizin kullanıcısı iki girdi
bırakır. Kubeat'te hesabı olmayan adların (uydurma adlar ve hiç girmemiş
dizin kullanıcıları) başarısız girişleri ve rolü olmayan dizin
kullanıcılarının reddi dakikada en fazla `UPTIME_LOGIN_AUDIT_PER_MINUTE`
(60) girdi yazar; fazlası dakikanın sonunda sayısıyla uyarı olarak loglanır.
Hesabı olan bir adın (yerel hesap ya da daha önce girmiş bir dizin veya SSO
kullanıcısı) yanlış parolası bu sınıra takılmaz: böyle adların sayısı
bellidir ve her biri bir kilit süresinde en fazla
`UPTIME_LOGIN_MAX_FAILURES` girdi yazar. LDAP
kapalıyken bir SSO ya da dizin hesabına parolayla girilmeye çalışılırsa
kayıtta "hesabın burada parolası yok" yazar. Her başarısız deneme ayrıca
bilgi olarak loglanır. Böylece uydurma adlarla bir deneme seli diski
dolduramaz ve Kubeat'in tanıdığı hesaplara yönelik denemeleri kayıttan
gizleyemez.

**API anahtarı.** `UPTIME_API_TOKEN` betikler ve otomasyon içindir: onu
`Authorization: Bearer ...` ile sunan istek yönetici yetkisiyle çalışır ve
kayıtta `api-token` olarak görünür. Arayüz artık bu anahtarı kullanmaz;
her isteği kullanıcının oturumuyla yapar.

**Kilitli hesaba girmek.** Kilit kullanıcı adına göredir: adını bilen biri
bir hesabı hatalı denemelerle, örneğin her zaman var olan `admin`'i,
kilitleyebilir. Kilit sürerken açık oturumlar çalışmaya devam eder. Başka
yollar önceden hazır tutulmalıdır:

- API anahtarı (`UPTIME_API_TOKEN`; chart onu `<release>-api-token`
  Secret'ında üretir) deneme sınırına takılmaz ve yönetici yetkisiyle
  çalışır: bir parola sıfırlanır (`PATCH /api/v1/users/{id}`) ya da geçici bir
  yönetici hesabı açılır.
- SSO (OIDC) tanımlıysa, SSO hesabı yönetici rolüne eşlenmiş biri sağlayıcı
  üzerinden girer; bu giriş parola sayacına bakmaz.
- Sayaç bellekte olduğundan backend'i yeniden başlatmak da kilitleri
  kaldırır.

#### LDAP

`UPTIME_LDAP_URL` ayarlandığında, yerel hesabı olmayan bir kullanıcı adı
dizinde aranır ve parolası dizine sorulur. Yerel hesabı olan bir ad her
zaman yerel parolayla denetlenir; SSO ile oluşmuş bir hesabın adı da dizine
sorulmaz (dizinde aynı adı taşıyan başka biri olabilir), o hesap yalnızca
SSO ile girer.

```
UPTIME_LDAP_URL=ldaps://ldap.example.com:636
UPTIME_LDAP_BASE_DN=dc=example,dc=com
UPTIME_LDAP_USER_FILTER=(uid={username})        # Active Directory: (sAMAccountName={username})
UPTIME_LDAP_BIND_DN=cn=svc-kubeat,ou=svcaccts,dc=example,dc=com
UPTIME_LDAP_BIND_PASSWORD=...
```

- Akış: arama hesabıyla bağlanılır, kullanıcı `UPTIME_LDAP_USER_FILTER` ile
  bulunur, kullanıcının DN'i ile girilen parola denenir. Ad, e-posta ve
  gruplar `UPTIME_LDAP_DISPLAY_NAME_ATTR` (`cn`), `UPTIME_LDAP_EMAIL_ATTR`
  (`mail`) ve `UPTIME_LDAP_GROUP_ATTR` (`memberOf`) özniteliklerinden okunur.
  `memberOf` vermeyen dizinler için `UPTIME_LDAP_GROUP_FILTER`
  (`(member={dn})`) ve `UPTIME_LDAP_GROUP_BASE_DN` ile gruplar aranır.
- TLS: `ldaps://` ya da `UPTIME_LDAP_START_TLS=true`; özel CA için
  `UPTIME_LDAP_CA_FILE` (chart'ta `backend.extraVolumes` ile bağlanır).
- **Roller gruplardan gelir.** **Kullanıcılar** sayfasındaki LDAP bölümünde
  grup DN'i → rol (+ kapsam) eşlemeleri yapılır. Birden çok grup eşleşirse en
  güçlü rol kazanır, aynı roldeki kapsamlar birleşir. Eşleşen grubu olmayan
  kullanıcı `UPTIME_LDAP_DEFAULT_ROLE` ayarlı değilse giremez; ayarlıysa o
  rolü `UPTIME_LDAP_DEFAULT_NAMESPACES` kapsamıyla alır.
- Dizin kullanıcısı ilk girişte burada bir hesap edinir (kaynak `ldap`);
  rolü her girişte dizindeki gruplara göre yenilenir. Yönetici bu hesabı
  devre dışı bırakabilir ama rolünü ve parolasını değiştiremez.
- Dizine ulaşılamazsa giriş "yanlış parola" değil, "dizine ulaşılamadı"
  hatası verir.

Gerçek bir dizine karşı test: `KUBEAT_TEST_LDAP=ldap://127.0.0.1:3893 go test
./internal/ldapauth/` (glauth ile; kullanıcılar `deploy/demo` altında değil,
testin açıklamasında).

#### SSO (OpenID Connect)

Keycloak, Azure AD, Okta, Dex, Authentik gibi bir OpenID Connect sağlayıcısı
tanımlanınca giriş sayfasında "<ad> ile giriş yap" düğmesi çıkar. Kubeat
OIDC istemcisidir: tarayıcı sağlayıcıya gönderilir, geri geldiğinde kod
backend tarafından PKCE ile takas edilir, ID token doğrulanır (imza, hedef,
nonce), kullanıcı bilgisi ID token ve userinfo'dan okunur.

```
UPTIME_OIDC_ISSUER=https://sso.example.com/realms/corp
UPTIME_OIDC_CLIENT_ID=kubeat
UPTIME_OIDC_CLIENT_SECRET=...                 # public client ise boş; PKCE her zaman kullanılır
UPTIME_OIDC_REDIRECT_URL=https://kubeat.apps.example/auth/callback
```

- Sağlayıcıda istemci bu redirect URL ile kaydedilir. Boş bırakılırsa
  `UPTIME_PUBLIC_URL` adresinin `/auth/callback` yolu kullanılır. Chart'ta
  `oidc.redirectURL` verilmemişse adres türetilir: `publicURL` verildiyse
  `<publicURL>/auth/callback`, verilmediyse
  `https://<route.host ya da ingress.host>/auth/callback`. Sonuncusu
  `ingress.tls.enabled=false` iken de `https://`'tir: TLS'in Ingress
  controller'ın önünde bittiği kurulumlar içindir; TLS hiçbir yerde
  bitmiyorsa SSO çalışmaz. Chart backend'in reddedeceği bir dönüş adresiyle
  (bkz. aşağıda açılıştaki denetim) kurulmaz; adresin hangi değerden
  geldiğini ve ne yapılacağını söyleyerek durur. Yolu olan bir `publicURL`
  (`https://portal.example.com/kubeat` gibi) SSO ile ancak `oidc.redirectURL`
  verilerek kullanılabilir. Kaydedilecek adresi kurulum notları yazar.
  Route'a host vermeyip router'a bıraktıysanız, router'ın verdiği host'u
  öğrendikten sonra `publicURL` değerini `https://<host>` yapın; chart o
  zamana kadar kurulum notlarında uyarır.
- İkisi de yoksa SSO yalnızca `localhost` üzerinde çalışan bir arayüzle
  (`next dev`) çalışır ve backend açılışta bunu uyarı olarak loglar.
  Arayüzün kendi adresi diye bildirdiği değere başka hiçbir yerde
  güvenilmez: joker (wildcard) redirect kabul eden bir sağlayıcı
  kullanıcının kodunu, kimlik doğrulamasız bir isteğin istediği herhangi
  bir adrese gönderirdi.
- Adres açılışta denetlenir: tam bir URL olmalı, yolu yalnızca
  `/auth/callback` olmalı (kullanıcı bilgisi, sorgu ya da `#` parçası
  olmadan) ve HTTPS kullanmalıdır (düz HTTP yalnızca `localhost` için;
  özel IP'ler ve cluster içi adlar serbesttir). Uymazsa SSO kapalı kalır:
  giriş sayfasında düğmesi çıkmaz, backend nedenini açılışta hata olarak
  loglar, **Kullanıcılar** sayfası da yöneticilere gösterir. İzleme ve
  diğer giriş yolları etkilenmez; bir giriş ayarı kontrolleri durdurmaz.
  `UPTIME_OIDC_CLIENT_ID` verilmemişse ya da `UPTIME_OIDC_CA_FILE`
  okunamıyorsa da böyledir. Ayar düzeltilip backend yeniden başlatılınca
  SSO açılır.
- Giriş, onu başlatan tarayıcıya bağlıdır: arayüz sağlayıcıya giderken
  state değerini o tarayıcıda kısa ömürlü (10 dakika) bir çerezde tutar ve
  dönüşte yalnızca aynı state ile devam eder. Böylece kimse kendi
  başlattığı bir girişin dönüş bağlantısını başkasına açtırıp onu kendi
  hesabına sokamaz; başka bir tarayıcıda başlatılmış bir girişin bağlantısı
  açılırsa giriş sayfası girişin buradan yeniden başlatılmasını ister.
  HTTPS üzerinden bu çerez de `__Host-` öneklidir, yani aynı apps alan
  adındaki başka bir host onu yerleştiremez (bkz. "Tarayıcı güvenliği").
- Sağlayıcıya gidip henüz dönmemiş girişler backend belleğinde 10 dakika
  tutulur, en fazla `UPTIME_LOGIN_THROTTLE_MAX_KEYS` (100000) kadar. Giriş
  başlatmak kimlik istemediği için bir başlatma seli belleği sınırsız
  büyütemez: sınıra varılınca en eski girişler unutulur, sahipleri girişi
  yeniden başlatır ve backend bunu uyarı olarak loglar.
- Hesap `preferred_username` (`UPTIME_OIDC_USERNAME_CLAIM`), `name` ve
  `email` claim'lerinden kurulur; gruplar `groups` claim'inden
  (`UPTIME_OIDC_GROUPS_CLAIM`). Okta için kapsamlara `groups` eklenir
  (`UPTIME_OIDC_SCOPES="openid profile email groups"`), Keycloak için bir
  group membership mapper, Azure AD için token'da grup claim'i açılır.
- **Roller gruplardan gelir**, LDAP ile aynı eşleme tablosundan:
  **Kullanıcılar** sayfasındaki grup eşlemesine LDAP DN'i yerine grup adı
  yazılır (Keycloak'ta `/platform-admins` gibi yol; baştaki `/` ve harf
  büyüklüğü önemsizdir). Eşleşen grubu olmayan kullanıcı
  `UPTIME_OIDC_DEFAULT_ROLE` yoksa giremez.
- Sağlayıcı kullanıcısı ilk girişte burada bir hesap edinir (kaynak
  `oidc`); rolü her girişte yenilenir. Aynı adda yerel ya da LDAP hesabı
  varsa sağlayıcı girişi reddedilir: bir hesap tek yoldan girer.
- Özel CA için `UPTIME_OIDC_CA_FILE`; sağlayıcı ulaşılamazsa giriş sayfası
  bunu söyler, oturumlar etkilenmez. Sağlayıcıya açılış sırasında değil,
  ilk girişte bağlanılır.
- OpenShift'in kendi OAuth sunucusu OIDC değildir; "OpenShift ile giriş"
  için cluster'ın bağlı olduğu kurumsal sağlayıcı (çoğunlukla Keycloak ya da
  Azure AD) doğrudan kullanılır.

**Yükseltme notu:** 0.37.0'a kadar chart dönüş adresini Route ya da Ingress
host'undan türetiyordu; artık `publicURL` verildiyse ondan türetir.
`publicURL` Route ya da Ingress host'undan farklıysa `redirect_uri`
`<publicURL>/auth/callback` olur: yükseltmeden önce bu adresi sağlayıcıya
kaydedin ya da eski adresi `oidc.redirectURL` ile sabitleyin, yoksa sağlayıcı
girişi `invalid redirect_uri` diye reddeder.

### Tarayıcı güvenliği

Arayüz her yanıtla tarayıcıya sayfanın neler yapabileceğini söyleyen
başlıklar gönderir:

- **Content-Security-Policy.** Sayfalar yalnızca Kubeat'in kendi
  script, stil, font ve görsellerini yükler; başka bir adrese bağlanamaz,
  başka bir adrese form gönderemez, eklenti ve `eval` çalıştıramaz, başka
  bir sitenin iframe'inde gösterilemez (clickjacking). Rozetler, push
  raporları ve CSV indirmesi hiçbir şey çalıştırmayan ayrı bir politikayla
  gelir. Satır içi (inline) script'lere izin verilir: Cache Components ile
  sayfaların kabuğu istekten önce üretildiği için istek başına bir nonce
  taşıyamaz.
- **Durum sayfalarını iframe'de göstermek** (wiki, Grafana, duvar ekranı)
  için izin verilen siteler `STATUS_PAGE_FRAME_ANCESTORS` ile verilir
  (chart'ta `frontend.statusPageFrameAncestors`, örn.
  `https://wiki.example.com`); varsayılan `'none'` hiçbirine izin vermez.
  Arayüzün geri kalanı hiçbir zaman iframe'de gösterilmez; rozetler
  `<img>` ile her yere konabilir.
- **Strict-Transport-Security.** İstek HTTPS ile geldiyse ya da
  `PUBLIC_URL` `https://` ise gönderilir (tarayıcı onu yalnızca HTTPS
  üzerinden aldığında dikkate alır): tarayıcı bu host'a `HSTS_MAX_AGE`
  (varsayılan 1 yıl) boyunca yalnızca HTTPS ile bağlanır; `0` kapatır.
  `includeSubDomains` ve `preload` kullanılmaz: host çoğunlukla paylaşılan
  bir apps alan adının altındadır.
- **Oturum çerezi** `COOKIE_SECURE=auto` (varsayılan) ile, `PUBLIC_URL`
  `https://` ise ya da tarayıcının bağlandığı ilk proxy isteğin HTTPS ile
  geldiğini söylüyorsa Secure işaretlenir; tarayıcı onu düz HTTP üzerinden
  hiç göndermez. Proxy'ler `X-Forwarded-Proto`'ya kendi değerlerini
  ekledikçe yalnızca ilk değer, tarayıcıya bakan proxy'ninki sayılır:
  `https, http` (TLS router'ın önündeki yük dengeleyicide biter) HTTPS'tir,
  `http, https` (düz HTTP sunan bir proxy router'a HTTPS ile bağlanır)
  değildir. `always` ve `never` de verilebilir.
  `localhost` dışından düz HTTP ile erişilen bir kurulumda çerez Secure
  olmamalıdır: TLS'siz bir Ingress'te (`ingress.tls.enabled=false`) chart
  adresi `http://` türettiği için `auto` bunu kendisi sağlar. `PUBLIC_URL`
  yine de `https://` iken kullanıcılar düz HTTP ile geliyorsa ya da önde
  düz HTTP sunan proxy kendi `X-Forwarded-Proto`'sunu eklemeden router'a
  HTTPS ile bağlanıyorsa (nginx `proxy_pass` varsayılan olarak eklemez; ilk
  değer router'ın eklediği `https` olur) `never` gerekir (chart'ta
  `frontend.cookieSecure=never`); yoksa tarayıcı çerezi kabul etmez ve giriş
  yapılamaz.
- **`upgrade-insecure-requests`** (Content-Security-Policy) yalnızca
  `X-Forwarded-Proto`'nun ilk değeri `https` iken gönderilir;
  `COOKIE_SECURE=never` ya da `http://` bir `PUBLIC_URL` kullanıcıların düz
  HTTP ile geldiğini söylediği için o zaman hiç gönderilmez. Düz HTTP'deki
  bir tarayıcı bu yönergeyle sayfanın script ve stillerini HTTPS'ten
  isterdi ve sayfa onlarsız açılırdı.
- **`__Host-` önekli çerezler.** Çerezler Secure iken adları önek alır:
  `__Host-kubeat_session` ve SSO girişi sürerken `__Host-kubeat_oidc_state`
  (`Path=/`, `Domain` yok). Tarayıcı böyle bir çerezi yalnızca bu host'un
  kendisinden ve HTTPS üzerinden kabul eder. Host çoğunlukla paylaşılan bir
  apps alan adının altında olduğu için önek olmadan başka bir ekibin
  Route'u (ya da oradaki bir script açığı) tüm alan adı için kendi oturum
  ya da state çerezini yerleştirip ziyaretçiyi farkında olmadan kendi
  hesabında çalıştırabilirdi. Secure iken yalnızca önekli ad okunur;
  çıkışta ve süresi dolmuş bir oturumda eski adlar da silinir. Yükseltmeden
  önceki bir `kubeat_session` tarayıcıda kalsaydı, kurulum onun oturumu
  sürerken düz adlara dönünce (`COOKIE_SECURE=never`) yeniden okunur ve
  çıkış yapmış kullanıcıyı o oturuma geri sokardı.
  Düz HTTP'de önek kullanılamaz; çerezler eski adlarıyla
  (`kubeat_session`, `kubeat_oidc_state`) kalır.

  **Yükseltme notu:** HTTPS ile sunulan kurulumlarda oturum çerezinin adı
  değiştiği için yükseltmeden sonra herkesin bir kez yeniden giriş yapması
  gerekir.
- Ayrıca `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`,
  kamera, mikrofon, konum, ödeme ve USB'yi kapatan bir `Permissions-Policy`
  ve `Cross-Origin-Opener-Policy: same-origin` gönderilir; `X-Powered-By`
  gönderilmez.

### Metrik anahtarı

`/metrics` her monitor'un adını, namespace'ini, durumunu ve sertifika
bitişini verir; keşiften gelen monitor'ların adlarında Service adları,
portlar ve Ingress host'ları da vardır. Anahtar yokken backend portuna
ulaşabilen herkes bunları, rolünün namespace sınırına bakılmaksızın okur.
`UPTIME_METRICS_TOKEN` ayarlanınca `/metrics` yalnızca bu anahtarı
`Authorization: Bearer ...` ile sunan isteklere yanıt verir; API anahtarı da
kabul edilir, kullanıcı oturumu edilmez. Diğer istekler `401` alır. Anahtar
ayarlı değilse `/metrics` açıktır ve backend bunu açılışta loglar.

Chart'ta `metrics.auth.enabled` varsayılan olarak açıktır: chart ayrı bir
anahtar üretir (`<release>-metrics-token` Secret'ı, `token` anahtarı),
güncellemelerde korur ve backend'e verir. `metrics.serviceMonitor.enabled`
ile gelen ServiceMonitor anahtarı aynı Secret'tan okuyup gönderir
(`authorization`); OpenShift user workload monitoring'de de başka bir şey
gerekmez. Anahtarı kendiniz yönetmek için `metrics.auth.existingSecret`
verilir; GitOps ile kurarken bu gereklidir (bkz. "GitOps ile kurulum"). Bu
anahtar bilerek API anahtarından ayrıdır: scrape eden Prometheus
onun bir kopyasını kendi yapılandırmasında tutar (OpenShift'te
`openshift-user-workload-monitoring` namespace'indeki bir Secret'ta) ve
orada metriklerden başka bir şey açmamalıdır.

**Yükseltme notu:** Prometheus'u elle yapılandırılmış (ServiceMonitor
kullanmayan) kurulumlarda scrape'ler bu sürümle `401` almaya başlar. Scrape
yapılandırmasına anahtarı ekleyin ya da `metrics.auth.enabled=false` verin:

```yaml
scrape_configs:
  - job_name: kubeat
    authorization:
      credentials_file: /etc/prometheus/kubeat/token   # <release>-metrics-token Secret'ından bağlanır
    static_configs:
      - targets: ["kubeat-backend.kubeat.svc:8080"]
```

Anahtarı okumak için:

```
kubectl -n kubeat get secret kubeat-metrics-token -o jsonpath='{.data.token}' | base64 -d
```

### Dışarıya açma: Route ve Ingress

Girişte parola (LDAP'ta dizin parolası), sonra her istekte oturum çerezi
gider; arayüz HTTPS ile sunulmalıdır.

- **Route** (`route.enabled=true`) edge TLS ile sunar ve düz HTTP'yi
  HTTPS'e yönlendirir. `route.annotations` router ayarlarını taşır:
  `haproxy.router.openshift.io/hsts_header: max-age=31536000` tarayıcının
  bundan sonra yalnızca HTTPS kullanmasını sağlar (paylaşılan apps alan
  adında `preload` eklemeyin); cluster yöneticisi `requiredHSTSPolicies` ile
  HSTS'i zorunlu tuttuysa Route bu annotation olmadan reddedilir.
  `haproxy.router.openshift.io/rate-limit-connections: "true"` ile
  `haproxy.router.openshift.io/rate-limit-connections.rate-http: "500"`
  istemci adresi başına 10 saniyede en fazla 500 isteğe izin verir. Sınır
  Route'taki her şeyi sayar (arayüz, durum sayfaları, rozetler, push
  raporları) ve aynı NAT arkasındaki istemciler onu paylaşır; düşük
  tutmayın.
- **Ingress** (`ingress.enabled=true`) varsayılan olarak TLS ile sunar
  (`ingress.tls.enabled=true`). Sertifika `ingress.tls.secretName`
  (varsayılan `<release>-tls`) adlı `kubernetes.io/tls` Secret'ındadır:
  kendiniz oluşturun ya da cert-manager'a oluşturtun
  (`ingress.annotations` ile `cert-manager.io/cluster-issuer: <issuer>`).
  Controller'ın HSTS ya da istek sınırı ayarları da `ingress.annotations`
  ile verilir. Chart `UPTIME_PUBLIC_URL`'i TLS açıkken
  `https://<ingress.host>`, kapalıyken `http://<ingress.host>` olarak
  türetir; OIDC dönüş adresi ikisinde de
  `https://<ingress.host>/auth/callback` olur (bkz. "SSO (OpenID
  Connect)"). TLS'i yalnızca TLS Ingress'in önünde, bir yük dengeleyicide
  bitiyorsa kapatın; o durumda `publicURL` değerini `https://` adresle
  verin. TLS kapalıyken kurulum notları uyarır.

**Yükseltme notu:** Ingress bu sürümle varsayılan olarak TLS ile sunulur ve
chart adresi `https://<ingress.host>` olarak türetir; adres `https://`
olduğu için oturum çerezi de Secure işaretlenir. 0.37.0'da Ingress'i düz HTTP
ile kullanan kurulumlar yükseltirken şunlardan birini yapmalıdır:

- `ingress.tls.secretName` (varsayılan `<release>-tls`) adlı
  `kubernetes.io/tls` Secret'ını oluşturun ya da cert-manager'a oluşturtun;
- TLS'siz sürdürmek için `ingress.tls.enabled=false` verin. TLS'i Ingress'in
  önündeki bir yük dengeleyicide bitiyorsa `https://` ile `publicURL` de
  verin. SSO (`oidc.issuer`) Ingress controller'ın önünde HTTPS ister:
  sağlayıcı kullanıcıyı `https://` bir adrese geri gönderir. TLS hiçbir
  yerde bitmiyorsa onu bir yerde sonlandırın ya da `oidc.issuer`'ı
  boşaltarak SSO'yu kapatın.

Hiçbiri yapılmazsa Ingress var olmayan bir Secret'a bakar: ingress-nginx
kendi varsayılan sertifikasıyla HTTPS'e yönlendirir, Traefik düz HTTP'de
kalır. Düz HTTP'de tarayıcı Secure çerezi kabul etmez; giriş yapan kullanıcı
hiçbir hata görmeden giriş sayfasına geri döner. Secret yoksa kurulum notları
uyarır (cert-manager onu oluştururken de, ilk kurulumda bu beklenir).

### Servis hesabı

Backend keşif için pod'un servis hesabıyla cluster'ı okur. Yetkiler salt
okunurdur ve kodun kullandığıyla sınırlıdır: yazma yoktur, Secret'lar
okunmaz, namespace'ler ve pod'lar yalnızca listelenir (`list`, tek tek
`get` yok). Yine de bir pod ya da workload listesi pod tanımının tamamını,
ortam değişkenlerine düz yazılmış değerler dahil, getirir; Route'un içine
yazılmış bir sertifika da özel anahtarıyla gelir. Kubeat bunları
kullanıcılarına göstermez, ama backend'in token'ını ele geçiren biri
varsayılan kurulumda bunları cluster'ın tamamında okuyabilir. Ekiplerin
paylaştığı bir cluster'da keşfi `discovery.namespaces` ile izlenecek
namespace'lerle sınırlayın (bkz. "Cluster-admin olmadan"); gizli değerleri
ortam değişkenlerine düz yazmak yerine `secretKeyRef` ile verin.

`prometheus.tokenFile` verildiğinde backend her sorguda bu token'ı
`prometheus.url`'e gönderir. Bu yüzden `prometheus.insecureSkipVerify`
onunla birlikte kullanılmamalıdır: sertifika denetlenmezse araya giren
token'ı alır. Bunun yerine `prometheus.caFile` verin (OpenShift'te service
CA); ikisi birlikte verilirse kurulum notları uyarır.

### Tedarik zinciri

- Taban imajlar (`golang`, `ubi9/ubi-minimal`, `node`,
  `ubi9/nodejs-24-minimal`) `latest` gibi kayan etiketlerle değil, tam sürüm
  etiketleriyle sabitlenir; bir sürümün neyle derlendiği böylece bellidir.
- Go sürümü `backend/go.mod`'daki `toolchain` satırıdır. CI onunla derler ve
  test eder; backend imajını derleyen `golang` imajı da aynı sürümdedir. İkisi
  birlikte yükseltilir.
- Dependabot ([.github/dependabot.yml](.github/dependabot.yml)) her hafta Go
  modülleri, npm paketleri, taban imajlar ve GitHub Actions için güncelleme
  PR'ları açar. Yeni Go ve Node ana sürümleri elle alınır: Node'un ana sürümü
  çalışma imajınınkiyle (`nodejs-24-minimal`) aynı olmalıdır.
- CI'daki `govulncheck` işi bilinen açıkları kodun gerçekten çağırdığı
  yerlerde arar (bağımlılıklar ve standart kütüphane); bulursa CI kırmızı
  olur.
- İmajlar yalnızca sürüm etiketinde derlenir; taban imajdaki bir düzeltme
  kullanıcılara bir sonraki sürümle ulaşır.

## Arayüz

Arayüz bir çalışma alanıdır: en solda bölümlerin dar menüsü, yanında tüm
monitor'ların listesi, sağda genel bakış ya da açılan monitor. Üst çubuk
bulunulan yeri, filonun nabzını (çalışan ve kesintideki monitor sayısı) ve
"Yeni monitor"u gösterir. Telefonda menü alta iner.

Arayüz İngilizce ve Türkçe'dir: tarayıcının istediği dil kullanılır, menünün
altındaki düğme değiştirir ve seçim bir çerezde kalır; ikisi de yoksa
`DEFAULT_LOCALE` geçerlidir. Bildirim metinlerinin dili backend'de ayrıdır
(`UPTIME_LANGUAGE`); chart'ta ikisini birden `language` değeri ayarlar.
Herkese açık durum sayfaları bunun dışındadır: her sayfa kendi ayarındaki
dilde (ayarsızsa `UPTIME_LANGUAGE`) gösterilir (bkz. "Durum sayfaları").

Durum her yerde renkle olduğu kadar şekille de okunur: çalışan daire, kesinti
elmas, sorunlu yarım daire, bakım taralı, duraklatılmış içi boş; renk körü
biri de listeyi okuyabilir.

- **Liste**: namespace'e ya da gruba göre toplanır; ada, adrese, gruba ya da
  etikete göre aranır; duruma göre (hepsi, çalışan, kesinti, sorunlu, pasif)
  süzülür, tip ve etiket süzgeçleri menüdedir. Her satır tek satırdır:
  durum, ad, kubectl kısaltmasıyla tür (`svc`, `route`, `deploy`...), son
  24 kontrol ve uptime; uptime yalnızca eşiklerin altındayken renklenir
  (`WARN_UPTIME_BELOW`, `CRITICAL_UPTIME_BELOW`). Sertifikasının bitmesine
  14 gün ya da daha az kalanlar satırında işaretlenir.
- **Toplu işlem**: satırın üzerine gelince çıkan kutularla birden çok
  monitor seçilir; alttaki çubuk onları birlikte durdurur, devam ettirir ya
  da siler. Grup başlığındaki kutu o gruptaki görünenlerin hepsini seçer.
- **Genel bakış**: filonun durumunu bir cümleyle söyler, son 24 saatin
  kesinti şeridini ve devam eden kesintileri gösterir; altında sayılar
  (çalışan, kesinti, bakım, MTTR...), her monitor'un bir kare olduğu cluster
  haritası ve gün gün etkinlik akışı vardır. Namespace sayfası aynısını tek
  namespace için gösterir.
- **Komut paleti**: `⌘K` (Windows ve Linux'ta `Ctrl+K`) bir monitor'a, bir
  namespace'e ya da bir sayfaya gider, yeni monitor açar; yalnızca klavyeyle
  kullanılır.
- **Monitor sayfası**: durum ve ne kadar süredir sürdüğü ("Erişilemiyor · 4
  gün 1 sa", "Çalışıyor · 5 gün 14 sa", yavaş yanıt verende "Sorunlu · 12 dk
  · yanıtlar 800 ms'den uzun" ve altında eşiği ve son kontrollerin sürelerini
  sayan kehribar bir satır), son 60 kontrol, yanıt süresi, uptime ve
  "MTBF · 7 gün" özetleri (`STATS_DAYS`), kesinti ve bakım bantlı grafik,
  pod'lar, olaylar, kontrol geçmişi ve ayarların özeti. Son kontrol
  başarısızsa **Neden?** bölümü hatanın ne anlama geldiğini, bağlantının
  hangi adımda (DNS, bağlantı, TLS, ilk bayt) takıldığını, ulaşılan adresi
  ve sertifikayı gösterir (bkz. "Başarısız kontrolün ayrıntıları"). Buradan
  durdurulur, susturulur, düzenlenir, kopyalanır ya da silinir; ⋯ menüsündeki
  **Bildirimi test et** monitor'u duyan her kanala birer test gönderir ve
  kanal başına sonucu listeler (bkz. "Monitor'den test").
- **Olay sayfası**: bir kesintinin baştan sona hikâyesi; bkz. "Olay
  sayfası".
- **Yeni monitor / düzenle**: tipe göre değişen tek form; yardım metinleri
  girilen değerleri okuyarak ne olacağını söyler ("Art arda 3 kontrol 800
  ms'den uzun sürerse monitor Sorunlu görünür ve yavaşlama bildirimi gider").
  Yavaş yanıt eşiği `NEW_MONITOR_SLOW_ABOVE_MS` ile başlar; 0 kapalıdır.
- **Bildirimler**: kanalların listesi (kapsamının altında "10 dk gecikmeli ·
  her 1 sa" gibi zamanlaması) ve formu. Formun **Zamanlama** bölümü gecikme
  ve tekrarı sayı ve birimle alır, cümle olarak geri okur ("Kesinti 10 dk
  sürerse haber verir; sonra her 1 sa tekrar hatırlatır"; ikisi de sıfırken
  "Hemen; tekrar genel ayar"). Olaylar arasında "yavaşladı" ve "yeniden
  hızlı" da seçilir.
- **Tema**: menünün altındaki düğme sistem, açık ve koyu arasında geçer;
  seçim tarayıcıda saklanır.
- **Logo**: kelimenin kendisidir, k'si işarettir (menüdeki mavi karo,
  sekmedeki k). k hep vurgu mavisindedir, hiçbir zaman bir durum renginde
  değil. Dosyalar ve kurallar: [docs/assets](docs/assets/README.md).

## Durum sayfaları

Bir durum sayfası, seçilen monitor'ların durumunu **giriş gerektirmeyen** bir
adreste yayınlar: `/status/<adres>`. Ekip dışındakiler, müşteriler ya da
duvardaki bir ekran içindir. **Durum sayfaları** menüsünden oluşturulur.

- Sayfa bir başlık, açıklama, isteğe bağlı sabit bir **duyuru** ve seçilen
  monitor'lardan oluşur. Her monitor'a sayfada görünecek ayrı bir ad
  verilebilir; verilmezse monitor'un arayüzdeki adı görünür: adı keşfin
  verdiği ad olan monitor'larda nesnenin adı (`web:80`), yeniden
  adlandırılmış ya da elle girilmiş olanlarda monitor'un adı.
- Her monitor için durum, son `UPTIME_STATUS_PAGE_DAYS` günün gün gün uptime
  çubuğu ve toplam uptime gösterilir; ayrıca son 24 saat, 7 gün, 30 gün ve
  sayfanın gün sayısı için uptime, hem monitor başına hem sayfanın bütünü
  için (bütünü, o dönemde kontrolü olan monitor'ların ortalamasıdır).
  Altında son kesintiler listelenir.
- Sayfa yalnızca adı, durumu, uptime'ı ve kesintilerin zamanını gösterir.
  Adresler, hata mesajları ve sayfada olmayan monitor'lar dışarı verilmez.
- Tek bir başarısız kontrol sayfada "kesinti" görünmez; kesinti, alarm eşiği
  aşılıp olay açıldığında görünür.
- "Yayında" işareti kaldırılan sayfa açılmaz (404).
- Editörler yalnızca kapsamlarındaki monitor'ları gösteren sayfaları yönetir.
  Monitor'u ve kuralı olmayan bir sayfa (yalnızca duyuru taşıyan ya da
  monitor'ları silinmiş bir sayfa) yöneticilerindir.

**Bölümler.** Sayfanın monitor'ları sıralı bölümlerde gruplanır ("Vitrin",
"Arka plan"); bölümü olmayanlar en üstte adsız bir grupta görünür. Bölümler
sayfayla birlikte verilir (`sections`), her monitor bölümünü adıyla
belirtir (`section`). Bir bölüm, içinde monitor ya da kural varken
kaldırılamaz.

**Üyelik kuralları.** Monitor'ları tek tek seçmek yerine sayfaya kural da
yazılabilir: "`shop` namespace'indeki monitor'lar", "`public` etiketini
taşıyanlar" ya da ikisi birden (`rules: [{namespace, tag, section}]`; boş
namespace her namespace, boş etiket her etiket demektir). Kurallar sayfa her
açıldığında değerlendirilir: annotation ile keşfedilen yeni bir monitor
kuralı tutuyorsa sayfada kendiliğinden belirir. Eşleşen monitor'lar kuralın
bölümünde, elle seçilenlerden sonra ve ada göre sıralı görünür; adları kendi
adlarıdır (arayüzdeki gibi). Bir monitor sayfada bir kez
görünür: elle seçilmişse kural onu bir daha eklemez, iki kural tutuyorsa
ilki alır. Editörler yalnızca kendi namespace'leri için kural yazabilir;
"her namespace" kuralı bütün namespace'lere yetkili bir editör ya da
yönetici ister. `GET /api/v1/status-pages/{id}/monitors` sayfanın o an
gösterdiklerini (elle seçilenler ve eşleşenler) bölüm bölüm verir; arayüz
önizlemeyi bununla gösterir.

**Güncellemeler.** Sabit duyurunun yanında sayfanın bir de tarihçesi vardır:
"İnceliyoruz", "Nedeni bulundu", "İzliyoruz", "Çözüldü", bir bildirim ya da
bakım duyurusu (`kind`: `investigating`, `identified`, `monitoring`,
`resolved`, `notice`, `maintenance`), başlık ve metinle; isteğe bağlı olarak
bir olaya bağlanır, sayfa o zaman olayın monitor'unun sayfadaki adını da
yazar. Güncellemeler sayfayı yönetebilenlerce yazılır, düzenlenir ve
silinir; her işlem kayda geçer. Sayfa son `UPTIME_STATUS_PAGE_UPDATES`
güncellemeyi yeniden eskiye, güne göre gruplanmış (UTC) gösterir. Olay
sayfasında "durum sayfasında göster" işaretlenen notlar da buraya karışır:
`note` türünde, başlığı olayın monitor'unun sayfadaki adı olan bir
güncelleme olarak; notun yazarı dışarı verilmez.

**Bakım pencereleri.** Sayfadaki bir monitor'u kapsayan (monitor'a,
namespace'e ya da herkese tanımlı) bakım pencerelerinden süren ya da
`UPTIME_STATUS_PAGE_UPCOMING` içinde başlayacak olanlar adı, başlangıcı,
bitişi ve kapsadığı monitor'ların sayfadaki adlarıyla listelenir;
tekrarlayan pencere süren ya da bir sonraki tekrarıyla.

**Ayarlar.** Sayfa başına: `noindex` (arama motorlarına "indeksleme" der;
arayüz robots meta'sını buna göre basar), `hide_paused` (durdurulmuş
monitor'lar sayfada görünmez), `language` (boşsa kurulumun dili
`UPTIME_LANGUAGE`, yoksa `tr` ya da `en`; görünüm geçerli olanı döner),
`detail_pages` (monitor'un kendi ayrıntı sayfası açılabilir; varsayılan
açık) ve `password`.

**Parola.** Parolalı bir sayfa yalnızca parolayı bilene açılır: parola
yalnızca yazılır, bcrypt ile saklanır (hesap parolalarının kurallarına
tabidir), `password: ""` kaldırır. Ziyaretçi parolayı
`POST /api/v1/status-pages/by-slug/{slug}/unlock` ile verir ve
`UPTIME_STATUS_PAGE_SESSION` süreli bir token alır (kurulumun gizli
anahtarıyla imzalanmış HMAC; parola değişince eski token'lar geçersizdir).
Görünüm, ayrıntı sayfası ve rozetler token'ı `Authorization: Bearer` ya da
`X-Status-Page-Token` başlığında ister (ikisi de varsa sayfayı açan
hangisiyse o geçer; Kubeat'e kendi anahtarıyla giren bir istemci sayfanın
token'ını ikinci başlıkta verir); yoksa yalnızca sayfanın başlığını
taşıyan `401 {"locked": true, "title": ...}` döner ki parola ekranı başlığı
gösterebilsin. Arayüz token'ı HttpOnly bir çerezde tutar. Parola denemeleri
girişteki gibi sayfa başına kısıtlanır (`UPTIME_LOGIN_MAX_FAILURES`,
`UPTIME_LOGIN_LOCKOUT`); kilitliyken doğru parola da 429 alır.
`UPTIME_SECRET_KEY` yoksa token'lar o çalıştırmaya özgü bir anahtarla
imzalanır ve backend yeniden başlayınca ziyaretçilere parola yeniden
sorulur.

**Ayrıntı sayfası.** `detail_pages` açıkken sayfadaki her monitor
`GET /api/v1/status-pages/by-slug/{slug}/monitors/{monitor_id}` ile kendi
sayfasında açılır: sayfadaki adı, durumu, dört dönemin uptime'ı, gün gün
geçmiş, son `UPTIME_STATUS_PAGE_SERIES` sürenin yanıt süresi serisi (monitor
sayfasındaki grafikle aynı dilimleme) ve son `UPTIME_STATUS_PAGE_DAYS` günün
kesintileri yalnızca başlangıç, bitiş ve süre olarak; neden ve adres
verilmez. Sayfada olmayan monitor ya da kapalı ayrıntı sayfası 404, parolalı
sayfada token yoksa 401 döner.

**Rozetler.** Yayındaki bir sayfada gösterilen her monitor'un iki rozeti
vardır; README ya da wiki sayfasına konabilir:

```
![durum](https://<kubeat-adresi>/api/badge/<monitor-id>/status)
![uptime](https://<kubeat-adresi>/api/badge/<monitor-id>/uptime)
```

`?label=metin` ile soldaki yazı değiştirilebilir. Hiçbir yayındaki sayfada
(elle ya da kuralla) olmayan monitor'un rozeti yoktur (404). Yalnızca
parolalı sayfalarda olan bir monitor'un rozeti o sayfalardan birinin
token'ını ister; `<img>` başlık taşıyamadığından böyle bir rozet ancak
token'ı gönderebilen bir istemciyle alınır.

Durum sayfası başka bir sitede iframe içinde gösterilecekse o sitenin
adresi `STATUS_PAGE_FRAME_ANCESTORS` ile verilir (bkz. "Tarayıcı
güvenliği"); varsayılan olarak hiçbir site gösteremez.

**Arayüzde düzenleme.** Durum sayfaları menüsünde bir sayfanın formu
sırayla şunları sorar: sayfa (başlık, adres, açıklama), duyuru, bölümler
(sıralı adlar; yukarı/aşağı taşınır, kaldırılan bölümün içindekiler en
üstteki adsız gruba geçer), servisler (her monitor için işaret, sayfada
görünen ad ve bölüm seçimi), üyelik kuralları (namespace, etiket, bölüm;
her kural altında okunur: "checkout namespace'indeki tier-1 etiketli her
monitor, Ödeme bölümünde"), sayfanın şu an gösterdikleri (kaydedilmiş
haliyle, elle seçilenler ve kuralla gelenler ayrı işaretli; "Yenile" ile
yeniden okunur), ayarlar (dil, arama motorlarından gizleme, durdurulmuş
monitor'ları gösterme, ayrıntı sayfaları, parola: ayarlı olduğu söylenir,
değiştirilir ya da kaldırılır; parolanın kendisi hiç gösterilmez) ve yayın.
Kaydetmek her monitor'u bulunduğu bölümle gönderir; bölümler hiçbir kayıtta
düzleşmez. Formun altında sayfanın **güncellemeleri** vardır: güne göre
listelenir; yeni güncelleme türü (inceliyoruz, nedeni bulundu, izliyoruz,
çözüldü, bildirim, bakım), başlığı, metni ve isteğe bağlı olarak sayfadaki
monitor'ların açık ya da yakın olaylarından biriyle yazılır, düzenlenir ve
silinir.

**Arayüzde görünüm.** Herkese açık sayfa bölümleri başlıklı gruplar olarak
çizer; sayfa `STATUS_PAGE_COMPACT_ABOVE` sayısından çok servis
gösteriyorsa her servis tek satıra iner (ad, alçak gün çubuğu, uptime,
durum). Sayfanın bütününün ve her servisin uptime'ı dört dönem için
yazılır (24 saat, 7 gün, 30 gün, sayfanın gün sayısı). Güncellemeler
ziyaretçinin kendi gününe göre gruplanır (sunucu UTC gününü verir,
tarayıcı yeniden gruplar), her biri türünün işaretini ve saatini taşır;
olay notları servis adıyla aralarına karışır. Süren ve yaklaşan bakım
pencereleri kendi bölümünde, kapsadıkları servislerle listelenir. Sayfa
dili için ne ayarlandıysa (yoksa kurulumun dili) o dilde gösterilir;
ziyaretçinin tarayıcı dili ve dil düğmesi bu sayfalarda geçersizdir.
`noindex` açık sayfa `<meta name="robots" content="noindex">` taşır.
Parolalı sayfa, parolayı soran bir formla açılır; form JavaScript'siz de
çalışır: `POST /status/<adres>/unlock` yoluyla arayüz parolayı backend'e
verir ve aldığı token'ı yalnızca `/status/<adres>` yoluna bağlı, HttpOnly
bir çerezde (`kubeat_sp_<adres>`) tutar; sayfa ve ayrıntı sayfaları token'ı
`X-Status-Page-Token` başlığıyla gönderir. Token süresi dolunca ya da
parola değişince backend 401 döner ve form yeniden görünür. Ayrıntı
sayfası (`/status/<adres>/<monitor-id>`) servisin adını, durumunu, dört
dönemin uptime'ını, gün gün geçmişini, yanıt süresi grafiğini (monitor
sayfasındaki grafiğin salt okunur hali; seçim ve yakınlaştırma yok) ve
kesintilerini başlangıç, bitiş ve süre olarak gösterir.

**Duvar ekranı.** Sayfanın başındaki "Duvar ekranı" düğmesi sayfayı tam
ekrana alır, yazıları ve satırları büyütür, alt bilgiyi gizler ve seçimi
tarayıcıda (localStorage) saklar; sayfa kendini yenilemeye devam eder. Bir
servis kesintiye düşünce ya da sayfanın durumu kötüleşince kısa bir ses
çalar; tarayıcılar sesi ancak bir tıklamadan sonra çaldırdığından ses,
düğmeye basıldığında açılır ve yeniden açılan bir duvar ekranında "Sesi
aç" ile yeniden istenir. Hareket azaltma isteyen ziyaretçi için hiçbir şey
canlanmaz.

## API

`UPTIME_API_TOKEN` ayarlıysa örneklere `-H "Authorization: Bearer $TOKEN"`
eklenmelidir.

| Metot | Yol | Açıklama |
| --- | --- | --- |
| `GET` | `/api/v1/health` | Backend sağlık durumu |
| `GET` | `/api/v1/license` | Lisansın durumu (`community`, `active`, `expiring`, `expired`, `invalid`), şu an açık özellikler ve bitiş tarihi. Müşteri adını, lisans numarasını ve sorunu yalnızca yöneticiler görür |
| `GET` | `/metrics` | Prometheus metrikleri |
| `GET` | `/api/v1/monitors?window=24h` | Monitor'lar; son durum ve uptime ile. İsteğe bağlı: `namespace=x`, `ids=1,2`, `history=40` (her monitor'un son kontrolleri) |
| `POST` | `/api/v1/monitors` | Monitor ekler |
| `GET` | `/api/v1/monitors/{id}?window=24h` | Tek monitor |
| `PATCH` | `/api/v1/monitors/{id}` | Monitor'u düzenler; `{"enabled": false}` ile durdurur |
| `DELETE` | `/api/v1/monitors/{id}` | Monitor'u ve sonuçlarını siler |
| `POST` | `/api/v1/monitors/{id}/clone` | Monitor'un `{"name"}` adıyla bir kopyasını oluşturur; gizli değerler de kopyalanır, kopyanın hedefi başka bir sunucuya çevrilirse yeniden girilmeleri gerekir |
| `POST` | `/api/v1/monitors/{id}/push-token` | Push monitor'una yeni bir token verir ve monitor'u yeni `push_token` ile döner; eski token hemen çalışmaz olur. Monitor'u değiştirebilenler kullanabilir, denetim kaydına yazılır |
| `GET`, `POST` | `/api/push/{token}?status=up&msg=...&ping=...` | Push monitor'unun raporu; API anahtarı gerekmez |
| `GET` | `/api/v1/monitors/{id}/results?limit=100` | Son kontrol sonuçları, yeniden eskiye; başarısızlar nedenleriyle (`diagnostics`, bkz. "Başarısız kontrolün ayrıntıları") |
| `GET` | `/api/v1/monitors/{id}/series?range=24h` | Yanıt süresi ve erişilebilirlik, zaman dilimlerine bölünmüş; `range` yerine `from=&to=` ile belirli bir aralık |
| `GET` | `/api/v1/monitors/{id}/uptime?windows=24h,168h` | İstenen her dönem için uptime |
| `GET` | `/api/v1/monitors/{id}/workload` | Monitor'un arkasındaki pod'lar; hazır olma, restart, CPU ve bellek |
| `GET` | `/api/v1/monitors/{id}/window?from=&to=` | İki an arasında olanlar: kontrol özeti, başarısız kontroller (`diagnostics` ile), kesişen olaylar, cluster olayları |
| `GET` | `/api/v1/monitors/{id}/grid?range=24h` | Kontrol geçmişi zaman dilimlerine bölünmüş: dilim başına kontrol, başarısız, ortalama yanıt, en sık hata; aralığın en sık hataları |
| `GET` | `/api/v1/monitors/{id}/channels` | Monitor'un alarmının gittiği etkin kanallar; her birinin `delay_seconds` ve `repeat_seconds` değeriyle |
| `POST` | `/api/v1/monitors/{id}/test-notification` | Monitor'u adıyla anan bir test bildirimini, alarmının gittiği her etkin kanala birer deneme olarak gönderir; kanal başına sonuçla döner (`channel_id`, `name`, `type`, `delivered`, kanalı yönetebilenlere `error`). Kapsamı tutan kanal yoksa `422`. Monitor'u değiştirebilenler kullanabilir, denetim kaydına yazılır |
| `GET` | `/api/v1/discovery` | Keşfin kullanılabilir olup olmadığı |
| `GET` | `/api/v1/discovery/namespaces` | Cluster'daki namespace'ler |
| `GET` | `/api/v1/discovery/candidates?namespace=x` | Namespace'teki izlenebilir hedefler |
| `POST` | `/api/v1/discovery/import` | Seçilen hedefleri monitor'a çevirir |
| `POST` | `/api/v1/discovery/restore` | Silinmiş bir annotation monitor'ünü yeniden izlemeye alır: `{namespace, source}` |
| `GET` | `/api/v1/incidents?monitor_id=1&limit=100` | Olaylar, yeniden eskiye. Süzgeçler `namespace` (tekrarlanabilir), `kind`, `state`, `monitor_id`, `q`, `from`, `to`; sıralama `sort`, `order`; sayfa `limit`, `offset` (toplam `X-Total-Count` başlığında); `format=csv` ile indirilebilir. Bkz. "Olay sayfası" |
| `GET` | `/api/v1/incidents/{id}` | Olay, monitor'ü ve zaman çizelgesi: başarısız kontroller, açılış, bildirimler, hatırlatmalar, notlar, cluster olayları, bakım pencereleri, kapanış |
| `GET` | `/api/v1/incidents/{id}/notes` | Olayın notları, eskiden yeniye |
| `POST` | `/api/v1/incidents/{id}/notes` | Not ekler: `{"text", "show_on_status_page"}` |
| `PATCH` | `/api/v1/incidents/{id}/notes/{note}` | Notu düzenler; verilmeyen alanlar değişmez. Yalnızca yazarı ya da yönetici |
| `DELETE` | `/api/v1/incidents/{id}/notes/{note}` | Notu siler; yalnızca yazarı ya da yönetici |
| `GET` | `/api/v1/reports?days=30&namespace=shop&format=csv` | Namespace ve monitor başına uptime, kesinti sayısı ve süresi, MTTR ve MTBF; `format=csv` ile indirilebilir |
| `GET` | `/api/v1/stats?days=7` | Görülebilen monitor'ların özeti: uptime, kesinti sayısı ve süresi, MTTR, MTBF, açık olaylar; `namespace` (tekrarlanabilir) ile daraltılır, `monitor_id` ile tek monitor'a iner (okunamayan monitor için `404`) |
| `GET` | `/api/v1/maintenance` | Bakım pencereleri, en son başlayan önce |
| `POST` | `/api/v1/maintenance` | Pencere ekler: `name`, `duration_seconds` ya da `ends_at`, isteğe bağlı `starts_at`, `namespace` ya da `monitor_id`, `repeat` ve `timezone` |
| `DELETE` | `/api/v1/maintenance/{id}` | Pencereyi siler; süren pencereyi bitirir |
| `GET` | `/api/v1/channels` | Bildirim kanalları (URL'ler maskelenir) |
| `POST` | `/api/v1/channels` | Kanal ekler: `{"name", "type", "target", "secret", "namespaces", "tags", "events", "delay_seconds", "repeat_seconds"}` |
| `PATCH` | `/api/v1/channels/{id}` | Kanalı düzenler; `{"enabled": false}` ile durdurur |
| `DELETE` | `/api/v1/channels/{id}` | Kanalı siler |
| `POST` | `/api/v1/channels/{id}/test` | Kanala test bildirimi gönderir |
| `GET` | `/api/v1/status-pages` | Durum sayfaları (yöneticiye hepsi, editöre kapsamındakiler) |
| `POST` | `/api/v1/status-pages` | Sayfa ekler: `slug`, `title`, `description`, `announcement`, `published`, `sections: [{name}]`, `monitors: [{monitor_id, display_name, section}]`, `rules: [{namespace, tag, section}]`, `noindex`, `hide_paused`, `language`, `detail_pages`, `password` (yalnızca yazılır; yanıtta `has_password`) |
| `PATCH` | `/api/v1/status-pages/{id}` | Sayfayı düzenler; verilmeyen alanlar değişmez. `password: ""` parolayı kaldırır |
| `DELETE` | `/api/v1/status-pages/{id}` | Sayfayı siler; monitor'lar kalır |
| `GET` | `/api/v1/status-pages/{id}/monitors` | Sayfanın şu an gösterdiği monitor'lar bölüm bölüm: elle seçilenler ve kuralların eşlediği (`explicit`), önizleme için |
| `GET` | `/api/v1/status-pages/{id}/updates?limit=` | Sayfanın güncellemeleri, yeniden eskiye |
| `POST` | `/api/v1/status-pages/{id}/updates` | Güncelleme ekler: `{"kind", "title", "body", "incident_id"}`; `kind`: `investigating`, `identified`, `monitoring`, `resolved`, `notice`, `maintenance` |
| `PATCH` | `/api/v1/status-pages/{id}/updates/{update}` | Güncellemeyi düzenler; verilmeyen alanlar değişmez, `incident_id: 0` olay bağını kaldırır |
| `DELETE` | `/api/v1/status-pages/{id}/updates/{update}` | Güncellemeyi siler |
| `GET` | `/api/v1/status-pages/by-slug/{slug}/view` | Yayındaki sayfanın herkese açık içeriği; parolalı sayfada token ister (`Authorization: Bearer` ya da `X-Status-Page-Token`), yoksa `401 {"locked": true, "title"}` |
| `POST` | `/api/v1/status-pages/by-slug/{slug}/unlock` | Parolalı sayfanın parolasını token'a çevirir: `{"password"}` → `{"token", "expires_at"}`; yanlışsa 401, çok denemede 429 |
| `GET` | `/api/v1/status-pages/by-slug/{slug}/monitors/{monitor_id}` | Sayfadaki bir monitor'un ayrıntısı (`detail_pages` açıksa): ad, durum, uptime dönemleri, gün gün geçmiş, yanıt süresi serisi, kesintilerin yalnızca başlangıç/bitiş/süresi |
| `GET` | `/api/v1/badges/{id}/{status\|uptime}` | Yayındaki bir sayfada olan monitor'un SVG rozeti; yalnızca parolalı sayfalarda olan monitor'unki token ister |

```
curl -X POST localhost:8090/api/v1/monitors -d '{
  "name": "ornek", "type": "http", "target": "https://example.com",
  "interval_seconds": 30, "timeout_seconds": 5
}'
```

Her monitor'da ortak alanlar: `name`, `type`, `target`, `interval_seconds`,
`timeout_seconds` ve isteğe bağlı olarak:

- `description`: serbest açıklama.
- `namespace`: elle eklenen bir monitor'un **grubu** (örneğin "Ödeme
  sistemleri"). Listede bu ad altında toplanır ve bildirim kanalları gruba
  göre sınırlanabilir. Keşfedilen monitor'larda bu alan nesnenin
  namespace'idir ve değiştirilemez.
- `tags`: serbest etiketler (`["prod", "kritik"]`), en çok 10 tane. Listede
  etikete göre süzülür ve aranır; keşfedilen monitor'lara da verilebilir.
- `failure_threshold`: art arda kaç başarısız kontrolün kesinti sayılacağı.
- `retry_interval_seconds`: bir kontrol başarısız olduğunda, kesinti
  kesinleşene kadar sonraki kontrolün kaç saniye sonra yapılacağı. `0` ise
  normal aralık beklenir. Kısa bir değer, kesintiyi daha erken doğrular ya da
  geçici bir hatayı daha erken eler.
- `slow_above_ms`: başarılı bir kontrolün "yavaş" sayılacağı yanıt süresi
  (milisaniye); `0` kapalı. `timeout_seconds`'ın altında olmalıdır, çünkü
  zaman aşımını geçen kontrol zaten başarısızdır. Art arda
  `failure_threshold` kontrol başarılı ama bu eşikten yavaşsa monitor
  `degraded` sayılır ve yanıtta `slow_since` dolar; olay açılmaz (bkz.
  "Yavaş yanıt").
- `inverted`: sonucu tersine çevirir; hedef **başarısız** olduğu sürece
  monitor "çalışıyor" sayılır. Erişilmemesi gereken şeyler için. Hedef
  politikasının reddettiği bir bağlantı ise beklenen başarısızlık sayılmaz:
  Kubeat hedefe hiç bağlanmamıştır ve ağın onu gerçekten engelleyip
  engellemediğini bilemez, bu yüzden kontrol politikanın hatasıyla başarısız
  olur. Yasak bir aralıktaki adresin (metadata servisi gibi) ağda kapalı
  olduğunu doğrulayacak bir monitor için o adrese istisna gerekir (bkz.
  "Hedef politikası").

## Monitor tipleri

| `type` | `target` | Ne kontrol edilir |
| --- | --- | --- |
| `http` | `https://host/yol` | HTTP isteği: durum kodu, gövdede metin ya da JSON değeri, sertifika süresi |
| `tcp` | `host:port` | TCP bağlantısı; istenirse TLS el sıkışması ve sertifika süresi |
| `ping` | `host` ya da IP | ICMP echo; yanıt süresi gidiş-dönüş süresidir |
| `dns` | alan adı | DNS sorgusu: kayıt var mı, beklenen değeri içeriyor mu |
| `push` | (boş) | Hedef kendisi rapor verir; rapor gelmezse başarısız |
| `grpc` | `host:port` | gRPC sağlık protokolü (`grpc.health.v1.Health/Check`) |
| `websocket` | `wss://host/yol` | WebSocket el sıkışması |
| `postgres` | `host:port` | PostgreSQL'e giriş yapıp sorgu çalıştırır |
| `mysql` | `host:port` | MySQL ya da MariaDB'ye giriş yapıp sorgu çalıştırır |
| `redis` | `host:port` | Redis'e bağlanır, gerekiyorsa giriş yapar, `PING` gönderir |
| `workload` | `namespace/tür/ad` | Bir Deployment, StatefulSet ya da DaemonSet'in hazır pod sayısı |

Hedeflerdeki ve kanal adreslerindeki host adları ASCII ile yazılır:
uluslararası bir ad `xn--` (Punycode) biçimiyle verilir (`bİlling.example`
yerine `xn--blling-i9a.example`). Böyle bir adı Go farklı biçimlerde çözer
ve gönderir; yazıldığı gibi kabul edilseydi monitor'un gizli değerlerinin
hangi sunucuya gittiği hedeften okunamazdı.

Tipe özel ayarlar, tipin adını taşıyan nesnede verilir (üç veritabanı tipi
`database` nesnesini paylaşır). Güncellemede bu nesne
gönderilirse o tipin **tüm** ayarlarının yerine geçer; gönderilmezse ayarlar
değişmez.

### `http`

```json
"http": {
  "method": "POST",
  "expected_status": "200-299,401",
  "keyword": "\"status\":\"ok\"",
  "invert_keyword": false,
  "json_path": "checks[0].state",
  "json_operator": "==",
  "json_expected": "up",
  "headers": { "X-Tenant": "a" },
  "body": "{\"deep\": true}",
  "content_type": "application/json",
  "auth": "basic",
  "auth_user": "probe",
  "auth_secret": "...",
  "max_redirects": 10,
  "tls_skip_verify": false
}
```

- `method`: `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE` ya da `OPTIONS`.
- `expected_status`: başarılı sayılan kodlar ve aralıklar; varsayılan `200-399`.
- `keyword`: yanıt gövdesinde bulunması gereken metin. `invert_keyword`
  verilirse tersine, bulunmaması gerekir (örneğin `error`).
- `json_path`: JSON yanıtta bakılacak değer: `status`, `checks[0].state`,
  `data.items[2].id`. `json_operator` (`==`, `!=`, `>`, `>=`, `<`, `<=`,
  `contains`) ile `json_expected` değerine karşılaştırılır. Sayılar değerce
  karşılaştırılır (`12` ile `12.0` eşittir); sıralama operatörleri iki tarafta
  da sayı ister. Sonuçta bulunan değerin en fazla ilk 100 karakteri
  gösterilir.
- `headers`: isteğe eklenen header'lar.
- `body`, `content_type`: istekle gönderilen gövde.
- `auth`: `basic` (`auth_user` + `auth_secret`) ya da `bearer` (`auth_secret`
  token olarak). Açıkça verilen bir `Authorization` header'ı bunun önüne geçer.
- `max_redirects`: kaç yönlendirmenin izleneceği; varsayılan `10`. `0` ise ilk
  yanıt değerlendirilir, yönlendirme olsa bile.
- `tls_skip_verify`: yalnızca bu monitor için sertifika doğrulamasını kapatır.

Header değerleri ve `auth_secret` API yanıtlarında `********` olarak
maskelenir; güncellemede bu değer gönderilirse kayıtlı değer korunur. Kayıtlı
değerler yalnızca girildikleri sunucuya gider: hedef başka bir sunucuya
taşınırsa (host'u ya da portu değişirse, `https://`'den `http://`'ye
dönerse) güncelleme `400` ile reddedilir ve yeniden girilmesi gereken
alanları sayar. `Host` header'ı eklenir, değişir ya da kaldırılırsa da
böyledir: ingress controller ve OpenShift router isteği bu header'ın adını
verdiği siteye iletir. Yalnızca yolun değişmesi aynı sunucu sayılır; aynı
host'ta ve aynı `Host` adıyla `http://`'den (port 80) `https://`'e (port 443)
geçmek de öyledir: istek aynı siteye, bu kez TLS ile gider. `http`
seçenekleri hiç gönderilmese de, yönetici için de böyledir: yoksa monitor'u
düzenleyebilen biri onu kendi sunucusuna yöneltip gösterilmeyen değerleri
orada okuyabilirdi. `Host` header'ının değeri baştaki ve sondaki boşluklar
atılarak saklanır ve bir host adı ya da IP adresi olmalıdır (portlu ya da
portsuz); değilse istek `400` ile reddedilir.

Yönlendirmeler de aynı kurala uyar: başka bir sunucuya (başka host ya da
port, `https://`'den `http://`'ye dönüş ya da başka bir `Host` adı) giden
bir yönlendirme isteğine monitor'un header'ları ve `auth` bilgisi eklenmez,
`Host` da yeni adresin adı olur. İstek gövdesi de başka bir sunucuya
gitmez: gövdeyi yeniden gönderecek bir `307` ya da `308` yönlendirmesi
izlenmez ve kontrol `the redirect would send the request body to another
server` hatasıyla başarısız olur. Aynı sunucu içindeki yönlendirmeler,
aynı host'ta `http://`'den (port 80) `https://`'e (port 443) geçiş de
dahil, hepsini taşımaya
devam eder; sunucu kendi adresini mutlak bir URL ile verse de istek
`Host` header'ının adını verdiği siteye gider.

**Yükseltme notu:** 0.37.0'a kadar monitor'un header'ları her
yönlendirmede, `auth` bilgisi ise aynı host'un başka bir portuna ya da bir
alt alan adına giden yönlendirmelerde de gönderiliyordu. Başka bir host'a
(örneğin `example.com`'dan `www.example.com`'a) ya da porta yönlendirilen ve
kimlik bilgisi isteyen monitor'lar artık `401` gibi bir hatayla düşer:
hedefi yönlendirmenin son adresine çevirip değerleri yeniden girin.

İstek gövdesi (`body`) maskelenmez: monitor'u düzenleyen onu görüp
değiştirebilmelidir. Ama çoğu zaman bir parola ya da token taşıdığı için
yalnızca monitor'u değiştirebilenlere döner; izleyiciler `body` ve
`content_type` alanlarını boş görür.

### `tcp`

`"tcp": {"tls": true, "tls_skip_verify": false}`: bağlantıdan sonra TLS el
sıkışması yapılır. Böylece HTTP olmayan servislerin (LDAPS, Kafka, API
server) sertifika süresi de izlenir ve sertifika alarmı onlar için de çalışır.

### `ping`

Ayarı yoktur. Yetkisiz ICMP soketi kullanılır; ek bir capability gerekmez ama
çekirdeğin buna izin vermesi gerekir (`net.ipv4.ping_group_range`). İzin
yoksa kontrol, nedenini söyleyen bir hatayla başarısız olur. OpenShift'te
(RHCOS) ek ayar gerekmez.

Hedef bir pod IP'si ya da cluster dışı bir adres olmalıdır: bir Service'in
ClusterIP'si sanal bir adrestir ve ICMP'ye yanıt vermez, `servis.namespace.svc`
adına ping "no reply" verir. Service için `http` ya da `tcp` kullanın.

### `dns`

`"dns": {"record_type": "A", "resolver": "10.96.0.10", "expected": "10.0.0.5"}`

- `record_type`: `A`, `AAAA`, `CNAME`, `MX`, `NS`, `TXT`, `SRV` ya da `PTR`;
  varsayılan `A`.
- `resolver`: sorulacak sunucu, `host` ya da `host:port`. Boşsa backend'in
  çalıştığı ortamın çözümleyicisi kullanılır (cluster içinde CoreDNS).
  Verilen sunucu hedef politikasına tabidir: NodeLocal DNSCache
  (`169.254.20.10`) için istisna gerekir.
- `expected`: bulunan kayıtlardan en az birinin içermesi gereken metin.

Bulunan kayıtlar sonuçla birlikte saklanır ve arayüzde görünür.

### `push`

Kontrol edilecek bir adres yoktur; izlenen şey (bir CronJob, bir yedekleme
betiği) işini bitirince Kubeat'e haber verir. Monitor oluşturulunca yanıtta
bir `push_token` döner (monitor okunurken de döner, ama yalnızca onu
değiştirebilenlere; izleyiciler görmez); rapor adresi:

```
https://<kubeat-adresi>/api/push/<token>?status=up&msg=tamam&ping=820
```

- `status`: `up` (varsayılan) ya da `down`; iş kendi başarısızlığını da
  bildirebilir.
- `msg`: sonuçla birlikte saklanan kısa bir metin.
- `ping`: işin bildirmek istediği süre, milisaniye.

`interval_seconds` raporun ne sıklıkta beklendiğidir; `timeout_seconds` ise
ne kadar gecikmeye göz yumulacağı. Bu süre içinde rapor gelmezse monitor
başarısız olur. Adres hem arayüzün adresinden hem cluster içinde backend
servisinden (`http://<release>-backend:8080/api/push/<token>`) kabul edilir;
API anahtarı ya da oturum gerekmez, token'ın kendisi sırdır.
`networkPolicy.enabled=true` iken başka namespace'lerden backend'e
ulaşılamaz; işler raporu arayüzün servisine gönderir
(`http://<release>-frontend.<namespace>.svc:3000/api/push/<token>`, bkz.
"Güvenlik").

```yaml
# Bir CronJob'un sonunda:
command: ["sh", "-c", "yedek-al && curl -fsS 'http://kubeat-backend:8080/api/push/<token>'"]
```

Token sızdıysa monitor sayfasındaki **Adresi yenile** düğmesi ya da
`POST /api/v1/monitors/{id}/push-token` yenisini verir: eski token hemen
çalışmaz olur, monitor ise geçmişi, olayları ve durum sayfalarındaki yeriyle
kalır. Rapor gönderen işler yeni adresle güncellenmelidir.

### `grpc`

`"grpc": {"service": "shop.Cart", "tls": false, "tls_skip_verify": false}`:
Kubernetes'in gRPC probe'larıyla aynı protokol. `service` boşsa sunucunun
genel durumu sorulur. Yanıt `SERVING` değilse kontrol başarısızdır.

### `postgres`, `mysql`, `redis`

```json
"database": {
  "username": "probe",
  "password": "...",
  "database": "orders",
  "query": "SELECT 1",
  "tls": false,
  "tls_skip_verify": false
}
```

Bir portun açık olması veritabanının çalıştığını göstermez; bu tipler giriş
yapıp gerçekten bir şey sorar.

- `postgres` ve `mysql`: `username` zorunludur. `query` giriş yaptıktan sonra
  çalıştırılır ve hata verirse kontrol başarısızdır; varsayılan `SELECT 1`.
  Yalnızca okuma yetkisi olan bir kullanıcı yeterlidir.
- `redis`: `PING` gönderilir, `PONG` beklenir. Sunucu parola istiyorsa
  `password`, ACL kullanıyorsa ayrıca `username` verilir; `database` ve
  `query` kullanılmaz.
- `password` API yanıtlarında `********` olarak maskelenir; güncellemede bu
  değer gönderilirse kayıtlı parola korunur. Hedefin host'u ya da portu
  değişirse parola, `http` seçeneklerindeki gibi yeniden girilmelidir.
  PostgreSQL ve MySQL'in hata mesajları sunucunun söylediğini taşır
  (`password authentication failed for user "probe"`), parolayı taşımaz.
  Redis'in hata yanıtı ise sonuca yalnızca türüyle yazılır
  (`the server answered with an error (WRONGPASS)`, `(NOAUTH)`); sunucunun
  açıklaması backend logundadır.

Gerçek sunuculara karşı testler, adresleri verildiğinde çalışır:

```
KUBEAT_TEST_POSTGRES=127.0.0.1:5432 KUBEAT_TEST_MYSQL=127.0.0.1:3306 \
KUBEAT_TEST_REDIS=127.0.0.1:6379 go test ./internal/checker/
```

### `workload`

`"workload": {"min_ready": 0}`; hedef `shop/deployment/web` biçimindedir, tür
`deployment`, `statefulset` ya da `daemonset` olabilir.

Bir Service'e atılan istek, arkasındaki pod'lardan birine düşer: 30 pod'dan
5'i hazır değilken servis kontrolü yine başarılı olabilir. Bu tip cluster'a
doğrudan sorar: workload'un kaç pod'u olmalı, kaçı hazır?

- `min_ready` `0` ise olması gereken **tüm** pod'lar hazır olmalıdır; bir pod
  bile eksikse kontrol başarısızdır.
- `min_ready` verilirse o kadar pod hazır olduğu sürece kontrol başarılıdır
  (3 kopyalı serviste `2`: tek pod'un düşmesi alarm üretmez).
- Sıfıra ölçeklenmiş bir workload başarısız sayılır.
- Rolling update sırasında pod'ların yer değiştirmesi kesinti değildir:
  workload bir güncellemeyi yayarken (yeni generation, güncellenmemiş ya da
  eski pod'lar varken) eksik pod'lar `UPTIME_WORKLOAD_ROLLOUT_GRACE` (10 dk)
  boyunca hoş görülür ve kontrol "rollout in progress" ayrıntısıyla başarılı
  sayılır. Yayım bu süreyi aşarsa ya da cluster vazgeçerse (Deployment'ın
  `progressDeadlineSeconds`'ı dolunca) olay açılır. Yayım bittikten sonra
  düşen bir pod normal kesintidir. Sayaç bellektedir; Kubeat yeniden
  başlarsa süren bir yayım için baştan sayar.
- Pod'lara ağ erişimi gerekmez, bu yüzden servisi olmayan işler (kuyruk
  tüketicileri, agent'lar) için de çalışır. Kubeat'in cluster'a bağlı olması
  ve `apps` grubundaki bu üç kaynağı okuyabilmesi gerekir; chart bu yetkiyi
  verir.
- "Hazır" Kubernetes'in tanımıdır: pod'un readiness probe'u geçiyor demektir.
  Probe'u olmayan ya da probe'u gerçeği yansıtmayan bir pod hazır görünür.

Monitor sayfasında workload'un pod'ları tek tek listelenir. Keşif sayfası
workload'ları da önerir; bunlar seçili gelmez, çünkü önündeki servisin
kontrolünün yerine geçmez, onu tamamlar.

### `websocket`

`"websocket": {"subprotocol": "graphql-ws", "tls_skip_verify": false}`:
sunucunun bağlantıyı WebSocket'e yükseltip yükseltmediğine bakılır.
`subprotocol` verilirse sunucunun onu kabul etmesi gerekir.

## Geçmiş verisi

Her kontrol sonucu `UPTIME_RETENTION` (varsayılan 7 gün) boyunca tek tek
saklanır. Ayrıca her monitor için saatlik özet tutulur (kontrol sayısı,
başarılı sayısı, başarılı kontrollerin yanıt süresi) ve bu özetler
`UPTIME_ROLLUP_RETENTION` (varsayılan 1 yıl) boyunca kalır. Kısa aralıklar
ham sonuçlardan, ham verinin ötesine geçen aralıklar özetlerden hesaplanır.

Monitor listesindeki uptime ve ortalama yanıt süresi de saatlik özetlerden
okunur: en fazla `UPTIME_ROLLUP_INTERVAL` kadar geriden gelir ve pencere saat
başına yuvarlanır. Ölçüm ayrıntıları: [docs/yuk-testi.md](docs/yuk-testi.md).

Yanıt süresi yalnızca başarılı kontrollerden hesaplanır; başarısız bir
kontrolün süresi çoğunlukla zaman aşımıdır ve yanıt süresi hakkında bir şey
söylemez.

### Kesinti anına gitmek

Grafikte **sürükleyerek** bir aralık işaretlenir. İşaretin üstünde saat
aralığı ve süresiyle küçük bir araç çubuğu çıkar; iki kenarındaki
tutamaçlardan sürükleyerek aralık ayarlanır, **Yakınlaştır** uygular, ✕ ya
da Escape bırakır. Erişilebilirlik şeridindeki kırmızı ya da sarı bir bloğa
**tıklamak** o kesintinin tamamını işaretler.

Yakınlaştırınca grafik o aralığı, iki yanında birer pay bırakarak gösterir
(öncesi ve sonrası görünsün diye) ve daha ince dilimlere geçer; içinde
yeniden sürükleyerek daha da yakınlaşılır. Tarayıcının geri düğmesi bir
adım uzaklaştırır, grafiğin üstündeki bağlantı son 24 saat gibi hazır
görünüme döner. Aralık adrese yazılır (`/monitors/7?from=…&to=…`), yani
paylaşılabilir. (Bildirimlerdeki "Kubeat'te aç" bağlantısı olayın kendi
sayfasına açılır; bkz. "Olay sayfası".) Grafiğin altında **Seçilen aralık**
paneli (`GET /api/v1/monitors/{id}/window?from=&to=`) belirir:

- o aralıktaki kontrol sayısı, kaçının başarısız olduğu, ortalama ve en
  yüksek yanıt süresi (ham kayıtlar `UPTIME_RETENTION` boyunca tutulur;
  daha eski bir aralıkta yalnızca olaylar bilinir);
- aralıkla kesişen olaylar;
- cluster'ın o aralıkta pod'lar ve onları yöneten nesneler hakkında
  bildirdiği olaylar, eskiden yeniye — yerine yenisi gelmiş pod'larınki
  dahil, cluster olayı hâlâ tutuyorsa (upstream Kubernetes 1 saat,
  OpenShift 3 saat). Olaylar aralığın başından bir kontrol aralığı önce
  başlatılır, çünkü kesinti ilk başarısız kontrolde açılır ve neden ondan
  az önce olmuştur;
- başarısız kontrollerin listesi ve hata metinleri.

Pod'ların o anki durumu kaydedilmez; panel bunu söyler ve canlı duruma
yönlendirir. Bir namespace'in olay listesi `UPTIME_EVENT_CACHE` (10 sn)
boyunca yeniden okunmaz; aynı sayfanın iki paneli ve ardışık yenilemeler
cluster'a tek istek yapar.

### Kontrol geçmişi ızgarası

Monitor sayfasındaki **Kontrol geçmişi**, uzun bir tablo yerine takvim
benzeri bir ızgaradır (`GET /api/v1/monitors/{id}/grid?range=24h`): her
hücre bir zaman dilimidir ve içindeki kontrollerin sonucuna göre boyanır —
sorunsuz, kısmi hata (ne kadar çoğu başarısızsa o kadar koyu), kesinti,
kontrol yok. **24 sa** görünümünde sütunlar saat, satırlar saatin
dilimleridir (5 dakika; kontrol aralığı daha uzunsa ona göre 10–60 dakika);
**7 gün** görünümünde satırlar gün, sütunlar günün saatleridir. Böylece
"her saat başı düşüyor" gibi desenler bir satır ya da sütun olarak görünür.

Bir hücrenin üzerine gelince o dilimin kontrol sayısı, kaçının başarısız
olduğu, ortalama yanıt süresi ve başarısızlıkların en sık verdiği **hata
metni** çıkar; tıklamak bunu ızgaranın altına sabitler ve "Bu dilimi
incele" bağlantısı o aralığı grafikte seçer. Izgaranın yanında aralıktaki
en sık beş hata, kaç kez ve ne zaman görüldükleriyle listelenir. Son
kontrollerin tek tek dökümü "tablo olarak göster" altında durur. Izgara ham
sonuçlardan hesaplanır, yani `UPTIME_RETENTION` kadar geriye gider.

### Başarısız kontrolün ayrıntıları

Başarısız bir kontrol, hatasının yanında nedenine dair bulduklarını da
saklar (`diagnostics`); başarılı kontroller yeni bir şey saklamaz.
Ayrıntılar sonuçla birlikte `UPTIME_RETENTION` boyunca tutulur ve sonuçla
silinir; bir sonucunki en fazla 1 KiB'tır.

HTTP(S) ve WebSocket kontrolleri şunları saklar:

- **Zamanlama**: ad çözme (DNS), bağlantı, TLS el sıkışması, ilk bayta
  kadar geçen süre ve toplam süre, milisaniye. Yönlendirme izlendiyse
  aşamalar son isteğinkidir, toplam süre hepsini kapsar. Olmayan aşama boş
  kalır: IP adresinde ad çözme, yeniden kullanılan bağlantıda bağlantı ve
  el sıkışma, düz HTTP'de el sıkışma yoktur.
- Bağlanılan **adres** (`ip:port`); bağlanılamadıysa son denenen adres.
  Her pod'u ayrı kontrol eden monitor'da ayrıntılar başarısız olan ilk
  pod'undur ve pod'un adı da yazılır.
- **TLS**: sürüm, sertifikanın adı (CN), vereninin adı, geçerli olduğu ilk
  beş ad ya da adres (kalanlar sayılır) ve bitiş tarihi. Doğrulamadan
  geçemeyen sertifika da böyle anlatılır: hangi adlar için verildiği,
  sorunun kendisidir.
- **Durum satırı** (`HTTP/1.1 503 Service Unavailable`) ve izlenen
  yönlendirme sayısı.
- Yalnızca şu **başlıklar**: `content-type`, `content-length`, `server`,
  `via`, `location`, `retry-after`, `x-envoy-upstream-service-time`,
  `x-request-id`, `www-authenticate`.

Bilerek **saklanmayanlar**: yanıt gövdesi hiçbir zaman saklanmaz ve
gösterilmez. Beklenmeyen bir durum kodunda gövdenin yalnızca ilk 4 KiB'ı,
aşağıdaki tanınmış sayfaları ayırt etmek için bellekte okunur; sonuçta
yalnızca açıklamanın kodu kalır. `Set-Cookie`, `Authorization` ve listede
olmayan hiçbir başlık saklanmaz. `location` kullanıcı adı ve parolası,
sorgusu (`?…` olarak gösterilir) ve parçası olmadan; `www-authenticate`
yalnızca şemalarıyla (`Bearer`, `Basic`; realm ve hata açıklaması olmadan)
saklanır. Değerler 100 bayta kısaltılır, kontrol karakterleri atılır.
Monitor'u düzenleyebilen biri hedefi cluster içindeki herhangi bir servise
çevirebilir (bkz. "Hedef politikası"); sonuçlar o servisin yanıtını okumanın
bir yolu olmamalıdır. Bu sınırlar bu yüzden ayar değil, sabittir.

**Açıklama.** Başarısızlığın büyük olasılıkla ne anlama geldiği sabit bir
kodla ve kodun cümlesinin istediği birkaç değerle saklanır:
`explanation: {code, params, text}`. Arayüz kodu kullanıcının dilinde
yazar; `text` aynı cümlenin İngilizcesidir, kodu tanımayan istemciler
içindir. Kurallar `backend/internal/explain` paketindeki tablodadır ve
sırayla denenir, ilk eşleşen kazanır:

| Kod (`params`) | Ne zaman | Büyük olasılıkla |
| --- | --- | --- |
| `route_no_endpoints` | 503 ve OpenShift router'ın "Application is not available" sayfası | Host ve yolla eşleşen bir Route yok ya da Route'un Service'inde hazır pod yok |
| `ingress_default_backend` | ingress-nginx'in "default backend - 404" yanıtı | Host ve yolla eşleşen bir Ingress kuralı yok |
| `ingress_no_endpoints` | 503 ve nginx'in kendi hata sayfası | ingress-nginx arkasında Ingress'in Service'inde hazır pod yok |
| `proxy_no_healthy_upstream` (`proxy`) | 503 ve "no healthy upstream" (Envoy), "No server is available to handle this request" (HAProxy), "no available server" (Traefik) | Vekilin bu servis için sağlıklı bir arka ucu yok |
| `proxy_upstream_connect_error`, `proxy_upstream_tls_error` (`proxy`) | 503 ve Envoy'un "upstream connect error or disconnect/reset before headers" yanıtı; içinde "TLS error" geçerse ikincisi | Envoy servise bağlanamadı ya da bağlantı yanıt gelmeden koptu; ikincisinde iki tarafın TLS ya da mTLS ayarları uyuşmuyor |
| `mesh_access_denied` | 403 ve "RBAC: access denied" | Mesh'in yetkilendirme politikası (Istio AuthorizationPolicy) bu istemciye izin vermiyor |
| `http_to_tls_port` | 400 ve Go sunucularının "Client sent an HTTP request to an HTTPS server" ya da nginx'in "The plain HTTP request was sent to HTTPS port" yanıtı | TLS bekleyen porta düz HTTP gönderildi |
| `http_401`, `http_403`, `http_404`, `http_429`, `http_500`, `http_502`, `http_503`, `http_504` | Yukarıdaki sayfalardan biri değilse durum kodu | Kodun genel anlamı; 404'te Ingress ya da Route kuralı eşleşmiyor olabilir |
| `blocked_by_policy` | Hedef politikasının reddi | Adres yasak bir aralıkta; `UPTIME_TARGET_ALLOW_CIDRS` izin verebilir |
| `dns_service_not_found` (`service`, `namespace`) | `<service>.<namespace>.svc[...]` biçimindeki ad çözülmüyor | O namespace'te o adda Service yok; çoğu zaman adreste yanlış namespace |
| `dns_not_found` (`host`) | Başka bir ad çözülmüyor | Ad yanlış ya da gösterdiği şey yok |
| `dns_failed` | DNS sunucusu yanıt vermedi ya da hata döndü | Cluster DNS'i |
| `connection_refused` | Bağlantı reddedildi | Dinleyen yok: süreç çalışmıyor ya da port (Service'in `targetPort`'u) yanlış |
| `connect_timeout` | Bağlantı denemesine zaman aşımına kadar yanıt gelmedi | Trafiği bir NetworkPolicy ya da güvenlik duvarı düşürüyor veya pod ya da node yok |
| `response_timeout` | Bağlanıldı ama yanıt (TLS el sıkışması dahil) zaman aşımına kadar gelmedi | Uygulama yavaş, takılmış ya da aşırı yüklü |
| `timeout` | Zaman aşımı, hangi aşamada olduğu bilinmiyor | İkisinden biri |
| `connection_reset`, `connection_closed` | Bağlantı sıfırlandı ya da yanıt gelmeden kapandı | Süreç çöktü, yeniden başladı ya da başka bir protokol (TLS, HTTP/2) bekliyor |
| `host_unreachable` | Adrese yol yok | Arkasındaki pod ya da node gitmiş |
| `tls_unknown_authority`, `tls_hostname_mismatch` (`host`), `tls_expired`, `tls_certificate` | Sertifika doğrulanamadı | Güvenilmeyen bir CA, başka adlar için verilmiş, süresi dolmuş ya da başka bir sorun |
| `tls_to_plain_port` | TLS isteğine düz metinle yanıt geldi | Port TLS konuşmuyor |
| `tls_handshake_failed` | Sunucu el sıkışmayı reddetti | İstemci sertifikası (mTLS) istiyor ya da ortak bir TLS sürümü veya şifre yok |
| `too_many_redirects` | `max_redirects` aşıldı | Çoğu zaman bir döngü: TLS'i sonlandıran bir Route ya da Ingress arkasında HTTPS'e yönlendiren uygulama |

Sayfa ve durum kodu kuralları HTTP ve WebSocket kontrollerine uygulanır;
bağlantı, DNS ve TLS kuralları TCP, gRPC, DNS, ping, veritabanı ve
WebSocket kontrollerinin hatalarına da. gRPC hatalarını metin olarak verir;
gRPC monitor'unda bilinen ifadeler, Envoy'un bir gRPC durumuna yazdığı
"no healthy upstream" gibileri de dahil, bu metinde aranır. Push ve
workload monitor'larının hataları açıklanmaz: biri kaynağın kendi raporu,
öbürü cluster API'sinin söylediğidir.

Ayrıntılar `GET /api/v1/monitors/{id}/results` yanıtında, `/window`'un
başarısız kontrollerinde ve monitor'un `last_result` alanında
`diagnostics` olarak gelir; başarılı kontrolde ve söyleyecek bir şeyi
olmayan başarısızlıkta `null`dur. Monitor listesindeki kısa geçmiş
(`history`) onları içermez.

**Bildirimlerde.** Kesinti bildiriminin "Neden" alanı (Slack, Mattermost,
Google Chat, Teams, Discord, Telegram, Gotify, e-posta ve PagerDuty
ayrıntıları) hatanın arkasına açıklamayı `UPTIME_LANGUAGE` dilinde ekler:
"unexpected status 503 — OpenShift router bu isteği gönderecek bir yer
bulamadı: …". Tek satırlık metin (ntfy'nin mesajı dahil), hatırlatmalar,
gruplanmış mesajlar ve webhook gövdesindeki `incident.cause` yalnızca
hatayı yazar.

### Raporlar

**Raporlar** sayfası (`GET /api/v1/reports?days=30`) son 1, 7, 30 ya da 90
günü (`REPORT_PERIODS`) namespace başına özetler: uptime, kontrol sayısı, ortalama yanıt,
kesinti sayısı, toplam ve en uzun kesinti süresi, MTTR ve MTBF; her
namespace'te en düşük uptime'lı monitor en üsttedir. Hesap saatlik
özetlerden ve `down` türündeki olaylardan yapılır; döneme taşan kesintilerin
yalnızca dönem içindeki kısmı sayılır, süren kesinti şu ana kadar sayılır.
Kullanıcı yalnızca kapsamındaki namespace'leri görür. `format=csv` (sayfada
"CSV indir") monitor başına bir satır verir; ekiplere giden aylık rapora
eklenmek için. Sonradan eklenen sütunlar (`monitored_seconds`,
`mttr_seconds`, `mtbf_seconds`) sondadır, eski sütunların yeri değişmez.

**MTTR ve MTBF.** MTTR (ortalama düzelme süresi) dönemdeki toplam kesinti
süresinin kesinti sayısına, MTBF (arızalar arası ortalama süre) çalışır
geçen sürenin kesinti sayısına bölümüdür. Çalışır geçen süre, monitor'ün
kontrol edildiği sürenin (`monitored_seconds`: dönemde kontrolü olan
saatler; monitor oluşturulmadan önceki ve durdurulmuşken geçen süre
sayılmaz, içinde bulunulan saat şu ana kadar sayılır) kesinti dışında kalan
kısmıdır. Kesinti yoksa ikisi de `null` döner: ölçülecek arıza yoktur
("arıza yok"). Namespace değerleri namespace'in bütün monitor'larının
toplamlarından hesaplanır, monitor değerlerinin ortalamasından değil.

**Genel özet.** `GET /api/v1/stats?days=7` aynı hesabı kullanıcının
görebildiği bütün monitor'lar için tek bir özet olarak verir; genel bakış
içindir: uptime, kesinti sayısı ve süresi, MTTR, MTBF ve şu an açık olaylar
(`open_incidents`; ayrıca `open_outages` ve `open_cert_warnings`).
`namespace` (tekrarlanabilir; boş değer grupsuz monitor'lar) ile daraltılır.
`days` 1 ile 365 arasındadır, varsayılanı 7. `monitor_id` ile tek monitor'a
iner; monitor sayfası uptime özetlerinin yanındaki "MTBF · 7 gün" okumasını
(altında kesinti sayısı, kesinti yoksa "–") buradan alır, dönemi genel
bakışla aynı `STATS_DAYS`'tir.

Rapor ve olay listesi CSV'lerinde `=`, `+`, `-`, `@`, sekme ya da satır başı
karakteriyle başlayan hücrelerin önüne `'` konur: monitor adları, hedefler
ve hata metinleri kullanıcıların ve izlenen servislerin yazdığıdır; dosyayı
açan tablo programı onları formül olarak çalıştırmamalıdır.

## Pod'lar ve kaynak kullanımı

Service üzerinden yapılan bir kontrol her seferinde tek bir pod'a düşer;
arkadaki pod'lardan birkaçı bozukken kontrollerin çoğu yine başarılı olur.
Bu yüzden keşifle oluşturulmuş bir monitor'un sayfasında, o hedefin
arkasındaki pod'lar da gösterilir: kaçı hazır, her birinin durumu
(`CrashLoopBackOff` gibi), restart sayısı, yaşı ve node'u.

### Her pod'u ayrı kontrol etmek

Bir Service portundan keşfedilmiş `http` ya da `tcp` monitor'da
`"each_pod": true` (formda "Her pod'u ayrı kontrol et", annotation ile
`kubeat.io/each-pod: "true"`) seçilirse kontrol Service adresine değil,
EndpointSlice'taki hazır pod'ların her birine aynı anda yapılır. İstek
değişmez: aynı yol, aynı `Host` başlığı, aynı TLS sunucu adı; yalnızca
bağlantı pod'un adresine kurulur. Pod'lardan biri bile başarısız olursa
kontrol başarısız sayılır ve hata metninde hangi pod'ların neden düştüğü
yazar (`1 of 3 pods failed: web-7d9f (unexpected status 503)`); hepsi
geçerse ayrıntıda pod adları görünür. Gecikme en yavaş pod'unkidir.

"Ready görünüyor ama cevap vermiyor" durumunu yalnızca bu yakalar: pod'un
readiness probe'u geçerken gerçek iş yolu bozulmuş olabilir. Hazır olmayan
pod'lar kontrole girmez; onlar `workload` monitor'unun işidir. Chart'ın
ClusterRole'ü bunun için `discovery.k8s.io/endpointslices` listeleme izni
içerir.

Pod'ların altında cluster'ın o pod'lar, aynı yöneticinin yerine yenisi
gelmiş eski pod'ları ve onları yöneten ReplicaSet, Deployment, StatefulSet
ya da DaemonSet hakkında son `UPTIME_EVENT_WINDOW` içinde kaydettiği
olaylar listelenir (`BackOff`, `Unhealthy`, `FailedScheduling`,
`ScalingReplicaSet` …): bir restart'ın ya da başlamayan pod'un nedeni çoğu
zaman oradadır. Hangi pod'un kime ait olduğu pod'un `ownerReferences`'ından
okunur; `web`'in olayları `web-admin`'inkilerle karışmaz. Chart'ın
ClusterRole'ü bunun için `events` listeleme izni içerir; izin yoksa pod
listesi yine gösterilir.

Bu bilgi sayfa açıldığında cluster API'sinden canlı okunur; Kubeat bunu
saklamaz ve bunun için alarm üretmez. Replika eksikliği ya da sürekli
yeniden başlayan pod alarmları cluster'ın kendi izleme altyapısının işidir.

Route ve Ingress monitor'ları yönlendirdikleri Service üzerinden çözülür.

`UPTIME_PROMETHEUS_URL` ayarlanırsa her pod için CPU ve bellek kullanımı da
gösterilir. Değerler Prometheus uyumlu bir API'den okunur; OpenShift'te bu
Thanos Querier'dır:

```
helm upgrade --install kubeat deploy/helm/kubeat \
  --set prometheus.url=https://thanos-querier.openshift-monitoring.svc:9091 \
  --set prometheus.tokenFile=/var/run/secrets/kubernetes.io/serviceaccount/token \
  --set prometheus.caFile=/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt \
  --set prometheus.openshiftMonitoringView=true
```

9091 portu tüm cluster'ın metriklerini yanıtlar ve yalnızca
`cluster-monitoring-view` rolündeki servis hesaplarına açıktır:
`prometheus.openshiftMonitoringView=true` bu bağlamayı yapar ve 9091 ile
birlikte gereklidir; yoksa pod'ların CPU ve belleği boş kalır. Keşif
`discovery.namespaces` ile sınırlıysa onun yerine namespace başına yanıt
veren 9092 portu kullanılır (bkz. "Cluster-admin olmadan"). Keşif cluster
çapındayken 9091 bu ayar olmadan verilirse kurulum notları uyarır.

Token'ın gittiği sunucunun sertifikası `prometheus.caFile` ile doğrulanır;
`prometheus.insecureSkipVerify` token ile birlikte kullanılmamalıdır (bkz.
"Servis hesabı").

**Yükseltme notu:** Keşif ClusterRole'ü bu sürümle namespace'leri yalnızca
listeler, tek tek okuma (`get`) izni vermez. OpenShift 4.14 ve öncesinde
Thanos Querier'ın 9091 portu bu `get` iznini `cluster-monitoring-view`
yerine kabul ediyordu: `prometheus.openshiftMonitoringView=true` vermeden 9091
kullanan kurulumlarda yükseltmeden sonra pod'ların CPU ve belleği `403` ile
boş kalır. `prometheus.openshiftMonitoringView=true` verin (4.15 ve sonrasında
zaten gerekliydi).

Minikube'da denemek için `make minikube-demo` küçük bir Prometheus kurar;
ardından:

```
make minikube-deploy HELM_ARGS="--set prometheus.url=http://prometheus.monitoring-demo.svc:9090"
```

Varsayılan sorgular pod düzeyindeki cgroup serilerini okur. Ortamınızda
farklıysa `UPTIME_PROMETHEUS_CPU_QUERY` ve `UPTIME_PROMETHEUS_MEMORY_QUERY`
ile değiştirilebilir; sorguda `%NAMESPACE%`, `%PODS%` ve `%WINDOW%` yer
tutucuları kullanılır ve sonuç her pod için `pod` etiketli tek seri olmalıdır.

## Aracın kendi sağlığı

İzleyeni de izlemek gerekir. Bunun için iki mekanizma var.

**Metrikler.** Backend `/metrics` adresinde Prometheus biçiminde metrik
yayınlar. Başlıcaları:

| Metrik | Anlamı |
| --- | --- |
| `kubeat_monitor_up{monitor,monitor_namespace,type}` | Son kontrol başarılıysa 1, değilse 0 |
| `kubeat_monitor_response_seconds` | Son kontrolün süresi |
| `kubeat_monitor_incident_open` | Açık kesinti varsa 1 |
| `kubeat_monitor_slow` | Monitor yanıt veriyor ama `slow_above_ms` eşiğinden yavaşsa 1 (bkz. "Yavaş yanıt") |
| `kubeat_monitor_cert_expiry_timestamp_seconds` | Sertifikanın bitiş zamanı |
| `kubeat_scheduler_last_tick_timestamp_seconds` | Scheduler'ın son çalıştığı an; ilerlemiyorsa kontrol yapılmıyordur |
| `kubeat_scheduler_dispatch_skipped_total` | Boş worker olmadığı için bekleyen kontroller; artıyorsa `UPTIME_WORKERS` düşüktür |
| `kubeat_target_throttled_total{host}` | Hedef host, son kontrolün yönlendirildiği bir host ya da ulaştığı bir adres dakikalık kontrol tavanına (`UPTIME_TARGET_MAX_RATE`, `UPTIME_CLUSTER_TARGET_MAX_RATE`, `UPTIME_TARGET_ADDRESS_MAX_RATE`) takıldığı için beklemeye başlayan kontroller, monitor'un kendi host'u etiketiyle; her bekleme kaç tick sürerse sürsün bir kez sayılır; ilk 100 host kendi etiketiyle, sonrakiler `other` olarak sayılır (bkz. "Hedef yükü") |
| `kubeat_results_dropped_total` | Veritabanına yazılamayan sonuçlar |
| `kubeat_notifications_total{channel_type,event,result}` | Bildirim gönderimleri |

Chart'ta `metrics.serviceMonitor.enabled=true` bir ServiceMonitor oluşturur;
böylece monitor durumları cluster'ın kendi Prometheus'unda sorgulanabilir
(OpenShift konsolunda Observe → Metrics). Metrikler bir anahtar ister
(`metrics.auth.enabled`, varsayılan açık); ServiceMonitor onu kendisi
gönderir, elle yapılandırılmış bir Prometheus'un da göndermesi gerekir (bkz.
"Metrik anahtarı"). İzlenen nesnenin namespace'i
`monitor_namespace` etiketindedir; `namespace` etiketini Prometheus kendisi
koyar ve Kubeat'in çalıştığı namespace'i gösterir (OpenShift'in user
workload monitoring'i bunu zorunlu tutar).

**Cluster'ın Alertmanager'ı ile alarm.** `metrics.prometheusRule.enabled=true`
bir PrometheusRule ekler; alarmlar cluster'ın kendi Alertmanager'ına ve
oradaki alıcılara (PagerDuty, Slack, e-posta…) gider, OpenShift konsolunda
Observe → Alerting altında görünür. Kubeat'e ayrı bir kanal tanımlamak
gerekmez; namespace'e göre yönlendirme Alertmanager'ın kendi kurallarıyla
yapılır. Kurallar:

| Alarm | Koşul |
| --- | --- |
| `KubeatMonitorDown` | `kubeat_monitor_incident_open == 1`; etiketlerinde `monitor`, `monitor_namespace`, `type` ve `severity` (`metrics.prometheusRule.severity`, varsayılan `warning`) |
| `KubeatCertificateExpiring` | Sertifikanın bitişine `metrics.prometheusRule.certWarningDays` (14) günden az kaldı |
| `KubeatDown` | Backend 5 dakikadır scrape edilemiyor (`critical`); bu sırada `KubeatMonitorDown` alarmları susar, çözülmez |
| `KubeatSchedulerStalled` | Scheduler 5 dakikadır ilerlemiyor (`critical`) |
| `KubeatMetricsIncomplete` | Monitor metrikleri veritabanından okunamıyor |

Her iki nesne de prometheus-operator CRD'lerini ister; OpenShift'te user
workload monitoring açık olmalıdır (`enableUserWorkload: true`).

**Heartbeat (dead man's switch).** `UPTIME_HEARTBEAT_URL` ayarlanırsa backend
bu adrese her `UPTIME_HEARTBEAT_INTERVAL` sürede bir `GET` atar. Karşı taraf
(healthchecks.io, Uptime Kuma push monitor vb.) istekler kesilince alarm
verir. Heartbeat yalnızca scheduler son aralıkta çalıştıysa gönderilir; süreç
ayakta ama kontrol yapmıyorsa da istekler kesilir. Bu servisin izlenen
cluster'ın dışında olması gerekir, yoksa ikisi birlikte düşer.

## Yedekleme ve geri yükleme

`UPTIME_BACKUP_DIR` ayarlanırsa backend veritabanının tutarlı bir kopyasını
`UPTIME_BACKUP_INTERVAL` (varsayılan 24 saat) aralıkla o dizine yazar ve en
yeni `UPTIME_BACKUP_KEEP` (varsayılan 7) kopyayı tutar. Kopya araç çalışırken
alınır; kontroller ve sonuçların yazılması bu sırada sürer. Her kopya
açılıp şema sürümü doğrulandıktan sonra saklanır. Dosyalar
`kubeat-20261012T030000Z.db` biçiminde adlandırılır.

Yeniden başlatma ek kopya üretmez: yeni kopya, en son kopyanın üzerinden
aralık kadar zaman geçince alınır.

Eski kopyalar yeni kopya yazıldıktan sonra değil, **önce** silinir: her
kopyadan önce dizin `UPTIME_BACKUP_KEEP - 1` kopyaya indirilir, böylece
yeni kopya en eskisinin yerine sığar ve volume dolunca yedekleme kalıcı
olarak durmaz. Yazım buna rağmen yer kalmadığı için (`ENOSPC`) başarısız
olursa en eski kopya silinip yazım bir kez daha denenir; log nedenini
söyler (`backup volume full; removed the oldest copy and retried`, WARN).
`UPTIME_BACKUP_KEEP=1` ile tek kopya önceden silinmez: önce yazmayı dener,
yalnızca volume doluysa tek kopyayı silip yeniden dener; yazım başarılı
olana kadar kısa bir süre yedeksiz kalınır ve bu WARN ile loglanır.
Başarısız bir deneme dizini `keep - 1` kopyada bırakabilir.

Yedek volume'u en az **(keep + 1) × veritabanı boyutu** olmalıdır: dizinde
en çok `keep` kopya bulunur ve veritabanı büyüdükçe her yeni kopya bir
öncekinden büyüktür; fazladan bir kopyalık yer bu payı verir. Chart'ın
varsayılanları (`backup.keep=7`, `backup.persistence.size=5Gi`) yaklaşık
640 MiB'a kadar bir veritabanı için yeter (5 Gi / 8); kopya veritabanının
boşluğu alınmış hâlidir, ondan biraz küçüktür.

Bir kopya silmek de yer açmıyorsa (veritabanı volume'dan büyük; paylaşılan
bir PVC'yi ya da thin-provisioned havuzu başka bir kiracı doldurmuş) son
kopya korunur: `keep ≥ 2` iken her deneme en fazla bir eski kopya siler,
son kopyaya dokunmaz (`backup volume full; keeping the only copy, no
retry`, WARN); yedekleme durur, her deneme ERROR loglar ve
`kubeat_backups_total{result="failed"}` sayacını artırır. `keep=1`'de tek
kopya da gidebilir. Dolu kalan volume bir dakika sonra değil, bir aralık
(`UPTIME_BACKUP_INTERVAL`) sonra yeniden denenir: volume bir dakikada
boşalmaz ve her deneme bir kopyaya mal olabilir; yeniden başlatma hemen
dener. Çözüm volume'u büyütmektir. Yarım kalan bir kopyanın `.partial`
dosyası (örneğin kopya sırasında pod yeniden başladıysa) bir sonraki
denemede silinir, yer tutmaz.

Yedek dizini veritabanından **ayrı bir volume** olmalıdır; aynı volume'daki
kopya veritabanıyla birlikte kaybolur. Chart'ta `backup.enabled=true` bunun
için ayrı bir PVC oluşturur. Bu yine aynı cluster'dadır: cluster'ın
tamamını kaybetmeye karşı kopyaların dışarıya da taşınması gerekir.

Kopyaları cluster dışına almak için (backend imajında `tar` olmadığından
`kubectl cp` çalışmaz):

```sh
kubectl exec deploy/kubeat-backend -- ls /backup
kubectl exec deploy/kubeat-backend -- cat /backup/kubeat-20261012T030000Z.db > kubeat-20261012T030000Z.db
kubectl get secret kubeat-secret-key -o jsonpath='{.data.key}' | base64 -d > kubeat-secret.key
```

Anahtar dosyasını kopyalarla birlikte ama ayrı ve erişimi kısıtlı bir yerde
saklayın. Kopyadaki gizli değerler, kopya alındığı sırada kullanılan
`UPTIME_SECRET_KEY` ile şifrelidir; anahtarı olmayan bir kopya işe yaramaz.

Geri yükleme bir yardımcı pod ile yapılır. Komutlar release adının `kubeat`
olduğunu varsayar; değilse `kubeat-data`, `kubeat-backup` ve
`kubeat-secret-key` adlarındaki ön eki değiştirin. Yardımcı pod backend
imajını kullanır, ek bir imaj çekilmez:

```yaml
# kubeat-restore.yaml: image, kurulumdaki backend imajı olmalıdır
# (kubectl get deploy kubeat-backend -o jsonpath='{..image}')
apiVersion: v1
kind: Pod
metadata:
  name: kubeat-restore
spec:
  restartPolicy: Never
  containers:
    - name: restore
      image: ghcr.io/yusufceng/kubeat-backend:<sürüm>
      command: ["sleep", "3600"]
      securityContext:
        allowPrivilegeEscalation: false
        runAsNonRoot: true
        capabilities: { drop: ["ALL"] }
        seccompProfile: { type: RuntimeDefault }
      volumeMounts:
        - { name: data, mountPath: /data }
        - { name: backup, mountPath: /backup }   # yeni kurulumda bu ikisini çıkarın
  volumes:
    - name: data
      persistentVolumeClaim: { claimName: kubeat-data }
    - name: backup                               # yeni kurulumda bu ikisini çıkarın
      persistentVolumeClaim: { claimName: kubeat-backup }
```

Aynı kurulumda, yedek volume'undaki bir kopyaya dönmek:

1. Backend'i durdurun ve pod'un gitmesini bekleyin:
   `kubectl scale deploy/kubeat-backend --replicas=0`.
2. `kubectl apply -f kubeat-restore.yaml`, ardından
   `kubectl wait --for=condition=Ready pod/kubeat-restore`.
3. Kopyayı yerine koyun:
   `kubectl exec kubeat-restore -- sh -c 'cp /backup/kubeat-20261012T030000Z.db /data/uptime.db.restore && mv /data/uptime.db.restore /data/uptime.db'`.
4. `kubectl exec kubeat-restore -- ls /data` ile bakın: backend düzgün
   kapandıysa yalnızca `uptime.db` vardır. Yanında `uptime.db-wal` ve
   `uptime.db-shm` kaldıysa (backend çökmüşse) bu ikisini silin; yoksa eski
   veritabanının yazılmamış sayfaları geri yüklenen kopyanın üzerine
   uygulanmaya çalışılır.
5. `kubectl delete pod kubeat-restore`, sonra
   `kubectl scale deploy/kubeat-backend --replicas=1`.

Cluster ya da namespace kaybolduysa, yeni bir kuruluma:

1. Saklanan anahtarla bir Secret oluşturun ve chart'ı onunla kurun:
   `kubectl create secret generic kubeat-restored-key --from-file=key=kubeat-secret.key`,
   `helm install kubeat ... --set secretKey.existingSecret=kubeat-restored-key`.
   Yedeklemeyi de açın (`backup.enabled=true`).
2. Backend hazır olunca (boş bir veritabanı oluşturur) yukarıdaki 1. ve 2.
   adımları uygulayın; yardımcı pod'da `backup` volume'u gerekmez.
3. Bilgisayardaki kopyayı yazın:
   `kubectl exec -i kubeat-restore -- sh -c 'cat > /data/uptime.db.restore && mv /data/uptime.db.restore /data/uptime.db' < kubeat-20261012T030000Z.db`.
4. Yukarıdaki 4. ve 5. adımlar.

Kopyadan sonraki kontrol sonuçları ve değişiklikler kaybolur. Eski bir
sürümün kopyası yeni sürümde açılır ve şeması yükseltilir; yeni bir sürümün
kopyası eski sürümde açılmaz. Anahtar yanlışsa backend başlamaz. Prova
kaydı ve hedef süreler: [docs/yedek-geri-yukleme.md](docs/yedek-geri-yukleme.md).

`kubeat_backup_last_success_timestamp_seconds` metriği son başarılı kopyanın
zamanını verir; ilerlemiyorsa yedekleme çalışmıyordur.

## Keşif

Backend cluster içinde çalışırken pod'un servis hesabıyla cluster'a bağlanır
ve arayüzdeki **Keşif** sayfasında seçilen namespace'teki izlenebilir
hedefleri listeler. Seçilenler tek adımda monitor'a çevrilir ve ana sayfada
namespace'e göre gruplanır. Erişim yalnızca okuma amaçlıdır.

| Kaynak | Önerilen kontrol |
| --- | --- |
| Route (OpenShift) | Route adresine HTTP |
| Ingress | Her host ve yol için HTTP |
| Service, pod'unda HTTP probe var | Service üzerinden probe'un yoluna HTTP |
| Service, pod'unda gRPC probe var | Service üzerinden gRPC sağlık kontrolü, probe'un sorduğu servis adıyla |
| Service, ikisi de yok | Porta TCP bağlantısı |
| Deployment, StatefulSet, DaemonSet | Hazır pod sayısı (`workload`); seçili gelmez |

Service için yol tahmin edilmez: uygulamanın kendi readiness (yoksa liveness)
probe'u neyin "sağlıklı" anlamına geldiğini zaten söyler; rastgele bir yol 404
dönüp yanlış alarm üretir.

Denemek için `make minikube-demo` örnek bir `shop-demo` namespace'i kurar.

Cluster dışında çalışırken keşif için hem `UPTIME_KUBECONFIG` hem
`UPTIME_KUBE_CONTEXT` verilmelidir; kubeconfig'in o anki bağlamı bilerek
kullanılmaz.

### Cluster-admin olmadan: namespace'le sınırlı keşif

Varsayılan kurulum keşif için bir ClusterRole oluşturur; bunu yalnızca
cluster yöneticisi kurabilir. Yalnızca kendi namespace'lerini yöneten bir
ekip Kubeat'i şöyle kurar:

```
helm upgrade --install kubeat oci://ghcr.io/yusufceng/charts/kubeat -n ekibim \
  --set route.enabled=true \
  --set 'discovery.namespaces={ekibim,ekibim-test}' \
  --set prometheus.url=https://thanos-querier.openshift-monitoring.svc:9092 \
  --set prometheus.tokenFile=/var/run/secrets/kubernetes.io/serviceaccount/token \
  --set prometheus.caFile=/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt
```

`discovery.namespaces` verilince chart cluster çapında hiçbir şey
oluşturmaz: sayılan her namespace'e salt okunur bir Role ve RoleBinding
koyar, backend de yalnızca o namespace'leri görür (`UPTIME_DISCOVERY_NAMESPACES`).
Keşif, annotation eşitlemesi, pod bazlı kontrol, workload monitor'lar ve
olaylar bu namespace'lerde aynen çalışır.

Pod CPU/belleği için Thanos Querier'ın **9092** portu kullanılır: bu uç
her sorguda bir namespace ister ve yalnızca o namespace'i yanıtlar; Role'deki
`metrics.k8s.io/pods` izni yeterlidir, `cluster-monitoring-view` gerekmez.
(Cluster çapında kurulumda 9091 portu ve `prometheus.openshiftMonitoringView`
kullanılır.) Bu kurulum gerçek OpenShift 4.21'de doğrulandı.

### Annotation ile sürekli keşif

Keşif sayfasından içe aktarmak tek seferliktir. Ekiplerin Kubeat'e hiç
girmeden izlenmesini istemesi için nesneye bir annotation yeter:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: odeme-api
  annotations:
    kubeat.io/monitor: "true"
    kubeat.io/port: "http"            # isteğe bağlı; yoksa her TCP port
    kubeat.io/path: "/healthz"        # isteğe bağlı; porta HTTP isteği yapılır
    kubeat.io/interval: "20s"         # isteğe bağlı
    kubeat.io/timeout: "3s"           # isteğe bağlı
    kubeat.io/failure-threshold: "2"  # isteğe bağlı
```

Backend her `UPTIME_DISCOVERY_SYNC_INTERVAL`'da (varsayılan 1 dk) cluster'ı
okur ve `kubeat.io/monitor: "true"` taşıyan Service, Ingress, Route,
Deployment, StatefulSet ve DaemonSet'ler için monitor oluşturur. Kontrol,
keşif sayfasındaki tabloyla aynı kuralla seçilir; `kubeat.io/path` verilirse
Service portuna o yola HTTP isteği yapılır. Annotation kaldırılınca ya da
nesne silinince monitor, geçmişiyle birlikte kaldırılır; hedef ya da
annotation değişince monitor güncellenir. Cluster okunamazsa hiçbir şey
değiştirilmez. Eşitleme "Hedef yükü" bölümündeki yük sınırlarına tabidir:
dış bir Ingress ya da Route host'u için 30 saniyeden kısa bir
`kubeat.io/interval` (ya da 30 saniyenin altında bir
`UPTIME_DISCOVERY_DEFAULT_INTERVAL`), dolu bir host, namespace ya da kurulum
kotası monitor'un oluşturulmamasına (ya da hedef değişikliğinin
uygulanmamasına) yol açar; nesne keşif durumunda API'nin cümlesiyle atlanmış
görünür. Var olan yönetilen bir monitor bu yüzden silinmez: dış sınırdan
önce oluşturulmuş daha hızlı bir monitor aralığıyla birlikte olduğu gibi
kalır ve atlananlar listesinde "kept at 10s" notuyla görünür. Hesap başına
oluşturma hızı eşitlemeye uygulanmaz.

Bu monitor'lar arayüzde **annotation** rozetiyle görünür. Hedefleri ve
zamanlamaları nesneden geldiği için arayüzden ya da API'den değiştirilemez
(`409`); etiket, açıklama, duraklatma ve bildirimler değiştirilebilir.

Böyle bir monitor'a sonradan header'lar, kimlik bilgisi (`auth`) ya da
istek gövdesi (`body`) eklenmişse, onlar da yalnızca girildikleri sunucuya
gider. Nesne monitor'u başka bir sunucuya götürürse (Route'un host'u
değişir, Ingress ya da Route TLS'i bırakıp `http://`'ye döner),
senkronizasyon hedefi günceller ama header değerlerini, `auth_secret`'ı,
istek gövdesini ve veritabanı parolasını siler; denetim kaydına neyin
silindiğini yazar. Değerler yeni sunucu için yeniden girilmelidir. Yalnızca
yolun değişmesi ve nesneye TLS eklenmesi (aynı host'ta `http://`'den
(port 80) `https://`'e (port 443) geçiş) aynı sunucu sayılır; kendi
portunda TLS'e geçen bir readiness probe'u gibi başka geçişler taşınma
sayılır.

Böyle bir monitor **silinebilir**. Nesnesindeki annotation yerinde kaldığı
için Kubeat silmeyi hatırlar ve monitor'ü yeniden oluşturmaz; keşif
sayfasında "Kubeat'te silindi" satırıyla görünür ve oradaki **Yeniden izle**
(`POST /api/v1/discovery/restore`) ile geri alınır. Annotation nesneden
kaldırıldığında bu kayıt da silinir: sonradan yeniden eklenirse monitor
baştan oluşturulur. Annotation'ı GitOps ile yönetiyorsanız kalıcı çözüm
onu kaynağından kaldırmaktır.

Keşif sayfasından elle içe aktarılmış bir nesneye
annotation eklenirse elle yapılan monitor'a dokunulmaz; durum keşif
sayfasında "already monitored by hand" olarak görünür. Her değişiklik
denetim kaydına `discovery` adıyla yazılır.

## Alarm

Bir monitor art arda `failure_threshold` kez başarısız olursa olay açılır;
art arda `UPTIME_RECOVERY_THRESHOLD` kez başarılı olunca kapanır. Her iki
durumda etkin tüm kanallara bir bildirim gider (gecikmeli bir kanal
kesintiyi ancak gecikmesini aşarsa duyar; bkz. "Gecikme ve tekrar"); olay
açıkken tekrar bildirim gönderilmez.

HTTPS hedeflerde her kontrol sertifikanın bitiş tarihini de kaydeder. Bitişe
`UPTIME_CERT_WARNING` (varsayılan 14 gün) kala ayrı bir "sertifika" olayı
açılır ve bildirim gider; sertifika yenilenince olay kapanır. Bu olay
kesintilerden bağımsızdır: hedef çalışırken de açık olabilir.

### Hatırlatma

`UPTIME_REMINDER_INTERVAL` ayarlanırsa süren bir kesinti bu aralıkla yeniden
bildirilir ("hâlâ erişilemiyor"). Varsayılan `0` olup hatırlatma gönderilmez.
Bakım penceresi içinde hatırlatma gitmez. Bir kanalın kendi aralığı
(`repeat_seconds`) varsa o kanal genel aralığı değil kendininkini izler
(bkz. "Gecikme ve tekrar").

### Yavaş yanıt

Bir monitor'e `slow_above_ms` verilirse, art arda `failure_threshold`
kontrol başarılı olup her biri bu eşikten uzun sürdüğünde monitor **yavaş**
sayılır: API'de `status` alanı `degraded` olur, `slow_since` serinin ilk
yavaş kontrolünü gösterir ve `kubeat_monitor_slow` metriği 1'e çıkar.
Art arda `UPTIME_RECOVERY_THRESHOLD` kontrol eşiğin altında kalınca durum
biter. Başarısız kontroller bu durumu değiştirmez: hedef düşmüşse zaten
`down`'dır ve kesintinin hikâyesi olayındadır; düzelince yavaşlık yeniden
değerlendirilir.

Olay **açılmaz**; durum monitor'ün kendisinde tutulur
(`monitors.slow_since`), böylece liste ve genel bakış onu ucuza okur. Her
geçiş bir bildirimdir: `slow` ("yavaş yanıt veriyor", eşik ve son yanıt
süresiyle) ve `slow_resolved` ("yeniden zamanında yanıt veriyor", ne kadar
yavaş kaldığıyla). Kanallar bunları `events` ile seçebilir; `events` boş
kanal her şeyi duyduğu gibi bunları da duyar. Diğer olaylar gibi gruplanır,
bakım penceresinde yavaşlık başlatılmaz (süren yavaşlık pencere içinde
bitebilir ve bildirilir). Bir olaya bağlı olmadığından **bildirim kaydına
yazılmaz**: olay sayfası yoktur, kanalın son gönderim durumu yine
güncellenir. Eşiği `0` yapmak ya da monitor'ü durdurmak durumu bildirim
göndermeden siler.

Yanıt süresi kontrolün ölçtüğü süredir: HTTP'de isteğin tamamı, TCP'de
bağlantı (ve TLS), DNS'te sorgu, push'ta kaynağın `ping` ile bildirdiği
değer (bildirmezse hiç yavaş sayılmaz).

**Arayüzde.** Eşik monitor formunun "Zamanlama ve alarm" bölümünde "Yavaş
yanıt eşiği" olarak girilir (ms; yardım metni ne olacağını okur, zaman
aşımından uzun bir değeri kırmızıyla söyler). Yavaş monitor her yerde
**Sorunlu**'dur: listede ve genel bakışta doğrulanan ve toparlananlarla aynı
yarım daire ve sayı, monitor sayfasında "Sorunlu · 12 dk · yanıtlar 800
ms'den uzun" çipi (süre `slow_since`'ten sayılır) ve altında eşiği ile son
`failure_threshold` kontrolün sürelerini sayan kehribar bir satır. Olay
açılmadığından olay listesine düşmez.

### Bakım pencereleri ve susturma

Bir bakım penceresi boyunca kapsamındaki monitor'lar kontrol edilmeye ve
geçmişe yazılmaya devam eder, ama onlar için yeni olay açılmaz ve bildirim
gitmez. Pencereden önce açılmış bir olay pencere içinde kapanabilir ve
"tekrar çalışıyor" bildirimi gider. Pencere bittiğinde hedef hâlâ
erişilemiyorsa olay o zaman açılır.

Kapsam tek bir monitor, bir namespace ya da tüm monitor'lar olabilir. Monitor
sayfasındaki "Alarmı sustur" düğmesi, o monitor için şimdi başlayan bir
pencere oluşturur. Bakım sırasındaki başarısız kontroller uptime yüzdesine
dahildir.

**Tekrarlayan pencereler.** Bir pencere her gün, her hafta (aynı gün) ya da
her ay (ayın aynı günü) tekrarlanabilir: `repeat` alanı `daily`, `weekly` ya
da `monthly`. Verilen başlangıç ve süre ilk tekrarı tanımlar; sonrakiler
`timezone` saat diliminde aynı saatte başlar, yani yaz/kış saati değişse de
02:00'deki pencere 02:00'de kalır. Arayüz saat dilimini tarayıcıdan alır.

- Pencere, iki tekrar arasındaki süreden kısa olmalıdır (günlük için 24
  saatten, haftalık için 7 günden, aylık için 28 günden).
- Aylık bir pencere, o günü olmayan aylarda atlanır (31'i olan pencere
  kasımda çalışmaz); başka bir güne kaydırılmaz.
- Listede tekrarlayan bir pencerenin başlangıcı ve bitişi, o an süren ya da
  sıradaki tekrarı gösterir. Silmek serinin tümünü kaldırır.

### Bildirim kanalları

| Tür | `target` | `secret` | Gönderilen |
| --- | --- | --- | --- |
| `slack` | Incoming webhook URL'i | – | `{"text": "..."}` |
| `mattermost` | Incoming webhook URL'i | – | `{"text": "..."}` |
| `googlechat` | Alan webhook URL'i | – | `{"text": "..."}` |
| `teams` | Workflow webhook URL'i | – | Adaptive Card içeren mesaj |
| `discord` | Webhook URL'i | – | `{"content": "..."}` |
| `telegram` | Sohbet kimliği (`-100...` ya da `@kanal`) | Bot token | Bot üzerinden mesaj |
| `pagerduty` | – | Events API v2 integration key | Kesintide alarm açar, düzelince aynı alarmı kapatır |
| `ntfy` | Konu adresi (`https://ntfy.sh/konu`) | Erişim token'ı (isteğe bağlı) | Başlık, öncelik ve etiketle mesaj |
| `gotify` | Sunucu adresi | Uygulama token'ı | Başlık ve öncelikle mesaj |
| `email` | Virgülle ayrılmış adresler | – | Düz metin e-posta |
| `webhook` | Herhangi bir URL | – | Aşağıdaki JSON gövdesi |

Her kanal durdurulabilir, düzenlenebilir ve test edilebilir (`PATCH
/api/v1/channels/{id}`, `POST .../test`). Başarısız bir test yanıtın durum
kodunu ve sağlayıcının JSON yanıtında belgelediği nedeni gösterir (Discord
`message`, Telegram `description`, ntfy `error`, Gotify `errorDescription`,
PagerDuty `errors`). Yanıt gövdesinin kendisi gösterilmez: kanalın adresi
herhangi bir servisi gösterebilir ve test, oradaki yanıtı okumaya yaramamalıdır.

**Monitor'den test.** Monitor sayfasından da test gönderilir: `POST
/api/v1/monitors/{id}/test-notification`, monitor'u adıyla anan bir test
bildirimini alarmının gittiği her etkin kanala (aşağıdaki kapsamla, `GET
.../channels` ile aynı seçim) birer deneme olarak, yeniden denemeden
gönderir ve kanal başına sonucu döner: `channel_id`, `name`, `type`,
`delivered` ve başarısızsa `error`. Hata metni kanalın adresinden ve
token'ından arındırılır ve yalnızca kanalı yönetebilenlere verilir
(yöneticiler; kanalı kendi namespace'leriyle sınırlı editörler), diğerleri
yalnızca iletilmediğini görür. Kapsamı tutan kanal yoksa yanıt `422`'dir.
Monitor'ü değiştirebilenler kullanabilir; her çağrı **Kayıt**'a yazılır.
Kanalın gecikmesi teste uygulanmaz; test anında gider. Arayüzde monitor
sayfasının ⋯ menüsündeki **Bildirimi test et** bunu çağırır ve "Test
bildirimi: api" başlıklı bir pencerede kanal başına adı, türü ve iletilip
iletilmediğini (iletilmediyse nedenini, görebilenlere) listeler; monitor'u
duyan kanal yoksa öğe kapalıdır ve nedenini altında söyler.

**Kapsam.** Üç süzgeç birlikte uygulanır, hepsi isteğe bağlıdır:

- `namespaces`: yalnızca o namespace'lerdeki monitor'lar; boşsa hepsi. Her
  ekibe kendi namespace'inin alarmları gider, ortak bir kanal her şeyi görür.
- `tags`: yalnızca bu etiketlerden birini taşıyan monitor'lar; boşsa hepsi.
  Monitor başına yönlendirme böyle yapılır: PagerDuty kanalına `pager`
  etiketi verilir, yalnızca `pager` etiketli monitor'lar gece uyandırır.
- `events`: `down`, `up`, `reminder`, `cert`, `slow`, `slow_resolved` alt
  kümesi; boşsa hepsi. PagerDuty için `down` ve `up`, sohbet kanalı için
  hepsi gibi. `slow` ve `slow_resolved` için bkz. "Yavaş yanıt".

Monitor sayfası, o monitor'un alarmının hangi kanallara gittiğini yazar
(`GET /api/v1/monitors/{id}/channels`, her kanalın gecikme ve tekrar
değeriyle); kapsamı tutan etkin kanal yoksa sayfa bunu uyarı olarak
gösterir — kimsenin duymayacağı bir monitor olmasın.

**Gecikme ve tekrar.** İki süre kanal başına ayarlanır, ikisi de isteğe
bağlıdır ve yalnızca kesinti olaylarına (`down`, `up`, `reminder`) uygulanır;
sertifika ve yavaşlık bildirimleri anında gider.

- `delay_seconds`: kanal bir kesintiyi ancak bu kadar süre sonra **hâlâ
  sürüyorsa** duyar. Kesinti bu süre içinde biterse kanal ne kesintiyi ne
  düzelmeyi duyar; gecikmeden sonra duyduysa düzelmeyi de duyar. `0` anında
  (varsayılan); en çok 24 saat (86400).
- `repeat_seconds`: kanalın, duyduğu bir kesinti için kendi hatırlatma
  aralığı; kanala söylendiği andan itibaren bu aralıkla "hâlâ erişilemiyor"
  gider ve kanal genel `UPTIME_REMINDER_INTERVAL` hatırlatmalarını almaz.
  `0` genel aralığı izler (varsayılan); verilirse en az 1 dakika (60), en
  çok 7 gün (604800).

Böylece bir **kademeli alarm** kurulur: Slack kanalı anında (`0`), PagerDuty
kanalı 10 dakika sonra hâlâ sürüyorsa (`delay_seconds: 600`), e-posta
kanalı saatte bir hatırlatarak (`repeat_seconds: 3600`):

```
curl -X POST localhost:8090/api/v1/channels -d '{"name": "ops-slack", "type": "slack", "target": "https://hooks.slack.com/services/..."}'
curl -X POST localhost:8090/api/v1/channels -d '{"name": "nobetci", "type": "pagerduty", "secret": "...", "delay_seconds": 600}'
curl -X POST localhost:8090/api/v1/channels -d '{"name": "ekip-posta", "type": "email", "target": "ekip@ornek.com", "repeat_seconds": 3600}'
```

Beş dakikada düzelen bir kesintiyi Slack ve e-posta duyar, nöbetçi
uyandırılmaz. Yarım saat süren bir kesintide nöbetçi onuncu dakikada çağrılır
ve düzelince alarmı kapatılır; e-posta kesintiyi, birinci saatte bir
hatırlatmayı (sürseydi) ve düzelmeyi alır. Yazılımın okuduğu kanallar
(`webhook`, `pagerduty`) da gecikebilir; gecikmeli bildirim o kanala
giderken yine gruplanmaz ve kendi kaydıyla gider. İnsanların okuduğu
kanallarda aynı anda süresi dolan gecikmeli bildirimler (birlikte düşen
monitor'lar) tek mesajda toplanır. Gecikmesi bakım penceresi içinde dolan
kesinti o kanala hiç söylenmez; kanalın kendi hatırlatması pencere içinde
atlanır. Her gönderim bildirim kaydına her zamanki gibi yazılır; gecikmeli
kesinti mesajının olay anı kesintinin açıldığı andır.

Bu takvim alarm motorunun belleğindedir ve kesinti düzelince, o sırada
kapalı ya da kapsam dışı kalan kanallar dahil her kanal için iptal edilir.
Kesintiyi duyan kanal, bu arada ayarları değişse ya da hatırlatma almasa da
(örneğin `events: ["down", "up"]`) düzelmeyi duyar. Backend yeniden
başlarsa takvim açık kesintilerden ve bildirim kaydından yeniden kurulur:
gecikmesi dolmamış bildirim zamanı gelince gider; gecikmesi dolmuş olanın
söylenip söylenmediğine bildirim kaydından bakılır — söylenmişse kanalın
sonraki hatırlatması kendi temposunda kalır (tempo kayıttaki gönderim
anından sayılır: geç söylenmişse o andan), yeniden başlatma sırasında
kaçmışsa kesinti hâlâ sürüyor olduğundan yeni başlangıçta söylenir.
Gecikmesi bakım penceresi içinde dolduğu için söylenmemiş kesinti ise
yeniden başlatmadan sonra da söylenmez: kaydı yoktur ama kaçırılmamış,
susturulmuştur. Yalnızca yeniden başlatma sırasında zamanı gelen bir
hatırlatma atlanır; sonraki kendi zamanında gider.

Arayüzde kanal formunun **Zamanlama** bölümü iki süreyi sayı ve birimle
(saniye, dakika, saat, gün) alır ve ne olacağını cümle olarak geri okur:
"Kesinti 10 dk sürerse haber verir; sonra her 1 sa tekrar hatırlatır",
ikisi de sıfırken "Hemen; tekrar genel ayar". Kanal listesi kapsamın altında
"10 dk gecikmeli · her 1 sa" yazar; monitor sayfasındaki kanal çipinin
açıklaması da her kanalın zamanlamasını söyler.

**Gruplama.** Aynı namespace'te aynı türden olaylar
`UPTIME_NOTIFY_GROUP_WAIT` (30 sn) içinde art arda açılırsa, insanların
okuduğu kanallara (Slack, Teams, Discord, Mattermost, Google Chat, Telegram,
ntfy, Gotify, e-posta) **tek mesaj** gider: "shop: 5 monitor erişilemiyor:
odeme-api (unexpected status 503), …". Bir namespace'in tamamı düştüğünde
30 ayrı mesaj yerine bir mesaj okunur; düzelme ve hatırlatmalar da aynı
şekilde gruplanır. Yazılımın okuduğu kanallar (`webhook`, `pagerduty`)
hiçbir zaman gruplanmaz ve **beklemez**: olay açıldığı anda, kendi kaydıyla
giderler; nöbetçiyi çağıran sayfa gruplama beklemesine takılmaz. `0`
gruplamayı kapatır.

Bekleme sırasında bir monitor hakkında daha yeni bir haber gelirse (düzeldi,
yeniden düştü ya da yeni bir hatırlatma), o monitor'un kuyrukta bekleyen
**hatırlatması düşer**: "hâlâ erişilemiyor" mesajı, "tekrar çalışıyor"
mesajından sonra ulaşmaz. Kesinti ve düzelme mesajları hiçbir zaman
düşmez — kesinti ne kadar kısa sürerse sürsün ikisi de iletilir.

**Biçim.** Her sağlayıcı kendi zengin biçimini alır: Slack ve Mattermost
renk şeridi, başlık ve alanlar içeren bir attachment; Teams renkli bir
Adaptive Card ve "Kubeat'te aç" düğmesi; Discord renkli bir embed; Google
Chat bir card; Telegram kalın başlıklı HTML; Gotify markdown; ntfy
tıklanabilir bir eylem; PagerDuty olayda `client_url`; webhook gövdesinde
`url` alanı; e-posta son satırında bağlantı. Bağlantı tek bir olayda
olayın sayfasına (`/incidents/<id>`; bkz. "Olay sayfası"), gruplanmış
mesajda namespace sayfasına gider ve `UPTIME_PUBLIC_URL` (chart'ta
`publicURL`, Route/Ingress host'undan türetilir) ayarlı değilse hiç konmaz.
Kesinti bildiriminin "Neden" alanı, tanınan bir başarısızlıkta hatanın ne
anlama geldiğini de yazar (bkz. "Başarısız kontrolün ayrıntıları").

Monitor adları, hedefler ve hata metinleri (push raporunun mesajı dahil)
yazıldığı gibi görünür. Slack, Mattermost ve Google Chat'te `&`, `<` ve `>`
kaçışlanır: metindeki `<!channel>` ya da `<users/all>` kimseyi çağırmaz,
`<adres|etiket>` biçiminde, etiketi adresini gizleyen bir bağlantı oluşmaz.
Slack düz `@channel` yazımını metin olarak gösterir (Kubeat `link_names`
istemez) ve Slack metninde başka bir bağlantı yazımı yoktur. Mattermost ise
metni markdown olarak okur ve kendi bahsetmelerini de tanır: Mattermost'a
giden metinde her `@` ve `]` işaretinden sonra görünmez bir boşluk
(U+200B) bulunur. Böylece `@channel`, `@here`, `@all` ve bir kişinin ya da
grubun `@adı` kimseyi çağırmaz; `[etiket](adres)` ne bağlantıya ne de
istemcinin indireceği bir resme dönüşür. Discord mesajı kimseyi etiketlemez
(`@everyone` dahil); Telegram'a giden metin HTML olarak kaçışlanır. Teams
ve Gotify mesajlarındaki markdown ise kaçışlanmaz.

**Gizli değerler.** URL'ler API yanıtlarında maskelenir (`https://host/…`),
`secret` ise hiç dönmez; yalnızca `has_secret` bilgisi verilir. Güncellemede
maskelenmiş adres ya da `********` gönderilirse kayıtlı değer korunur. Gotify
ve ntfy token'ı adresteki sunucuya gönderir: adres başka bir sunucuya
(şeması, host'u ya da portu değişerek) taşınırsa kayıtlı `secret` korunmaz,
yeniden girilmesi gerekir (`400`). Hata mesajları adresi ve token'ı içermez.

E-posta için bir SMTP sunucusu tanımlanmalıdır (`UPTIME_SMTP_*`); tanımlı
değilse e-posta kanalı eklenemez. Varsayılan bağlantı güvenliği `starttls`
olup sunucu STARTTLS sunmuyorsa gönderim reddedilir, şifresiz gönderilmez.

Telegram ve PagerDuty internetteki servislerdir. Cluster'dan doğrudan çıkış
yoksa backend'e `HTTPS_PROXY` verilebilir ya da adresler
`UPTIME_TELEGRAM_API_URL` ve `UPTIME_PAGERDUTY_EVENTS_URL` ile kendi
vekilinize yönlendirilebilir. Bu adreslere ve vekile tam adresleriyle
güvenilir; loopback'te dinlemeleri istisna gerektirmez (bkz. "Hedef
politikası").

Genel webhook kanalına şu gövdeyle `POST` yapılır. `text` alanı insan tarafından
okunabilir özettir.

```json
{
  "event": "down",
  "text": "🔴 odeme-api erişilemiyor (https://...). Art arda 3 kontrol başarısız. Son hata: ...",
  "monitor": { "id": 1, "name": "odeme-api", "type": "http", "target": "https://..." },
  "incident": { "id": 7, "kind": "down", "started_at": "...", "resolved_at": null, "cause": "..." }
}
```

`event` değeri `down`, `up`, `reminder`, `cert_expiring`, `cert_renewed`,
`slow`, `slow_resolved` ya da `test` olur; `incident.kind` ise `down` ya da
`cert`. Yavaşlık olaylarında `incident` yoktur; yerine `slow` nesnesi
gelir: `{"since": "...", "threshold_ms": 500, "latency_ms": 1200}`.
Monitor'den gönderilen testte `monitor` nesnesi dolu, `incident` boştur.

### Olay sayfası

Her olayın kendi sayfası vardır (`/incidents/<id>`; API'de
`GET /api/v1/incidents/{id}`); bildirimlerdeki "Kubeat'te aç" bağlantısı
buraya açılır. Sayfa olayı, monitor'ünü (adı, namespace'i, tipi, hedefi,
kaynağı ve izlediği nesnenin türü: `service`, `ingress`, `route`,
`deployment`, `statefulset`, `daemonset`) ve olayın hikâyesini tek bir zaman
çizelgesinde, eskiden yeniye gösterir:

- serinin ilk başarısız kontrolü ve olay sürerken başarısız olanlar, hata
  metinleriyle; `UPTIME_INCIDENT_TIMELINE_LIMIT`'ten (100) fazlaysa baştan
  ve sondan yarısı gösterilir, aradaki atlananların sayısı yazılır;
- olayın açıldığı an: alarm eşiğine ulaşan kontrol (ilk başarısız
  kontrolden sonradır; bu kayıttan önce açılmış olaylarda ilk başarısız
  kontrol);
- her kanala yapılan her bildirim denemesi (aşağıda); bunlar da aynı
  sınırı aşarsa aynı biçimde kısaltılır;
- hatırlatmalar ve o ana kadar kesintinin ne kadar sürdüğü;
- notlar;
- cluster'ın monitor'ün pod'ları ve onları yöneten nesneler hakkındaki
  olayları, kesintinin başından bir kontrol aralığı önce başlayarak
  ("Kesinti anına gitmek"teki gibi; cluster olayları birkaç saat tutar);
- monitor'ü kapsayan bakım pencereleri;
- kapanış ve toplam süre.

Kontroller ham sonuçlardan (`UPTIME_RETENTION`), bildirimler bildirim
kaydından (`UPTIME_DELIVERY_RETENTION`) okunur; eski bir olayda yalnızca
kalanlar görünür. Sertifika olaylarında başarısız kontrol ve cluster olayı
yoktur. Kullanıcı yalnızca okuyabildiği namespace'lerin olaylarını görür;
diğerleri için sayfa 404'tür.

**Bildirim kaydı.** Alarm motoru her kanala yaptığı her gönderimi, yeniden
denemeler bittikten sonra olayın kaydına yazar: kanalın adı ve türü (kanal
sonradan silinse de kalır), bildirilen olay (`down`, `up`, `reminder`,
`cert_expiring`, `cert_renewed`) ve olduğu an, gönderimin bittiği an ve
iletilip iletilmediği. Yavaşlık bildirimleri (`slow`, `slow_resolved`) bir
olaya bağlı olmadığından kayda girmez; testler de girmez. Gruplanmış bir mesaj, kapsadığı her olaya ayrı yazılır
ve kaç monitor'ü kapsadığı da kaydedilir. Hata metni yazılmadan önce kanalın
adresinden ve token'ından arındırılır; sayfada yalnızca kanalı
yönetebilenlere (yöneticiler ve kanalı kendi namespace'leriyle sınırlı
editörler; kanal silinmişse yalnızca yöneticiler) gösterilir, diğerleri
yalnızca iletilmediğini görür. Kayıt `UPTIME_DELIVERY_RETENTION` (1 yıl)
boyunca tutulur ve `UPTIME_RETENTION_INTERVAL` sıklığıyla temizlenir; olay
silinince (monitor'üyle birlikte) kaydı da silinir.

**Notlar.** Monitor'ü değiştirebilen herkes olaya not ekler ("geri alındı,
14:05", "bilinen: veritabanı göçü"), çözülmüş olaylara da: notlar olay
sonrası değerlendirmenin kaydıdır. Bir not en çok 4000 karakterdir ve satır
sonları korunur. Notu yalnızca yazarı ya da bir yönetici düzenler veya
siler; yazarın hesabı silinirse not kalır, yalnızca yöneticiler değiştirir.
Ekleme, düzenleme ve silme **Kayıt**'a yazılır. `show_on_status_page`
işaretli notlar, monitor'ü gösteren durum sayfalarında gösterilmek içindir.

**Olay listesi.** `GET /api/v1/incidents` süzgeçleri birlikte uygular:

- `namespace`: tekrarlanabilir (`namespace=shop&namespace=billing`); boş
  değer grupsuz monitor'ları seçer. Kullanıcı yalnızca kapsamındakileri
  görür; kapsamı dışındaki bir namespace boş sonuç verir.
- `kind`: `down` ya da `cert`; `state`: `open` ya da `resolved`;
  `monitor_id`; `q`: monitor adında geçen metin (harf büyüklüğü önemsiz).
- `from`, `to` (RFC 3339): bu aralıkla kesişen olaylar.

Sıralama `sort` ile: `started_at` (varsayılan, yeniden eskiye), `duration`
(en uzun önce; süren olay şu ana kadar) ya da `monitor` (A'dan Z'ye);
`order=asc` ya da `desc` yönü değiştirir. Sayfa `limit` (varsayılanı ve
üst sınırı `UPTIME_MAX_RESULTS`) ve `offset` ile seçilir; eşleşen olayların
toplamı `X-Total-Count` başlığındadır. `format=csv` eşleşen olayların
hepsini (`limit` verilmedikçe) dosya olarak verir.

Arayüzdeki **Olaylar** listesi bu süzgeçleri adreste tutar (bir süzgeçli
liste bağlantı olarak paylaşılabilir); zaman süzgecinin "son …" seçenekleri
`INCIDENT_RANGES`, sayfa uzunluğu `INCIDENTS_LIMIT` ile ayarlanır. "CSV
indir" o an seçili süzgeçle eşleşen bütün olayları, listenin sırasıyla
indirir.

## Kubeat Enterprise lisansı

Kubeat lisanssız da tam bir izleme aracıdır: monitor'ler, keşif, alarmlar,
bildirim kanalları, raporlar ve bir durum sayfası lisans istemez. Kubeat
Enterprise lisansı şu özellikleri açar:

| Özellik | Lisanssız |
| --- | --- |
| `sso`: LDAP ve OIDC ile giriş, dizin gruplarını rollere eşlemek | Ayarlı olsa da kapalıdır; yalnızca yerel hesaplar girer |
| `scoped_access`: kullanıcıları ve grupları namespace'lerle sınırlamak | Her rol tüm namespace'leri (`*`) görür |
| `audit`: denetim kaydını görmek | Kayıt yine yazılır; lisans gelince geçmişiyle görünür |
| `status_pages`: birden fazla durum sayfası | Bir durum sayfası oluşturulabilir |

Lisans, Kubeat'in çevrimdışı anahtarıyla imzalanmış bir dosyadır. İçinde
müşteri, açtığı özellikler ve bitiş tarihi yazar. Backend imzayı kendi
içindeki açık anahtarla doğrular; hiçbir yere bağlanmaz, lisans sunucusu
yoktur. Chart'ta dosya bir Secret olarak verilir:

```sh
kubectl create secret generic kubeat-license --from-file=license=acme.license
helm upgrade kubeat ... --set license.existingSecret=kubeat-license
```

Chart dışında dosyanın yolu `UPTIME_LICENSE_FILE` ile verilir. Backend
dosyayı `UPTIME_LICENSE_RELOAD_INTERVAL` (1 dakika) aralıkla kontrol eder:
Secret'ı yenilemek yeterlidir, yeniden başlatma gerekmez. Lisansın durumu
**Kullanıcılar** sayfasının altında görünür.

Bitişten 30 gün önce yöneticiler her sayfada bir uyarı görür, backend de
log'a yazar. `kubeat_license_expiry_timestamp_seconds` metriği bitiş
zamanını verir; buna kendi alarmınızı kurabilirsiniz.

Lisans bittiğinde ya da kullanılamaz olduğunda:

- **İzleme durmaz.** Kontroller, alarmlar, bildirimler ve durum sayfaları
  çalışmaya devam eder.
- Lisansın açtığı özellikler kapanır: LDAP ve OIDC ile giriş yapılamaz
  (açık oturumlar süreleri dolana kadar sürer), denetim kaydı görünmez,
  yeni bir namespace sınırı ya da ikinci bir durum sayfası oluşturulamaz.
- **Hiçbir erişim genişlemez.** Namespace'le sınırlı kullanıcılar sınırlı
  kalır; sınır daraltılabilir ya da kaldırılabilir ama başka
  namespace'lere taşınamaz ve genişletilemez. Mevcut
  durum sayfaları yayında kalır ve düzenlenebilir.
- Yöneticilerin hepsi dizin ya da SSO hesabıysa arayüze yerel bir yönetici
  hesabıyla girilir: chart'ın oluşturduğu `admin`, gerekirse
  `UPTIME_ADMIN_PASSWORD_RESET` ile. API anahtarı da çalışmaya devam eder.

## Yapılandırma

Ayarlar koda gömülmez; hepsi ortam değişkeniyle verilir. Örnek değerler
[.env.example](.env.example) dosyasındadır.

| Değişken | Varsayılan | Açıklama |
| --- | --- | --- |
| `UPTIME_ADDR` | `:8080` | Backend'in dinlediği adres |
| `UPTIME_DB_PATH` | `uptime.db` | SQLite dosyasının yolu |
| `UPTIME_API_TOKEN` | – | Betikler için yönetici yetkisiyle çalışan API anahtarı; boşsa API yalnızca oturumla kullanılır |
| `UPTIME_SECRET_KEY` | – | Veritabanındaki token ve parolaları şifreleyen anahtar; en az 32 karakter. Zorunlu: yoksa backend başlamaz (bkz. `UPTIME_ALLOW_PLAINTEXT_SECRETS`) |
| `UPTIME_SECRET_KEY_FILE` | – | Anahtarın okunduğu dosya, örneğin bağlanmış bir Secret; içeriği bayt bayt, sondaki satır sonu dahil kullanılır. `UPTIME_SECRET_KEY` ile birlikte verilemez. Chart dışı kurulumlar içindir: chart anahtarı `UPTIME_SECRET_KEY` olarak verdiğinden `backend.extraEnv` ile verilmez |
| `UPTIME_ALLOW_PLAINTEXT_SECRETS` | `false` | Anahtar yokken de başlar; gizli değerler şifresiz yazılır |
| `UPTIME_ADMIN_PASSWORD` | – | Hesap yokken `admin` yöneticisini bu parolayla oluşturur; parola kurallarına uymazsa backend başlamaz. Boşsa ilk yönetici `/setup`'ta oluşturulur |
| `UPTIME_METRICS_TOKEN` | – | `/metrics`'in istediği anahtar (`Authorization: Bearer ...`; API anahtarı da geçer); boşsa `/metrics` herkese açıktır |
| `UPTIME_ADMIN_PASSWORD_RESET` | `false` | `admin` parolasını `UPTIME_ADMIN_PASSWORD` değerine çeker; bir kez kullanılır |
| `UPTIME_SESSION_TTL` | `12h` | Oturum süresi |
| `UPTIME_LOGIN_MAX_FAILURES` | `10` | Bir kullanıcı adını kilitleyen hatalı deneme sayısı |
| `UPTIME_LOGIN_LOCKOUT` | `15m` | Kilidin süresi; denemesi bundan eski adlar bu aralıkla sayaçtan silinir |
| `UPTIME_LOGIN_THROTTLE_MAX_KEYS` | `100000` | Deneme sayacının tuttuğu, kilitli olmayan en fazla kullanıcı adı; doluyken yeni bir ad, en az denenmiş adlardan en uzun süredir denenmeyenin yerini alır, kilitli adlar unutulmaz. Aynı zamanda sürmekte olan (sağlayıcıya gidip henüz dönmemiş) SSO girişlerinin en fazla sayısı; aşılınca en eskileri unutulur |
| `UPTIME_LOGIN_CONCURRENCY` | `0` | Aynı anda çalışan bcrypt karşılaştırması sayısı; `0` CPU sayısının yarısı, en az 1, LDAP açıkken en az 2 (her dizin girişi de bir karşılaştırma harcar). Dizine bağlanmaların ayrı sırası vardır (aynı anda 16) |
| `UPTIME_LOGIN_QUEUE_WAIT` | `3s` | Bir girişin sırasını (bcrypt ya da dizin) en fazla bekleme süresi; sonra 503 |
| `UPTIME_LOGIN_AUDIT_PER_MINUTE` | `60` | Kayda bir dakikada yazılan en fazla başarısız giriş (hesabı olan adların, yani yerel hesapların ve daha önce girmiş dizin ya da SSO kullanıcılarının yanlış parolası hariç); fazlası yalnızca uyarı olarak sayılır |
| `UPTIME_PASSWORD_MIN_LENGTH` | `8` | En kısa parola (karakter); en uzunu 72 bayttır |
| `UPTIME_AUDIT_RETENTION` | `2160h` | Kayıt girdilerinin saklanma süresi (90 gün) |
| `UPTIME_LDAP_URL` | – | Dizin adresi (`ldap://` ya da `ldaps://`); boşsa LDAP kapalı |
| `UPTIME_LDAP_BASE_DN` | – | Kullanıcıların arandığı DN |
| `UPTIME_LDAP_USER_FILTER` | `(uid={username})` | Kullanıcıyı bulan süzgeç |
| `UPTIME_LDAP_BIND_DN`, `UPTIME_LDAP_BIND_PASSWORD` | – | Arama hesabı; boşsa anonim arama |
| `UPTIME_LDAP_START_TLS` | `false` | `ldap://` bağlantısını StartTLS ile şifreler |
| `UPTIME_LDAP_CA_FILE` | – | Dizin sertifikasının CA'sı |
| `UPTIME_LDAP_INSECURE_SKIP_VERIFY` | `false` | Dizin sertifikasını doğrulamaz |
| `UPTIME_LDAP_DISPLAY_NAME_ATTR`, `UPTIME_LDAP_EMAIL_ATTR`, `UPTIME_LDAP_GROUP_ATTR` | `cn`, `mail`, `memberOf` | Okunan öznitelikler |
| `UPTIME_LDAP_GROUP_BASE_DN`, `UPTIME_LDAP_GROUP_FILTER` | – | `memberOf` yoksa grupların arandığı yer ve süzgeç (`(member={dn})`) |
| `UPTIME_LDAP_TIMEOUT` | `10s` | Dizine bağlanmanın (TLS el sıkışması dahil) ve her dizin işleminin zaman aşımı |
| `UPTIME_LDAP_DEFAULT_ROLE`, `UPTIME_LDAP_DEFAULT_NAMESPACES` | – | Eşleşen grubu olmayan dizin kullanıcısının rolü ve kapsamı; boşsa giremez |
| `UPTIME_OIDC_ISSUER` | – | OpenID Connect sağlayıcısının issuer adresi; boşsa SSO kapalı |
| `UPTIME_OIDC_CLIENT_ID` | – | Sağlayıcıda kayıtlı istemci kimliği (issuer ile zorunlu; yoksa SSO kapalı kalır) |
| `UPTIME_OIDC_CLIENT_SECRET` | – | İstemci sırrı; public client için boş |
| `UPTIME_OIDC_REDIRECT_URL` | – | Sağlayıcının geri döndüğü adres: `https://<kubeat>/auth/callback`; boşsa `UPTIME_PUBLIC_URL` adresinin `/auth/callback` yolu. HTTPS olmalıdır (düz HTTP yalnızca `localhost`), uymazsa SSO kapalı kalır ve backend nedenini loglar; ikisi de yoksa SSO yalnızca `localhost`'taki bir arayüzle çalışır |
| `UPTIME_OIDC_SCOPES` | `openid profile email` | İstenen kapsamlar, boşlukla ayrılmış |
| `UPTIME_OIDC_USERNAME_CLAIM` | `preferred_username` | Kullanıcı adının okunduğu claim |
| `UPTIME_OIDC_DISPLAY_NAME_CLAIM` | `name` | Görünen adın okunduğu claim |
| `UPTIME_OIDC_EMAIL_CLAIM` | `email` | E-postanın okunduğu claim |
| `UPTIME_OIDC_GROUPS_CLAIM` | `groups` | Grupların okunduğu claim (ID token ya da userinfo) |
| `UPTIME_OIDC_NAME` | `SSO` | Giriş düğmesinde görünen ad |
| `UPTIME_OIDC_CA_FILE` | – | Sağlayıcının sertifikasını doğrulayacak CA dosyası |
| `UPTIME_OIDC_INSECURE_SKIP_VERIFY` | `false` | Sağlayıcının sertifikasını doğrulamaz |
| `UPTIME_OIDC_TIMEOUT` | `10s` | Sağlayıcıya yapılan isteklerin zaman aşımı |
| `UPTIME_OIDC_DEFAULT_ROLE` | – | Eşleşen grubu olmayan sağlayıcı kullanıcısının rolü; boşsa giremez |
| `UPTIME_OIDC_DEFAULT_NAMESPACES` | – | O rolün kapsamı (virgülle; `*` hepsi) |
| `UPTIME_DB_BUSY_TIMEOUT` | `5s` | Kilitli veritabanı için bekleme süresi |
| `UPTIME_READ_HEADER_TIMEOUT` | `5s` | HTTP istek başlığı okuma süresi |
| `UPTIME_HTTP_READ_TIMEOUT` | `30s` | Bir isteğin gövdesiyle birlikte okunma süresi |
| `UPTIME_HTTP_WRITE_TIMEOUT` | `2m` | Başlıkları okunan bir isteğin yanıtının yazılıp bitmesi için süre |
| `UPTIME_HTTP_IDLE_TIMEOUT` | `2m` | Boşta bekleyen keep-alive bağlantının kapatılma süresi |
| `UPTIME_HTTP_MAX_HEADER_BYTES` | `1048576` | İstek başlıklarının en büyük boyutu (bayt) |
| `UPTIME_SHUTDOWN_TIMEOUT` | `10s` | Kapanışta açık isteklerin bitmesi için süre |
| `UPTIME_SCHEDULER_TICK` | `1s` | Zamanı gelen monitor'ların ve monitor değişikliklerinin kontrol sıklığı |
| `UPTIME_MAX_JITTER` | `5s` | Çalışırken eklenen bir monitor'un ilk kontrolünden önceki rastgele gecikmenin üst sınırı. Açılışta var olan monitor'lar kendi aralıklarına yayılır |
| `UPTIME_WORKERS` | `20` | Aynı anda çalışabilecek kontrol sayısı |
| `UPTIME_RESULT_BATCH_SIZE` | `100` | Tek seferde yazılan en fazla sonuç sayısı |
| `UPTIME_RESULT_FLUSH_INTERVAL` | `1s` | Dolmamış batch'in yazılma aralığı |
| `UPTIME_RETENTION` | `168h` | Ham kontrol sonuçlarının saklanma süresi |
| `UPTIME_RETENTION_INTERVAL` | `1h` | Eski sonuçların silinme sıklığı |
| `UPTIME_ROLLUP_INTERVAL` | `1m` | Saatlik özetlerin güncellenme sıklığı |
| `UPTIME_ROLLUP_RETENTION` | `8760h` | Saatlik özetlerin saklanma süresi; grafik ve uptime en fazla bu kadar geriye gider |
| `UPTIME_DELIVERY_RETENTION` | `8760h` | Bildirim kaydının (hangi kanala hangi olay için ne gitti, iletildi mi) saklanma süresi; `UPTIME_RETENTION_INTERVAL` sıklığıyla temizlenir, olaylar kalır |
| `UPTIME_INCIDENT_TIMELINE_LIMIT` | `100` | Olay sayfasında gösterilen en fazla başarısız kontrol sayısı, ayrıca en fazla bildirim sayısı; fazlası varsa baştan ve sondan yarısı gösterilir. En az 2 |
| `UPTIME_SERIES_POINTS` | `120` | Bir grafikteki yaklaşık nokta sayısı |
| `UPTIME_HTTP_INSECURE_SKIP_VERIFY` | `false` | TLS kullanan tüm kontrollerde (HTTP, TCP, gRPC, WebSocket) sertifika doğrulamasını kapatır |
| `UPTIME_KEYWORD_BODY_LIMIT_BYTES` | `1048576` | Keyword ya da JSON değeri aranırken yanıt gövdesinin okunan kısmı |
| `UPTIME_TARGET_DENY_CIDRS` | – | Kontrollerin ve bildirimlerin bağlanmayacağı, yerleşik listeye (loopback, link-local, metadata, multicast) eklenen aralıklar; virgülle ayrılmış CIDR. Bkz. "Hedef politikası" |
| `UPTIME_TARGET_ALLOW_CIDRS` | – | Yasaklardan istisna tutulan aralıklar; virgülle ayrılmış CIDR, her yasağın önüne geçer |
| `UPTIME_MIN_CHECK_INTERVAL` | `5s` | Bir monitor için izin verilen en kısa kontrol aralığı |
| `UPTIME_MIN_CHECK_INTERVAL_EXTERNAL` | `30s` | Hedefi cluster dışında olan bir monitor'un en kısa kontrol (ve tekrar) aralığı; `UPTIME_MIN_CHECK_INTERVAL`'dan kısa olamaz, `0` ayrı kural yok demektir. Bkz. "Hedef yükü" |
| `UPTIME_TARGET_MAX_RATE` | `60` | Cluster dışındaki bir host'a tüm monitor'ların birlikte dakikada yapabileceği en fazla kontrol; fazlası sonraki tick'i bekler. `0` sınırsız |
| `UPTIME_CLUSTER_TARGET_MAX_RATE` | `600` | Aynısı, cluster içindeki bir host için. `0` sınırsız |
| `UPTIME_TARGET_ADDRESS_MAX_RATE` | `600` | Kontrollerin ulaştığı bir IP adresine, hangi adla ulaşılırsa ulaşılsın ve adresin sınıfı ne olursa olsun, tüm monitor'ların birlikte dakikada yapabileceği en fazla kontrol; aynı adrese çözülen adlar bunu paylaşır (bkz. "Hedef yükü", paylaşılan adres paragrafı). `0` adres kovasını kapatır |
| `UPTIME_MAX_MONITORS_PER_HOST` | `5` | Cluster dışındaki bir host'u kontrol edebilecek en fazla monitor sayısı. `0` sınırsız |
| `UPTIME_MAX_MONITORS` | `1000` | Toplam monitor kotası; yalnızca yeni monitor'ları bağlar, üstündekiler çalışmaya devam eder. `0` sınırsız |
| `UPTIME_MAX_MONITORS_PER_NAMESPACE` | `200` | Bir namespace'in (ya da grubun) monitor kotası; yalnızca yeni monitor'ları bağlar. `0` sınırsız |
| `UPTIME_MONITOR_CREATE_PER_MINUTE` | `30` | Bir hesabın (API anahtarı tek hesap sayılır) bir dakikada oluşturabileceği monitor sayısı; fazlası 429 alır ve dakikada bir kayıt girdisi yazılır. `0` sınırsız |
| `UPTIME_USER_AGENT` | – | Kontrollerin ve bildirimlerin gönderdiği `User-Agent`; boşsa `kubeat/<sürüm> (+https://github.com/yusufceng/Kubeat; <UPTIME_PUBLIC_URL>)` (adres yalnızca `UPTIME_PUBLIC_URL` ayarlıysa) |
| `UPTIME_STATS_WINDOW` | `24h` | Uptime yüzdesinin hesaplandığı varsayılan süre |
| `UPTIME_MAX_RESULTS` | `1000` | Tek istekte dönen en fazla kontrol sonucu |
| `UPTIME_STATUS_PAGE_DAYS` | `90` | Durum sayfasında gösterilen gün sayısı; uptime dönemlerinden biri de bu kadardır |
| `UPTIME_STATUS_PAGE_INCIDENT_WINDOW` | `336h` | Durum sayfasında listelenen çözülmüş kesintilerin ne kadar geriye gittiği |
| `UPTIME_STATUS_PAGE_UPDATES` | `20` | Durum sayfasında listelenen en fazla güncelleme sayısı (olay notları dahil) |
| `UPTIME_STATUS_PAGE_UPCOMING` | `168h` | Durum sayfasının ne kadar ilerideki bakım pencerelerini listeleyeceği |
| `UPTIME_STATUS_PAGE_SERIES` | `24h` | Durum sayfasındaki monitor ayrıntı sayfasının yanıt süresi grafiğinin kapsadığı süre |
| `UPTIME_STATUS_PAGE_SESSION` | `12h` | Parolalı durum sayfasında ziyaretçinin verdiği parolanın ne kadar süre geçerli kaldığı |
| `UPTIME_DEFAULT_FAILURE_THRESHOLD` | `3` | Monitor'da belirtilmezse, olay açmak için gereken art arda başarısız kontrol sayısı |
| `UPTIME_RECOVERY_THRESHOLD` | `1` | Olayı kapatmak için gereken art arda başarılı kontrol sayısı |
| `UPTIME_CERT_WARNING` | `336h` | Sertifika bitişine bu kadar kala uyarı olayı açılır |
| `UPTIME_REMINDER_INTERVAL` | `0` | Süren kesintinin yeniden bildirilme aralığı; `0` kapalı |
| `UPTIME_NOTIFY_ATTEMPTS` | `3` | Kanal başına bildirim deneme sayısı |
| `UPTIME_NOTIFY_RETRY_DELAY` | `5s` | Bildirim denemeleri arasındaki bekleme |
| `UPTIME_NOTIFY_TIMEOUT` | `5s` | Tek bir bildirim denemesinin zaman aşımı |
| `UPTIME_NOTIFY_QUEUE_SIZE` | `100` | Gönderilmeyi bekleyebilecek en fazla bildirim sayısı |
| `UPTIME_PUBLIC_URL` | – | Kullanıcıların Kubeat'e eriştiği adres; bildirimlerdeki bağlantılar bununla kurulur, boşsa bağlantı konmaz. `UPTIME_OIDC_REDIRECT_URL` boşsa SSO dönüş adresi de bundan türetilir |
| `UPTIME_LANGUAGE` | `en` | Bildirim metinlerinin ve ilk yönetici hesabının görünen adının dili: `en` ya da `tr` |
| `UPTIME_NOTIFY_GROUP_WAIT` | `30s` | Aynı namespace'teki aynı tür olayların insanlara giden kanallarda tek mesajda toplanmak için beklediği süre; `0` kapalı |
| `UPTIME_DISCOVERY_ENABLED` | `true` | Cluster keşfini açar |
| `UPTIME_DISCOVERY_NAMESPACES` | – | Keşfi virgülle ayrılmış bu namespace'lerle sınırlar; boşsa tüm cluster (cluster çapında okuma yetkisi ister) |
| `UPTIME_DISCOVERY_SYNC_INTERVAL` | `1m` | `kubeat.io/monitor` annotation'lı nesnelerin ne sıklıkta okunup monitor'larının eşitleneceği; `0` kapalı |
| `UPTIME_DISCOVERY_DEFAULT_INTERVAL` | `30s` | Annotation'la oluşturulan monitor'un, nesne belirtmediyse kontrol aralığı |
| `UPTIME_DISCOVERY_DEFAULT_TIMEOUT` | `5s` | Annotation'la oluşturulan monitor'un, nesne belirtmediyse zaman aşımı |
| `UPTIME_KUBECONFIG` | – | Cluster dışında çalışırken kullanılacak kubeconfig dosyası |
| `UPTIME_KUBE_CONTEXT` | – | Kubeconfig içindeki bağlam; kubeconfig verildiyse zorunlu |
| `UPTIME_KUBE_TIMEOUT` | `10s` | Cluster API istekleri için zaman aşımı |
| `UPTIME_EVENT_WINDOW` | `2h` | Monitor sayfasında pod'ların yanında gösterilen cluster olaylarının ne kadar geriye gideceği |
| `UPTIME_EVENT_LIMIT` | `20` | En fazla kaç olay gösterileceği |
| `UPTIME_EVENT_CACHE` | `10s` | Bir namespace'in olay listesinin cluster'dan yeniden okunmadan önce ne kadar süre kullanılacağı; `0` her seferinde okur |
| `UPTIME_WORKLOAD_ROLLOUT_GRACE` | `10m` | Workload monitor'un süren bir rolling update'te eksik pod'ları ne kadar hoş göreceği; `0` hiç |
| `UPTIME_PROMETHEUS_URL` | – | Pod CPU/bellek kullanımının okunduğu Prometheus uyumlu API; boşsa kullanım gösterilmez |
| `UPTIME_PROMETHEUS_TOKEN_FILE` | – | Her istekte okunan bearer token dosyası; token yalnızca `UPTIME_PROMETHEUS_URL`'deki sunucuya gider, başka bir sunucuya yönlendirmede gönderilmez |
| `UPTIME_PROMETHEUS_CA_FILE` | – | Sunucu sertifikasının doğrulanacağı CA dosyası |
| `UPTIME_PROMETHEUS_INSECURE_SKIP_VERIFY` | `false` | Sunucu sertifikasını doğrulamaz |
| `UPTIME_PROMETHEUS_TIMEOUT` | `10s` | Sorgu zaman aşımı |
| `UPTIME_PROMETHEUS_RATE_WINDOW` | `5m` | CPU kullanımının ortalamasının alındığı süre |
| `UPTIME_PROMETHEUS_CPU_QUERY` | – | Varsayılan CPU sorgusunun yerine kullanılır |
| `UPTIME_PROMETHEUS_MEMORY_QUERY` | – | Varsayılan bellek sorgusunun yerine kullanılır |
| `UPTIME_BACKUP_DIR` | – | Veritabanı kopyalarının yazılacağı dizin; boşsa yedekleme kapalıdır |
| `UPTIME_BACKUP_INTERVAL` | `24h` | Kopyalar arasındaki süre |
| `UPTIME_BACKUP_KEEP` | `7` | Saklanan kopya sayısı; yedek volume'u en az (keep + 1) × veritabanı boyutu olmalıdır |
| `UPTIME_LICENSE_FILE` | – | Kubeat Enterprise lisans dosyasının yolu, örneğin bağlanmış bir Secret; boşsa topluluk sürümü (bkz. "Kubeat Enterprise lisansı") |
| `UPTIME_LICENSE_RELOAD_INTERVAL` | `1m` | Lisans dosyasının değişip değişmediğine bakma aralığı; yenilenen lisans yeniden başlatmadan etkinleşir |
| `UPTIME_HEARTBEAT_URL` | – | Dead man's switch adresi; boşsa heartbeat gönderilmez |
| `UPTIME_HEARTBEAT_INTERVAL` | `1m` | Heartbeat aralığı; `UPTIME_SCHEDULER_TICK`'in en az iki katı |
| `UPTIME_HEARTBEAT_TIMEOUT` | `10s` | Heartbeat isteğinin zaman aşımı |
| `UPTIME_METRICS_TIMEOUT` | `5s` | `/metrics` için veritabanı okumasının zaman aşımı |
| `UPTIME_SMTP_HOST` | – | E-posta kanalları için SMTP sunucusu |
| `UPTIME_SMTP_PORT` | `587` | SMTP portu |
| `UPTIME_SMTP_FROM` | – | Gönderen adresi, örn. `Kubeat <kubeat@ornek.com>` |
| `UPTIME_SMTP_SECURITY` | `starttls` | `starttls`, `tls` ya da `none` |
| `UPTIME_SMTP_USERNAME` | – | SMTP kullanıcı adı; boşsa kimlik doğrulama yapılmaz |
| `UPTIME_SMTP_PASSWORD` | – | SMTP parolası |
| `UPTIME_TELEGRAM_API_URL` | `https://api.telegram.org` | Telegram Bot API adresi |
| `UPTIME_PAGERDUTY_EVENTS_URL` | `https://events.pagerduty.com/v2/enqueue` | PagerDuty Events API v2 adresi |
| `BACKEND_URL` | `http://localhost:8080` | Frontend'in backend'e eriştiği adres |
| `BACKEND_TIMEOUT_MS` | `3000` | Frontend'in backend isteği zaman aşımı (ms) |
| `DEFAULT_LOCALE` | `en` | Tarayıcısı bizde olan bir dil istemeyen ziyaretçinin arayüz dili: `en` ya da `tr` |
| `PUBLIC_URL` | – | Kullanıcıların Kubeat'e eriştiği adres (backend'deki `UPTIME_PUBLIC_URL`); `https://` ise oturum çerezi Secure işaretlenir ve HSTS gönderilir, `http://` ise `upgrade-insecure-requests` gönderilmez. Chart `publicURL`'den ya da Route/Ingress host'undan verir |
| `COOKIE_SECURE` | `auto` | Oturum çerezinin Secure (yalnızca HTTPS) işaretlenmesi: `auto` (`PUBLIC_URL` `https://` ise ya da `X-Forwarded-Proto`'nun ilk, tarayıcıya bakan değeri `https` ise), `always` ya da `never` (`localhost` dışından düz HTTP ile erişim için; `upgrade-insecure-requests` de gönderilmez) |
| `STATUS_PAGE_FRAME_ANCESTORS` | `'none'` | Durum sayfalarını iframe içinde gösterebilecek siteler, CSP `frame-ancestors` kaynak listesi olarak (örn. `https://wiki.example.com`); arayüzün geri kalanı hiç gösterilemez |
| `HSTS_MAX_AGE` | `31536000` | HTTPS ile gelen tarayıcıya gönderilen `Strict-Transport-Security` süresi (saniye); `0` kapalı |
| `REFRESH_INTERVAL_MS` | `5000` | Açık sayfaların veriyi yenileme aralığı (ms) |
| `STATUS_PAGE_REFRESH_MS` | `30000` | Açık bir durum sayfasının yenilenme aralığı (ms) |
| `STATUS_PAGE_COMPACT_ABOVE` | `12` | Bu sayıdan çok servis gösteren durum sayfasında her servis tek satıra iner |
| `STATUS_PAGE_UPDATES_LIMIT` | `50` | Durum sayfası düzenlenirken listelenen güncelleme sayısı |
| `DASHBOARD_FLAT_LIMIT` | `50` | Bu sayıya kadar monitor listesinde tüm namespace'ler açık gelir; üzerinde yalnızca sorunlu olanlar açılır |
| `LIST_BEATS` | `24` | Monitor listesinde ve namespace tablosunda her satırda gösterilen son kontrol sayısı |
| `WARN_UPTIME_BELOW` | `99.9` | Listede ve tablolarda uptime bu yüzdenin altındaysa kehribar renkte gösterilir; üstündeyse sessiz kalır |
| `CRITICAL_UPTIME_BELOW` | `99` | Uptime bu yüzdenin altındaysa kırmızı gösterilir |
| `HISTORY_LIMIT` | `60` | Monitor sayfasındaki kalp atışı çubuğunda gösterilen kontrol sayısı |
| `RESULTS_LIMIT` | `30` | Monitor sayfasında listelenen kontrol sayısı |
| `MONITOR_INCIDENTS` | `6` | Monitor sayfasının olay panelinde "Tümünü göster"den önce listelenen olay sayısı |
| `OVERVIEW_EVENTS` | `12` | Genel bakışta ve namespace sayfasında listelenen son olay sayısı |
| `OVERVIEW_OUTAGES` | `5` | Genel bakışın özetinde satır satır gösterilen devam eden kesinti sayısı; fazlası listede süzülerek açılır |
| `FLAP_WINDOW` | `10m` | Bir monitor'un bu süreden kısa ve her biri bir öncekinin bitiminden bu süre içinde başlayan, art arda en az üç kesintisi etkinlik akışında tek satır, olay listesinde tek satır olarak gösterilir; `0s` her kesintiyi ayrı gösterir |
| `FLEET_STRIP_HOURS` | `24` | Genel bakıştaki ve namespace sayfasındaki kesinti şeridinin kapsadığı saat |
| `FLEET_STRIP_SLOTS` | `96` | Bu şeridin çubuk sayısı (varsayılanla her çubuk 15 dakika) |
| `STATS_DAYS` | `7` | Genel bakışta ve namespace sayfasında MTTR, kesinti sayısı ve uptime gibi rakamların kapsadığı gün (en çok 365) |
| `NEW_MONITOR_INTERVAL_SECONDS` | `60` | Yeni monitor formunun başlangıçtaki kontrol aralığı |
| `NEW_MONITOR_TIMEOUT_SECONDS` | `10` | Yeni monitor formunun başlangıçtaki zaman aşımı |
| `NEW_MONITOR_FAILURE_THRESHOLD` | `3` | Yeni monitor formunun başlangıçtaki alarm eşiği |
| `NEW_MONITOR_SLOW_ABOVE_MS` | `0` | Yeni monitor formunun başlangıçtaki yavaş yanıt eşiği (ms); `0` kapalı |
| `PUSH_INTERNAL_URL` | `BACKEND_URL` | Push monitor'larında "cluster içinden" gösterilen rapor adresinin kökü |
| `INCIDENTS_LIMIT` | `100` | Bir sayfada listelenen olay sayısı |
| `INCIDENT_RANGES` | `24h,168h,720h,2160h` | Olay listesinin zaman süzgecinde sunulan "son …" dönemleri |
| `REPORT_PERIODS` | `30,1,7,90` | Raporlarda sunulan dönemler, gün olarak (1–365); ilki varsayılandır |
| `AUDIT_LIMIT` | `200` | Kayıt sayfasında listelenen girdi sayısı |
| `MAINTENANCE_DURATIONS` | `30m,1h,2h,4h,8h,24h` | Susturma ve bakım için sunulan süreler |
| `MAINTENANCE_LIMIT` | `50` | Listelenen bakım penceresi sayısı |
| `UPTIME_WINDOWS` | `24h,168h,720h` | Monitor sayfasında uptime'ı gösterilen dönemler |
| `CHART_RANGES` | `24h,1h,168h,720h` | Grafik için sunulan aralıklar; ilki varsayılandır |

Süreler Go biçimindedir (`500ms`, `5s`, `1m30s`).

## Katkı

Branch stratejisi ve commit kuralları: [CONTRIBUTING.md](CONTRIBUTING.md)

## Lisans

Kaynak kodu [Apache License 2.0](LICENSE) ile yayınlanır. Katkılar aynı
lisansla kabul edilir. Kubeat Enterprise özellikleri bir lisans dosyasıyla
açılır (bkz. "Kubeat Enterprise lisansı").
