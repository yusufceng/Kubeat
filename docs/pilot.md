# Pilot: şirket cluster'ında ilk gerçek kullanım

Kubeat'i bir ekibin gerçekten kullanacağı bir cluster'a kurup üç hafta
izlemek. Amaç özellik eklemek değil, dört soruya cevap almak:

1. Gerçek bir kesintiyi, ekip fark etmeden önce yakalayıp bildiriyor mu?
2. Ekip, biz yanında olmadan kendi monitor'lerini ekleyip kullanıyor mu?
3. Yanlış alarm oranı kabul edilebilir mi?
4. Ücretli olacak özelliklerden (SSO, namespace sınırı, denetim kaydı,
   çoklu durum sayfası) hangileri ekibe gerçekten gerekli?

Pilotun sonunda karar: **devam** (kalıcı kurulum), **düzelt** (listelenen
sorunlar giderilip ikinci tur) ya da **durdur**.

Kurulum adımları 0.42.0 chart'ıyla sıfırdan bir minikube'da baştan sona
denendi (2026-10-27); OpenShift'e özgü maddeler (Route, SCC, LDAP, OIDC,
yedekleme) Developer Sandbox'ta doğrulandı. Aynı gün pilot değerleri 0.42.2
ile Developer Sandbox'a da kuruldu ve sonraki her sürümle yükseltildi: pilot
lisansı, HSTS'li Route, LDAP ve
OIDC girişi, namespace sınırlı keşif, yedek ve bir pilot durum sayfası orada
çalışıyor. Daha ayrıntılı teknik kontrol listesi:
[openshift-dogrulama.md](openshift-dogrulama.md).

Güncelleme (2026-10-27): şirket cluster'ı ve pilot ekibi yok; pilot aynı
takvimle (karar 2026-11-27) Developer Sandbox'ta sürer ve geri bildirim
sprint ekibinin müşteri ve SRE ajanlarından gelir
([agents/sprint-takimi.md](agents/sprint-takimi.md), [sprints/](sprints/)).
Aşağıdaki ekip adımları ve ön koşullar gerçek bir ekip ve cluster
bulunduğunda olduğu gibi geçerlidir. Sandbox boşta kalan pod'ları
uyuttuğundan kesinti yakalama ve yanlış alarm ölçümleri orada güvenilir
değildir; sandbox ürünü göstermek ve alıştırmak içindir.

## Kapsam

| | Öneri |
| --- | --- |
| Cluster | Bir OpenShift cluster'ı; üretim olabilir, Kubeat yalnızca okur ve kontrol eder |
| Ekip | Bir ekip, 5–10 kişi; uygulamaları Route ya da Ingress ile dışarı açık olsun |
| Namespace'ler | Ekibin 2–3 namespace'i (prod + test gibi) |
| Süre | 3 hafta: 1 hafta kurulum ve alışma, 2 hafta kullanım; sonunda karar |
| Kubeat'in yeri | `kubeat` namespace'i |
| Keşif | Cluster-admin varsa cluster geneli; yoksa `discovery.namespaces` ile ekibin namespace'leri (bkz. README "Cluster-admin olmadan") |
| Lisans | Pilot boyunca tüm özellikler açık; ekibin hangilerini kullandığına bakılır |

Başarı ölçütleri, pilot başında ekiple birlikte yazılır. Başlangıç önerisi:

- Ekibin kendi eklediği (bizim değil) en az 10 monitor.
- Pilot boyunca en az bir gerçek kesinti Kubeat tarafından bildirilmiş.
- Haftada en az 3 farklı kişi giriş yapmış.
- Yanlış alarm sayısı gerçek kesinti sayısını geçmemiş.
- Ekibin cevabı: "Pilot bitince kapatalım mı?" sorusuna **hayır**.

## Ön koşullar

Kurulumdan önce toplanacaklar. Çoğu başka ekiplerden istenir, bu yüzden
pilotun ilk işi budur:

