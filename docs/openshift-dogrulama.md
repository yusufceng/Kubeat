# Gerçek OpenShift'te doğrulama

Kubeat bugüne kadar yalnızca minikube üzerindeki OpenShift benzetiminde
çalıştı ([deploy/demo/openshift-sim](../deploy/demo/openshift-sim)). Bu
belge, gerçek bir cluster'da neyin nasıl doğrulanacağını ve sonucun nasıl
geri bildirileceğini anlatır. Hepsi bir öğleden sonraya sığar.

## Ön koşullar

- OpenShift 4.14 ya da üstü (ROSA, ARO, OCP on AWS/EKS fark etmez);
  `cluster-admin` yetkisi. Kubeat'in kendisi cluster-admin istemez; kurulum
  için ClusterRole oluşturmak gerekir.
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
  URL `https://<kubeat-route>/auth/callback`), LDAP için bir arama hesabı.

## Kurulum

```
helm upgrade --install kubeat oci://ghcr.io/yusufceng/charts/kubeat --version <sürüm> \
  -n kubeat --create-namespace \
  --set route.enabled=true --set route.host=kubeat.apps.<cluster-domain> \
  --set metrics.serviceMonitor.enabled=true --set metrics.prometheusRule.enabled=true \
  --set prometheus.url=https://thanos-querier.openshift-monitoring.svc:9091 \
  --set prometheus.tokenFile=/var/run/secrets/kubernetes.io/serviceaccount/token \
  --set prometheus.caFile=/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt \
  --set prometheus.openshiftMonitoringView=true \
  --set language=tr
```

`podSecurityContext` boş kalır; UID'yi SCC atar. Giriş parolası:
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
| 16 | Yükseltme süresi | `helm upgrade` sırasında monitor listesini izle | Kontroller kaç saniye durdu? (tek replica; 10–20 sn beklenir) |
| 17 | Tarayıcı güvenlik başlıkları | `curl -sI https://<route>/login`; sonra arayüzde birkaç sayfa gez | `Content-Security-Policy` ve `Strict-Transport-Security` var, `X-Powered-By` yok; tarayıcı konsolunda CSP ihlali yok |

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
