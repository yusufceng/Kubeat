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
cluster dışına alın ve kopyayı pod'daki dosyaya karşı doğrulayın:
[yedek-geri-yukleme.md "Yedek kopyaları cluster dışına almak"](yedek-geri-yukleme.md#yedek-kopyaları-cluster-dışına-almak).
Blok kopyayı `exec` üzerinden 512 KiB'lik parçalarla alır; her parçayı ve
tüm dosyayı pod'daki sha256 ile, başlığı `SQLite format 3` ile karşılaştırır.
Backend imajında `tar` gerekmez; blok `kubectl` ile de `oc` ile de
(`KUBECTL=oc`) çalışır. Anahtar `secretKey.existingSecret` ile verildiyse
ilk satıra o Secret'ın adını ekleyin (`SECRET=<ad>`); yoksa blok anahtarı
bulamaz ve "yedek doğrulandı" demez. **"yedek doğrulandı" demiyorsa
yükseltmeyin.** Düz
`exec … cat` ile alınmış bir kopya (0.49 ve öncesinin belgesindeki yol)
doğrulanmamıştır ve sessizce kesilmiş olabilir; ona güvenerek yükseltmeyin.

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

### 0.42'den 0.52'ye

1. [Yükseltmeden önce yedek](#yükseltmeden-önce-yedek).
2. 0.43'ün ön kontrolleri ("0.43'e yükseltme", Yapılacak): son gönderimi
   başarısız kanalları düzeltin ya da kapatın; aynı PagerDuty servisine
   bağlı birden çok kurulumu ayırın. `UPTIME_NOTIFY_RETRY_MAX` denetimi
   0.46'dan beri uyarıya döner: 1 dakikadan ya da bir denemenin
   süresinden kısa bir değer 0.46–0.52'de backend'i durdurmaz, alt
   sınıra yükseltilir ve her açılışta `WARN` yazılır; değeri yine de
   düzeltin: 0.43, 0.44 ya da 0.45'e dönüşte backend'i durdurur (0.42 bu
   ayarı tanımaz).
3. 0.44'ün adımları ("0.44'e yükseltme", Yapılacak): yedeği kapatmayı ya
   da `existingClaim`'e geçişi bu yükseltmeyle aynı adımda yapmayın; açık
   kesintilere bakın; `KubeatDown` susturma kuralını ekleyin;
   `backend.terminationGracePeriodSeconds`'ı `UPTIME_SHUTDOWN_TIMEOUT`'tan
   en az 5 sn uzun tutun; ServiceMonitor kapalıysa
   `up{job="<release>-backend", namespace="<namespace>"}` serisini
   toplayan bir scrape olsun; log alarmlarınız `check result dropped`
   satırına bakıyorsa `batch insert failed`'a taşıyın.
4. 0.45'in adımları ("0.45'e yükseltme", Yapılacak): `latest` etiketinden
   bir sürüm etiketine geçin; `KubeatNotificationsFailing`'in
   yönlendirmesini kanal etiketlerine göre ve `KubeatDown` susturma
   listesini güncelleyin.
5. 0.46'nın adımları ("0.46'ya yükseltme", Yapılacak): açık PagerDuty
   olaylarını kapatın; kuruluma `instanceName` verin; imajları
   aynalıyorsanız docs/pilot.md'deki imzalı yola geçin.
6. 0.47'nin adımları ("0.47'ye yükseltme", Yapılacak): kapsamı sınırlı
   kullanıcılarınızın kapsam dışı iç hedeflerini gözden geçirin; başarısız
   kontrolün hata metnindeki çözülen IP'ye bakan alarm ya da panoyu
   `diagnostics.address`'e taşıyın.
7. 0.48'in adımları ("0.48'e yükseltme", Yapılacak): giriş bir vekilin
   arkasındaysa `UPTIME_TRUST_PROXY_HOPS`'u ayarlayın; kapsamı sınırlı
   kullanıcıların iç hedefleri artık çözüm anında da denetlenir (iki
   etiketli kısa ad/genel ad boşluğu kapandı), gözden geçirin.
8. 0.49'un adımları ("0.49'a yükseltme", Yapılacak): `UPTIME_TRUST_PROXY_HOPS`'u
   somut hop sayısına göre gözden geçirin (güvenilemeyen `X-Forwarded-For`'da
   artık çalışma anında `WARN` gelir); egress'i varsayılan-reddet olan bir
   cluster'da backend'in cluster DNS'ine (kube-dns/openshift-dns) egress'ine
   izin verin, yoksa kapsamı sınırlı kullanıcıların dış hedef create'i güvenli
   tarafta reddedilir.
9. 0.50'nin adımları ("0.50'ye yükseltme", Yapılacak): `UPTIME_TRUST_PROXY_HOPS`'u
   (7. ve 8. adımlardaki hop değerinin yerine) arayüz yoluna göre yeniden
   türetilen rehbere göre ayarlayın (fazla değer artık güvenli taraf değildir,
   emin değilseniz küçüğünü seçin) ve `sign-in refused` satırının `source`
   alanıyla doğrulayın; chart dışı kurulumda değer `0`'dan büyükse backend'e
   `UPTIME_FRONTEND_TOKEN`, arayüze `FRONTEND_TOKEN` olarak aynı değeri verin;
   GitOps ile kuruyorsanız beşinci Secret'ı (`token` anahtarı) oluşturup
   `frontendToken.existingSecret` ile verin; isteğe bağlı
   `frontend.networkPolicy.enabled`'ı ve vekilli kurulumda, vekil cluster
   dışındaysa, isteğe bağlı `UPTIME_TARGET_SCOPE_TRUST_PROXY`'yi değerlendirin.
10. 0.51'in adımları ("0.51'e yükseltme", Yapılacak): kapsamı sınırlı
    kullanıcınız ya da annotation monitor'leriniz varsa "Her bağlantıda kapsam"
    ön kontrolünü koşun; kurum ağındaki hedefler için
    `UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS`'i, vekilli kurulumda
    `UPTIME_TARGET_SCOPE_TRUST_PROXY`'yi değerlendirin; kapsamı sınırlı kullanıcının kurduğu eski monitor'ü yönetici
    olarak kaydetmeyin.
11. 0.52'nin adımları ("0.52'ye yükseltme", Yapılacak): HTTP monitor'lerinin
    `slow_above_ms` eşiklerine ve uzak HTTPS hedefi çoksa worker doluluğuna
    bakın; kapsam reddi alarmlarınızı `reason` ve `source`'a göre gözden
    geçirin; `UPTIME_TARGET_RESOLVE_TIMEOUT`'un 2 sn'ye indiğini hesaba katın;
    LDAP'lı kurulumda kaynak tavanına karar verin;
    `KubeatSignInSourceDegraded` doğru hop değerinde yanıyorsa (`hops_zero`,
    `hops_too_low`) değeri artırmayın, susturun;
    `metrics.prometheusRule.clusterLabel` `reason` ya da `surface` ise
    değiştirin; namespace sınırlı keşifte yükseltmeden önce keşif durumuna
    bakın.
12. Alarm kuralları 0.43, 0.44, 0.45 ve 0.52'de değişti, 0.46–0.51'de
    değişmedi: yeni `kubeat.self` alarmlarını, `KubeatUnhealthy`'yi,
    `KubeatSignInSourceDegraded`'i ve `KubeatDiscoveryNamespaceSkipped`'i
    Alertmanager yönlendirmesine ekleyin; kuralları kopyalıyorsanız
    0.52'ninkileri kopyalayın.
13. Yükseltme (ayarlar bir values dosyasında değilse `-f values.yaml`
    yerine `--reset-then-reuse-values`, Helm 3.14+):

    ```sh
    helm -n kubeat upgrade kubeat oci://ghcr.io/yusufceng/charts/kubeat --version 0.52.0 -f values.yaml
    ```

14. Geri dönüş: şema aynı (26), `helm rollback` ile 0.42.2'ye; önce açık
    PagerDuty olaylarını kapatın. Dönüşte 0.43–0.52'nin yeniden denemesi,
    kurulum kimliği, alarmları ve metrikleri kalkar; 0.42'nin chart'ı
    PVC'lere kaldırma korumasını basmaz, `helm uninstall` onları yine siler.

### 0.41'den 0.52'ye

1. 0.42'nin adımı ("0.42'ye yükseltme", Yapılacak): LDAP ya da OIDC ile
   giriş, namespace'le sınırlı kullanıcılar ya da dizin grupları, denetim
   kaydı ya da birden çok durum sayfası kullanıyorsanız lisansı
   yükseltmeden önce verin.
2. Sonra "0.42'den 0.52'ye"deki adımlar.
3. Geri dönüş: şema aynı (26), `helm rollback` ile 0.41.1'e.

0.43–0.51'den gelirken başladığınız sürümden sonraki bölümleri
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
- **Açık kesintiler** (0.44; 0.46'da PagerDuty kanalı varsa `&kind=down`
  olmadan koşun: açık kesinti ve sertifika olayları PagerDuty'de açık
  alarmlardır, yavaşlık alarmı olay değildir ve PagerDuty'de
  `kubeat-…-slow-` anahtarıyla aranır; keşif monitor'ünün adı namespace'i
  zaten taşır):

  ```sh
  api 'incidents?state=open&kind=down' | jq -r '.[] | "\(.id)\t\(.monitor_name)\t\(.started_at)"'
  ```

- **Bildirim ayarları** (0.43; 0.46'da kısa `UPTIME_NOTIFY_RETRY_MAX`
  reddedilmez, alt sınıra yükseltilip `WARN` yazılır). Çıktı boşsa
  varsayılanlar geçerlidir. Ayarlar backend Deployment'ından okunur,
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

- **Her bağlantıda kapsam** (0.51). Önce kapsamı sınırlı bir editör olup
  olmadığını bulun: yerel hesaplar, dizin grupları ve LDAP/OIDC varsayılan
  rolü. İlk iki sayıdan biri sıfırdan büyükse ya da üçüncü komut
  `..._DEFAULT_ROLE=editor` gösteriyor ve aynı önekin `_DEFAULT_NAMESPACES`
  satırı yoksa ya da `*` içermiyorsa `LIMITED=true`:

  ```sh
  api users | jq '[.[] | select(.role == "editor" and ((.namespaces // []) | index("*") | not))] | length'
  api ldap/groups | jq '[.[] | select(.role == "editor" and ((.namespaces // []) | index("*") | not))] | length'
  kubectl -n "$NS" get deploy "$REL-backend" -o jsonpath='{range .spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}' \
    | grep -E '^UPTIME_(LDAP|OIDC)_DEFAULT_(ROLE|NAMESPACES)='
  LIMITED=false   # ya da true
  ```

  Sonra süzgeci bir dosyaya yazıp koşun:

  ```sh
  cat > kapsam-on-kontrol.jq <<'JQ'
  # Monitors a 0.51 upgrade would hold to their namespace and refuse.
  # Use: api monitors | jq -r --argjson limited <true|false> -f kapsam-on-kontrol.jq
  # limited: whether some user may edit monitors in only some namespaces
  # (step 1). Without one only annotation monitors are held (every install).
  # Output: verdict, endpoint (hedef|seçenek), id, namespace, name, target.
  def bare: ascii_downcase | sub("\\.+$"; "") | ltrimstr("[") | rtrimstr("]");
  def host:
    if .type == "http" or .type == "websocket" then .target | capture("^[a-zA-Z]+://(?<h>\\[[^]]*\\]|[^/:?#]*)").h
    elif .type == "dns" or .type == "ping" then .target
    else .target | sub(":[0-9]+$"; "") end
    | bare;
  def resolverhost:
    if test("^\\[") then capture("^\\[(?<h>[^]]*)\\]").h
    elif (split(":") | length) == 2 then sub(":[0-9]+$"; "")
    else . end
    | bare;
  def ns: split(".") as $l | [range(1; $l | length) | select($l[.] == "svc" or $l[.] == "pod") | $l[. - 1]] | last;
  def private: test("^(10|127)\\.|^172\\.(1[6-9]|2[0-9]|3[01])\\.|^192\\.168\\.|^100\\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\\.|^169\\.254\\.|^(f[cd]|fe80:)|^::1$");
  # A host's verdict for monitor $m: refused by its spelling (kesin), judged
  # where it resolves (çözüme bağlı; not a host taken from a cluster object),
  # or nothing.
  def verdict($m; $resolves):
    if test("^[0-9.]+$|:") then (if private then "kesin" else empty end)
    elif (contains(".") | not) then "kesin"
    elif test("\\.(svc|cluster\\.local)$") then (if ns == $m.namespace then empty else "kesin" end)
    elif $resolves then "çözüme bağlı"
    else empty end;
  .[] | select(.namespace != "" and .type != "push" and .type != "workload" and ($limited or .managed)) | . as $m
  | ((host | verdict($m; $m.source == "") | "\(.)\thedef"),
     (if .type == "dns" and ((.dns.resolver // "") != "") then .dns.resolver | resolverhost | verdict($m; true) | "\(.)\tseçenek" else empty end))
  | "\(.)\t\($m.id)\t\($m.namespace)\t\($m.name)\t\($m.target)"
  JQ
  api monitors | jq -r --argjson limited "$LIMITED" -f kapsam-on-kontrol.jq
  ```

  Satır: karar, uç nokta (`hedef` ya da host adı taşıyan bir `seçenek`), id,
  namespace, ad, hedef. `kesin` adından reddedilendir (başka namespace'in
  Service adı, kısa ad, çıplak özel adres). `çözüme bağlı` dış görünen bir
  addır: yükseltmeden sonra backend'in çözücüsünde cluster içine ya da
  `UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS` dışındaki özel bir adrese çözülürse
  reddedilir; vekile giden istekte backend'de çözülemezse de reddedilir
  (NXDOMAIN'i yalnız `UPTIME_TARGET_SCOPE_TRUST_PROXY` vekile bırakır). Süzgeç
  bunu bilemez ve elle kurulan monitor'lerin her dış adını listeler. Kurum
  aralığındaki bir adres `kesin` görünür, `UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS`'teyse
  reddedilmez. `LIMITED=false` iken yalnız annotation monitor'lerine bakar.
  Süzgeç yalnız hedefe ve host adı taşıyan seçeneğe bakar; kontrolün çalışırken
  ulaştığı öteki yerler önceden görülemez. Yükseltmeden sonra kesin liste:

  ```sh
  api monitors | jq -r '.[] | select(.last_result.diagnostics.explanation.code == "blocked_by_scope")
    | "\(.id)\t\(.namespace)\t\(.name)\t\(.target)"'
  ```

- **Kapsam dışı iç hedefler** (0.47, 0.48'de genişledi). Kapsamı sınırlı
  kullanıcılar ve cluster içi görünen hedefli monitor'ler; 0.47'de bu
  kullanıcılar kapsamları dışındaki iç hedefleri artık yeniden oluşturamaz
  ya da değiştiremez, 0.48'de bu adı dış görünse de iç adrese çözülen
  hedefleri (iki etiketli `servis.namespace` kısa adı, iç adrese çözülen
  genel ad) de kapsar. Var olanlar çalışmaya devam eder, bu yalnızca gözden
  geçirmedir (hangi monitor'ün hangi kullanıcıya ait olduğunu birlikte
  değerlendirin):

  ```sh
  api users | jq -r '.[] | select(.role != "admin" and ((.namespaces // []) | index("*") | not))
    | "\(.username)\t\((.namespaces // []) | join(","))"'
  api monitors | jq -r '
    .[]
    | (.target | sub("^[a-z0-9+.-]+://"; "") | split("/")[0] | split(":")[0] | ascii_downcase) as $h
    | select(
        ($h | test("\\.svc$|\\.svc\\.|\\.cluster\\.local$"))        # Service/pod adı
        or ($h | test("^(10|127)\\.|^192\\.168\\.|^172\\.(1[6-9]|2[0-9]|3[01])\\.|^100\\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\\."))  # ClusterIP/pod IP
        or ($h | test("^[^.]+$"))                                   # noktasız kısa ad
        or ($h | test("^[^.]+\\.[^.]+$"))                           # iki etiketli kısa ad
      )
    | "\(.id)\t\(.namespace)\t\(.target)"'
  ```

  > Bu sorgu bir **inceleme yardımıdır**, kesin değildir: iki etiketli bir
  > genel adı (`status.example.com` değil ama `durum.example`) iç sanıp
  > fazladan işaretler ve iç adrese çözülen genel bir adı (`172-30-0-1.sslip.io`)
  > ad biçiminden göremez. 0.48'de asıl denetim sunucudadır: backend adı kendi
  > çözücüsüyle çözüp kararı verir (bkz. README "Hedef politikası" → "Çözüm
  > anında denetim"). 0.49'da bu denetim çözüm hata ya da zaman aşımı verince de
  > kapalı kalır (CoreDNS kesintisinde fail-open kapandı): kapsamı sınırlı bir
  > kullanıcı yalnızca ispatlanabilir biçimde cluster dışına çözülen bir ad
  > belirleyebilir, çözüme `UPTIME_TARGET_RESOLVE_TIMEOUT` (varsayılan 5 sn)
  > sınırı kondu. Var olan monitor'ler çalışmaya devam eder.

- **30 saniyeden sık monitor'lar ve kotalar** (0.41). Hedefi cluster
  dışında olanlar 30 saniye kuralına girer:

  ```sh
  api monitors | jq -r '.[] | select(.interval_seconds < 30) | "\(.id)\t\(.interval_seconds)s\t\(.target)"'
  api monitors | jq '{toplam: length, namespace_200_ustu: [group_by(.namespace)[] | select(length > 200) | .[0].namespace]}'
  ```

Bitince port-forward'u kapatın: `kill $PF`.

## 0.52'ye yükseltme

0.52 bir güvenilirlik ve gözlem sürümüdür; güvenlik sürümü değildir. Kapsam
reddi artık DNS arızasından ve yarıda kalan kontrolden ayrı sayılır ve
denetim satırları dakika başına sınırlıdır. Her HTTP kontrolü kendi
bağlantılarını açıp kapatır; bu, 0.51'de kapsama bağlı monitor'lerin bağlantı
tutmasından gelen gerilemeyi giderir. Giriş throttle'ına kaynak başına opt-in
bir tavan, kaynak ayrımı gerilediğinde de bir sayaç ve bir chart kuralı gelir.
Okunamayan bir namespace keşfi durdurmaz. Şema değişmez (26), migration yok;
chart kurulumunda yeni zorunlu ayar yok.

Kapsam reddinin ayrımı, iki zaman aşımı ve satır sınırı minikube'da ölçüldü
(2026-11-08): CoreDNS'in tamamen kapanması, SERVFAIL veren ve 3 sn gecikmeli
yanıt veren bir bölge, vekilli ve vekilsiz kontrol, kapsamı sınırlı bir
editörün monitor'leri. Okunamayan namespace, iki node'lu bir minikube'da bir
namespace'in RoleBinding'i kaldırılarak sınandı. HTTP bağlantılarının bellek
ölçümü ajan içidir ([yuk-testi.md](yuk-testi.md) "HTTP kontrolünün
bağlantıları"). GKE'nin kube-dns/Cloud DNS'i ve kurumsal DNS ölçülmedi.

### Yapılacak

- **HTTP monitor'lerinin yanıt süresi eşiklerine bakın.** Her HTTP kontrolü
  yeni bağlantı açtığından süreye ad çözme, TCP bağlantısı ve HTTPS'te TLS el
  sıkışması girer. Uzak ya da el sıkışması yavaş bir hedefte `slow_above_ms`
  0.51'e göre daha erken aşılabilir (README "Yavaş yanıt").
- **Uzak HTTPS hedefi çoksa worker doluluğunu izleyin.** Kontrol süresi
  uzadığından aynı `UPTIME_WORKERS` daha az kontrol taşır. Yükseltmeden sonra
  `kubeat_scheduler_dispatch_skipped_total` ve `KubeatWorkersSaturated`'a
  bakın, gerekirse `UPTIME_WORKERS`'ı artırın. Özel bir GKE cluster'da Cloud
  NAT'tan aynı adrese giden çok monitor varsa NAT port sınırına da bakın
  (`nat/dropped_sent_packets_count`); ölçülmedi.
- **Kapsam reddi alarmlarınızı gözden geçirin.** DNS arızasıyla gelen ret
  0.51'de API'de `target`, kontrolde `connection` sayılıyordu; 0.52'de
  `resolve_error`'dır. `reason="target"` ya da `"connection"`'a bakan bir
  alarm DNS kesintisinde artık yanmaz; bu bilerek yapıldı. Gerçek bir
  cross-tenant girişimi için alarmı `source="user"` ve `reason="target"`'a
  bağlayın, DNS'i `reason="resolve_error"` ile ayrıca izleyin (README "Kapsam
  ihlali denetimi"). Yanıt vermeyen bir çözücü kontrolde sayılmaz, monitor
  zaman aşımıyla DOWN olur; DNS kesintisini monitor DOWN alarmları ya da
  cluster DNS'inin kendi metrikleriyle de izleyin.
- **`UPTIME_TARGET_RESOLVE_TIMEOUT`.** Varsayılan 5 sn'den 2 sn'ye iner ve
  yalnız API'deki oluşturma ve değiştirmeyi sınırlar. 2–5 sn'de yanıt veren
  bir çözücüde kapsamı sınırlı kullanıcının oluşturması artık reddedilir.
  Değeri arayüzün `BACKEND_TIMEOUT_MS`'inden (3 sn) küçük tutun; çözücünüz 2
  sn'den yavaşsa değeri 3 sn'nin altında büyütün (ör. `2500ms`). `5s` 0.51'in
  süresini geri getirir, ama 3 sn'yi aşan çözümde arayüz isteği bırakır.
- **LDAP'lı kurulumda kaynak tavanına karar verin.** LDAP tanımlıyken
  `UPTIME_LOGIN_SOURCE_MAX_FAILURES` verilmemişse açılışta bir `WARN` yazılır.
  Önerilen değer `20`'dir. Aynı NAT ya da kurumsal vekil (PAC) arkasındaki
  kullanıcılar tek kaynaktır; bu yüzden varsayılan kapalıdır. Açıkça `0`
  vermek satırı `INFO`'ya indirir. Tavanı açmadan önce hop değerini
  `sign-in refused` satırının `source` alanıyla doğrulayın: gerekenden küçük
  bir değer tavanı bütün girişler için tek kilide çevirir (README "Kaynak
  tavanı").
- **Doğru değerde yanan kaynak ayrımı kuralı.** İki kurulumda
  `KubeatSignInSourceDegraded` hop değeri doğruyken de yanar:
  - Arayüz Service'i önünde vekil olmadan dışarı açıksa (`LoadBalancer` ya da
    `NodePort`, hop `0`): `hops_zero`.
  - Başlık yazmayan bir katman (PROXY protocol'süz L4 yük dengeleyici, NAT)
    Kubeat'in genel adresinden bağlanıyorsa: `hops_too_low`.

  İkisinde de değeri artırmayın; fazlası kaynak başına sınırı atlatılabilir
  kılar. Kuralı Alertmanager'da `alertname`, `reason` ve `namespace` ile
  susturun (README "Sayaç ve kural").
- **Okunamayan bir namespace varsa.** 0.51'de namespace sınırlı keşifte
  okunamayan tek bir namespace bütün eşitlemeyi durduruyordu. Böyle bir
  namespace varsa 0.52'nin ilk turu öteki namespace'lerde birikmiş değişiklikleri
  bir seferde uygular; buna annotation'ı kaldırılmış monitor'lerin
  geçmişleriyle silinmesi de girer. 0.51'in keşif durumunda
  (`GET /api/v1/discovery`) `sync.error` boş değilse eşitleme durmuştur:
  geçmişini korumak istediğiniz monitor'lerin nesnelerinde annotation'ın
  yerinde olduğunu doğrulayın ve yedeğin doğrulandığından emin olun.
- **`metrics.prometheusRule.clusterLabel`** `reason` ya da `surface` ise
  `helm upgrade` render'da durur; yükseltmeden önce başka bir ad verin.
- **Kuralları kopyalıyorsanız yeniden kopyalayın** (aşağıda "Alarm kuralları:
  değişti").
- **Yedeği doğrulamalı blokla alın** (yukarıda "Yükseltmeden önce yedek").
  "yedek doğrulandı" demiyorsa yükseltmeyin.

### Davranış değişikliği

- **Kapsam reddinde çözüm hatası ve iptal
  ([#142](https://github.com/yusufceng/Kubeat/issues/142)).**
  `kubeat_target_scope_denied_total` yeni `reason="resolve_error"` değerini ve
  kapalı kümeli `source` etiketini (`user`, `annotation`, `unrecorded`)
  taşır; her seri açılışta 0'dan başlar.
  - Ne sayılır: API'de SERVFAIL, vekil istisnası dışındaki NXDOMAIN ve çözüm
    zaman aşımı. Kontrolde vekile giden ya da ayarlarda yazılı olmayan bir ada
    yönelen bağlantının çözüm hatası.
  - Ne sayılmaz: doğrudan bağlanan kontrolde monitor'ün yazılı hedefinin
    çözüm hatası sıradan bir DNS hatasıdır. Annotation'dan gelen monitor'ün
    kendi host'u nesnenin namespace'iyle kapsamdadır ve hiç çözülmez. Kontrolün zaman aşımı ya da
    kapanış çözümü yarıda keserse red sayılmaz, kontrol bağlamın hatasıyla
    biter.
  - Ne değişmez: istemcinin gördüğü 403 + `target_scope` ve kontrol sonucu
    `blocked_by_scope`.
  - Kontrol anındaki çözüm `UPTIME_TARGET_RESOLVE_TIMEOUT`'a bağlı değildir,
    monitor'ün kendi zaman aşımıyla sınırlıdır. 0.51'de vekile giden kontrol
    API'nin süresini kullanıyordu.
- **Denetim satırı sınırı ve API reddinin log satırı
  ([#143](https://github.com/yusufceng/Kubeat/issues/143)).**
  - Kapsam reddinin `deny`/`target-scope` satırları ve `WARN`'ları dakikada
    en çok `UPTIME_LOGIN_AUDIT_PER_MINUTE` kadar yazılır (varsayılan 60).
    Giriş hataları, API redleri ve kontrol redleri ayrı bütçe kullanır.
    Sınır aşılınca dakika sonunda bir `WARN` kaç satırın yazılmadığını
    söyler. Sayaç her reddi saymaya devam eder.
  - API reddi de kullanıcıyı, namespace'i, nedeni ve hedefi yazan bir `WARN`
    bırakır; kontrol anı `WARN`'ı namespace'i ve `source`'u da taşır.
  - Keşiften içe aktarma ve geri yükleme, bakım penceresi ve workload hedefi
    redleri de denetime yazılır ve sayılır. Push token, silme, test bildirimi
    ve olay notu redleri README'de "Denetlenmeyen red yolları" olarak,
    gerekçesiyle listelenir.
- **HTTP kontrolünün bağlantıları
  ([#144](https://github.com/yusufceng/Kubeat/issues/144)).** Her HTTP
  kontrolü, kapsama bağlı olsun olmasın, yeni bağlantı açar ve bitince
  kapatır. Sonuçları:
  - Yanıt süresine el sıkışma girer (yukarıda "Yapılacak").
  - Yenilenen sertifika bir sonraki kontrolde görünür ve sertifika olayı o
    kontrolle kapanır.
  - Kapsama bağlı çok monitor'lü kurulumda bellek düşer.

  Ayrı sertifika sunan birden çok sunucunun arkasındaki bir hedefte sertifika
  olayı kontrolden kontrole açılıp kapanabilir (README "Birden çok sunucu").
  Aynı sunucuya giden bir yönlendirme kontrolün kendi bağlantısını kullanır.
  Yeni ayar yok.
- **Okunamayan namespace
  ([#147](https://github.com/yusufceng/Kubeat/issues/147)).**
  - Namespace sınırlı keşifte, Role'ü kaldırılmış ya da RoleBinding'iyle
    silinmiş bir namespace atlanır ve tur öteki namespace'lerde sürer.
  - Atlanan namespace'in monitor'leri ve "Kubeat'te silindi" kayıtları,
    namespace okunana ya da listeden çıkarılana kadar değişmez.
  - Atlanan namespace'ler üç yerde görünür: `GET /api/v1/discovery`'de
    (`sync.skipped_namespaces`), namespace başına bir `WARN` satırında ve
    Keşif sayfasında.
  - Yeni seriler: `kubeat_discovery_namespaces_skipped` ve
    `kubeat_discovery_sync_errors_total{reason}`. İkisi de atlanan
    namespace'in adını etiket olarak taşımaz (`namespace` etiketi Kubeat'in
    kendi namespace'idir).
- **Kaynak ayrımı sayacı
  ([#146](https://github.com/yusufceng/Kubeat/issues/146)).**
  - `kubeat_signin_source_degraded_total{reason,surface}` throttle'ın
    kaynağını ayıramadığı her girişi, parola değiştirmeyi ve durum sayfası
    açılışını sayar.
  - `kubeat_throttled_total{surface,kind}` durum sayfası kilidi ile parola
    değiştirmenin 429'larını sayar. `kubeat_signin_throttled_total`'ın anlamı
    değişmez.
  - Yeni `hops_too_low` nedeni ve `WARN`'ı için `UPTIME_PUBLIC_URL`'ün host'u
    açılışta ve 5 dakikada bir çözülür. Emin olmadığında (CDN, CNAME) susar.

Ayrıca (opt-in ya da davranış değiştirmez):

- **Kaynak tavanı ([#145](https://github.com/yusufceng/Kubeat/issues/145)).**
  `UPTIME_LOGIN_SOURCE_MAX_FAILURES` (varsayılan `0`, kapalı) tek bir
  kaynağın bütün adlardaki hatalı girişlerine tavan koyar. Tavanı aşan giriş
  `kubeat_signin_throttled_total{kind="source_total"}` olarak sayılır ve
  hiçbir parola denenmez: dizine bind gitmez. Yanıt öteki kilitlerle aynıdır
  (429 + `Retry-After`). Değer verilmiş ama hop değeri `0` ya da
  `UPTIME_FRONTEND_TOKEN` boşsa açılışta bir `WARN` düşer.
- **`server resolve AD...`
  ([#143](https://github.com/yusufceng/Kubeat/issues/143)).** Backend
  container'ında koşulan komut, bir adı kapsam denetiminin çözdüğü gibi çözer
  ve kapsamı sınırlı bir kullanıcı için kararı yazar; hiçbir ayarı açmaz.
  Vekilli kurulumda `UPTIME_TARGET_SCOPE_TRUST_PROXY`'yi açmadan önce ön şartı
  sınamak için: `oc -n kubeat exec deploy/kubeat-backend -- server resolve
  <ad>` ([pilot.md](pilot.md)).

**Etki tablosu (S82).** Her hücrede sonuç, önce kapsamı açmayan çare
yazılıdır.

| Kurulum | Yönetici ya da `*` | Kapsamı sınırlı | Annotation | Kaydı olmayan |
| --- | --- | --- | --- | --- |
| Tek kiracılı | Değişmez | (yok) | Sonuç aynı. Kendi host'u nesnenin namespace'iyle kapsamdadır ve çözülmez; onun DNS arızası sıradan DNS hatasıdır. Kontrolün başka bir ada (vekile giden ya da ayarlarda yazılı olmayan) yönelen bağlantısında DNS arızası `resolve_error`, gerçek ret `connection` sayılır, ikisi de `source="annotation"`; satırlar dakikada sınırlı. Çare: cluster DNS'i ve egress | Değişmez (açılışta her namespace'in kaydı) |
| Çok kiracılı | Değişmez | Aynı 403. API'de DNS arızası `resolve_error{source="user"}` sayılır ve bir `WARN` yazar; çözüm başına 2 sn. Kontrolde vekile giden ya da yazılı olmayan bir adın DNS arızası da `resolve_error{source="user"}`; doğrudan bağlanan kontrolde yazılı hedefin DNS hatası sayılmaz. Çare: DNS ve egress, gerekirse süreyi 3 sn'nin altında büyütmek | Tek kiracılıdaki gibi | Namespace'ine bağlı (değişmez); vekile giden ya da yazılı olmayan bir adın DNS arızası `resolve_error{source="unrecorded"}`. Çare: DNS ve egress; hedefi `servis.namespace.svc` ile yazmak; yöneticinin kaydetmesi son çare (kapsamı açar) |
| Vekilli | Değişmez | Backend'de çözülemeyen vekilli ad yine reddedilir, artık `resolve_error` sayılır. Çare: vekil cluster dışındaysa ve `server resolve` NXDOMAIN diyorsa `UPTIME_TARGET_SCOPE_TRUST_PROXY=true`; SERVFAIL ve zaman aşımında ayar yardım etmez | Kendi host'u değişmez (çözülmez). Vekile giden başka bir adın backend'de çözülemeyişi reddedilir, artık `resolve_error{source="annotation"}`. Çare: Kapsamı sınırlı sütunundaki gibi | Aynı, `source="unrecorded"` |

Kaynak kullanımı: vekile giden kontrolde yanıt vermeyen bir çözücü kontrolü
5 sn'de değil monitor'ün zaman aşımında bitirir, worker'lar dolabilir
(minikube'da 3 sn gecikmeli çözücü, 2 worker ve 18 monitor'le
`kubeat_checks_in_flight` 2'de kaldı, `kubeat_scheduler_dispatch_skipped_total`
arttı; yanıt vermeyen çözücüyle ölçülmedi). Çare `UPTIME_WORKERS` ya da
daha kısa bir monitor zaman aşımıdır.

### Geri dönüş

Şema aynı (26), `helm rollback` ile 0.51.0'a. Dönüşte:

- `resolve_error` ve `source` etiketi kalkar: `source` seçicili kurallar
  eşleşmez, DNS arızası yine `target` ya da `connection` sayılır.
  `UPTIME_TARGET_RESOLVE_TIMEOUT` 0.51'de vekile giden kontrolü de sınırlar.
- Denetim satırı sınırı ve API reddinin log satırı kalkar.
- HTTP: kapsama bağlı monitor'lerde boşta bağlantı birikmesi geri gelir.
  Bütün HTTP monitor'lerinde (her pod'u ayrı kontrol edenler hariç) eski
  sertifikanın okunması geri gelir. Yanıt süresi grafiği bir kez daha değişir.
- `UPTIME_LOGIN_SOURCE_MAX_FAILURES` 0.51'de tanınmaz ve yok sayılır.
- Okunamayan bir namespace yine bütün eşitlemeyi durdurur.
- Yeni seriler, iki kural ve `server resolve` kalkar; kuralları kopyaladıysanız
  0.51'inkileri geri koyun.

### Alarm kuralları: değişti

- Yeni grup `kubeat.signin` (`kubeat.monitors` ile `kubeat.self` arasında):
  `KubeatSignInSourceDegraded`.
  - Ne zaman yanar: son 15 dakikada giriş throttle'ı istemcileri
    ayıramadığında (`hops_zero`, `no_frontend_token`, `hops_too_high`,
    `hops_too_low`). Kurulum ve neden başına bir alarmdır.
  - Ciddiyet: `metrics.prometheusRule.signInSeverity` (varsayılan `warning`).
    `KubeatUnhealthy`'nin parçası değildir.
  - Hop değeri yanlış bir kurulumda yükseltmeden sonraki ilk girişte yanar.
    Doğru değerde yandığı iki kurulum yukarıda "Yapılacak"tadır.
- Yeni grup `kubeat.discovery` (sonda): `KubeatDiscoveryNamespaceSkipped`.
  Eşitleme 10 dakikadır en az bir namespace'i atlıyorsa yanar; ciddiyeti
  `warning`'dir; atlanan namespace'in adı etiketlerinde yoktur.
- `metrics.prometheusRule.clusterLabel` artık `reason` ya da `surface`
  olamaz; böyle bir değerle `helm upgrade` render'da durur.

Kuralları başka bir depoya kopyalıyorsanız 0.52'ninkileri kopyalayın ve iki
alarmı Alertmanager yönlendirmesine ekleyin.

## 0.51'e yükseltme

0.51 bir güvenlik sürümüdür: namespace'le sınırlı kullanıcısı olan kurulumlar
yükseltmeli. Kapsamı sınırlı bir kullanıcının kurduğu monitor'ün kontrolü
artık açtığı her bağlantıda o kullanıcının hedef kapsamına tabidir; 0.50'ye
kadar kapsam yalnız hedef belirlenirken denetleniyordu (README "Hedef
politikası" → "Her bağlantıda"). Annotation'la kurulan monitor'ler her
kurulumda nesnelerinin namespace'ine bağlanır. Ayrıca kapsam denetimine kurum
ağı aralıkları için opt-in bir ayar, durum sayfası kilidi ile parola
değiştirmeye kaynak adrese göre sınır ve platforma göre hop değerleri gelir.
Şema değişmez (26), migration yok; chart kurulumunda yeni zorunlu ayar yok.
Kontrol anındaki kapsam, kapsam kaydı ve kurum aralıkları minikube'da iki
namespace ve kapsamı sınırlı bir editörle, monitor'ler arayüz formundan
kurularak sınandı (2026-11-07); kaydı olmayan monitor'lerin yükseltme yolu
`go test` ile sınandı. `ingress-nginx`'in hop değeri `1` ve arayüzün
ağ politikası Calico'lu minikube'da ölçüldü; GKE satırları ölçülmedi. Durum
sayfası kilidi ve parola değiştirmenin iki kaynaklı provası ve uydurma
`X-Forwarded-For` adımı sürümden sonra sandbox Route'unda geçti (2026-11-07).

### Yapılacak

- **Yükseltmeden önce ön kontrolü koşun** (kapsamı sınırlı kullanıcınız ya da
  annotation monitor'leriniz varsa; aşağıda "Ön kontrolleri betiklemek" → "Her
  bağlantıda kapsam"). Süzgeç
  yükseltmeden sonra kapsamın reddedeceği monitor'leri listeler; kapsamı
  sınırlı kullanıcı yoksa yalnız annotation monitor'lerine bakar. Kesin liste
  yükseltmeden sonra `blocked_by_scope` süzgecindedir.
- **Kurum ağındaki hedefler için `UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS`.**
  Kurum ağındaki özel bir adrese bağlanan kontroller 0.51'de `blocked_by_scope`
  ile DOWN olur: kapsamı sınırlı kullanıcıların monitor'lerinde, kapsamı sınırlı
  kullanıcısı olan kurulumda 0.51'den önce kurulmuş her monitor'de (yöneticinin
  kurduğu dahil, yönetici kaydedene kadar; aşağıda "Kaydı olmayan monitor'ler")
  ve her kurulumda annotation monitor'lerinin kendi host'u dışındaki
  bağlantılarında. Kurumun cluster dışındaki aralıklarını virgülle verin (ör.
  `10.50.0.0/16`): bu aralıklara çözülen ad ya da çıplak adres kapsamı sınırlı
  kullanıcı için cluster dışı sayılır. Pod, Service ve node (makine) ağı ile
  API VIP'i listeye girmez; router VIP'i yalnız bilerek girer (node ağındaysa
  `/32` ile) ve o VIP'teki her Route'u, başka ekiplerinkiler de, açar.
  Geçersiz bir aralık (IPv6 biçiminde yazılmış IPv4 aralığı dahil) ya da API
  sunucusunun veya backend pod'unun adresini içeren bir aralık backend'i
  başlatmaz; log nedenini yazar (README "Hedef politikası" → "Kurum ağı").
  İlk açılışın geçmesi güvence değildir: pod ağının bir parçasını içeren bir
  aralık, pod başka bir node'a taşınınca backend'i başlatmaz. Varsayılan boştur ve 0.50'nin oluşturma anındaki davranışı değişmez.
- **Vekilli kurulum.** Backend'in çözücüsü genel adları çözemiyorsa (hava
  boşluklu, vekilli kurulum), vekile giden ve backend'in çözemediği bir dış
  host'a ulaşan kontrol DOWN olur. Bu, annotation monitor'lerinde kendi host'u
  dışındaki host'ta (ör. bir kimlik sağlayıcının giriş sayfası), kapsamı
  sınırlı kullanıcısı olan kurulumda da 0.51'den önce kurulmuş monitor'lerin
  (yöneticininki dahil) hedefinde olur. Vekil cluster dışındaysa çare
  `UPTIME_TARGET_SCOPE_TRUST_PROXY=true`'dur (yalnız NXDOMAIN'i vekile bırakır;
  SERVFAIL ve zaman aşımı yine red sayılır). Yöneticinin kurduğu monitor'ü bir
  kez kaydetmek de onu düzeltir (aşağıda). Öteki durumda monitor'ü yönetici
  olarak yeniden kurun.
- **Kaydı olmayan monitor'ler.** Monitor'ler kapsam kaydını 0.51'de alır.
  Açılışta kapsamı sınırlı editör yoksa annotation monitor'leri dışında 0.51'den
  önceki her monitor her namespace'in kaydını alır (log: `monitors set up
  before the target scope was recorded keep reaching every namespace`) ve
  değişen bir şey olmaz. Varsa her biri kendi namespace'inin kapsamına bağlanır
  ve sayıları her açılışta bir `WARN` satırına yazılır (`monitors set up before
  the target scope was recorded are held to their own namespace until an
  administrator saves them`): bir yöneticinin bir namespace'e koyup başka bir
  namespace'e, çıplak bir adrese, kurum ağına (aralık tanımlı değilse) ya da
  vekilli kurulumda yalnız vekilin çözdüğü bir dış ada bağlanan monitor'ü de
  DOWN olur. Yönetici onu bir kez
  kaydedince (formu değiştirmeden kaydetmek ya da hedefi aynı değerle `PATCH`
  etmek) kaydı yazılır ve düzelir:
  `api "monitors/$ID" -X PATCH -H 'Content-Type: application/json' -d "$(api "monitors/$ID" | jq -c '{target}')"`.
  **Kapsamı sınırlı bir kullanıcının kurduğu eski monitor'ü yönetici olarak
  kaydetmeyin:** kayıt yokken yazanın kim olduğu bilinmez ve yöneticinin
  kaydı, formdan da, ona her namespace'in kaydını yazar; kayıtsız kaldıkça
  namespace'ine bağlıdır.
- **Yedeği doğrulamalı blokla alın** (yukarıda "Yükseltmeden önce yedek").
  "yedek doğrulandı" demiyorsa yükseltmeyin.

### Davranış değişikliği

- **Her bağlantıda kapsam
  ([#131](https://github.com/yusufceng/Kubeat/issues/131)).** Kapsamı sınırlı
  bir kullanıcının kurduğu monitor'ün kontrolü yaptığı her bağlantıda o
  kullanıcının kapsamına tabidir; kural oluşturmadakiyle aynıdır (kurum
  aralıkları ve vekilli kurulum ayarı dahil). Reddedilen bağlantı monitor'ü
  DOWN yapar: açıklama `blocked_by_scope`, hata ve bildirim iki dilde tek
  genel bir metindir ve adres yazmaz; ters monitor'de beklenen başarısızlık
  sayılmaz. Her red `kubeat_target_scope_denied_total{reason="connection"}`'da
  sayılır (seri 0'dan başlar); `checker` kullanıcılı denetim satırı monitor
  başına bir kez yazılır (duraklatıp açınca, ayarlar değişince ya da backend
  yeniden başlayınca bir satır daha). Yöneticinin, API token'ının ve kapsamı
  `*` olan kullanıcının monitor'leri etkilenmez; istisna, kapsamı sınırlı
  kullanıcısı olan kurulumda 0.51'den önce kurulmuş ve yöneticinin henüz
  kaydetmediği monitor'lerdir (yukarıda "Kaydı olmayan monitor'ler").
  Kontrolün bağlandığı uç noktalar oluşturma ve değiştirmede de denetlenir:
  kapsamı sınırlı kullanıcı, hedefi kapsamının dışındaki bir monitor'ün
  seçeneklerini değiştiremez (403); onu duraklatabilir. CoreDNS kesintisinde bu kontrollerin bir kısmı
  DOWN olur ve bildirim bunu kapsam reddi diye söyler (S82).
- **Kapsam kaydı.** Monitor'ün ayarlarında kontrolün bağlandığı yeri en son
  adlandıran kullanıcının namespace'leri tutulur. Kayıt daha geniş bir
  kapsama yalnız hedef ya da host adı taşıyan bir seçenek değişince geçer:
  yöneticinin bir monitor'ü ad, etiket, aralık ya da kontrolün başka bir
  seçeneğiyle kaydetmesi, formdan da, kaydı genişletmez; istisna yukarıdaki
  kaydı olmayan monitor'dür. Kaydın namespace'lerinin hepsini tutmayan biri
  kontrolün herhangi bir seçeneğini değiştirirse kayıt ortak namespace'lere
  daralır; formun saklanandan farklı yazdığı bir seçenek de (ör. API'yle
  kurulmuş bir monitor'de) buna girer. Daralmış bir monitor'ü sahibi de
  yönetici de hedefi ya da host adı taşıyan bir seçeneği farklı bir değere
  çevirerek ya da kopyalayarak geri alır.
- **Annotation monitor'leri her kurulumda.** Kapsamı sınırlı kullanıcısı
  olmayan kurulumda da annotation'la kurulan monitor nesnesinin namespace'ine
  bağlanır: hedefini nesneyi düzenleyen belirler ve Kubeat onun kim olduğunu
  bilmez. Kendi host'unda kalan annotation monitor'ü değişmez. Kontrolü
  kendi host'undan başka bir namespace'e ya da cluster içine, kurum aralığı
  tanımlı değilse özel adresli bir kurum host'una, vekilli kurulumda da
  backend'in çözemediği başka bir dış host'a ulaşan annotation monitor'ü
  `blocked_by_scope` ile DOWN olur (çareler yukarıda). Açılışta log: 
  `annotation monitors set up before the target scope was recorded are held
  to their object's namespace`.
- **Durum sayfası kilidi ve parola değiştirme kaynak adrese göre
  ([#134](https://github.com/yusufceng/Kubeat/issues/134)).** Bir kaynağın
  `UPTIME_LOGIN_MAX_FAILURES` yanlış denemesi yalnız o kaynağı kilitler; sayfa
  ve hesap başına gevşek tavan `UPTIME_LOGIN_ACCOUNT_MAX_FAILURES`'tır ve doğru
  parola onu sıfırlar. Hop değeri `0` ya da gerekenden küçükse (ör. WAF'lı
  Route'ta `1`) veya `UPTIME_FRONTEND_TOKEN` yoksa bu iki yüzey de giriş gibi tek paylaşılan kilide geriler; girişin hop
  `WARN`'ları bu yüzeylerde de düşer ve yüzeyi adlandırır (eksik değer `WARN`
  vermez; fazla değerde başlığını uzatan istemci kaynağını seçebilir). Kilit
  yanıtı aynıdır (429 + `Retry-After`). Ret satırları (`status page unlock
  refused`, `password change refused`) `source` alanını taşır; 429'lar yalnız
  throttle'ın dakikalık `sign-in attempts refused` `WARN`'ında görünür. Yeni
  ayar yok.

Ayrıca (davranış değiştirmez ya da opt-in):

- **Kurum ağı aralıkları ([#132](https://github.com/yusufceng/Kubeat/issues/132)).**
  Yeni opt-in `UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS` (yukarıda "Yapılacak").
  Hedef politikası ve yük korumaları bu ayarı okumaz.
- **Hop değeri komutlarda ve kurulum notunda
  ([#133](https://github.com/yusufceng/Kubeat/issues/133)).** Hızlı başlangıç,
  GKE ve OpenShift komutları ile banka pilot reçetesi hop değerini taşır (düz
  Route ve `ingress-nginx` `1`; GCE Ingress ve GKE Gateway `2`); pilot vekilli
  kapsam ayarını kapalı verir. Kurulum notu Ingress'te sınıfı
  (`ingress.className` ya da `kubernetes.io/ingress.class`) biliniyorsa tek
  değer önerir; Route'ta, router politikası Route annotation'ıyla
  sabitlenmedikçe, `IfNone`/`Never` notunu hep yazar; arayüz politikası
  açıkken doğrulama satırı, iki politika açıkken push hatırlatması, varsayılan
  `from` OpenShift dışında kalırsa render'ı durdurmayan bir `WARNING` verir.
  Chart'ın hop varsayılanı `0` kaldı.

### Geri dönüş

Şema aynı (26), `helm rollback` ile 0.50.0'a. 0.50 kapsam kaydını tanımaz ve
0.50'de kaydedilen (duraklatılan dahil) bir monitor'ün kaydını düşürür; 0.51'e yeniden geçişte o
monitor kaydı olmayan monitor gibi değerlendirilir (yukarıda). Dönüşte her
bağlantıdaki kapsam, annotation monitor'lerinin namespace'e bağlanması,
durum sayfası kilidi ile parola değiştirmenin kaynak sınırı kalkar;
`UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS` 0.50'de tanınmaz ve yok sayılır (kurum
hedeflerinin oluşturması yine reddedilir, var olanlar çalışır). Kurum
aralığını ayarlayıp backend'i başlatamazsanız ayarı boşaltmak da geri
dönüştür.

### Alarm kuralları: değişmedi

Chart'ın PrometheusRule'u 0.50'ye göre değişmedi; kuralları kopyaladıysanız
yeniden kopyalamanız gerekmez. `blocked_by_scope` ile DOWN olan monitor
kendi kuralınızla (ör. `kubeat_target_scope_denied_total{reason="connection"}`
artışı) izlenebilir; chart buna hazır kural getirmez.

## 0.50'ye yükseltme

0.50 bir güvenlik sürümüdür: 0.48–0.49'da yayımlanan kaynak-başına giriş
throttle'ını (CWE-307) kullanıcının asıl yolunda (tarayıcı → Route/Ingress →
arayüz → backend) çalışır kılar ve backend'in `X-Forwarded-For`'a yalnız
arayüzden gelen istekte güvenmesini sağlar. "0.49'a yükseltme"deki iki giriş
sınırı ("arayüz yolu istemci adresini taşımaz", "cluster içinden uydurma
`X-Forwarded-For`") bu sürümle kapanır; arayüzün ağ politikası kapalıyken
arayüz Service'ine doğrudan gelen uydurma başlık kalır (aşağıda). Ayrıca
vekilli kurulum için opt-in bir kapsam ayarı getirir ve yedeğin cluster dışına
alınmasını doğrulamalı tek bir bloğa bağlar. Şema değişmez (26), migration
yok; chart kurulumunda yeni zorunlu ayar yok. Arayüz yolu `go test` ve chart
testleriyle sınandı; gerçek bir Route arkasındaki prova sürümden sonradır.
Aşağıdaki hop değerlerinden düz OpenShift Route `1` ölçüldü (sandbox,
2026-11-07); ötekiler henüz gerçek bir cluster'da ölçülmedi.

### Yapılacak

- **Giriş bir Route ya da Ingress arkasındaysa `UPTIME_TRUST_PROXY_HOPS`'u yeni
  rehbere göre gözden geçirin.** Değer artık arayüzün önündeki vekillerinizin
  `X-Forwarded-For`'a yazdığı girdi sayısıdır (README "Hesaplar ve giriş",
  "Vekil-hop rehberi"):
  - düz OpenShift Route `1`; önünde başlığı yazan bir WAF ya da L7 yük
    dengeleyici olan Route `2` (`2`'yi yalnız WAF başlığı yazıyorsa seçin);
  - GKE `ingress-nginx` (L4 LoadBalancer, `externalTrafficPolicy: Local`,
    varsayılan ayarlar) `1`; GKE'nin
    kendi Ingress'i (GCE) `2`; önde L7 GCLB + `ingress-nginx` yalnız
    `use-forwarded-headers` ve `compute-full-forwarded-for` açıksa `3`,
    varsayılanlarla `1` (kaynak o zaman GCLB vekilidir);
  - vekil yok ve arayüze yalnız port-forward ile ulaşılıyorsa `1`; arayüz
    Service'i önünde vekil olmadan doğrudan dışarı açıksa (`LoadBalancer`,
    `NodePort`) `0`.

  0.48–0.49 rehberiyle verilmiş değerlerden GKE'nin kendi Ingress'i `1` → `2`;
  GCLB + `ingress-nginx` `2` → iki ayar açıksa `3`, varsayılanlarla `1`; düz
  Route `1` ve WAF'lı Route `2` aynı kalır. **Fazla değer artık
  güvenli taraf değildir:** başlığa ekleyen bir vekilin (Route `Append`, GCLB)
  arkasında istemci başlığı fazladan bir girdiyle uzatıp kaynağını seçebilir
  ve bu isteklerde `WARN` düşmez. Eksik değer `WARN` vermez, throttle'ı vekilin
  adresine anahtarlar. **Emin değilseniz küçük değeri seçin.** Yükseltmeden
  sonra arayüzden bir kez yanlış parola deneyin ve `sign-in refused` satırının
  `source` alanının tarayıcınızın adresi olduğunu doğrulayın (README "Görülen
  kaynağı doğrulamak"). Chart varsayılanı `0` kaldı; kurulum notu (NOTES)
  Route ya da Ingress açıkken giriş türüne uyan değeri önerir. Değeri
  `--set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=1` ile verin.
- **Chart dışı kurulumda değer `0`'dan büyükse arayüzün sırrını verin:**
  backend'e `UPTIME_FRONTEND_TOKEN`, arayüze `FRONTEND_TOKEN` olarak aynı
  rastgele değer (ör. `openssl rand -hex 24`; yazdırılabilir ASCII, boşluksuz).
  Verilmezse backend açılışta `WARN` yazar ve `X-Forwarded-For`'a hiçbir eşten
  güvenmez: her tarayıcı arayüz pod'una anahtarlanır (tek paylaşılan kilit,
  bypass değil). Chart sırrı kendisi üretir (`<release>-frontend-token`).
- **GitOps ile kuruyorsanız beşinci Secret'ı oluşturun:** `token` anahtarlı
  bir Secret'ı `frontendToken.existingSecret` ile verin; yoksa her eşitleme
  yeni sır yazar ve pod'lardan yalnız biri yeniden başladığında tarayıcı
  girişleri arayüz pod'una anahtarlanır (tek paylaşılan kilit, `WARN`; README
  "GitOps ile kurulum").
- **İsteğe bağlı: arayüzün ağ politikası.** Değer `0`'dan büyükken cluster
  içindeki bir pod arayüz Service'ine kendi yazdığı bir `X-Forwarded-For` ile
  bağlanıp kaynağını seçebilir; bunu yalnız `frontend.networkPolicy.enabled=true`
  (varsayılan kapalı; NetworkPolicy uygulayan bir ağ eklentisi ister) kapatır.
  `from`'un varsayılanı OpenShift router'ıdır; başka bir platformda ingress
  controller'ın namespace'ini (GCE Ingress'te yük dengeleyicinin aralıklarını)
  verin. `networkPolicy.enabled` de açıksa push gönderen namespace'leri `from`'a
  ekleyin, yoksa push monitor'leri yanlış DOWN düşer (README "Arayüzün ağ
  politikası"). Kurulum notu bu amaçla artık `networkPolicy.enabled`'ı değil
  bunu önerir.
- **İsteğe bağlı, vekilli kurulumda: `UPTIME_TARGET_SCOPE_TRUST_PROXY`.** Dış
  adları yalnız kurumsal vekilin çözdüğü bir kurulumda (`HTTP_PROXY`/`HTTPS_PROXY`,
  `NO_PROXY`) kapsamı sınırlı kullanıcının vekile giden HTTP/WebSocket
  monitor'leri ve webhook türü kanalları 0.49'dan beri yerel NXDOMAIN yüzünden
  reddediliyordu. `true` (varsayılan `false`) bu NXDOMAIN'i kabul eder; cluster
  içine çözülen ad, SERVFAIL ve zaman aşımı yine reddedilir. **Yalnız vekil
  cluster dışındaysa açın:** o zaman kapsamın sınırı vekilin ulaştığı yerdir ve
  node IP'lerini, VIP'leri ve kurum aralıklarını vekilin ACL'i kapatmalıdır;
  kapatmıyorsa ya da vekil cluster içindeyse ayar kapalı kalır (README "Hedef
  politikası" → "Vekilli kurulum").
- **Yedeği yeni doğrulamalı blokla alın**
  ([yedek-geri-yukleme.md "Yedek kopyaları cluster dışına almak"](yedek-geri-yukleme.md#yedek-kopyaları-cluster-dışına-almak);
  yukarıda "Yükseltmeden önce yedek"). Blok "yedek doğrulandı" demiyorsa
  yükseltmeyin. Düz `exec … cat` ile alınmış eski bir kopya doğrulanmamıştır:
  yeni kuruluma geri yükleme onu yalnız açık onayla (`DOGRULAMASIZ=1`) yükler,
  kesik bir kopyayı hiç yüklemez. Dolu-disk alarm örneğini runbook'tan
  kopyaladıysanız PVC adı `<release>-data`'dır (örnek önceden sabit
  `kubeat-data` yazıyordu).

### Davranış değişikliği

- **Arayüz istemci adresini taşır
  ([#118](https://github.com/yusufceng/Kubeat/issues/118)).** Arayüz giriş,
  parola değiştirme ve durum sayfası kilidi isteklerinde tarayıcıdan gelen
  `X-Forwarded-For`'u backend'e aynen taşır. Değer rehbere göre ayarlıysa
  Route/Ingress arkasında giriş throttle'ı artık istemcinin adresine
  anahtarlanır. Değer `0` kaldıysa `WARN` artık arayüz yolunda da düşer ve
  metni değerin nasıl seçileceğini söyler. 0.49'da arayüz yolunda beklenen
  "zincir kısa" `WARN`'ı 0.50'de yalnız değer fazlayken düşer (backend'e
  doğrudan gelen giriş "güvenilmeyen eş" `WARN`'ı verir); sürüyorsa değeri
  düşürün. `sign-in refused` satırı throttle'ın anahtarladığı adresi `source`
  alanında yazar (yalnız operatör logu). Parola değiştirme ve durum sayfası
  kilidinin sayaçları henüz kaynağa göre değil, hesaba ve sayfaya göredir.
- **`X-Forwarded-For`'a yalnız arayüzden güvenilir
  ([#119](https://github.com/yusufceng/Kubeat/issues/119)).** Değer `0`'dan
  büyükken backend başlığı yalnız arayüzün sırrını (`X-Frontend-Token`) sunan
  istekte okur. Başka bir eşte (backend'e doğrudan bağlanan bir pod, bir betik,
  backend'e port-forward) kaynak o eştir ve dakika-sınırlı bir "güvenilmeyen
  eş" `WARN`'ı düşer: cluster içindeki bir pod backend'e doğrudan bağlanarak
  uydurma kaynaklarla hesap tavanını doldurup `admin`'i kilitleyemez; arayüz
  Service'i üzerinden gelen yolu yalnız `frontend.networkPolicy.enabled`
  kapatır (yukarıda). Chart sırrı `<release>-frontend-token`
  Secret'ında üretir ve iki pod'a verir. Değer `0` iken bir şey değişmez.
- **Yükseltme sırasında kısa bir pencere.** Backend `Recreate` ile, arayüz
  rolling update ile yenilenir. Değer `0`'dan büyükse yeni backend açıldığında
  henüz yenilenmemiş 0.49 arayüz pod'ları sırrı göndermez ve backend onları
  güvenilmeyen eş sayar: bu sürede tarayıcı girişleri arayüz pod'una
  anahtarlanır (0.49'daki gibi tek paylaşılan kilit) ve "güvenilmeyen eş"
  `WARN`'ı düşer. Yeni arayüz pod'ları hazır olunca kendiliğinden biter.
- **Hedef kapsamında host'un yazılışı
  ([#121](https://github.com/yusufceng/Kubeat/issues/121)).** Kapsamı sınırlı
  bir kullanıcı için host vekilin ve çözücünün okuduğu gibi okunur: küçük
  harfle ve sondaki noktalar atılarak (`api.odeme.svc..` `api.odeme.svc`'dir).
  Adres gibi yazılmamış rakamlı bir host (`172.30.1`, `0xac.0x1e.0.1`,
  `2887647233`) ve ASCII olmayan bir host artık reddedilir; adres `172.30.0.1`
  gibi, uluslararası ad `xn--` biçiminde yazılır. Yalnız yeni hedef, klon ve
  hedef değişikliği etkilenir; var olan monitor'ler ve kanallar yeniden
  denetlenmez, admin ve kapsamı `*` olan kullanıcılar etkilenmez.
  `UPTIME_TARGET_SCOPE_TRUST_PROXY` kapalıyken başka bir şey değişmez;
  `HTTP_PROXY`/`HTTPS_PROXY` verilmiş ve ayar kapalıysa backend açılışta bir
  kez `WARN` yazar (vekil cluster içindeyse bu satır bilgi içindir).
- **Zamanlayıcı ([#127](https://github.com/yusufceng/Kubeat/pull/127)).**
  Kuyrukta yer varken sırası gelen kontrol, tick süresi dolmuş olsa da artık
  her zaman gönderilir. `kubeat_scheduler_dispatch_skipped_total` yalnız tüm
  worker'lar gerçekten doluyken artar; önceden nadiren boş worker varken de
  artabiliyordu. Kapanışta bağlam iptal edilince kuyruğa yeni iş girmez. Ayar
  ya da chart değişikliği yok.

### Geri dönüş

Şema aynı (26), `helm rollback` ile 0.49.0'a. `<release>-frontend-token`
Secret'ı yetim kalır: chart varsayılanında `helm.sh/resource-policy: keep`
taşır, 0.49 onu okumaz ve 0.50'ye yeniden geçişte aynı sır kullanılır
(`backend.persistence.keepOnUninstall=false` ise rollback Secret'ı siler;
0.50'ye yeniden geçişte yenisi üretilir, zararsız). Dönüşte arayüzün `X-Forwarded-For` taşıması, backend'in başlığa
yalnız arayüzden güvenmesi, `frontend.networkPolicy`,
`UPTIME_TARGET_SCOPE_TRUST_PROXY`, host yazılışı denetimi, `source` alanı ve
zamanlayıcı düzeltmesi kalkar; "0.49'a yükseltme"deki iki giriş sınırı geri
gelir (değer `0`'dan büyükse 0.49'da `networkPolicy.enabled=true` backend'e
doğrudan geleni kapatır). 0.50 rehberiyle verilmiş bir hop değeri 0.49'da
kaynakları ayırmaz: kaynak arayüz pod'u kalır ve "zincir kısa" `WARN`'ı düşer.

### Alarm kuralları: değişmedi

Chart'ın PrometheusRule'u 0.49'a göre değişmedi; kuralları kopyaladıysanız
yeniden kopyalamanız gerekmez. `KubeatWorkersSaturated`'ın ifadesi aynıdır;
baktığı `kubeat_scheduler_dispatch_skipped_total` artık yalnız tüm worker'lar
doluyken artar (yukarıda "Zamanlayıcı").

## 0.49'a yükseltme

0.49, 0.48'de yayımlanan giriş throttle'ını gerçek vekil/NAT topolojisinde
savunulur kılar (güvenilemeyen `X-Forwarded-For` yapılandırmasında artık uyarır
ve güvenli tarafa düşer) ve 0.48'in çözüm anında kapsam denetimini DNS hatasında
güvenli tarafa alır (CoreDNS kesintisinde fail-open kapanır, çözüme ayrı bir
zaman aşımı konur). Ayrıca giriş/dizin erişilebilirliğini, giriş throttle/
lockout'unu ve kapsam dışı hedef reddini operatöre metrik ve denetim kaydıyla
görünür kılar. Şema değişmez (26); hiçbir madde yeni zorunlu ayar getirmez.

### Yapılacak

- **Bilinen sınır (0.48–0.49): arayüz yolu istemci adresini taşımaz.** Route
  ve Ingress yalnız arayüze gider; 0.48 ve 0.49'da arayüz giriş, parola
  değiştirme ve durum sayfası kilidi isteklerini backend'e `X-Forwarded-For`
  olmadan iletir. Tarayıcıdan gelen her deneme arayüz pod'unun adresine
  anahtarlanır: `UPTIME_TRUST_PROXY_HOPS` kaç olursa olsun kaynak-başına
  throttle ad-başına tek paylaşılan kilide geriler; `0` iken `WARN` bu yolda
  düşmez, `1` iken "zincir kısa" `WARN`'ı düşer ama kaynak yine arayüz pod'udur.
  Aşağıdaki hop sayıları bu sürümlerde yalnız backend'e doğrudan gelen istekte
  (port-forward, API) işe yarar. 0.50 bunu düzeltir
  ([#118](https://github.com/yusufceng/Kubeat/issues/118)): arayüz başlığı
  aynen taşır ve değer arayüz yoluna göre yeniden türetildi (README "Hesaplar
  ve giriş", "Vekil-hop rehberi"): vekilsiz (arayüze port-forward) `1`, düz
  OpenShift Route `1`, önünde başlığı yazan bir WAF ya da L7 yük dengeleyici
  olan Route `2`, GKE `ingress-nginx` (L4 LoadBalancer, `externalTrafficPolicy:
  Local`, varsayılan ayarlar) `1`, önde L7 GCLB + `ingress-nginx`
  (`use-forwarded-headers` ve `compute-full-forwarded-for`) `3`, GKE'nin kendi
  Ingress'i (GCE) `2`; düz Route `1` ölçüldü (sandbox, 2026-11-07), ötekiler
  henüz ölçülmedi. 0.50'ye geçerken 0.48–0.49
  rehberiyle verilmiş değerleri gözden geçirin (düz Route `1` ve WAF'lı Route
  `2` aynı kalır; `2`'yi yalnız WAF başlığı yazıyorsa seçin): GKE'nin kendi
  Ingress'i (yalnız GCLB) `1` → `2`; GCLB + `ingress-nginx` yalnız
  `use-forwarded-headers` ve `compute-full-forwarded-for` açıksa `3`,
  varsayılanlarla `1` (kaynak o zaman GCLB vekilidir; istemciyi ayırmak için iki
  ayarı açın). Eksik değer `WARN` vermez. 0.49'da arayüz yolunda beklenen
  "zincir kısa" `WARN`'ı 0.50'de yalnız değer fazlayken düşer (backend'e
  doğrudan gelen giriş 0.50'de "güvenilmeyen eş" `WARN`'ı verir); sürüyorsa
  değeri düşürün. Fazla değer 0.50'de güvenli tarafa düşmez: başlığa ekleyen
  bir vekilin (Route `Append`, GCLB) arkasında istemci başlığı fazladan bir
  girdiyle uzatıp kaynağını seçebilir ve bu isteklerde `WARN` düşmez; emin
  değilseniz küçük değeri seçin. Doğruluğu `sign-in refused` satırının `source`
  alanından denetleyin. 0.49'da kalırken kilitli bir hesap için README "Kilitli
  hesaba girmek" yolları geçerlidir.
- **Bilinen sınır (0.48–0.49): cluster içinden uydurma `X-Forwarded-For`.**
  `UPTIME_TRUST_PROXY_HOPS` `0`'dan büyükken 0.48 ve 0.49'un backend'i başlığa
  bağlanan eşe bakmadan güvenir: cluster'daki bir pod `backend:8080`'e kendi
  yazdığı bir başlıkla bağlanıp her denemede yeni bir kaynak gösterebilir,
  kaynak-başına sınırı atlayıp hesap tavanını (`UPTIME_LOGIN_ACCOUNT_MAX_FAILURES`,
  50) doldurarak `admin`'i herkese kilitleyebilir. 0.49'da kalırken
  `networkPolicy.enabled=true` backend'e doğrudan geleni kapatır (push işlerine
  etkisi README "Ağ erişimi"nde). 0.50 bunu düzeltir
  ([#119](https://github.com/yusufceng/Kubeat/issues/119)): backend başlığı
  yalnız arayüzün sırrını (`X-Frontend-Token`) sunan istekte okur, chart sırrı
  `<release>-frontend-token` Secret'ında üretir ve iki pod'a verir. Chart dışı
  bir kurulumda değer `0`'dan büyükse 0.50'ye geçerken backend'e
  `UPTIME_FRONTEND_TOKEN`, arayüze `FRONTEND_TOKEN` olarak aynı rastgele değeri
  verin; verilmezse `X-Forwarded-For` yok sayılır ve `WARN` düşer. GitOps'ta
  Secret'ı kendiniz oluşturup `frontendToken.existingSecret` ile verin. Arayüz
  Service'ine doğrudan gelen uydurma başlığı 0.50'de de yalnız isteğe bağlı
  arayüz politikası (`frontend.networkPolicy.enabled`) kapatır (README
  "Hesaplar ve giriş", "Arayüzün ağ politikası").
- **Giriş bir vekilin arkasındaysa `UPTIME_TRUST_PROXY_HOPS`'u somut hop sayısına
  göre gözden geçirin.** 0.49, `X-Forwarded-For` görülürken `hops=0` iken ve
  ayarlanan hop sayısı gerçek zincirden kısa/uzun geldiğinde çalışma anında
  dakika-sınırlı bir `WARN` yazar (satır anahtarlanan adresi de verir; görülen
  kaynağı bu satırdan doğrulayın). 0.49'da arayüz yolunda hiçbir değer
  kaynakları ayırmaz (yukarıdaki sınır); 0.50'nin sayılarını şimdiden vermek
  zararsızdır: kaynak arayüz pod'u kalır ve "zincir kısa" `WARN`'ı düşer.
  Paylaşılan kurumsal NAT/vekil arkasında aynı kaynak adresi birçok kullanıcı
  için tektir; o popülasyonda `UPTIME_LOGIN_ACCOUNT_MAX_FAILURES`'ı daha yüksek
  tutmayı değerlendirin (güvenilir-vekil IP allowlist yol haritasında).
- **Egress'i varsayılan-reddet olan (hava boşluklu) bir cluster'da backend
  pod'unun cluster DNS'ine (kube-dns/openshift-dns) egress'ine izin verin.**
  0.49'da kapsamı sınırlı bir kullanıcının dış görünüşlü hedef oluşturması ya da
  değiştirmesi yazma anında bir DNS sorgusu yapar; chart'ın NetworkPolicy'si
  egress'i açmaz (yalnız Ingress). İzin yoksa bu kullanıcıların meşru dış hedef
  create'leri güvenli tarafta reddedilir (admin, `*` kapsamlı kullanıcılar ve tek
  kiracılı kurulumlar etkilenmez). Çözüm zaman aşımını
  `UPTIME_TARGET_RESOLVE_TIMEOUT` ile ayarlayabilirsiniz (varsayılan 5 sn).
- İsteğe bağlı: yeni `kubeat_signin_failures_total{reason}`,
  `kubeat_signin_throttled_total{kind}` ve
  `kubeat_target_scope_denied_total{reason}` serilerini Prometheus'unuzda bir
  alarma bağlayın (README "Hesaplar ve giriş" örnek PromQL'i). Seriler `/metrics`
  token'lı yüzeyde kalır ve enum sızdırmaz.

### Davranış değişikliği

- Giriş: `X-Forwarded-For` görülürken `UPTIME_TRUST_PROXY_HOPS=0` iken ya da XFF
  zinciri ayarlanan hop sayısından kısa geldiğinde backend çalışma anında
  dakika-sınırlı bir `WARN` yazar; hops gerçek zincirden uzunsa görülen kaynak
  istemci-kontrollü en soldaki girdi yerine bağlanan eşe (peer) düşer. Böylece
  uydurma bir XFF ile throttle atlanamaz ya da bir kurban kilitlenemez; yanlış
  ayarın maliyeti "bypass" değil "tek paylaşılan kilit"tir.
- Hedef kapsamı: kapsamı sınırlı bir kullanıcı için ad çözümü hata, zaman aşımı
  ya da NXDOMAIN verince dış görünüşlü hedef artık reddedilir (önce sessizce
  kabul ediliyordu); iç adrese çözülen hedefin 403 metni tek genel mesaja
  indirildi. Admin, `*` kapsamlı kullanıcılar ve tek kiracılı kurulum etkilenmez.
- **Bilinen sınır: vekilli kurulumda dış hedef.** Dış adları yalnız kurumsal vekilin
  çözdüğü (`HTTPS_PROXY`, `NO_PROXY`; backend'in çözücüsünde NXDOMAIN) bir kurulumda
  kapsamı sınırlı kullanıcının bu adlara HTTP/WebSocket monitor'ü ve webhook türü
  kanalı da reddedilir. Her deneme bir `deny`/`target-scope` satırı ve
  `reason=target` sayımı bırakır; bu bir cross-tenant girişimi değildir. 0.49'da
  geçici yol: hedefi yönetici kurar ya da keşiften içe aktarılır. 0.50'de opt-in
  `UPTIME_TARGET_SCOPE_TRUST_PROXY` (yalnız vekil cluster dışındaysa ve vekilin
  ACL'i node IP'lerini, VIP'leri ve kurum aralıklarını kapatıyorsa) bu reddi
  kaldırır ([#121](https://github.com/yusufceng/Kubeat/issues/121); README "Hedef
  politikası" → "Vekilli kurulum").
- Gözlem (davranış değil): kapsam/yetki dışı hedef reddi artık bir denetim satırı
  (`deny`/`target-scope`, 90 gün, yöneticiye açık) ve
  `kubeat_target_scope_denied_total{reason}` sayacı üretir; istemciye dönen 403
  ve başarılı yol değişmez.

### Geri dönüş

Şema aynı (26), `helm rollback` ile 0.48.0'a. Dönüşte giriş `WARN`'ı ve
güvenli-fallback, kapsam denetiminin DNS-hatası reddi,
`UPTIME_TARGET_RESOLVE_TIMEOUT` ve yeni metrik/denetim kayıtları kalkar.

### Alarm kuralları: değişmedi

Chart'ın PrometheusRule'u değişmedi; yeni metrikler için bir alarm eklemek
isterseniz README "Hesaplar ve giriş"teki örnek PromQL'i kendi Alertmanager
yönlendirmenize ekleyin.

## 0.48'e yükseltme

0.48 kapsamı sınırlı bir kullanıcının iç hedeflerini çözüm anında denetler
(iki etiketli kısa ad ve iç adrese çözülen genel adla kapsam aşımı kapanır),
kanal geri geldiğinde susturulmuş bir monitor'ün gece bildirimi göndermesini
önler ve kanal testi/kaydını duraklatmayı kaldırır, girişi hesap-başına sert
kilit yerine kaynak adres başına throttle eder ve chart'a node yerleşimi ile
frontend Service değerleri ekler. Şema değişmez (26).

### Yapılacak

- Giriş bir Ingress ya da yük dengeleyici arkasındaysa
  `UPTIME_TRUST_PROXY_HOPS`'u gerçek vekil sayısına ayarlayın: 0.48 giriş
  denemelerini kaynak adrese göre throttle eder ve adresi
  `X-Forwarded-For`'un sonundan bu sayı kadar vekili atlayarak okur.
  Ayarlanmazsa (varsayılan 0) tüm istekler tek bir vekil adresinden gelmiş
  sayılır ve throttle tek paylaşılan bir kilide geriler (0.47'nin
  hesap-başına kilidine yakın ama daha gevşek). Değeri chart'ta
  `backend.extraEnv` ya da values ile verin.

  **Somut hop sayısı:** 0.48 ve 0.49'da arayüz istemci adresini taşımadığı
  için arayüzden gelen girişte hiçbir değer kaynakları ayırmaz ("0.49'a
  yükseltme", bilinen sınır). Arayüz yoluna göre türetilmiş değerler 0.50'den
  itibaren README "Hesaplar ve giriş", "Vekil-hop rehberi"ndedir.

  **Görülen kaynağı doğrulayın.** 0.49'dan itibaren yanlış ayar sessizce etkisiz
  kalmaz: `X-Forwarded-For` geldiği hâlde `UPTIME_TRUST_PROXY_HOPS=0` iken ve
  zincir ayarlanan hop sayısından kısa geldiğinde backend loga dakikada en fazla
  bir kez WARN yazar ve bu satır throttle'ın anahtarladığı adresi (eş) adıyla
  verir. Girişi bir kez yanlış deneyip logdaki WARN satırındaki adresin
  beklediğiniz vekil ya da istemci adresi olduğunu doğrulayın; değilse hop
  sayısını düzeltin. 0.49'da hop sayısı gerçek zincirden uzunsa görülen kaynak,
  istemcinin uydurabileceği en soldaki girdiye değil, bağlanan eşe (peer) düşer:
  fazla ayar artık "başlık uydurulabilir" değil "tek paylaşılan kilit" maliyeti
  taşır (ADR-0004).
- Kapsamı sınırlı (yönetici olmayan, namespace listesi `*` olmayan)
  kullanıcılarınız varsa: 0.48 bir hedefin adı "dış" görünse de onu
  çözüm/bağlantı anında çözer ve çözülen adres cluster içindeyse ve
  namespace'i adından okunamıyorsa sınırlı kullanıcı için reddeder. Böylece
  `api.payments`, `kubernetes.default` gibi iki etiketli kısa adlar ve iç
  adrese çözülen genel adlar da kapsam dışıysa reddedilir. Var olan
  monitor'ler ve kanallar yeniden denetlenmez; yalnızca yeni hedef
  oluşturma, klonlama ve hedef değiştirme kısıtlanır.
- Hesap kilidine dayanan bir betiğiniz varsa gözden geçirin: 0.48 hesabı
  kilitlemek yerine kaynağı throttle eder; gevşek bir hesap tavanı
  (`UPTIME_LOGIN_ACCOUNT_MAX_FAILURES`, varsayılan 50, `0` kapatır) yalnız
  çok yüksek sayıda başarısız denemede devreye girer.

### Davranış değişikliği

- **Kullanıcı başına hedef kapsamı çözüm anında denetlenir.** Kapsamı
  sınırlı bir kullanıcı, adı "dış" görünse de iç adrese çözülen bir hedefe
  (iki etiketli kısa ad, iç IP'ye bakan genel ad) kapsamı dışındaysa monitor
  ya da kanal webhook'u kuramaz; çözümleme karar başına bir kez sabitlenir
  (TOCTOU). Yönetici ve kapsamı `*` olan kullanıcı ile cluster dışı hedefler
  değişmez. 403 gövdesine makine-okur bir `code` alanı eklendi; API revizyonu
  30. ADR-0003 (c)'nin kalan boşluğu kapandı. Sınır: adın çözümle gerçek
  bağlantı arasında değişmesi (DNS rebinding) API katmanında kapatılamaz;
  deny-CIDR derinlemesine savunma olarak kalır.
- **Giriş kaynak adrese göre throttle edilir.** 0.48 başarısız girişleri
  hesabı kilitlemek yerine kaynak adrese göre throttle eder (CWE-307): bilinen
  bir hesabı az istekle kilitleyip yöneticiyi dışarıda bırakma riski kalkar.
  429 yanıtı `Retry-After` taşır. `UPTIME_TRUST_PROXY_HOPS` ve
  `UPTIME_LOGIN_ACCOUNT_MAX_FAILURES` yukarıda "Yapılacak"ta; ADR-0004.
- **Susturulmuş monitor ve kanal devamı.** Yeniden başlatmadan sonra kanal
  geri geldiğinde bekleyen bir kesinti bildirimi artık bakım penceresini
  gözetir (susturulmuş ya da bakımdaki monitor gece bildirimi göndermez).
  Başarılı bir kanal testi ya da kanalı kaydetmek, duraklatılmış kanalın
  duraklatmasını kaldırır ve bekleyenleri sıradaki geçişte öne alır. Yeni
  `UPTIME_NOTIFY_HUMAN_RETRY_MAX` insanların okuduğu kanallara (Slack, Teams,
  e-posta, Telegram, Discord, Mattermost, Google Chat, ntfy, Gotify) daha kısa
  bir yeniden deneme üst sınırı tanır; webhook ve PagerDuty
  `UPTIME_NOTIFY_RETRY_MAX`'ta kalır. Varsayılanı `0` =
  `UPTIME_NOTIFY_RETRY_MAX`, yani ayarlanana dek davranış değişmez.
- **Chart: node yerleşimi ve frontend Service değerleri.** backend ve
  frontend için `nodeSelector`, `tolerations`, `priorityClassName` ve
  `frontend.service.type`/`.annotations` değerleri eklendi; hepsinin
  varsayılanı bugünkü davranışa eşittir (boş = ClusterIP, kısıtsız
  scheduling), mevcut kurulumlar değişmeden yükselir. docs GKE'nin kendi
  Ingress'iyle kurulumu, infra-node yerleşimini ve dolu veri diski için
  platformdan bağımsız bir alarm yolunu (`kubelet_volume_stats_available_bytes`)
  anlatır.

### Geri dönüş

Şema 26: `helm rollback` ile 0.47.0'a dönülür. Dönüşte kapsam denetimi yine
yalnız adı "iç" görünen hedeflere uygulanır (iki etiketli kısa ad/genel ad
boşluğu yeniden açılır), giriş yine hesap-başına kilitlenir
(`UPTIME_TRUST_PROXY_HOPS` ve `UPTIME_LOGIN_ACCOUNT_MAX_FAILURES` yok sayılır),
susturulmuş monitor'ün kanal dönüşündeki bildirimi yine bakımı gözetmez,
`UPTIME_NOTIFY_HUMAN_RETRY_MAX` ile chart'ın yeni yerleşim değerleri yok
sayılır (değerleri vermiş olmak zarar vermez).

### Alarm kuralları: değişmedi

Chart'ın PrometheusRule'u 0.47.0 ile aynıdır. Dolu veri diski için eklenen
platformdan bağımsız alarm bir chart kuralı değil, docs'taki bir reçetedir
(kendi Prometheus ya da Grafana Cloud kural deponuza eklersiniz).

## 0.47'ye yükseltme

0.47 kaldırmada yönetici, API ve metrik Secret'larını veri PVC'siyle
birlikte bırakır, başarısız bir kontrolün hatasındaki çözülen iç cluster
adresini gizler ve kapsamı sınırlı bir kullanıcının monitor ya da kanal
webhook'u kurabileceği iç hedefleri kendi namespace'leriyle sınırlar;
yayım ve Release değişmez olur, derleme girdileri digest'e sabitlenir.
Şema değişmez (26).

### Yapılacak

- Kapsamı sınırlı (yönetici olmayan, namespace listesi `*` olmayan)
  kullanıcılarınız varsa: 0.47'de bir kullanıcı yalnızca adından okunan
  namespace'i kapsamındaki iç cluster hedeflerine (ör.
  `servis.namespace.svc`) monitor ya da kanal webhook'u kurabilir. Var
  olan monitor'ler ve kanallar yeniden denetlenmez, çalışmaya devam eder;
  kısıt yalnızca yeni hedef oluşturma, klonlama ve hedef değiştirmede
  uygulanır. Bir kullanıcının kapsamı dışında, çıplak ClusterIP/pod IP'si
  ya da Kubeat'in kendi namespace'indeki kısa adla kurulmuş hedefleri
  varsa, o kullanıcı bunları artık yeniden oluşturamaz ya da hedeflerini
  değiştiremez (yönetici ve kapsamı `*` olan kullanıcı etkilenmez). Betik:
  "Kapsam dışı iç hedefler".
- Başarısız bir kontrolün hata metnindeki çözülen IP'ye bakan log
  alarmınız ya da panonuz varsa `diagnostics.address` alanına taşıyın:
  0.47'de hata metnindeki çözülen iç adres `[address hidden]` ile
  gizlenir (aşağıda "Davranış değişikliği"), ama başarısız kontrolün
  `diagnostics` ayrıntısındaki bağlanılan **adres** (`ip:port`) belgelenmiş
  bir tanı alanı olarak açık kalır.
- Felaket kurtarma (geri yükleme, node kaybı, dolu volume) için
  [docs/yedek-geri-yukleme.md](yedek-geri-yukleme.md) runbook'una bakın;
  riskli bir değişiklikten önce `UPTIME_BACKUP_INTERVAL`'ı düşürüp
  doğrulanmış bir kopya alın (RPO en kötü durumda bir aralık kadardır).

### Davranış değişikliği

- **Yönetici, API ve metrik Secret'ları kaldırmada kalır.**
  `backend.persistence.keepOnUninstall` açıkken (varsayılan `true`)
  `<release>-admin`, `<release>-api-token` ve `<release>-metrics-token`
  Secret'ları artık `helm.sh/resource-policy: keep` taşır: veri ve yedek
  PVC'leri gibi `helm uninstall`'da silinmez ve aynı adla yeniden
  kurulumda eski değerler (yönetici parolası, API ve metrik anahtarları)
  sahiplenilip korunur; kurulum notlarının silme komutu üçünü de sayar.
  `keepOnUninstall=false` ile üçü de veri PVC'siyle gider ve yeniden
  üretilir.
- **Başarısız kontrolün hatasında iç adres gizlenir.** Bir monitor
  kontrolünün sonucundaki hata metni (`result.Error`) çözülen IPv4/IPv6
  adreslerini `[address hidden]` ile gizler; bu hata bir monitor'ü
  okuyabilen herkese döner ve aksi hâlde bir iç cluster adresini
  sızdırırdı (0.46'nın kanal yolunda kapattığı sızıntının aynı sınıfı,
  farklı yüzeyde). Hata sınıfı ve host adı kalır; `netguard`'ın reddi
  yasakladığı adresi ve aralığı, DNS kontrolü bulduğu kayıtları ve
  başarısız kontrolün `diagnostics.address` tanı alanını bilerek açık
  bırakır.
- **Kapsamı sınırlı kullanıcının iç hedefleri kendi namespace'leriyle
  sınırlı.** Yukarıda "Yapılacak"ta. Monitor API'si ve kanal webhook'u
  yolunu (webhook, Slack, Teams, Discord, Mattermost, Google Chat, Gotify,
  ntfy) kapsar; e-posta, Telegram ve PagerDuty operatör uçları olduğu için
  kapsanmaz, cluster dışı hedefler de eskisi gibi yalnızca hedef
  politikasına (`UPTIME_TARGET_DENY_CIDRS`) tabidir.
- **Yayım ve Release değişmez, derleme girdileri sabit.** Ürün değişmez.
  `v*` etiketleri ve GitHub Release'leri taşınmaz ve silinemez (ruleset ve
  repo "Immutable releases" ayarı); iş akışlarındaki üçüncü taraf
  action'lar, Dockerfile temel imajları ve registry servis imajı digest'e
  pinlendi, iş akışı düzeyi `packages:write` yalnız iten işlere verildi.

### Geri dönüş

Şema 26: `helm rollback` ile 0.46.0'a dönülür. Dönüşte üç Secret'tan
`keep` annotation'ı kalkar ama değerler lookup'la korunur (Secret
silinmez); başarısız kontrolün hatasındaki iç adres yine görünür; kapsam
kısıtı kalkar (her kullanıcı yine hedef politikasının izin verdiği her iç
adrese monitor kurabilir). Yayımın değişmezliği ve digest pinleri repo ve
iş akışı ayarıdır, sürümle inip çıkmaz.

### Alarm kuralları: değişmedi

Chart'ın PrometheusRule'u 0.46.0 ile aynıdır; kopyalanan kuralların
yenilenmesi gerekmez.

## 0.46'ya yükseltme

0.46 bildirimlere kurulum kimliğini ekler, ulaşılamayan bir insan kanalının
makine kanallarını geciktirmesini bitirir ve yayımı CI'ı yeşil bir main
commit'ine bağlar. Yükseltme notları, 0.38'den bu yana olanlar dahil,
artık bu dosyadadır; README'de bağlantı kalır. Şema değişmez (26).

### Yapılacak

- Açık PagerDuty olaylarını yükseltmeden önce kapatın: 0.46 yeni olayları
  kimlikli bir `dedup_key` ile açar (aşağıda "Davranış değişikliği").
  Yükseltmeden önce açılmış bir olay sonradan hatırlatılır ya da yeniden
  gönderilirse PagerDuty'de kimlikli ikinci bir alarm açılır; düzelme
  ikisini de kapatır. Betik: "Açık kesintiler".
- Kuruluma bir kimlik verin: `instanceName` (`UPTIME_INSTANCE_NAME`). Boşsa
  `publicURL`'ün host'u, chart'ta o da yoksa Route ya da Ingress host'u
  kullanılır; hiçbiri yoksa kurulum kimliksizdir. Route ya da Ingress host'u
  olan kurulum values'a dokunmadan da 0.46'da kimlik alır ve PagerDuty
  anahtarı değişir: ilk madde ona da geçerlidir.
  Kimlik anahtara ASCII harf, rakam, nokta, tire ve alt çizgiyle girer;
  Türkçe ve öteki aksanlı Latin harfleri en yakın ASCII harfe çevrilir
  (`Üretim` → `Uretim`), öteki karakterlerin her dizisi tek tireye döner.
  Backend açılışta kullanılan kimliği ve anahtardaki hâlini tek bir `INFO`
  satırıyla loglar; anahtardaki hâl boşsa ya da kimlikten farklıysa (ör.
  host'ta port varsa) satır `WARN`'dır.
- Birden çok kurulumunuz aynı PagerDuty servisine bağlıysa ayrı servisleri
  filonun tamamı 0.46'ya geçip her kurulum kimlik alana kadar koruyun: 0.45
  ve öncesi de kimliksizdir, kimliksiz iki kurulumun aynı numaralı olayları
  yine tek alarmda birleşir. Bu cümle 0.43 notundaki "aynı servise
  bağlamayın" cümlesinin yerine geçer.
- `UPTIME_NOTIFY_RETRY_MAX`'ı 1 dakikadan ya da bir denemenin süresinden
  kısa verdiyseniz değeri alt sınıra yükseltin ya da `0` yapın: 0.46 böyle
  bir değerle açılır ama her açılışta `WARN` yazar ve 0.45'e dönüşte backend
  yine durur. Betik: "Bildirim ayarları".
- İmajları ve chart'ı aynalıyorsanız docs/pilot.md'deki imzalı yola geçin:
  `oras cp -r` imzaları, SBOM'ları ve provenance'ı kopyayla birlikte taşır
  ve aynalanan kopya `cosign verify --trusted-root` ile ağsız doğrulanır;
  imzasız `skopeo` yolu bir notta kaldı.

### Davranış değişikliği

- **Bildirimlerde kurulum kimliği.** PagerDuty `dedup_key`'i
  `kubeat-<kimlik>-incident-<id>` ve `kubeat-<kimlik>-slow-<monitor id>`
  olur. Backend'in açılışından önce açılmış bir olayın ya da yavaşlığın
  düzelmesi kimlikli ve eski anahtarla iki istek olarak gider. Webhook
  gövdesinde ve PagerDuty `custom_details`'te `instance` alanı gelir; kimlik
  Slack ve Mattermost alt bilgisinde, Teams ve Google Chat kartında ve
  e-posta gövdesinde görünür. E-posta konusu ve mesaj metinleri değişmez.
- **Ulaşılamayan insan kanalı ötekileri bekletmez.** Ulaşılamayan ya da
  meşgul bir insan kanalı (Slack, Teams, Mattermost, Google Chat, Telegram,
  Discord, ntfy, Gotify, e-posta) artık webhook ve PagerDuty gibi yeniden
  deneme zamanına kadar almıyor sayılır, `UPTIME_NOTIFY_GROUP_WAIT=0` iken
  de: yeni bildirimler ve namespace grupları ona denenmez, bekletilir ve
  kanal gelince bekleyenler kanalın sıradaki geçişinde gider. Reddeden
  (4xx) kanal yine duraklamaz; `UPTIME_NOTIFY_RETRY_MAX=0` ile eskisi gibi
  hiçbir kanal duraklamaz. 0.45'te insanların okuduğu kanal almıyor
  sayılmıyordu: meşgul (429) kanalda yalnızca o mesaj bekliyor, öteki
  mesajlar anında deneniyordu; birden çok namespace'e yayılan bir kesintide
  ulaşılamayan bir e-posta relay'i ya da Slack, teslimat döngüsünü grup
  başına bir deneme süresi (varsayılanlarla yaklaşık 25 sn) tutuyor ve bu
  arada açılan kesintilerin PagerDuty ve webhook sayfalarını o kadar
  geciktiriyordu. 0.46'da sayfa, kaç namespace düşmüş olursa olsun, en çok
  bir deneme süresi bekler. Bedeli: kanal döndükten sonra ona giden yeni bir
  mesaj en çok o anki bekleme süresi kadar gecikir (1, 2, 4… dk; üst sınır
  `UPTIME_NOTIFY_RETRY_MAX`, varsayılan 15 dk). Kanalı düzenlemek ya da
  kanal testi bekletmeyi kaldırmaz (test kuyruğu atlar); düzeltilmiş ayar
  sıradaki geçişte kullanılır.
- **Kuyruk gauge'u.** Yeni `kubeat_notification_queue_length` teslimat
  kuyruğunda o an bekleyen bildirim sayısını gösterir; boşta 0'dır.
- **Kısa `UPTIME_NOTIFY_RETRY_MAX` backend'i durdurmaz.** 1 dakikadan ya da
  bir denemenin süresinden kısa bir değer alt sınıra yükseltilir ve
  açılışta verilen ve kullanılan değerle bir `WARN` satırı yazılır.
  0.43–0.45 böyle bir değerle açılmıyordu.
- **Kurulum notları.** Davranış değişmez; `helm upgrade` notu heartbeat
  için iki kopyalanabilir satır verir, PrometheusRule CRD'si yoksa (`helm
  template`'te `--api-versions` verilmemişse de) kural seçeneğini önermez
  ve README'nin operator'süz yoluna yollar, kurallar kapalıyken
  heartbeat'in bildirim alamayan kanalı duymadığını söyler ve API
  anahtarıyla backend port-forward satırlarını gösterir.
- **Yayım kapısı.** Ürün değişmez; `v*` etiketinden yayım yalnızca
  main'den ya da bir `release/v*` dalından erişilebilen ve CI'ın push
  koşusunda beş zorunlu check'i yeşil olan bir commit'ten çıkar (CI
  sürüyorsa kapı en çok 30 dakika bekler; düşerse hiçbir şey itilmemiştir).
  GitHub Release metni sürümün bu dosyadaki bölümüne bağlanır.

### Geri dönüş

Şema 26: `helm rollback` ile 0.45.0'a dönülür. Önce açık PagerDuty
olaylarını kapatın: 0.46'nın kimlikli anahtarıyla açılmış bir olay 0.45'in
anahtarıyla kapanmaz. Dönüşte `instanceName` yok sayılır, webhook
gövdesinden `instance` alanı ve kuyruk gauge'u kalkar, ulaşılamayan bir
insan kanalı yine her yeni grupta denenir ve 0.46'nın `WARN`'la kabul
ettiği kısa bir `UPTIME_NOTIFY_RETRY_MAX` backend'i yine durdurur (dönmeden
önce değeri alt sınıra yükseltin ya da `0` yapın). Kimliği ya da
`publicURL`'ü sonradan değiştirmek de PagerDuty anahtarını değiştirir: önce
açık olayları kapatın.

### Alarm kuralları: değişmedi

Chart'ın PrometheusRule'u 0.45.0 ile aynıdır (13 kural); kopyalanan
kuralların yenilenmesi gerekmez. Yeni `kubeat_notification_queue_length`
gauge'u kurala bağlı değildir.

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
  Her kuruluma ayrı bir servis verin (0.46'dan itibaren kurulum kimliği bu
  birleşmeyi önler, "0.46'ya yükseltme").
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