| Ne | Kimden | Not |
| --- | --- | --- |
| `ghcr.io`'ya erişim ya da iç registry | Platform ekibi | Erişim yoksa imajlar ve chart aynalanır (aşağıda) |
| Cluster'da RWO StorageClass | Platform ekibi | Veri 2 Gi, yedek 5 Gi; yedek en az (keep + 1) × veritabanı boyutu, varsayılan 7 kopya ile ~640 MiB'a kadar veritabanı için yeter (README "Yedekleme ve geri yükleme") |
| Route host | Platform / DNS | `kubeat.apps.<cluster-domain>`; router'ın verdiği host da olur |
| AD/LDAP bilgileri | Kimlik ekibi | Sunucu (`ldaps://`), base DN, arama hesabı ve parolası, CA sertifikası; yönetici grubunun ve ekip grubunun DN'leri |
| Bildirim adresi | Ekip | Slack, Teams, Mattermost ya da Google Chat webhook URL'i; ya da SMTP |
| Vekil (proxy) | Ağ ekibi | Cluster'dan Slack/Teams'e çıkış vekil üzerindense `HTTPS_PROXY` ve `NO_PROXY` |
| User workload monitoring | Platform ekibi | Pod CPU/bellek ve Alertmanager alarmı için; isteğe bağlı |
| İzlenen namespace'lerde Kubeat'e izin | Platform / güvenlik ekibi | Ekip namespace'leri proje şablonuyla varsayılan-ret NetworkPolicy alıyorsa `allow-from-kubeat` politikası (README "Ağ erişimi", İzlenen namespace'ler: şablona, var olan namespace'lere ya da AdminNetworkPolicy ile); yoksa Service ve pod kontrolleri `connect_timeout` ile ilk gün yanlış kesinti açar, Route'lar çalışır |
| Lisans dosyası | Biz | İmzalama aracıyla, pilot süresi + 1 ay |

