# Gerçek OpenShift'te doğrulama

Kubeat minikube üzerindeki OpenShift benzetiminde
([deploy/demo/openshift-sim](../deploy/demo/openshift-sim)) ve Red Hat
Developer Sandbox'taki (ROSA) gerçek bir OpenShift'te çalışıyor. Sandbox
tek namespace verir ve cluster-admin yetkisi yoktur: Route, `restricted-v2`
SCC, LDAP ve OIDC ile giriş, yedekleme ve yükseltmeler orada doğrulandı;
cluster-admin isteyen maddeler (ClusterRole ile keşif, ServiceMonitor,
Thanos yetkisi, NetworkPolicy) ilk tam yetkili kurulumda denenir. Bu belge,
böyle bir cluster'da neyin nasıl doğrulanacağını ve sonucun nasıl geri
bildirileceğini anlatır; pilot planı [pilot.md](pilot.md) dosyasındadır.
Vekilli ve hava boşluklu kurumsal kurulumun ek adımları aşağıda "Vekilli ve
hava boşluklu OpenShift"tedir. Hepsi bir öğleden sonraya sığar.

## Ön koşullar

**Desteklenen sürümler:** OpenShift 4.14 ve üstü (gerçek cluster'da doğrulanan OpenShift 4.21, Kubernetes 1.34) ve Kubernetes 1.3x+; minikube'da doğrulandı. GKE: ölçülmedi, prova kullanıcıda ([gke-dogrulama.md](gke-dogrulama.md)). Vekilli ve hava boşluklu OpenShift: ölçülmedi, prova kullanıcıda (aşağıda "[Vekilli ve hava boşluklu OpenShift](#vekilli-ve-hava-boşluklu-openshift)").

- Bu sürümlerden biri: ROSA, ARO, AWS üzerinde OpenShift ya da kurumun kendi
  veri merkezinde (on-prem); vekilli ve hava boşluklu kurulum da olur, ek
  adımları aşağıdaki bölümdedir. `cluster-admin` yetkisi gerekir. Kubeat'in
  kendisi cluster-admin istemez; kurulum için ClusterRole oluşturmak gerekir.
- User workload monitoring açık olmalı; değilse:
  ```
  oc -n openshift-monitoring create configmap cluster-monitoring-config \
    --from-literal=config.yaml='enableUserWorkload: true' --dry-run=client -o yaml | oc apply -f -
  ```
- İmajlar GHCR'de yayımlanmış olmalı: `make release` (ana dalda, temiz
  ağaçla) `v<sürüm>` etiketini iter, GitHub Actions `kubeat-backend`,
  `kubeat-frontend` imajlarını ve chart'ı yayımlar. İlk yayımdan sonra
  GitHub'da paketlerin görünürlüğünü **public** yapın (Packages → paket →
  Package settings → Change visibility); aksi hâlde `imagePullSecrets`
  gerekir.
- İzlenecek bir şeyler: Route'u olan bir iki uygulama namespace'i. Yoksa
  `deploy/demo` altındaki örnek namespace'i `oc apply` ile kurun.
- İsteğe bağlı: Keycloak ya da kurumsal IdP'de bir OIDC istemcisi (redirect
  URL `https://<kubeat-route>/auth/callback`; `publicURL` verilirse
  `<publicURL>/auth/callback`, kurulum notları yazar), LDAP için bir arama
  hesabı.

**Bağlam.** Bu belgedeki `oc` ve `helm` komutları `--context` olmadan
yazıldı ve geçerli bağlamda çalışır. Kubeconfig'inizde başka cluster'lar da
varsa her komuta doğrulanacak cluster'ın bağlamını verin (`oc --context
<bağlam>`, `helm --kube-context <bağlam>`) ya da başlamadan `oc whoami
--show-server` çıktısının o cluster'ın API adresi olduğunu görün.

## Kurulum

Komut README "OpenShift"tekiyle aynıdır:

```
helm upgrade --install kubeat oci://ghcr.io/yusufceng/charts/kubeat --version 0.52.0 \
  -n kubeat --create-namespace \
  --set route.enabled=true --set route.host=kubeat.apps.<cluster-domain> \
  --set metrics.serviceMonitor.enabled=true --set metrics.prometheusRule.enabled=true \
  --set prometheus.url=https://thanos-querier.openshift-monitoring.svc:9091 \
  --set prometheus.tokenFile=/var/run/secrets/kubernetes.io/serviceaccount/token \
  --set prometheus.caFile=/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt \
  --set prometheus.openshiftMonitoringView=true \
  --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=1
