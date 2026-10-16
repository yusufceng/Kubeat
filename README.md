# Kubeat

OCP/Kubernetes ortamlarındaki servisler için uptime monitoring aracı.
Go + Next.js + SQLite.

Proje planı: [docs/proje-plani.md](docs/proje-plani.md) · Yol haritası ve bilinen sınırlar: [docs/yol-haritasi.md](docs/yol-haritasi.md)

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
helm upgrade --install kubeat oci://ghcr.io/yusufceng/charts/kubeat --version 0.28.0 \
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
  [values.yaml](deploy/helm/kubeat/values.yaml).

### OpenShift

OpenShift'te chart şu değerlerle kurulur; `podSecurityContext` boş bırakılır,
UID ve fsGroup'u SCC atar:

```
helm upgrade --install kubeat oci://ghcr.io/yusufceng/charts/kubeat --version 0.28.0 \
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
(`/api/v1/health`), `/metrics`, yayımlanmış durum sayfaları ve rozetler
kimlik istemez; hiçbiri bir şey değiştirmez. Push raporları
(`/api/push/{token}`) da anahtar istemez: adresteki token o monitor'un
sırrıdır ve yalnızca o monitor'a rapor yazabilir. Token'ı (ve rapor
adresini) yalnızca monitor'u değiştirebilenler görür; izleyicilere
gösterilmez, çünkü onunla monitor'u düşmüş ya da çalışıyor gösterebilirler.

Cluster'a ya da metrik sunucusuna yapılan bir istek başarısız olduğunda
hatanın kendisini (Kubeat'in servis hesabı, API sunucusunun ve metrik
sunucusunun adresi, sertifika ayrıntıları, kubeconfig bağlamı) yalnızca
yöneticiler görür. Diğerlerine hatanın türü söylenir: servis hesabının hangi
namespace'te neyi yapamadığı, kimliğinin kabul edilmediği, sunucuya
ulaşılamadığı ya da sunucunun sertifikasına güvenilmediği; türü belli olmayan
hatalarda ayrıntının logda olduğu yazar. Hatanın tam metni her zaman backend
logundadır.

Helm chart anahtarı ilk kurulumda rastgele üretir, bir Secret'ta saklar ve
güncellemelerde korur. Ek olarak `networkPolicy.enabled=true`, backend'e
yalnızca arayüz pod'larının bağlanmasına izin verir (NetworkPolicy uygulayan
bir ağ eklentisi gerekir). Bu durumda başka namespace'lerdeki işler push
raporlarını arayüzün adresine göndermeli ya da `networkPolicy.backendIngressFrom`
ile izin almalıdır.

### Hedef politikası

Monitor hedeflerini ve bildirim kanallarının adreslerini, onları
düzenleyebilen her kullanıcı belirler; backend bu adreslere cluster'ın
içinden bağlanır. Böyle bir adres backend'in kendisine (loopback) ya da
bulut sağlayıcısının metadata servisine (`169.254.169.254`) götürmesin diye
kontroller ve bildirimler her bağlantıyı **hedef politikasına** göre açar.
Politika bağlantı anında, ad çözüldükten sonra bağlanılacak adrese bakar:
zararsız görünen bir alan adı, iki sorgu arasında değişen bir DNS yanıtı ve
yönlendirmenin (redirect) her adımı aynı kurala takılır. Yöneticinin verdiği
Telegram, PagerDuty ve SMTP adresleri de politikadan geçer.

Yerleşik olarak yasak olan aralıklar:

| Aralık | Neden |
| --- | --- |
| `127.0.0.0/8`, `::1/128` | Loopback: backend pod'unun kendisi |
| `0.0.0.0/8`, `::/128` | "Bu makine"; `0.0.0.0` loopback'e ulaşır |
| `169.254.0.0/16`, `fe80::/10` | Link-local; bulut metadata servisi `169.254.169.254`'tedir |
| `fd00:ec2::254/128` | AWS metadata servisinin IPv6 adresi |
| `100.100.100.200/32` | Alibaba Cloud metadata servisi |
| `224.0.0.0/4`, `ff00::/8`, `255.255.255.255/32` | Multicast ve broadcast |

Özel ağlar (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `100.64.0.0/10`,
`fd00::/8`) bilerek yasak **değildir**: Kubeat'in işi cluster içindeki
servisleri kontrol etmektir ve ClusterIP'ler (OpenShift'te `172.30.0.0/16`),
pod IP'leri ve `*.svc` adları bu aralıklardadır. IPv6 biçiminde yazılmış
IPv4 adresleri (`::ffff:127.0.0.1`) ve NAT64 adresleri (`64:ff9b::/96`),
taşıdıkları IPv4 adresine göre denetlenir.

Yasak bir adrese giden kontrol ya da kanal testi, adresi ve aralığı söyleyen
bir hatayla başarısız olur: `169.254.169.254 is blocked by the target policy
(169.254.0.0/16); UPTIME_TARGET_ALLOW_CIDRS can allow it`. İki ayar,
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

Açılışta `target policy in effect` log satırı eklenen yasakları ve
istisnaları yazar. Çözücüsü verilmemiş DNS monitor'ları ve ad çözme,
pod'un kendi çözücüsünü (CoreDNS) kullanır; politika ona uygulanmaz.
Backend'e `HTTP_PROXY`/`HTTPS_PROXY` verildiyse vekil üzerinden giden bir
isteğin hedefi önce backend'de çözülüp denetlenir; vekilin kendi adresi de
politikaya tabidir, loopback'te dinleyen bir vekil için istisna gerekir.
Lokal geliştirmede `make backend` loopback'i istisna tutar
(`TARGET_ALLOW_CIDRS`), böylece makinedeki servisler kontrol edilebilir.

Backend'in HTTP sunucusu bir isteğin tamamını okumaya `UPTIME_HTTP_READ_TIMEOUT`
(30 sn), yanıtını yazmaya `UPTIME_HTTP_WRITE_TIMEOUT` (2 dk) süre tanır, boşta
bekleyen bağlantıyı `UPTIME_HTTP_IDLE_TIMEOUT` (2 dk) sonra kapatır ve en fazla
`UPTIME_HTTP_MAX_HEADER_BYTES` (1 MiB) başlık kabul eder. Gövdesini yavaş
gönderen ya da bağlantısını boşta tutan bir istemci bağlantıları süresiz
meşgul edemez.

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
  secret; sondaki satır sonu atılır). İkisi birden verilemez.
- Şifrelemeden bilerek vazgeçmek için `UPTIME_ALLOW_PLAINTEXT_SECRETS=true`
  verilir: değerler şifresiz yazılır ve backend bunu her açılışta uyarı
  olarak loglar. Veritabanında şifreli değer varken anahtar verilmezse ya da
  yanlış anahtar verilirse backend bu ayarla da başlamaz.
- Anahtar sonradan ayarlanırsa, açılışta daha önce şifresiz yazılmış değerler
  şifrelenir ve veritabanı bir kez sıkıştırılır (`VACUUM`), ki şifresiz
  kopyaları dosyanın boş alanında ve WAL dosyasında kalmasın. Bu, veritabanı
  kadar boş disk ister ve büyük bir veritabanında o açılışı uzatır. Anahtardan
  önce alınmış yedekler şifresizdir; yenileri onları silene kadar
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
güncellemelerde korur ve release kaldırılsa bile silmez. Okumak için:

```
kubectl -n kubeat get secret kubeat-secret-key -o jsonpath='{.data.key}' | base64 -d
```

**GitOps (Argo CD, OpenShift GitOps).** Chart ürettiği Secret'ları
(`<release>-secret-key`, `<release>-admin`, `<release>-api-token`) Helm'in
`lookup` fonksiyonuyla geri okuyarak korur. Şablonu `helm template` ile üreten
araçlarda `lookup` çalışmaz: her render yeni değerler üretir ve
senkronizasyon Secret'ların üzerine yazar. Şifreleme anahtarı değişince backend
bir sonraki açılışta başlamaz ve şifreli değerler kurtarılamaz; API anahtarı
değişir; `admin` Secret'ı hesabın parolasını artık tutmaz. Bu araçlarla
kurarken üç Secret'ı da önceden oluşturup (Sealed Secrets, External Secrets
vb.) `secretKey.existingSecret`, `apiToken.existingSecret` ve
`auth.existingSecret` ile verin ya da chart'ın ürettiklerini senkronizasyonun
dışında bırakın.

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
| Editör (`editor`) | Kapsamındaki monitor'ları, olayları, bakım pencerelerini; yalnızca kapsamıyla sınırlı kanalları ve durum sayfalarını | Kapsamında monitor ekler, düzenler, siler, susturur, keşiften içe aktarır; kapsamıyla sınırlı kanal, bakım penceresi ve durum sayfası açar |
| İzleyici (`viewer`) | Kapsamındaki monitor'ları (push monitor'larının rapor adresi hariç) ve olayları; kapsamını ilgilendiren bakım pencerelerini | Hiçbir şeyi |

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
üzerinden Secure) bir çerezde saklar. Oturum `UPTIME_SESSION_TTL`
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
deneme sayılmaz. Parola değiştirirken sorulan mevcut parola da aynı sınırla,
ayrı bir sayaçla korunur. Sayaç backend belleğindedir, yeniden başlatınca
sıfırlanır ve en fazla `UPTIME_LOGIN_THROTTLE_MAX_KEYS` (100000) ad tutar;
son denemesi kilit süresinden eski adlar her kilit süresinde bir silinir.
Sınır dolarsa (uydurma adlarla bir saldırı) henüz tutulmayan adlar yer
açılana kadar kilitli sayılır ve backend bunu uyarı olarak loglar.

**Parola denetimleri.** Bir parola denetimi (bcrypt karşılaştırması ya da
dizine bağlanma) bir çekirdeği çeyrek saniye kadar meşgul eder. Aynı anda en
fazla `UPTIME_LOGIN_CONCURRENCY` (varsayılan: CPU sayısının yarısı, en az 1)
denetim çalışır; sırasını `UPTIME_LOGIN_QUEUE_WAIT` (3 sn) içinde bulamayan
giriş 503 ile geri çevrilir. Böylece bir giriş seli kontrollerin CPU'sunu
tüketemez. Hesabı olmayan bir adın reddi de bir parola denetimi kadar sürer:
yanıtın süresi hangi adların yerel hesabı olduğunu söylemez. Frontend
backend'i `BACKEND_TIMEOUT_MS` (3 sn) kadar beklediğinden, sırada bu kadar
bekleyen giriş arayüzde "backend'e ulaşılamadı" olarak da görünebilir.

**Kayıt.** Kim ne zaman neyi oluşturdu, değiştirdi, sildi; girişler ve
başarısız denemeler **Kayıt** sayfasında (yalnızca yöneticiler) listelenir
ve `UPTIME_AUDIT_RETENTION` (90 gün) boyunca tutulur. Başarısız girişlerden
kayda bir adın kilit süresi içindeki ilk hatası ve adı kilitleyen hata
yazılır, aradakiler yazılmaz; hepsi birlikte dakikada en fazla
`UPTIME_LOGIN_AUDIT_PER_MINUTE` (60) girdi olur. Fazlası dakikanın sonunda
sayısıyla uyarı olarak loglanır; her başarısız deneme ayrıca bilgi olarak
loglanır. Böylece uydurma adlarla bir deneme seli ne diski doldurur ne de
kayıttaki diğer girdileri görünmez kılar.

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
zaman yerel parolayla denetlenir.

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

- Sağlayıcıda istemci bu redirect URL ile kaydedilir; chart `route.host`
  ya da `ingress.host` verilmişse adresi kendisi türetir. Route'a host
  vermeyip router'a bıraktıysanız `UPTIME_OIDC_REDIRECT_URL` boş da
  kalabilir: her giriş, arayüzün o an sunulduğu adresin `/auth/callback`
  yolunu kullanır (yalnızca HTTPS; sağlayıcı zaten kayıtlı olmayan adresi
  reddeder). Kayıtlı adresin Route'un gerçek host'u olması yine şarttır.
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

## Arayüz

Arayüz bir çalışma alanıdır: solda tüm monitor'ların listesi, sağda genel
bakış ya da açılan monitor.

Arayüz İngilizce ve Türkçe'dir: tarayıcının istediği dil kullanılır, üst
çubuktaki düğme değiştirir ve seçim bir çerezde kalır; ikisi de yoksa
`DEFAULT_LOCALE` geçerlidir. Bildirim metinlerinin dili backend'de ayrıdır
(`UPTIME_LANGUAGE`); chart'ta ikisini birden `language` değeri ayarlar.

- **Liste**: namespace'e ya da gruba göre toplanır; ada, adrese, gruba ya da
  etikete göre aranır; duruma (sorunlu, çalışıyor, pasif), tipe ve etikete göre
  süzülür. Her satırda durum, 24 saatlik uptime ve son kontroller görünür.
- **Toplu işlem**: listedeki "Seç" ile birden çok monitor işaretlenip birlikte
  durdurulur, devam ettirilir ya da silinir; grup başlığındaki kutu o gruptaki
  görünenlerin hepsini seçer.
- **Genel bakış**: durum sayıları, devam eden kesintiler, son olaylar,
  namespace bazında dağılım ve süresi en yakın sertifikalar.
- **Monitor sayfası**: son kontrollerin çubuğu, yanıt süresi ve uptime
  özetleri, grafik, pod'lar, olaylar, kontrol geçmişi ve ayarların özeti.
  Buradan durdurulur, düzenlenir, kopyalanır ya da silinir.
- **Yeni monitor / düzenle**: tipe göre değişen tek form.
- **Tema**: sağ üstteki düğme sistem, açık ve koyu arasında geçer; seçim
  tarayıcıda saklanır.

## Durum sayfaları

Bir durum sayfası, seçilen monitor'ların durumunu **giriş gerektirmeyen** bir
adreste yayınlar: `/status/<adres>`. Ekip dışındakiler, müşteriler ya da
duvardaki bir ekran içindir. **Durum sayfaları** menüsünden oluşturulur.

- Sayfa bir başlık, açıklama, isteğe bağlı bir **duyuru** ve seçilen
  monitor'lardan oluşur. Her monitor'a sayfada görünecek ayrı bir ad
  verilebilir; verilmezse keşfedilen monitor'larda nesnenin adı (`web:80`),
  diğerlerinde monitor'un adı görünür.
- Her monitor için durum, son `UPTIME_STATUS_PAGE_DAYS` günün gün gün uptime
  çubuğu ve toplam uptime gösterilir; altında son kesintiler listelenir.
- Sayfa yalnızca adı, durumu, uptime'ı ve kesintilerin zamanını gösterir.
  Adresler, hata mesajları ve sayfada olmayan monitor'lar dışarı verilmez.
- Tek bir başarısız kontrol sayfada "kesinti" görünmez; kesinti, alarm eşiği
  aşılıp olay açıldığında görünür.
- "Yayında" işareti kaldırılan sayfa açılmaz (404).
- Editörler yalnızca kapsamlarındaki monitor'ları gösteren sayfaları yönetir.
  Monitor'u olmayan bir sayfa (yalnızca duyuru taşıyan ya da monitor'ları
  silinmiş bir sayfa) yöneticilerindir.

**Rozetler.** Yayındaki bir sayfada gösterilen her monitor'un iki rozeti
vardır; README ya da wiki sayfasına konabilir:

```
![durum](https://<kubeat-adresi>/api/badge/<monitor-id>/status)
![uptime](https://<kubeat-adresi>/api/badge/<monitor-id>/uptime)
```

`?label=metin` ile soldaki yazı değiştirilebilir. Hiçbir yayındaki sayfada
olmayan monitor'un rozeti yoktur (404).

## API

`UPTIME_API_TOKEN` ayarlıysa örneklere `-H "Authorization: Bearer $TOKEN"`
eklenmelidir.

| Metot | Yol | Açıklama |
| --- | --- | --- |
| `GET` | `/api/v1/health` | Backend sağlık durumu |
| `GET` | `/metrics` | Prometheus metrikleri |
| `GET` | `/api/v1/monitors?window=24h` | Monitor'lar; son durum ve uptime ile. İsteğe bağlı: `namespace=x`, `ids=1,2`, `history=40` (her monitor'un son kontrolleri) |
| `POST` | `/api/v1/monitors` | Monitor ekler |
| `GET` | `/api/v1/monitors/{id}?window=24h` | Tek monitor |
| `PATCH` | `/api/v1/monitors/{id}` | Monitor'u düzenler; `{"enabled": false}` ile durdurur |
| `DELETE` | `/api/v1/monitors/{id}` | Monitor'u ve sonuçlarını siler |
| `POST` | `/api/v1/monitors/{id}/clone` | Monitor'un `{"name"}` adıyla bir kopyasını oluşturur; gizli değerler de kopyalanır, kopyanın hedefi başka bir sunucuya çevrilirse yeniden girilmeleri gerekir |
| `GET`, `POST` | `/api/push/{token}?status=up&msg=...&ping=...` | Push monitor'unun raporu; API anahtarı gerekmez |
| `GET` | `/api/v1/monitors/{id}/results?limit=100` | Son kontrol sonuçları, yeniden eskiye |
| `GET` | `/api/v1/monitors/{id}/series?range=24h` | Yanıt süresi ve erişilebilirlik, zaman dilimlerine bölünmüş; `range` yerine `from=&to=` ile belirli bir aralık |
| `GET` | `/api/v1/monitors/{id}/uptime?windows=24h,168h` | İstenen her dönem için uptime |
| `GET` | `/api/v1/monitors/{id}/workload` | Monitor'un arkasındaki pod'lar; hazır olma, restart, CPU ve bellek |
| `GET` | `/api/v1/monitors/{id}/window?from=&to=` | İki an arasında olanlar: kontrol özeti, başarısız kontroller, kesişen olaylar, cluster olayları |
| `GET` | `/api/v1/monitors/{id}/grid?range=24h` | Kontrol geçmişi zaman dilimlerine bölünmüş: dilim başına kontrol, başarısız, ortalama yanıt, en sık hata; aralığın en sık hataları |
| `GET` | `/api/v1/monitors/{id}/channels` | Monitor'un alarmının gittiği etkin kanallar |
| `GET` | `/api/v1/discovery` | Keşfin kullanılabilir olup olmadığı |
| `GET` | `/api/v1/discovery/namespaces` | Cluster'daki namespace'ler |
| `GET` | `/api/v1/discovery/candidates?namespace=x` | Namespace'teki izlenebilir hedefler |
| `POST` | `/api/v1/discovery/import` | Seçilen hedefleri monitor'a çevirir |
| `POST` | `/api/v1/discovery/restore` | Silinmiş bir annotation monitor'ünü yeniden izlemeye alır: `{namespace, source}` |
| `GET` | `/api/v1/incidents?monitor_id=1&limit=100` | Olaylar, yeniden eskiye |
| `GET` | `/api/v1/reports?days=30&namespace=shop&format=csv` | Namespace başına uptime, kesinti sayısı ve süresi; `format=csv` ile indirilebilir |
| `GET` | `/api/v1/maintenance` | Bakım pencereleri, en son başlayan önce |
| `POST` | `/api/v1/maintenance` | Pencere ekler: `name`, `duration_seconds` ya da `ends_at`, isteğe bağlı `starts_at`, `namespace` ya da `monitor_id`, `repeat` ve `timezone` |
| `DELETE` | `/api/v1/maintenance/{id}` | Pencereyi siler; süren pencereyi bitirir |
| `GET` | `/api/v1/channels` | Bildirim kanalları (URL'ler maskelenir) |
| `POST` | `/api/v1/channels` | Kanal ekler: `{"name", "type", "target", "secret", "namespaces"}` |
| `PATCH` | `/api/v1/channels/{id}` | Kanalı düzenler; `{"enabled": false}` ile durdurur |
| `DELETE` | `/api/v1/channels/{id}` | Kanalı siler |
| `POST` | `/api/v1/channels/{id}/test` | Kanala test bildirimi gönderir |
| `GET` | `/api/v1/status-pages` | Durum sayfaları |
| `POST` | `/api/v1/status-pages` | Sayfa ekler: `slug`, `title`, `description`, `announcement`, `published`, `monitors: [{monitor_id, display_name}]` |
| `PATCH` | `/api/v1/status-pages/{id}` | Sayfayı düzenler; verilmeyen alanlar değişmez |
| `DELETE` | `/api/v1/status-pages/{id}` | Sayfayı siler; monitor'lar kalır |
| `GET` | `/api/v1/status-pages/by-slug/{slug}/view` | Yayındaki sayfanın herkese açık içeriği |
| `GET` | `/api/v1/badges/{id}/{status\|uptime}` | Yayındaki bir sayfada olan monitor'un SVG rozeti |

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
- `inverted`: sonucu tersine çevirir; hedef **başarısız** olduğu sürece
  monitor "çalışıyor" sayılır. Erişilmemesi gereken şeyler için.

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
taşınırsa (şeması, host'u ya da portu değişirse) güncelleme `400` ile
reddedilir ve yeniden girilmesi gereken alanları sayar. `http` seçenekleri hiç
gönderilmese de, yönetici için de böyledir: yoksa monitor'u düzenleyebilen
biri onu kendi sunucusuna yöneltip gösterilmeyen değerleri orada okuyabilirdi.

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

```yaml
# Bir CronJob'un sonunda:
command: ["sh", "-c", "yedek-al && curl -fsS 'http://kubeat-backend:8080/api/push/<token>'"]
```

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
  değişirse parola, `http` seçeneklerindeki gibi yeniden girilmelidir. Hata
  mesajları sunucunun söylediğini taşır (`password authentication failed for
  user "probe"`), parolayı taşımaz.

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
paylaşılabilir; bildirimlerdeki "Kubeat'te aç" bağlantısı da doğrudan
kesintinin aralığına açılır. Grafiğin altında **Seçilen aralık** paneli
(`GET /api/v1/monitors/{id}/window?from=&to=`) belirir:

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

### Raporlar

**Raporlar** sayfası (`GET /api/v1/reports?days=30`) son 1, 7, 30 ya da 90
günü namespace başına özetler: uptime, kontrol sayısı, ortalama yanıt,
kesinti sayısı, toplam ve en uzun kesinti süresi; her namespace'te en
düşük uptime'lı monitor en üsttedir. Hesap saatlik özetlerden ve `down`
türündeki olaylardan yapılır; döneme taşan kesintilerin yalnızca dönem
içindeki kısmı sayılır, süren kesinti dönemin sonuna kadar sayılır.
Kullanıcı yalnızca kapsamındaki namespace'leri görür. `format=csv` (sayfada
"CSV indir") monitor başına bir satır verir; ekiplere giden aylık rapora
eklenmek için.

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
| `kubeat_monitor_cert_expiry_timestamp_seconds` | Sertifikanın bitiş zamanı |
| `kubeat_scheduler_last_tick_timestamp_seconds` | Scheduler'ın son çalıştığı an; ilerlemiyorsa kontrol yapılmıyordur |
| `kubeat_scheduler_dispatch_skipped_total` | Boş worker olmadığı için bekleyen kontroller; artıyorsa `UPTIME_WORKERS` düşüktür |
| `kubeat_results_dropped_total` | Veritabanına yazılamayan sonuçlar |
| `kubeat_notifications_total{channel_type,event,result}` | Bildirim gönderimleri |

Chart'ta `metrics.serviceMonitor.enabled=true` bir ServiceMonitor oluşturur;
böylece monitor durumları cluster'ın kendi Prometheus'unda sorgulanabilir
(OpenShift konsolunda Observe → Metrics). İzlenen nesnenin namespace'i
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

Yedek dizini veritabanından **ayrı bir volume** olmalıdır; aynı volume'daki
kopya veritabanıyla birlikte kaybolur. Chart'ta `backup.enabled=true` bunun
için ayrı bir PVC oluşturur. Bu yine aynı cluster'dadır: cluster'ın
tamamını kaybetmeye karşı kopyaların dışarıya da taşınması gerekir.

Geri yükleme:

1. Backend'i durdurun (`kubectl scale deploy/kubeat-backend --replicas=0`).
2. Seçtiğiniz kopyayı veritabanı dosyasının yerine kopyalayın ve yanındaki
   `-wal` ile `-shm` dosyalarını silin.
3. Backend'i başlatın. Kopyadan sonraki kontrol sonuçları ve değişiklikler
   kaybolur.

Kopyadaki gizli değerler, kopya alındığı sırada kullanılan
`UPTIME_SECRET_KEY` ile şifrelidir. Başka bir kuruluma geri yüklerken o
anahtarı da taşıyın; yoksa backend başlamaz.

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
değiştirilmez.

Bu monitor'lar arayüzde **annotation** rozetiyle görünür. Hedefleri ve
zamanlamaları nesneden geldiği için arayüzden ya da API'den değiştirilemez
(`409`); etiket, açıklama, duraklatma ve bildirimler değiştirilebilir.

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
durumda etkin tüm kanallara bir bildirim gider; olay açıkken tekrar bildirim
gönderilmez.

HTTPS hedeflerde her kontrol sertifikanın bitiş tarihini de kaydeder. Bitişe
`UPTIME_CERT_WARNING` (varsayılan 14 gün) kala ayrı bir "sertifika" olayı
açılır ve bildirim gider; sertifika yenilenince olay kapanır. Bu olay
kesintilerden bağımsızdır: hedef çalışırken de açık olabilir.

### Hatırlatma

`UPTIME_REMINDER_INTERVAL` ayarlanırsa süren bir kesinti bu aralıkla yeniden
bildirilir ("hâlâ erişilemiyor"). Varsayılan `0` olup hatırlatma gönderilmez.
Bakım penceresi içinde hatırlatma gitmez.

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

**Kapsam.** Üç süzgeç birlikte uygulanır, hepsi isteğe bağlıdır:

- `namespaces`: yalnızca o namespace'lerdeki monitor'lar; boşsa hepsi. Her
  ekibe kendi namespace'inin alarmları gider, ortak bir kanal her şeyi görür.
- `tags`: yalnızca bu etiketlerden birini taşıyan monitor'lar; boşsa hepsi.
  Monitor başına yönlendirme böyle yapılır: PagerDuty kanalına `pager`
  etiketi verilir, yalnızca `pager` etiketli monitor'lar gece uyandırır.
- `events`: `down`, `up`, `reminder`, `cert` alt kümesi; boşsa hepsi.
  PagerDuty için `down` ve `up`, sohbet kanalı için hepsi gibi.

Monitor sayfası, o monitor'un alarmının hangi kanallara gittiğini yazar
(`GET /api/v1/monitors/{id}/channels`); kapsamı tutan etkin kanal yoksa
sayfa bunu uyarı olarak gösterir — kimsenin duymayacağı bir monitor
olmasın.

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
`url` alanı; e-posta son satırında bağlantı. Bağlantı tek monitor'da
monitor sayfasına, gruplanmış mesajda namespace sayfasına gider ve
`UPTIME_PUBLIC_URL` (chart'ta `publicURL`, Route/Ingress host'undan
türetilir) ayarlı değilse hiç konmaz.

Monitor adları, hedefler ve hata metinleri (push raporunun mesajı dahil)
yazıldığı gibi görünür: Slack, Mattermost ve Google Chat'te `&`, `<` ve `>`
kaçışlanır; metindeki `<!channel>` ya da `<users/all>` kimseyi çağırmaz,
etiketi adresini gizleyen bir bağlantı oluşmaz. Discord mesajı kimseyi
etiketlemez (`@everyone` dahil); Telegram'a giden metin HTML olarak
kaçışlanır.

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
vekilinize yönlendirilebilir. Bu adresler de hedef politikasına tabidir;
vekil loopback'te dinliyorsa istisna gerekir.

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

`event` değeri `down`, `up`, `reminder`, `cert_expiring`, `cert_renewed` ya
da `test` olur; `incident.kind` ise `down` ya da `cert`.

## Yapılandırma

Ayarlar koda gömülmez; hepsi ortam değişkeniyle verilir. Örnek değerler
[.env.example](.env.example) dosyasındadır.

| Değişken | Varsayılan | Açıklama |
| --- | --- | --- |
| `UPTIME_ADDR` | `:8080` | Backend'in dinlediği adres |
| `UPTIME_DB_PATH` | `uptime.db` | SQLite dosyasının yolu |
| `UPTIME_API_TOKEN` | – | Betikler için yönetici yetkisiyle çalışan API anahtarı; boşsa API yalnızca oturumla kullanılır |
| `UPTIME_SECRET_KEY` | – | Veritabanındaki token ve parolaları şifreleyen anahtar; en az 32 karakter. Zorunlu: yoksa backend başlamaz (bkz. `UPTIME_ALLOW_PLAINTEXT_SECRETS`) |
| `UPTIME_SECRET_KEY_FILE` | – | Anahtarın okunduğu dosya, örneğin bağlanmış bir Secret; `UPTIME_SECRET_KEY` ile birlikte verilemez |
| `UPTIME_ALLOW_PLAINTEXT_SECRETS` | `false` | Anahtar yokken de başlar; gizli değerler şifresiz yazılır |
| `UPTIME_ADMIN_PASSWORD` | – | Hesap yokken `admin` yöneticisini bu parolayla oluşturur; parola kurallarına uymazsa backend başlamaz. Boşsa ilk yönetici `/setup`'ta oluşturulur |
| `UPTIME_ADMIN_PASSWORD_RESET` | `false` | `admin` parolasını `UPTIME_ADMIN_PASSWORD` değerine çeker; bir kez kullanılır |
| `UPTIME_SESSION_TTL` | `12h` | Oturum süresi |
| `UPTIME_LOGIN_MAX_FAILURES` | `10` | Bir kullanıcı adını kilitleyen hatalı deneme sayısı |
| `UPTIME_LOGIN_LOCKOUT` | `15m` | Kilidin süresi; denemesi bundan eski adlar bu aralıkla sayaçtan silinir |
| `UPTIME_LOGIN_THROTTLE_MAX_KEYS` | `100000` | Deneme sayacının tuttuğu en fazla kullanıcı adı; doluyken henüz tutulmayan adlar kilitli sayılır |
| `UPTIME_LOGIN_CONCURRENCY` | `0` | Aynı anda çalışan parola denetimi (bcrypt ya da dizine bağlanma) sayısı; `0` CPU sayısının yarısı, en az 1 |
| `UPTIME_LOGIN_QUEUE_WAIT` | `3s` | Bir girişin parola denetimi sırasını en fazla bekleme süresi; sonra 503 |
| `UPTIME_LOGIN_AUDIT_PER_MINUTE` | `60` | Kayda bir dakikada yazılan en fazla başarısız giriş; fazlası yalnızca uyarı olarak sayılır |
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
| `UPTIME_LDAP_TIMEOUT` | `10s` | Dizin işlemlerinin zaman aşımı |
| `UPTIME_LDAP_DEFAULT_ROLE`, `UPTIME_LDAP_DEFAULT_NAMESPACES` | – | Eşleşen grubu olmayan dizin kullanıcısının rolü ve kapsamı; boşsa giremez |
| `UPTIME_OIDC_ISSUER` | – | OpenID Connect sağlayıcısının issuer adresi; boşsa SSO kapalı |
| `UPTIME_OIDC_CLIENT_ID` | – | Sağlayıcıda kayıtlı istemci kimliği (issuer ile zorunlu) |
| `UPTIME_OIDC_CLIENT_SECRET` | – | İstemci sırrı; public client için boş |
| `UPTIME_OIDC_REDIRECT_URL` | – | Sağlayıcının geri döndüğü adres: `https://<kubeat>/auth/callback`; boşsa her giriş, arayüzün o an sunulduğu adresi kullanır |
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
| `UPTIME_SERIES_POINTS` | `120` | Bir grafikteki yaklaşık nokta sayısı |
| `UPTIME_HTTP_INSECURE_SKIP_VERIFY` | `false` | TLS kullanan tüm kontrollerde (HTTP, TCP, gRPC, WebSocket) sertifika doğrulamasını kapatır |
| `UPTIME_KEYWORD_BODY_LIMIT_BYTES` | `1048576` | Keyword ya da JSON değeri aranırken yanıt gövdesinin okunan kısmı |
| `UPTIME_TARGET_DENY_CIDRS` | – | Kontrollerin ve bildirimlerin bağlanmayacağı, yerleşik listeye (loopback, link-local, metadata, multicast) eklenen aralıklar; virgülle ayrılmış CIDR. Bkz. "Hedef politikası" |
| `UPTIME_TARGET_ALLOW_CIDRS` | – | Yasaklardan istisna tutulan aralıklar; virgülle ayrılmış CIDR, her yasağın önüne geçer |
| `UPTIME_MIN_CHECK_INTERVAL` | `5s` | Bir monitor için izin verilen en kısa kontrol aralığı |
| `UPTIME_STATS_WINDOW` | `24h` | Uptime yüzdesinin hesaplandığı varsayılan süre |
| `UPTIME_MAX_RESULTS` | `1000` | Tek istekte dönen en fazla kontrol sonucu |
| `UPTIME_STATUS_PAGE_DAYS` | `90` | Durum sayfasında gösterilen gün sayısı |
| `UPTIME_STATUS_PAGE_INCIDENT_WINDOW` | `336h` | Durum sayfasında listelenen çözülmüş kesintilerin ne kadar geriye gittiği |
| `UPTIME_DEFAULT_FAILURE_THRESHOLD` | `3` | Monitor'da belirtilmezse, olay açmak için gereken art arda başarısız kontrol sayısı |
| `UPTIME_RECOVERY_THRESHOLD` | `1` | Olayı kapatmak için gereken art arda başarılı kontrol sayısı |
| `UPTIME_CERT_WARNING` | `336h` | Sertifika bitişine bu kadar kala uyarı olayı açılır |
| `UPTIME_REMINDER_INTERVAL` | `0` | Süren kesintinin yeniden bildirilme aralığı; `0` kapalı |
| `UPTIME_NOTIFY_ATTEMPTS` | `3` | Kanal başına bildirim deneme sayısı |
| `UPTIME_NOTIFY_RETRY_DELAY` | `5s` | Bildirim denemeleri arasındaki bekleme |
| `UPTIME_NOTIFY_TIMEOUT` | `5s` | Tek bir bildirim denemesinin zaman aşımı |
| `UPTIME_NOTIFY_QUEUE_SIZE` | `100` | Gönderilmeyi bekleyebilecek en fazla bildirim sayısı |
| `UPTIME_PUBLIC_URL` | – | Kullanıcıların Kubeat'e eriştiği adres; bildirimlerdeki bağlantılar bununla kurulur, boşsa bağlantı konmaz |
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
| `UPTIME_BACKUP_KEEP` | `7` | Saklanan kopya sayısı |
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
| `REFRESH_INTERVAL_MS` | `5000` | Açık sayfaların veriyi yenileme aralığı (ms) |
| `STATUS_PAGE_REFRESH_MS` | `30000` | Açık bir durum sayfasının yenilenme aralığı (ms) |
| `DASHBOARD_FLAT_LIMIT` | `50` | Bu sayıya kadar monitor listesinde tüm namespace'ler açık gelir; üzerinde yalnızca sorunlu olanlar açılır |
| `LIST_BEATS` | `14` | Monitor listesinde her satırda gösterilen son kontrol sayısı |
| `HISTORY_LIMIT` | `60` | Monitor sayfasındaki kalp atışı çubuğunda gösterilen kontrol sayısı |
| `RESULTS_LIMIT` | `30` | Monitor sayfasında listelenen kontrol sayısı |
| `OVERVIEW_EVENTS` | `12` | Genel bakışta listelenen son olay sayısı |
| `OVERVIEW_CERTIFICATES` | `5` | Genel bakışta listelenen, süresi en yakın sertifika sayısı |
| `NEW_MONITOR_INTERVAL_SECONDS` | `60` | Yeni monitor formunun başlangıçtaki kontrol aralığı |
| `NEW_MONITOR_TIMEOUT_SECONDS` | `10` | Yeni monitor formunun başlangıçtaki zaman aşımı |
| `NEW_MONITOR_FAILURE_THRESHOLD` | `3` | Yeni monitor formunun başlangıçtaki alarm eşiği |
| `PUSH_INTERNAL_URL` | `BACKEND_URL` | Push monitor'larında "cluster içinden" gösterilen rapor adresinin kökü |
| `INCIDENTS_LIMIT` | `100` | Bir sayfada listelenen olay sayısı |
| `AUDIT_LIMIT` | `200` | Kayıt sayfasında listelenen girdi sayısı |
| `MAINTENANCE_DURATIONS` | `30m,1h,2h,4h,8h,24h` | Susturma ve bakım için sunulan süreler |
| `MAINTENANCE_LIMIT` | `50` | Listelenen bakım penceresi sayısı |
| `UPTIME_WINDOWS` | `24h,168h,720h` | Monitor sayfasında uptime'ı gösterilen dönemler |
| `CHART_RANGES` | `24h,1h,168h,720h` | Grafik için sunulan aralıklar; ilki varsayılandır |

Süreler Go biçimindedir (`500ms`, `5s`, `1m30s`).

## Katkı

Branch stratejisi ve commit kuralları: [CONTRIBUTING.md](CONTRIBUTING.md)

## Lisans

[Apache License 2.0](LICENSE). Katkılar aynı lisansla kabul edilir.