Hedef politikası özel ağ aralıklarını (`10/8`, `172.16/12`, `192.168/16`)
**yasaklamaz**: şirket içi bir Mattermost ya da SMTP için istisna gerekmez.
Yalnızca loopback ve bulut metadata adresleri yasaktır (README "Hedef
politikası").

### İmajları aynalamak

`ghcr.io` cluster'dan erişilemiyorsa iki imaj ve chart iç registry'ye
imzalarıyla aynalanır ve kurulumdan önce orada doğrulanır. Gerekenler
[ORAS](https://oras.land) (`oras cp -r`), cosign v3 ve Sigstore'un trusted
root'u; doğrulama Sigstore'a bağlanmaz, trusted root'u bağlı bir makinede
almak yeter:

```sh
# Bağlı bir makinede: trusted root (README "Hava boşluklu ortamda doğrulamak")
cosign initialize
cp ~/.sigstore/root/tuf-repo-cdn.sigstore.dev/targets/trusted_root.json .

# İmajlar ve chart, imzalarıyla
R=registry.sirket.local/kubeat
oras cp -r ghcr.io/yusufceng/kubeat-backend:0.49.0  $R/kubeat-backend:0.49.0
oras cp -r ghcr.io/yusufceng/kubeat-frontend:0.49.0 $R/kubeat-frontend:0.49.0
oras cp -r ghcr.io/yusufceng/charts/kubeat:0.49.0   registry.sirket.local/charts/kubeat:0.49.0

# İç ağda, aynalanan kopyaların imzası
cosign verify --trusted-root trusted_root.json \
  --certificate-identity https://github.com/yusufceng/Kubeat/.github/workflows/release.yml@refs/tags/v0.49.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  $R/kubeat-backend:0.49.0 $R/kubeat-frontend:0.49.0 registry.sirket.local/charts/kubeat:0.49.0
```

- `oras cp -r` imajı iki mimarisi, SBOM'u ve provenance'ıyla, `-r` de imza
  bundle'ıyla kopyalar; digest değişmez. Yayım iş akışı aynı yolu her
  sürümde CI'da koşar: bir registry'ye aynalar, ağ kesikken doğrular (README
  "Hava boşluklu ortamda doğrulamak").
- `cosign verify` her referansın imzasını ve sertifikadaki kimliği (bu
  sürümün etiketinde koşan yayım iş akışı) denetler; biri geçmezse kurulum
  yapılmaz. Release'teki digest'lerle doğrulayıp kurmak için README
  "Digest'le sabitlemek".

Values'ta `backend.image.repository`, `frontend.image.repository` ve
registry parola istiyorsa `imagePullSecrets` verilir; chart
`oci://registry.sirket.local/charts/kubeat` adresinden kurulur.

**Not: imzasız yol.** `skopeo copy --all` imajı attestation'larıyla, `helm
pull` ile `helm push` chart'ı taşır, ikisi de imzayı taşımaz: iç registry'de
doğrulama yapılamaz (README "İmzalarla aynalamak"). Yalnızca imza
doğrulanmayacaksa:

```sh
skopeo copy --all docker://ghcr.io/yusufceng/kubeat-backend:0.49.0  docker://$R/kubeat-backend:0.49.0
skopeo copy --all docker://ghcr.io/yusufceng/kubeat-frontend:0.49.0 docker://$R/kubeat-frontend:0.49.0
helm pull oci://ghcr.io/yusufceng/charts/kubeat --version 0.49.0        # kubeat-0.49.0.tgz
helm push kubeat-0.49.0.tgz oci://registry.sirket.local/charts
```

## Kurulum

### 1. Namespace ve Secret'lar

```sh
oc new-project kubeat
oc -n kubeat create secret generic kubeat-license --from-file=license=sirket.license
oc -n kubeat create secret generic kubeat-ldap-bind --from-literal=password='<arama hesabının parolası>'
oc -n kubeat create configmap kubeat-ldap-ca --from-file=ca.crt=ad-ca.pem
```

### 2. values-pilot.yaml

Köşeli parantezli yerler cluster'a göre doldurulur. Vekil gerekmiyorsa
`backend.extraEnv` bloğu silinir; user workload monitoring yoksa
`metrics` ve `prometheus` blokları silinir.

```yaml
language: tr
publicURL: https://kubeat.apps.<cluster-domain>

route:
  enabled: true
  host: kubeat.apps.<cluster-domain>
  annotations:
    haproxy.router.openshift.io/hsts_header: max-age=31536000

license:
  existingSecret: kubeat-license

ldap:
  url: ldaps://ad.sirket.local:636
  baseDN: DC=sirket,DC=local
  userFilter: "(sAMAccountName={username})"
  bindDN: CN=svc-kubeat,OU=Service Accounts,DC=sirket,DC=local
  bindPasswordSecret:
    name: kubeat-ldap-bind
    key: password
  extraEnv:
    UPTIME_LDAP_CA_FILE: /etc/kubeat/ldap-ca/ca.crt

backend:
  extraVolumes:
    - name: ldap-ca
      configMap:
        name: kubeat-ldap-ca
  extraVolumeMounts:
    - name: ldap-ca
      mountPath: /etc/kubeat/ldap-ca
      readOnly: true
  extraEnv:
    HTTPS_PROXY: http://proxy.sirket.local:3128
    NO_PROXY: .svc,.cluster.local,.sirket.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16
    # Yalnız vekil cluster dışındaysa (0.50'den beri; README "Vekilli kurulum")
    UPTIME_TARGET_SCOPE_TRUST_PROXY: "true"

backup:
  enabled: true

discovery:
  enabled: true
  # namespaces: [ekip-prod, ekip-test]   # cluster-admin yoksa

metrics:
  serviceMonitor:
    enabled: true
  prometheusRule:
    enabled: true
prometheus:
  url: https://thanos-querier.openshift-monitoring.svc:9091
  tokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
  caFile: /var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt
  openshiftMonitoringView: true
```

`podSecurityContext` boş kalır; UID'yi SCC atar. Backend'in dışarıya
çıkışı (Slack, Teams) vekil üzerindense `NO_PROXY`'de cluster içi adlar ve
aralıklar olmalıdır, yoksa kontroller de vekile gider.

`UPTIME_TARGET_SCOPE_TRUST_PROXY`, pod'lar dış adları çözemiyor ve vekil
cluster dışında çalışıyorsa açılır. O zaman namespace'le sınırlı bir editör,
yalnız vekilin çözdüğü bir dış ada (Slack, Teams ya da bir SaaS'ın durum
adresi) HTTP monitor'ü ve kanal kurabilir. Vekil cluster içindeyse satırı
silin: o vekil cluster'ın adlarını çözüp ClusterIP'lere bağlanabilir. Ayar
kapalıyken ve vekil verilmişken backend açılışta bir `WARN` yazar; satırı
sildiyseniz aşağıdaki "Uyarı ve hata" kontrolünde yalnız bu satır beklenir.
0.49 bu ayarı tanımaz ve yok sayar.

### 3. Kurulum

```sh
helm upgrade --install kubeat oci://ghcr.io/yusufceng/charts/kubeat --version 0.49.0 \
  -n kubeat -f values-pilot.yaml
oc -n kubeat rollout status deploy/kubeat-backend
oc -n kubeat rollout status deploy/kubeat-frontend
```

Kurulum notları giriş parolasını nasıl okuyacağınızı ve SSO için neyin
eksik olduğunu yazar; saklayın.

### 4. Kurulum sonrası kontrol

| Ne | Komut | Beklenen |
| --- | --- | --- |
| Pod'lar | `oc -n kubeat get pods` | İki pod `Running`, `restricted-v2` SCC |
| Lisans | `oc -n kubeat logs deploy/kubeat-backend \| grep license` | `license in effect`, müşteri adı ve bitiş tarihi |
| Dizin | aynı log, `directory sign-in enabled` | LDAP adresi; `LDAP or OIDC is configured, but` uyarısı **yok** |
| Keşif | aynı log, `cluster discovery enabled` | `mode: in-cluster`; namespace'le sınırlıysa namespace'ler |
| Yedek | `oc -n kubeat exec deploy/kubeat-backend -- ls /backup` | İlk kopya açılışta alınır: `kubeat-<tarih>.db` |
| Route | Tarayıcıda `https://kubeat.apps.<cluster-domain>` | Giriş sayfası; HTTP'den HTTPS'e yönlendirme |
| Uyarı ve hata | `oc -n kubeat logs deploy/kubeat-backend \| grep -E '"level":"(WARN\|ERROR)"'` | Boş |
| Ekip namespace'inde politika | Ekibin bir Service'ine monitor ekleyin (keşif ya da `kubeat.io/monitor` annotation'ı) | İlk kontrol yeşil; ekip namespace'indeki bir Service monitor'ü `connect_timeout` vermiyor. Veriyorsa namespace'in varsayılan-ret politikası Kubeat'i engelliyor: ön koşullardaki `allow-from-kubeat` (README "Ağ erişimi", İzlenen namespace'ler) |

Bir şey eksikse `helm upgrade` aynı komutla tekrarlanır; backend 10–20
saniye içinde yeniden kalkar.

## İlk gün: yönetici kurulumu

Hepsi arayüzde, yaklaşık bir saat. Sıra önemli: önce kendi girişinizi
dizin hesabınıza taşıyın, sonra ekibi ekleyin.

1. **Giriş.** `admin` ile girin; parola:
   `oc -n kubeat get secret kubeat-admin -o jsonpath='{.data.password}' | base64 -d`.
2. **Yönetici grubu.** Kullanıcılar → LDAP → grup eşlemesi: yönetici
   grubunun DN'si → rol **Yönetici**. Çıkıp kendi AD hesabınızla girin;
   Kullanıcılar sayfasında hesabınızın kaynağı `ldap`, rolü yönetici olmalı.
   Bundan sonra `admin` yalnızca acil durum hesabıdır.
3. **Ekip grubu.** Ekibin AD grubu → rol **Editör**, namespace'ler ekibin
   namespace'leri. Ekip üyeleri böylece yalnızca kendi namespace'lerini
   görür ve oradaki Service'lere ve genel adreslere monitor ekleyebilir.
   0.48'den beri editör kurum ağına (özel aralıklara) çözülen bir ada ve router
   VIP'ine çözülen bir Route adına monitor kuramaz (README "Hedef politikası");
   bunları yönetici kurar ya da keşiften içe aktarılır. Yukarıdaki vekilli
   kurulumda yalnız vekilin çözdüğü bir dış ad 0.49'da da reddedilir; 0.50'de
   `UPTIME_TARGET_SCOPE_TRUST_PROXY: "true"` ile editör onu kurabilir (vekil
   cluster dışındaysa; README "Vekilli kurulum"). Gruba girmeyen biri giriş
   yapamaz (varsayılan rol boş); isteniyorsa `ldap.extraEnv` altında
   `UPTIME_LDAP_DEFAULT_ROLE: viewer` ve `UPTIME_LDAP_DEFAULT_NAMESPACES:
   "*"` ile dizindeki herkese izleyici rolü verilir. AD'de gruplar iç
   içedir ve `memberOf` yalnızca doğrudan üyelikleri verir: ya ekibin
   doğrudan üye olduğu grubu eşleyin ya da `ldap.extraEnv` altında zincir
   süzgecini açın (`UPTIME_LDAP_GROUP_FILTER:
   "(member:1.2.840.113556.1.4.1941:={dn})"` ve grupların durduğu dar bir
   `UPTIME_LDAP_GROUP_BASE_DN`; README "LDAP"). Eşleşen grubu olmayan üye
   giremez ve denetim kaydında `login_denied` görünür.
4. **Keşif.** Keşif → ekibin namespace'ini seçin → Tara. Kubeat her
   Service, Route, Ingress ve Deployment için bir kontrol önerir ve
   gerekçesini yazar (readiness probe'u, Route adresi...). Route'ları ve
   probe'u olan Service'leri seçip "izlemeye alın"; workload'lar (hazır
   pod sayısı) seçili gelmez, onları ekip isterse ikinci turda ekleyin.
   Ekibin namespace'i varsayılan-ret politikalıysa Service kontrolleri
   `connect_timeout` ile düşer, Route'lar çalışır: ön koşullardaki
   `allow-from-kubeat` politikası önce uygulanmış olmalı.
5. **Kanal.** Bildirimler → Yeni kanal: ekibin Slack/Teams webhook'u,
   kapsam ekibin namespace'leri. Kaydedince kanal satırındaki test
   düğmesiyle bir deneme gönderin; ekip kanalında görünmeli. Gelmiyorsa
   satır nedenini gösterir (vekil, politika, 4xx).
6. **Annotation'ı deneyin.** Ekibin bir Service'ine
   `oc -n <ns> annotate svc/<ad> kubeat.io/monitor=true` yazın; bir
   dakika içinde monitor listesinde `annotation` rozetiyle görünür. Bu,
   ekibe göstereceğiniz asıl yol: manifest'e bir satır, monitor
   kendiliğinden gelir ve gider.
7. **Durum sayfası (isteğe bağlı).** Durum sayfaları → ekibin
   servisleri için bir sayfa; ekip bunu kendi müşterilerine ya da
   yöneticisine gösterebilir. Pilotta buna ilgi, `status_pages`
   özelliğinin değerini ölçer.
8. **Duyuru.** Aşağıdaki metni ekibin kanalına atın.

## Ekibe duyuru

```
Merhaba. <cluster> üzerinde Kubeat'i deniyoruz: cluster içindeki
servisleri izleyip kesinti olunca <kanal>'a yazıyor. Üç hafta sürecek bir
pilot; sonunda sizin görüşünüze göre karar vereceğiz.

Adres: https://kubeat.apps.<cluster-domain> — AD hesabınızla girin.

Ne yapabilirsiniz:
- Genel bakışta <namespace'ler> içindeki servislerin durumunu görürsünüz.
- Kendi servisinizi izletmek için manifest'ine şu annotation'ı ekleyin,
  gerisi kendiliğinden olur:  kubeat.io/monitor: "true"
- Keşif sayfasından da seçip ekleyebilirsiniz; cluster dışındaki bir
  adresi "Yeni monitor" ile elle eklersiniz.
- Bakım yapacaksanız Bakım pencereleri'nden bir pencere açın; o sırada
  alarm gitmez.

Yanlış alarm, kafa karıştıran bir ekran ya da "keşke şu olsaydı" dediğiniz
her şeyi <kişi>'ye yazın; her hafta 15 dakika toplanıp konuşacağız.
```

## Pilot boyunca

**Dokunmayın.** Pilot sırasında Kubeat yükseltilmez, kontrol aralıkları
toplu değiştirilmez, kanallar yeniden düzenlenmez. Bir hata ekibi
engelliyorsa düzeltme yapılır ve ekibe söylenir; başka her şey pilot
sonuna not edilir. Ölçülen şey ürünün bugünkü hali olmalı.

**Haftalık ölçüm** (10 dakika, arayüzden):

| Ne | Nereden |
| --- | --- |
| Namespace başına monitor sayısı; kaçı annotation ile | Genel bakış (kenar çubuğu), monitor rozetleri |
| Kim giriş yapmış, ne zaman | Kullanıcılar → "Son giriş" |
| Kesintiler, süreleri, MTTR | Raporlar → 7 gün → "CSV indir" |
| Haftanın olayları ve kaçının gerçek olduğu | Olaylar → 7 gün süzgeci; her olayı ekiple "gerçek miydi?" diye işaretleyin |
| Kim ne eklemiş, ne silmiş | Denetim kaydı |
| Kapsam/yetki dışı hedef kurma girişimi (cross-tenant) | Denetim kaydında `deny`/`target-scope` satırları (kapsamı sınırlı kullanıcının başka ekibin Service'ine ya da API sunucusuna monitor/kanal kurma denemesi); Prometheus varsa `kubeat_target_scope_denied_total{reason}` (`reason=target` hedef kapsam dışı, `reason=namespace` düzenleme yetkisi yok) |
| Bildirim teslim oranı | Bildirimler → "Son gönderim"; Prometheus varsa `kubeat_notifications_total` |
| Başarısız girişler ve dizin kesintisi | Prometheus varsa `kubeat_signin_failures_total{reason}` (dizin düşünce `directory_unreachable` artar; tüm dizin/SSO girişleri genel 401 görür ve bu sırada yerel `admin` ile girilir, neden ayrıca logda) ve `kubeat_signin_throttled_total{kind}` (kaynak/hesap kilidi); `/metrics` anahtarıyla korunur |

API, Route'tan değil cluster içinden erişilir (README "API"); betikle toplamak için
`oc -n kubeat port-forward svc/kubeat-backend 18080:8080` ve
`Authorization: Bearer $(oc -n kubeat get secret kubeat-api-token -o jsonpath='{.data.token}' | base64 -d)`
ile `GET /api/v1/reports?days=7&format=csv`, `/api/v1/users`,
`/api/v1/monitors`.

**Haftalık görüşme** (15 dakika, ekipten 2–3 kişi). Sorular her hafta
aynı; cevaplar bir tabloya yazılır:

1. Bu hafta Kubeat'e kaç kez baktınız, ne için?
2. Kubeat'in yakaladığı bir kesinti oldu mu? Önce kim fark etti: Kubeat mi,
   müşteri mi, başka bir araç mı?
3. Yanlış ya da gereksiz alarm oldu mu? Hangi monitor, sizce neden?
4. Eklemek isteyip ekleyemediğiniz bir şey oldu mu?
5. Kafa karıştıran bir ekran, yazı ya da adım?
6. Hiç kullanmadığınız bir özellik?
7. Bunu başka bir ekibe önerir misiniz? (1–5)
8. Sadece şu özellikler ücretli olsa yine kullanır mıydınız: AD ile giriş,
   namespace sınırı, denetim kaydı, birden fazla durum sayfası? Hangisi
   olmazsa olmaz?

**Sorun kaydı.** Her sorun, ekibin söylediği cümleyle birlikte bir satır:
tarih, kim, ne, engelliyor mu. Üründe düzeltilecek olanlar GitHub'a
`needs-triage` etiketiyle açılır; depo herkese açık olduğundan şirket adı,
host adları ve kişi adları yazılmaz.

**Sorun çıkarsa.**

```sh
oc -n kubeat logs deploy/kubeat-backend --since=1h | grep -E '"level":"(WARN|ERROR)"'
oc -n kubeat rollout restart deploy/kubeat-backend     # 10–20 sn kontrol durur
helm -n kubeat rollback kubeat                        # bir önceki values'a
```

Veritabanı bozulursa yedekten dönüş README "Yedekleme ve geri yükleme"
bölümündedir; şifreleme anahtarı `kubeat-secret-key` Secret'ındadır ve
yedekle birlikte saklanmalıdır.

## Yükseltmeden önce yedek

Sandbox her sürümle yükseltilir; gerçek bir pilotta yükseltme yalnızca
ekibi engelleyen bir hata için yapılır (yukarıda "Dokunmayın"). İkisinde de
yükseltmeden önce en yeni yedek kopyası ve şifreleme anahtarı cluster
dışına alınır ve kopya pod'daki dosyaya karşı doğrulanır. Sürüme özgü
adımlar, geri dönüş ve alarm kurallarının değişip değişmediği
[yukseltme.md](yukseltme.md)'dedir; o dosya her yükseltmeden önce bu adımı
ister.

Blok tek yerde, müşteri runbook'unda durur:
[yedek-geri-yukleme.md "Yedek kopyaları cluster dışına almak"](yedek-geri-yukleme.md#yedek-kopyaları-cluster-dışına-almak).
Backend imajında `tar` olmadığından `kubectl cp` kullanılamaz; `oc exec` ile
düz `cat` ise büyük ikili dosyada akışı sessizce ve değişken noktada kesebilir
(Developer Sandbox'ta birkaç MB'tan sonra,
[#117](https://github.com/yusufceng/Kubeat/pull/117)). Bu yüzden blok kopyayı
512 KiB'lik parçalarla alır, her parçayı ve tüm dosyayı pod'daki sha256 ile,
başlığı `SQLite format 3` ile karşılaştırır. **Blok "yedek doğrulandı"
demiyorsa yükseltme yapılmaz.**

Sandbox'ta ve OpenShift'te blok `oc` ile çalışır; bloğun ilk satırını şöyle
yazın, gövdesi aynı kalır:

```sh
KUBECTL=oc NS=<namespace> RELEASE=kubeat sh -s <<'EOF'
```

- `NS`, sandbox'ta kendi namespace'inizdir; `RELEASE` chart'ın release
  adıdır (`kubeat-backend` adının ön eki). Blok anahtarı chart'ın ürettiği
  `<release>-secret-key` Secret'ından okur; kurulum
  `secretKey.existingSecret` ile yapıldıysa (ör. runbook'taki yeni kuruluma
  geri yüklemeden sonra `kubeat-restored-key`) ilk satıra `SECRET=<ad>`
  ekleyin.
- Backend pod'u yoksa (sandbox uyutmuşsa) önce
  `oc -n <namespace> scale deploy/kubeat-backend --replicas=1` ve
  `oc -n <namespace> rollout status deploy/kubeat-backend`.
- Kopya en yeni yedektir: yaşı en çok `backup.interval` kadardır ve
  kaybedilebilecek veri budur. Kopyayı ve anahtarı ayrı, erişimi kısıtlı
  yerlerde saklayın; yükseltme sorun çıkarırsa dönüş README "Yedekleme ve
  geri yükleme"dedir.
- Minikube'da `kubectl` yalnızca `--context minikube` ile:
  `KUBECTL="kubectl --context minikube"`.

## Pilot sonunda

Ekiple son görüşme, üç karar sorusu:

1. Kesintileri yakaladı mı; yanlış alarm oranı kabul edilebilir mi?
2. Ekip kendi başına kullandı mı (annotation sayısı, giriş yapan kişi
   sayısı)?
3. Ücretli özelliklerden hangileri olmadan olmaz?

**Devam** kararı verildiyse kurulum kalıcıya çevrilir:

- Yedek kopyaları cluster dışına da taşıyan bir iş (README "Yedekleme").
- Dışarıdaki bir dead man's switch'e heartbeat (`heartbeat.urlSecret`):
  Kubeat kendisi düşerse haberiniz olsun.
- `networkPolicy.enabled=true`: backend'e yalnızca arayüz ve Prometheus
  bağlanır.
- `admin` hesabının parolası değiştirilir ve Secret kasaya kaldırılır.
- Lisans süresi ve kapsamı sözleşmeye göre yenilenir.

**Durdur** kararı verildiyse, veriyi saklamak istiyorsanız kaldırmadan önce
son yedeği ve anahtarı dışarı alın (yukarıda "Yükseltmeden önce yedek";
oradaki blok bunu doğrulayarak yapar; README "Yedekleme ve geri yükleme").
Kopyalama komutları backend Deployment'ı üzerinden çalışır ve kaldırmayla o
da gider. Sonra:

```sh
helm -n kubeat uninstall kubeat
```

Kaldırma Deployment'ları siler ama chart 0.44.0'dan beri **veri ve yedek
PVC'lerini bırakır** (0.43.0 ve öncesi onları da siliyordu, minikube'da
doğrulandı); `helm uninstall` bıraktıklarını "These resources were kept due
to the resource policy" diye sayar. Geride şifreleme anahtarı
(`kubeat-secret-key` Secret'ı) ve elle oluşturduğunuz lisans, LDAP parolası
ve CA da kalır; aynı adla `kubeat` namespace'ine yeniden kurulum kaldığı
yerden sürer. Kurulum gerçekten bitiyorsa PVC'leri bilerek silin:

```sh
oc -n kubeat delete pvc kubeat-data kubeat-backup
```

Hepsini silmek için `oc delete project kubeat`.

## Bilinen sınırlar

Pilotta görülecek ve bilerek böyle olan şeyler:

- Backend tek replica çalışır. Yeniden başlatma ve yükseltme sırasında
  kontroller 10–20 saniye durur; bu sürede kesinti yakalanmaz.
- Yedek PVC'si aynı cluster'dadır; cluster kaybında kopyalar da gider.
  Kalıcı kurulumda dışarı taşınır.
- Her cluster'a ayrı Kubeat kurulur; cluster'lar arası tek bir görünüm
  yoktur.
- Ham kontrol sonuçları 7 gün tutulur (`UPTIME_RETENTION`), sonrası bir
  yıl saatlik özet olarak; kontrol kontrol geçmiş 7 günle sınırlıdır.