```

Son satır giriş throttle'ının tarayıcının adresini okumasıdır (düz Route'ta
`1`); kurallar açık olduğundan o satır olmadan `KubeatSignInSourceDegraded`
ilk girişte `hops_zero` ile yanar. Arayüz Türkçe olsun isterseniz
`--set language=tr` ekleyin. `podSecurityContext` boş kalır; UID'yi SCC
atar. Giriş parolası:
`oc -n kubeat get secret kubeat-admin -o jsonpath='{.data.password}' | base64 -d`.

## Kontrol listesi

Her madde için beklenen sonuç ve bakılacak yer yazılı. Olmayanı not alın.

| # | Ne | Nasıl | Beklenen |
| --- | --- | --- | --- |
| 1 | Pod'lar restricted-v2 SCC ile çalışıyor | `oc -n kubeat get pods -o yaml \| grep -E 'openshift.io/scc\|runAsUser'` | `restricted-v2`, rastgele UID, iki pod `Running` |
| 2 | Salt okunur kök FS, veri PVC'de | `oc -n kubeat exec deploy/kubeat-backend -- touch /x` | "Read-only file system"; `/data/uptime.db` var |
| 3 | Route üzerinden arayüz | Tarayıcıda `https://kubeat.apps.<domain>` | Giriş sayfası, `admin` ile giriş |
| 4 | Keşif Route'ları görüyor | Keşif → namespace seç | Route satırları "Route adresi" gerekçesiyle, Service satırları probe yoluyla |
| 5 | Route'un arkasındaki pod'lar | Route'tan oluşan monitor'un sayfası → Pod'lar | Pod listesi, olaylar; CPU/bellek Thanos'tan |
| 6 | Thanos yetkisi | Aynı sayfa, CPU/bellek sütunu | Değer geliyor; "metrics server could not be queried" **yok** |
| 7 | ServiceMonitor scrape ediliyor | Konsol → Observe → Metrics → `kubeat_monitor_up`; Observe → Targets | Monitor başına seri, `monitor_namespace` etiketi izlenen nesnenin; hedef `up`, metrik anahtarıyla `401` yok |
| 8 | PrometheusRule alarmı | Bir monitor'u bilerek düşür (Service'i scale 0) → Observe → Alerting | `KubeatMonitorDown` önce Pending, sonra Firing; Alertmanager alıcısına düşüyor |
| 9 | Annotation ile keşif | `oc -n <ns> annotate svc/<ad> kubeat.io/monitor=true kubeat.io/each-pod=true` | 1 dk içinde monitor "annotation" rozetiyle; annotation kalkınca siliniyor |
| 10 | Pod bazlı kontrol | Yukarıdaki monitor'da `oc exec <pod> -- pkill -STOP <süreç>` | "1 of N pods failed: <pod>" ve olay; pod Ready kalırken |
| 11 | Workload monitor rollout'ta sessiz | Deployment'ı `oc rollout restart` et | Olay açılmıyor, ayrıntı "rollout in progress" |
| 12 | Ping (ICMP) | `ping` tipinde monitor | Çalışıyor; "ping_group_range" hatası varsa not alın (RHCOS'ta bilinmiyor) |
| 13 | Yedekleme | `backup.enabled=true` ile kur, `oc -n kubeat exec deploy/kubeat-backend -- ls /backup` | Dosya üretiliyor, PVC'ye yazılabiliyor (fsGroup) |
| 14 | NetworkPolicy | `networkPolicy.enabled=true` ile kur, başka namespace'ten `curl kubeat-backend.kubeat.svc:8080/api/v1/health` | Reddediliyor; arayüz çalışmaya devam ediyor; ServiceMonitor scrape'i sürüyor (`metricsFrom`); push raporu `kubeat-frontend.kubeat.svc:3000/api/push/<token>` üzerinden geçiyor |
| 15 | OIDC / LDAP | `oidc.*` ya da `ldap.*` değerleriyle kur (Route host'u router'a bırakıldıysa `publicURL=https://<host>` da ver), Kullanıcılar'da grup eşlemesi yap | "SSO ile giriş" çalışıyor, rol gruptan geliyor; giriş sonrası Route adresine dönülüyor |
| 16 | Yükseltme süresi | `helm upgrade` sırasında monitor listesini, durum sayfasını ve yayındaki bir sayfadaki monitor'ün rozetini izle; rozeti saniyede bir yokla: `while true; do printf '%s %s\n' "$(date +%T)" "$(curl -s -o /dev/null -m 5 -w '%{http_code}' https://<route>/api/badge/<monitor-id>/status)"; sleep 1; done` | Kontroller kaç saniye durdu, rozet kaç saniye `502` verdi? Tek replica ve `Recreate`: sandbox Route'unda bir yükseltmede ~25 sn ölçüldü (2026-11-08; rozet `502`, durum sayfası "okunamıyor"; README "Durum sayfaları" → "Sınırlar") |
| 17 | Tarayıcı güvenlik başlıkları | `curl -sI https://<route>/login`; sonra arayüzde birkaç sayfa gez | `Content-Security-Policy` ve `Strict-Transport-Security` var, `X-Powered-By` yok; tarayıcı konsolunda CSP ihlali yok |
| 18 | Varsayılan-ret namespace'te kontrol | Proje şablonundan `default-deny` alan (ya da elle `default-deny` uygulanan) bir namespace'teki Service'e monitor ekle (`each-pod` ile de); sonra README "Ağ erişimi", İzlenen namespace'ler'deki `allow-from-kubeat` politikasını uygula | Politika yokken Service ve `each_pod` monitor'leri `connect_timeout`, aynı uygulamanın Route monitor'ü yeşil; politikayla yeşile dönüyor |
| 19 | Uydurma `X-Forwarded-For` ve kural (0.50–0.52) | Tarayıcıdan `admin`'e bir yanlış parola; geliştirici araçlarında (Network) isteği "Copy as cURL" ile alıp `-H 'X-Forwarded-For: 192.0.2.99'` ekleyerek aynı makineden çalıştır; `oc -n kubeat logs deploy/kubeat-backend --since=15m \| grep 'sign-in refused'`. Sonra hop'u geçici olarak `0` yapıp (aynı komut, `UPTIME_TRUST_PROXY_HOPS=0`) yeni pod ilk kez scrape edildikten sonra bir yanlış giriş daha, Observe → Alerting; hop'u `1`'e geri al | İki satırın `source`'u da masaüstünüzün adresi, uydurma başlıklıda `192.0.2.99` **değil** (`192.0.2.99` ise değer fazla ya da router `IfNone`/`Never`; README "Uydurma başlıkla negatif adım"). `0`'da `KubeatSignInSourceDegraded` `reason="hops_zero"` ile Firing (kuralda `for` yok; sandbox'ta UWM ile 2026-11-08'de görüldü), `1`'e dönünce son girişten 15 dk sonra çözülür |
| 20 | Kapsamı sınırlı kullanıcının iç hedefi (0.49; log satırı 0.52) | Kullanıcılar → yerel hesap, rol Editör, tek namespace (`<ns-a>`); o hesapla başka bir namespace'in Service'ine monitor: `http://<servis>.<ns-b>.svc:8080/` | Form 403 ile reddeder; metin adres ve neden söylemez. Logda `WARN` `a request was refused by the target scope: its target leads outside the caller's namespaces` (`user`, `namespace`, `reason=target`, `host`); `kubeat_target_scope_denied_total{reason="target",source="user"}` 1 artar; denetim kaydında `deny`/`target-scope` satırı (README "Hedef politikası" → "Kapsam ihlali denetimi") |
| 21 | Okunamayan namespace ve kuralı (0.52) | Kurulum komutuna `--set 'discovery.namespaces={<ns-a>,<ns-b>}'` ekleyip kur; `oc -n <ns-b> delete rolebinding kubeat-discovery`; en az 11 dk bekle (bir eşitleme turu, `UPTIME_DISCOVERY_SYNC_INTERVAL` varsayılanı 1 dk, ve kuralın `for: 10m`'i); sonra aynı komutla `helm upgrade` | Logda `WARN` `discovery sync skipped a namespace it could not read` (`<ns-b>`, `forbidden`), keşif sayfasında da; `kubeat_discovery_namespaces_skipped` `1`, `kubeat_discovery_sync_errors_total{reason="forbidden"}` artıyor; 10 dk sonra `KubeatDiscoveryNamespaceSkipped` Firing; `<ns-b>`'nin monitor'leri yerinde. `helm upgrade` RoleBinding'i geri koyar: sonraki turda gauge `0`, alarm çözülür (README "Keşif" → "Okunamayan namespace") |
| 22 | Dizinli kurulumda kaynak tavanı (0.52) | `ldap.*` ile kur (madde 15), hop `1`. Üç açılış: `UPTIME_LOGIN_SOURCE_MAX_FAILURES` yok, `"0"` ve `"20"` (`--set-string backend.extraEnv.UPTIME_LOGIN_SOURCE_MAX_FAILURES=…`); her birinde `oc -n kubeat logs deploy/kubeat-backend \| grep UPTIME_LOGIN_SOURCE_MAX_FAILURES`. `"20"`'de aynı tarayıcıdan dizinde **olmayan** 21 ayrı adla (`yok-01` … `yok-21`) birer yanlış parola | Yokken `WARN` `LDAP is on and UPTIME_LOGIN_SOURCE_MAX_FAILURES is not set`; `"0"`'da aynı konu `INFO` (`UPTIME_LOGIN_SOURCE_MAX_FAILURES is 0`); `"20"`'de satır yok. 21. deneme `429` alır, `kubeat_signin_throttled_total{kind="source_total"}` artar; başka bir kaynaktan (ör. telefonun mobil ağı) giriş açık kalır (README "Hesaplar ve giriş" → "Kaynak tavanı"). Dizindeki gerçek adlarla denemeyin: yanlış parolaları dizinin hesap kilidine de sayılır |

## Geri bildirim

Şunları toplayıp iletin; kodu buna göre düzeltirim:

```
oc version
oc -n kubeat get pods -o wide
oc -n kubeat get pods -o yaml | grep -E 'scc|runAsUser|fsGroup|image:'
oc -n kubeat logs deploy/kubeat-backend --since=1h | grep -E 'level":"(WARN|ERROR)"'
oc -n kubeat get route,servicemonitor,prometheusrule
oc -n openshift-user-workload-monitoring get pods
```

Ve kontrol listesinde başarısız olan maddelerin ekran görüntüsü ya da hata
metni. Thanos yetkisi (6) ve ping (12) en belirsiz olanlar: ikisi de
benzetimde doğrulanamadı.

## Vekilli ve hava boşluklu OpenShift

**Durum: ölçülmedi, prova kullanıcıda.** Bu bölüm bir kurumsal kurulumda
koşulup geri bildirimi gelmeden 1.0 metni bu yolu "ölçülmedi, prova
kullanıcıda" diye yazar ([yol haritası](yol-haritasi.md), 32. adım).

Kapsam: kurumun kendi veri merkezindeki (on-prem) ya da çıkışı kısıtlı bir
OpenShift. Dışarıya yalnız cluster dışındaki kurumsal vekilden çıkılır,
imajlar ve chart iç registry'den gelir. Değerler [pilot.md](pilot.md)
"2. values-pilot.yaml"deki banka reçetesidir: `HTTPS_PROXY` ve `NO_PROXY`,
LDAP, hop `1`, kaynak tavanı açıkça `"0"`. Yukarıdaki kontrol listesi bu
kurulumda da geçerlidir; aşağıdakiler onun üstüne eklenir.

Ek ön koşullar:

- İç registry ve ona yazma yetkisi. Bağlı bir makinede ORAS, cosign v3 ve
  Sigstore'un trusted root'u ([pilot.md](pilot.md) "İmajları aynalamak").
- Ağ ekibinden: vekilin cluster dışında çalıştığının ve ACL'inin yazılı
  onayı ([pilot.md](pilot.md) "Vekilli kapsam"taki ilk iki onay kutusu) ve
  adım 8 için vekilin 10 dakikalık erişim kaydı.
- Kurum CA'sıyla imzalı bir HTTPS hedefi (adım 7).
- Adım 6 için node drain yetkisi; yoksa adımın drain'siz değişkeni.

Komutlar yukarıdaki gibi `--context`'siz yazıldı; bağlamı siz verin
(yukarıda "Bağlam"). `<iç-registry>`, `<cluster-domain>` ve kurum adları
sizin değerlerinizdir.

**Kurulum.** `values-pilot.yaml`'a imajların aynası eklenir
(`backend.image.repository: <iç-registry>/kubeat/kubeat-backend`,
`frontend.image.repository: <iç-registry>/kubeat/kubeat-frontend`, registry
parola istiyorsa `imagePullSecrets`); chart iç registry'den kurulur:

```
helm upgrade --install kubeat oci://<iç-registry>/charts/kubeat --version 0.52.0 \
  -n kubeat -f values-pilot.yaml
```

### 1. İmzalı aynalama

Aynalama [pilot.md](pilot.md) "İmajları aynalamak"taki bloktur (`oras cp
-r`); arka planı README "Tedarik zinciri" → "İmzalarla aynalamak" ve "Hava
boşluklu ortamda doğrulamak". İç ağda, aynalanan kopyaları doğrulayın ve
pod'ların onları çektiğini görün:

```
R=<iç-registry>/kubeat
cosign verify --trusted-root trusted_root.json \
  --certificate-identity https://github.com/yusufceng/Kubeat/.github/workflows/release.yml@refs/tags/v0.52.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  $R/kubeat-backend:0.52.0 $R/kubeat-frontend:0.52.0 <iç-registry>/charts/kubeat:0.52.0
oc -n kubeat get pods -o jsonpath='{range .items[*]}{.spec.containers[0].image}{"\n"}{end}'
```

Beklenen:

- Her referans için `Verification for <referans> --` ve imzanın hangi
  denetimlerden geçtiğini sayan satırlar; çıkış kodu 0. cosign Sigstore'a
  bağlanmaz.
- Pod'ların imajları `<iç-registry>/kubeat/kubeat-backend:0.52.0` ve
  `<iç-registry>/kubeat/kubeat-frontend:0.52.0`; `ImagePullBackOff` yok.

### 2. Açılış satırları

```
oc -n kubeat logs deploy/kubeat-backend | grep -E '"level":"(WARN|ERROR)"'
oc -n kubeat logs deploy/kubeat-backend | grep 'UPTIME_LOGIN_SOURCE_MAX_FAILURES is 0'
```

Beklenen:

- İlk komutta yalnız vekil satırı: `HTTP_PROXY/HTTPS_PROXY is set and
  UPTIME_TARGET_SCOPE_TRUST_PROXY is off`.
- İkinci komutta LDAP'ın tavan satırı `INFO` olarak; values'taki açık `"0"`
  onu `WARN`'dan indirir. Satır values'tan silinirse ilk komutta ikinci bir
  `WARN` görünür (`LDAP is on and UPTIME_LOGIN_SOURCE_MAX_FAILURES is not
  set`), `"20"` verilirse satır hiç yoktur ([pilot.md](pilot.md) "4. Kurulum
  sonrası kontrol" → "Uyarı ve hata").

### 3. Cluster vekili ve `NO_PROXY`

OpenShift cluster'ın vekilini (`proxy/cluster`) uygulama pod'larına
kendiliğinden vermez; backend'in `HTTPS_PROXY` ve `NO_PROXY`'si values'tan
gelir. İkisini cluster'ınkiyle karşılaştırın:

```
oc get proxy/cluster -o jsonpath='{.status.noProxy}{"\n"}'
oc get network.config/cluster -o jsonpath='{.status.clusterNetwork[*].cidr} {.status.serviceNetwork[*]}{"\n"}'
oc -n kubeat get deploy kubeat-backend -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="NO_PROXY")].value}{"\n"}'
```

Beklenen:

- Backend'in `NO_PROXY`'si cluster içini kapsar: `.svc`, `.cluster.local`,
  ikinci komuttaki pod ve Service ağı, API'nin iç adı
  (`api-int.<cluster-domain>`) ve kurumun iç alanı. Eksik bir cluster içi ad
  ya da aralık o bağlantıyı vekile gönderir.
- Bir kanalın test düğmesi (Bildirimler → kanal) vekil üzerinden teslim
  eder; monitor sayfasındaki pod paneli CPU ve belleği Thanos'tan okur
  (`thanos-querier.openshift-monitoring.svc` vekile gitmez).
- Vekil adresi kullanıcı adı ve parola taşıyorsa `HTTPS_PROXY`'yi
  yazdırmayın, geri bildirime koymayın.

### 4. `server resolve` ve `NO_PROXY`

```
oc -n kubeat exec deploy/kubeat-backend -- server resolve status.saglayici.example hooks.saglayici.example <NO_PROXY'deki-bir-kurum-adı>
```

Beklenen:

- Vekile giden dış adlarda `NXDOMAIN, no such name for this pod's resolver:
  …; with UPTIME_TARGET_SCOPE_TRUST_PROXY=true an HTTP(S) request handed to
  the proxy is let through`. Bu, [pilot.md](pilot.md) "Vekilli kapsam"taki
  üçüncü onaydır; SERVFAIL ya da zaman aşımı satırında ayarı açmayın.
- `NO_PROXY`'deki adlarda vekil satırı geçmez: istek vekile gitmez, backend
  doğrudan bağlanır ve `UPTIME_TARGET_SCOPE_TRUST_PROXY` istisna vermez. Komut
  `NO_PROXY`'yi okumaz ve NXDOMAIN'de aynı satırı yazar
  ([backlog](backlog.md), "`server resolve`'un vekil istisnasını da
  değerlendirmesi"). Böyle bir ad kontrolde de çözülmez: cluster DNS'i kurum
  alanını kurumsal DNS'e iletmelidir (`dns.operator/default`, `servers`).
- İletim doğruysa kurum adı için `resolves to …, outside the cluster`
  (adres `UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS`'teyse) ya da `resolves to …,
  inside the cluster` (değilse: kapsamı sınırlı kullanıcıya kapalıdır,
  yönetici kurar; README "Kurum ağı").

### 5. F5 ya da WAF önlü Route: hop ve uydurma `X-Forwarded-For`

[pilot.md](pilot.md) "4. Kurulum sonrası kontrol"deki "Giriş kaynağı" ve
"Uydurma başlık" satırları: tarayıcıdan bir yanlış parola, sonra aynı istek
"Copy as cURL" ve `-H 'X-Forwarded-For: 192.0.2.99'` ile. Ardından:

```
oc -n kubeat logs deploy/kubeat-backend --since=15m | grep 'sign-in refused'
oc -n kubeat logs deploy/kubeat-backend --since=15m | grep -c 'is lower than needed'
```

Beklenen:

- Başlığı yazan (L7) bir F5 ya da WAF'ta hop `2`, yalnız TCP geçiren (L4)
  bir F5'te `1` ile iki satırın `source`'u da masaüstünüzün adresi;
  uydurma başlıklıda `192.0.2.99` değil. `192.0.2.99` ise değer fazladır ya
  da router `IfNone`/`Never`'dır ([pilot.md](pilot.md) "Hop değeri").
- Eksik değer (L7 bir F5'te `1`) sessizdir: `source` F5'in router'a
  bağlandığı çıkış adresidir (SNAT havuzu), `hops_too_low` sayılmaz, ikinci
  komut `0` döner ve `KubeatSignInSourceDegraded` yanmaz. 0.52'nin sinyali
  yalnız Kubeat'in kendi genel adresini tanır; önde başlık yazan katmanların
  çıkış aralıkları için sinyal S101 ile sonraki sprintte
  ([backlog](backlog.md), "`hops_too_low`'u önde başlık yazan katmanların
  çıkış aralıklarına genişletmek"). O zamana kadar doğrulama bu iki satırdır.

### 6. Drain: kurum aralığı pod'un yeni adresini içerince

Kurum aralığı (`UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS`) pod ağının bir parçasını
içerirse ilk açılış geçer, pod başka bir node'a taşınınca yakalanır. 0.53'ten
(S96) beri backend yine açılır ve yalnız o girdi yok sayılır (README "Hedef
politikası" → "Kurum ağı" → "Açılış koruması"). Adım iki parçadır; ilki kapsamı
hiç genişletmez, önce onu koşun.

**a) API sunucusunun adresi.** Values'taki kurum aralıklarına API sunucusunun
Service adresini `/32` ile geçici olarak ekleyip aynı komutla `helm upgrade`
yapın:

```
oc -n default get svc kubernetes -o jsonpath='{.spec.clusterIP}{"\n"}'
oc -n kubeat logs deploy/kubeat-backend | grep 'target scope external ranges ignored'
```

**b) Drain.** Backend'in node'unu ve öteki node'ların pod alt ağlarını
(OVN-Kubernetes) okuyun:

```
oc -n kubeat get pod -l app.kubernetes.io/component=backend -o wide
oc get nodes -l node-role.kubernetes.io/worker -o jsonpath='{range .items[*]}{.metadata.name} {.metadata.annotations.k8s\.ovn\.org/node-subnets}{"\n"}{end}'
```

Backend'in node'u dışındaki, pod'un gidebileceği node'ların alt ağlarını (ör.
`10.129.0.0/23`; pod'u infra node'lara sabitlediyseniz onlarınkini) kurum
aralıklarına geçici olarak ekleyip `helm upgrade` yapın. Bu sürede kapsamı
sınırlı kullanıcılar o node'lardaki pod adreslerine monitor kurabilir: adımı
bakım penceresinde ya da kapsamı sınırlı kullanıcı yokken koşun, bitince
aralıkları hemen çıkarın. `helm upgrade` pod'u yeniden oluşturur; yeni pod
listedeki bir node'a düştüyse aşağıdaki sonuç drain'siz de görünür. Değilse:

```
oc adm drain <backend-node> --ignore-daemonsets --delete-emptydir-data
oc -n kubeat get pod -l app.kubernetes.io/component=backend -o wide
oc -n kubeat logs deploy/kubeat-backend | grep 'target scope external ranges ignored'
oc adm uncordon <backend-node>
```

Values'ta cluster ağlarını beyan ettiyseniz (`targetScope.clusterNetworks`,
0.53) iki parçanın `helm upgrade`'i de render hatasıyla durur; beyanın beklenen
sonucu budur (README "Kurum ağı"). Açılışın davranışını görmek için beyanı
adım boyunca kaldırın.

Beklenen:

- Backend `Running`, `CrashLoopBackOff` değil; izleme sürer.
- Tek `ERROR` satırı `target scope external ranges ignored: …`; `ignored`
  alanında (a)'da `<adres>/32 holds the API server's address …`, (b)'de
  `<aralık> holds <pod adresi>, an address of the backend's own pod …`.
- Observe → Metrics'te `kubeat_target_scope_external_ranges_ignored` `1`,
  Observe → Alerting'de `KubeatScopeExternalRangesIgnored` Firing (kuralda
  `for` yok).
- Yok sayılan aralığa giden kapsamı sınırlı monitor'ler `blocked_by_scope` ile
  DOWN. Eklenen aralıkları values'tan çıkarıp `helm upgrade` yapınca üçü de
  kalkar.
- 0.52'de aynı adımda backend açılmaz (`invalid configuration`,
  `CrashLoopBackOff`); izleme durur.

### 7. Kurum CA'sı

Reçete README "OpenShift" → "Kurum CA'sı"dadır (0.53, S2). Etiketli
ConfigMap'e kurum CA'sını enjeksiyon ancak cluster yöneticisi onu
`proxy/cluster` nesnesinde `spec.trustedCA` ile verdiyse getirir; vermediyse
README'deki OpenShift dışı yol (etiketsiz ConfigMap) kullanılır.
`values-pilot.yaml`'da `backend.extraVolumes` ve `extraVolumeMounts` LDAP'ın
CA'sını zaten taşır: reçetenin volume'unu aynı listelere ekleyin. Ayrı bir
`-f values-ca.yaml` listeyi değiştirir ve LDAP'ın CA'sını düşürür.

```
oc get proxy/cluster -o jsonpath='{.spec.trustedCA.name}{"\n"}'
oc -n kubeat get configmap kubeat-trusted-ca -o jsonpath='{.data.ca-bundle\.crt}' | grep -c 'BEGIN CERTIFICATE'
oc -n kubeat rollout restart deploy/kubeat-backend
```

Beklenen:

- İlk komut kurum CA'sını taşıyan ConfigMap'in adını yazar; boşsa kurum CA'sı
  enjeksiyonla gelmez, README'deki OpenShift dışı yol (etiketsiz ConfigMap)
  kullanılır.
- Sayı 0'dan büyük.
- Kurum CA'sıyla imzalı hedefe kurulan monitor reçeteden önce
  `tls_unknown_authority`, reçete ve yeniden başlatmadan sonra ilk kontrolde
  UP. Herkese açık CA'lı, vekilden giden bir hedef UP kalır.

### 8. Kurumsal vekilde yanıt süresi ve kontrol başına bir `CONNECT`

0.52'den beri her HTTP kontrolü kendi bağlantısını açar: vekilli kurulumda
her `https://` kontrolü vekilde bir `CONNECT` tünelidir ve yanıt süresine
vekile bağlanma ile tünelin kurulması eklenir ([yuk-testi.md](yuk-testi.md)
"HTTP kontrolünün bağlantıları (0.52)"). Vekilden giden 30 sn aralıklı bir
monitor seçin ve aynı adresi backend pod'undan ölçün (`curl` pod'un
`HTTPS_PROXY` ve `NO_PROXY`'sini kullanır):

```
oc -n kubeat exec deploy/kubeat-backend -- curl -sS -o /dev/null \
  -w 'connect=%{time_connect} tls=%{time_appconnect} total=%{time_total}\n' https://status.saglayici.example/
```

Ağ ekibinden de backend'in çıkış adresi için vekilin 10 dakikalık erişim
kaydını alın.

Beklenen:

- Monitor sayfasındaki yanıt süresi `total`'a yakın. `connect` vekile
  bağlanmadır, `tls` ile `connect` arasındaki fark tünel ve TLS el
  sıkışmasıdır: yanıt süresinin vekilden gelen payı budur. Vekilsiz
  sandbox'ta 0.52 dış HTTPS'te yanıt süresine ~140–160 ms ekledi
  (2026-11-08).
- Erişim kaydında o monitor için 10 dakikada ~20 `CONNECT
  status.saglayici.example:443` satırı: kontrol başına bir tane, fazlası
  yok. 0.51'de bağlantılar yeniden kullanıldığından tünel sayısı daha azdı.
- Vekil yavaşlarsa kontroller daha uzun sürer: Observe → Metrics'te
  `kubeat_checks_in_flight` yükselir, sırası gelen kontroller boş worker
  bulamazsa `KubeatWorkersSaturated` 15 dakika sonra yanar (çare
  `UPTIME_WORKERS`).

### 9. OADP ile geri yükleme (S41)

**Ölçülmedi, prova kullanıcıda.** OADP (Velero) yolu S7 ile sonraki
sprintte belgelenir. O zamana kadar geri yükleme
[yedek-geri-yukleme.md](yedek-geri-yukleme.md)'deki yoldur: "Yedek
kopyaları cluster dışına almak" bloğu (`KUBECTL=oc`) ve "Cluster ya da
namespace kaybında, yeni bir kuruluma". OADP yedeğin yerine ancak veri
volume'unun CSI snapshot'ını alıyorsa geçer; fs-backup (Kopia, Restic) canlı
SQLite'ı ve WAL'ı tutarlı almaz (README "GitOps ile kurulum" → "Kurulum
notları görünmez", "Yedek" satırı).

Beklenen (runbook'un yolunda): dışarı alma bloğu "yedek doğrulandı", geri
yükleme bloğu "geri yüklendi" der; yeni kurulumda monitor ve kanal sayısı
yedektekiyle aynı, kanal adresleri okunuyor (anahtar da taşındı). OADP'yi
denediyseniz sonucu ayrıca yazın.

### Geri bildirim: vekilli ve hava boşluklu kurulum

Şablonu doldurup GitHub'da bir issue'yla iletin. Depo herkese açıktır: alan
adlarını, IP adreslerini, registry, vekil ve cluster adlarını maskeleyin;
parola, token ve anahtar yazmayın.

```
OpenShift sürümü; kurulum türü (on-prem, ROSA, ARO, AWS):
Vekil: türü; cluster dışında mı; ACL onayı alındı mı:
Route'un önü: yok / L4 F5 / L7 F5 ya da WAF (X-Forwarded-For yazıyor mu); verilen hop:
İç registry; aynalama: oras cp -r / cosign save ve load:
Kurum CA'sı proxy/cluster spec.trustedCA'da mı:

| Adım | Sonuç (geçti / kaldı / koşulmadı / geçerli değil) | Ölçülen | Not |
| --- | --- | --- | --- |
| 1 İmzalı aynalama | | | |
| 2 Açılış satırları | | WARN ve INFO satırları | |
| 3 Cluster vekili ve NO_PROXY | | eksik ad ya da aralık | |
| 4 server resolve ve NO_PROXY | | her adın satırı | |
| 5 F5/WAF önlü Route | | iki satırın source'u (maskeli) | |
| 6 Drain | | ERROR satırı, gauge, alarm | |
| 7 Kurum CA'sı | | önce ve sonra sonuç | |
| 8 Yanıt süresi ve CONNECT | | total, tls; 10 dk'daki CONNECT sayısı | |
| 9 OADP geri yükleme | | | |
```

Ve şu çıktılar:

```
oc version
oc -n kubeat get pods -o wide
oc -n kubeat logs deploy/kubeat-backend --since=1h | grep -E 'level":"(WARN|ERROR)"'
oc -n kubeat get route,servicemonitor,prometheusrule,configmap
```
