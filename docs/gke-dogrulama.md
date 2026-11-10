# Gerçek GKE'de doğrulama

**Durum: ölçülmedi, prova kullanıcıda.** Kubeat'in GKE yolu
([gke.md](gke.md), `values-gke.yaml`) CI'da render edilir; gerçek bir GKE'de
kurulmadı. Bu belge o kurulumda neyin nasıl doğrulanacağını ve sonucun nasıl
geri bildirileceğini anlatır; [openshift-dogrulama.md](openshift-dogrulama.md)
deseniyle yazıldı. Koşulup geri bildirimi gelmeden 1.0 metni GKE'yi
"destekleniyor" değil "ölçülmedi, prova kullanıcıda" diye yazar
([yol haritası](yol-haritasi.md), 32. adım). Hepsi bir öğleden sonraya sığar;
adım 6 ve 7'nin ölçümü onar dakika bekler.

## Ön koşullar

- Bir GKE cluster'ı (Standard ya da Autopilot), `kubectl`, `gcloud` ve Helm
  3.14+ ya da Helm 4. Cluster'da namespace, NetworkPolicy ve
  `kube-system`'deki pod'ları okuma yetkisi (adım 7).
- NetworkPolicy'yi uygulayan bir ağ: Dataplane V2 ya da ağ politikası eklentisi
  ([gke.md](gke.md) "1. Önce", 4. madde). Uygulamıyorsa adım 2 geçmez.
- [gke.md](gke.md) "1. Önce"nin üç adımı: global statik IP ve DNS'te A kaydı
  (`kubeat.example.com` sizin alan adınızdır), ManagedCertificate, heartbeat
  Secret'ı.
- Grafana Cloud (ya da Mimir):
  - Alloy Kubeat'in metriklerini toplar (README "Aracın kendi sağlığı" →
    "Operator'süz cluster" → "Elle scrape"); job `kubeat-backend`, `namespace`
    etiketi `kubeat`.
  - Kubeat'in kuralları depodadır: ya PrometheusRule CRD'si ve Alloy
    `mimir.rules.kubernetes` (kurulum komutuna
    `--set metrics.prometheusRule.enabled=true` eklenir) ya da `mimirtool`
    kopyası (README "Operator'süz cluster" → "Kopyalamak").
  - Alarmlar bir alıcıya düşer (Grafana Cloud Alerting ya da Alertmanager).
- Adım 6 için: node'larının dış adresi olmayan (özel) ve internete Cloud NAT'tan
  çıkan bir cluster. Değilse adım 6 "geçerli değil"dir.
- Adım 5 için: arayüz `ingress-nginx` ile açılıyorsa. Değilse adım 5 "geçerli
  değil"dir.
- Adım 6–8 için yoklamanıza izin verilen bir dış HTTPS adresi (`<dış-hedef>`,
  ör. kendi servisinizin sağlık yolu). Yirmi monitor ona 30 sn aralıkla dakikada
  40 istek gönderir; Kubeat'in host başına tavanı dakikada 60'tır (README
  "Hedef yükü").

**Bağlam.** Bu belgedeki `kubectl` ve `helm` komutları `--context` olmadan
yazıldı ve geçerli bağlamda çalışır. Kubeconfig'inizde başka cluster'lar da
varsa her komuta doğrulanacak cluster'ın bağlamını verin (`kubectl --context
<bağlam>`, `helm --kube-context <bağlam>`; `gcloud` için `--project <proje>`)
ya da başlamadan `kubectl config current-context` çıktısının o cluster olduğunu
görün.

## Kurulum

`values-gke.yaml` [gke.md](gke.md) "2. values-gke.yaml"deki bloktur; alan adını
kendi adınızla değiştirin:

```
helm upgrade --install kubeat oci://ghcr.io/yusufceng/charts/kubeat --version 0.52.0 \
  -n kubeat -f values-gke.yaml
kubectl -n kubeat rollout status deploy/kubeat-backend
kubectl -n kubeat logs deploy/kubeat-backend | grep "public URL's addresses"
```

Beklenen:

- Kurulum notunda "TLS ends at GKE's load balancer" satırı; hop, TLS ya da
  arayüz politikası için `WARNING` yok ([gke.md](gke.md) "3. Kurulum").
- Son komut `throttle: a source at one of the public URL's addresses will be
  taken for hops too low` satırını ve statik IP'nizi yazar. Satır
  `no check of the hops against the public URL's addresses` diyorsa alan adı
  henüz çözülmüyor ya da CNAME'dir: adım 4'ün sinyali susar.

## Adımlar

Her adımda komut ve beklenen çıktı var. Olmayanı, adımın numarasıyla geri
bildirime yazın.

### 1. ManagedCertificate ve https

```
kubectl -n kubeat get managedcertificate kubeat -o jsonpath='{.status.certificateStatus}{"\n"}'
kubectl -n kubeat describe ingress kubeat | grep 'ingress.kubernetes.io/backends'
curl -sS -o /dev/null -w '%{http_code} %{ssl_verify_result}\n' https://kubeat.example.com/login
curl -sS -o /dev/null -w '%{http_code}\n' http://kubeat.example.com/login
```

Beklenen:

- `Active` (alan adı doğruysa genelde 15–60 dakikada; o zamana kadar
  `Provisioning`).
- Arka uçlar `HEALTHY`. `UNHEALTHY` kalıyorsa yük dengeleyicinin sağlık kontrolü
  arayüze ulaşamıyor: adım 2'deki `from` ya da hazırlık probe'u (`/login`).
- Üçüncü komut `200 0`: giriş sayfası, sertifika doğrulandı.
- Dördüncü komut `200` ise 80 açıktır ve giriş formu düz HTTP'den de gelir;
  `kubernetes.io/ingress.allow-http: "false"` ya da FrontendConfig yönlendirmesi
  verildiyse `404` ya da `301` ([gke.md](gke.md) "4. Kurulumdan sonra", 5.
  madde). Hangisini gördüğünüzü yazın.

### 2. Arayüz politikası: yük dengeleyicinin aralıkları

```
gcloud container clusters describe <cluster> --location <konum> --format='value(networkConfig.datapathProvider,networkPolicy.enabled)'
kubectl -n kubeat get networkpolicy -o name
```

Sonra README "Hesaplar ve giriş" → "Politikanın uygulandığını doğrulamak"taki
negatif adım: `from`'da olmayan geçici bir namespace'teki pod'dan arayüz
Service'ine istek (curl imajı README'de sabitlidir):

```
kubectl create namespace kubeat-np-check
kubectl -n kubeat-np-check run np-check --rm -i --restart=Never --image=<README'deki curl imajı> -- \
  curl -sS -m 5 -o /dev/null -w '%{http_code}\n' http://kubeat-frontend.kubeat.svc:3000/login
kubectl delete namespace kubeat-np-check
```

Beklenen:

- İlk komutta `ADVANCED_DATAPATH` ya da ikinci sütunda `True`.
- Listede `networkpolicy.networking.k8s.io/kubeat-frontend`.
- Negatif adım `curl: (28)` ile biten bir zaman aşımı; `200` dönüyorsa
  politika uygulanmıyor.
- Aynı anda tarayıcıda `https://kubeat.example.com` giriş sayfasını açıyor ve
  adım 1'deki arka uçlar `HEALTHY`: yük dengeleyicinin iki aralığı
  (`35.191.0.0/16`, `130.211.0.0/22`) geçiyor, öteki her şey kapalı.

### 3. Hop `2`: görülen kaynak ve uydurma `X-Forwarded-For`

Tarayıcınızdan `admin`'e bir kez yanlış parola girin. Sonra geliştirici
araçlarında (Network) aynı isteği "Copy as cURL" ile alın, komuta
`-H 'X-Forwarded-For: 192.0.2.99'` ekleyip aynı makineden çalıştırın:

```
kubectl -n kubeat logs deploy/kubeat-backend --since=15m | grep 'sign-in refused'
```

Beklenen:

- İki satırın `source`'u da tarayıcınızın internete çıktığı adres
  (`curl -s https://ifconfig.me` gibi bir servisle karşılaştırın).
- Uydurma başlıklı satırda `192.0.2.99` **değil**. `192.0.2.99` ise istemci
  kaynağını seçebiliyor: değer fazla (README "Uydurma başlıkla negatif adım").
- `source` statik IP'nizse değer eksik: adım 4'ün durumu.

### 4. Hop `1`: `hops_too_low` ve Grafana Cloud'da `KubeatSignInSourceDegraded`

Hop'u geçici olarak `1` yapın, tarayıcıdan bir yanlış parola daha girin:

```
helm upgrade kubeat oci://ghcr.io/yusufceng/charts/kubeat --version 0.52.0 \
  -n kubeat -f values-gke.yaml --set-string backend.extraEnv.UPTIME_TRUST_PROXY_HOPS=1
kubectl -n kubeat logs deploy/kubeat-backend --since=15m | grep -E 'sign-in refused|is lower than needed'
```

Grafana Cloud'da (Explore) ve Alerting'de:

```
sum by (reason) (increase(kubeat_signin_source_degraded_total{namespace="kubeat"}[15m]))
```

Bitince aynı `helm upgrade`'i `--set-string` satırı olmadan koşup hop'u `2`'ye
geri alın.

Beklenen:

- `sign-in refused` satırının `source`'u statik IP'niz (yük dengeleyicinin
  kendi eklediği girdi).
- Dakikada en çok bir `WARN`: `UPTIME_TRUST_PROXY_HOPS=1 is lower than
  needed`, adres statik IP'niz.
- Sorgu `reason="hops_too_low"` için 0'dan büyük; `KubeatSignInSourceDegraded`
  `reason="hops_too_low"` ile yanar (kuralda `for` yok; yeni pod ilk kez scrape
  edildikten sonraki ilk girişte) ve alıcınıza düşer.
- Hop `2`'ye dönünce son sayılan girişten 15 dakika sonra çözülür.
- Kurulumun başındaki satır `no check of the hops` dediyse sinyal susar: `WARN`
  yok, sorgu 0. Bunu ayrıca yazın.

### 5. `ingress-nginx` yolu: controller Service'inin `externalTrafficPolicy`'si

Yalnız arayüz `ingress-nginx` ile açılıyorsa (README "Cluster'a kurulum" →
"GKE kontrol listesi"; hop `1`). Namespace ve Service adı kurulumunuzunkidir:

```
kubectl -n ingress-nginx get svc ingress-nginx-controller -o jsonpath='{.spec.externalTrafficPolicy}{"\n"}'
kubectl -n kubeat logs deploy/kubeat-backend --since=15m | grep 'sign-in refused'
```

Beklenen:

- `Local`. Yük dengeleyici istemcinin adresini korur, bir yanlış parolanın
  `source`'u tarayıcınızın adresidir.
- `Cluster` ise `source` bir node'un iç adresidir ve sinyal susar: node'un
  adresi Kubeat'in genel adresi olmadığından `hops_too_low` sayılmaz, `WARN` ve
  kural yoktur. Throttle bütün tarayıcılar için o node'ların kilidine düşer;
  değeri artırmayın, Service'i `Local` yapın.

### 6. En az 20 monitor'le Cloud NAT

0.52'den beri her HTTP kontrolü kendi bağlantısını açar; kapanan bağlantının
portu Cloud NAT'ta `tcp_time_wait_timeout_sec` (varsayılan 120 sn) boyunca aynı
hedefe yeniden verilmez (README "Cluster'a kurulum" → "Bağlantı yükü (GKE)").
Önce NAT'ın ayarlarını okuyun:

```
gcloud compute routers nats describe <nat> --router <router> --region <bölge> \
  --format='value(minPortsPerVm,enableDynamicPortAllocation,tcpTimeWaitTimeoutSec)'
```

Sonra aynı dış adrese giden 20 HTTPS monitor'ü API ile kurun (README "API";
anahtar `kubeat-api-token` Secret'ındadır, yazdırmayın):

```
kubectl -n kubeat port-forward svc/kubeat-backend 8090:8080 &
TOKEN=$(kubectl -n kubeat get secret kubeat-api-token -o jsonpath='{.data.token}' | base64 -d)
for i in $(seq -w 1 20); do
  curl -sS -o /dev/null -w '%{http_code}\n' -X POST -H "Authorization: Bearer $TOKEN" \
    localhost:8090/api/v1/monitors -d "{\"name\": \"nat-$i\", \"type\": \"http\",
    \"target\": \"https://<dış-hedef>/?kubeat=$i\", \"interval_seconds\": 30, \"timeout_seconds\": 10}"
done
```

On dakika bekleyin. Cloud Monitoring → Metrics Explorer'da backend pod'unun
node'u için `router.googleapis.com/nat/port_usage` ve
`router.googleapis.com/nat/dropped_sent_packets_count` (`reason`'a göre).

Beklenen:

- API her monitor için `201`.
- `port_usage` yaklaşık `20 × 120 / 30 = 80` ve node'un öteki iş yüklerinin
  aynı hedefe kullandığı portlar (kaba hesap; ölçülmedi).
- Hesaba göre varsayılan NAT'ta (`minPortsPerVm` 64, dinamik ayırma kapalı) bu
  sayı 64'ü aşar: `dropped_sent_packets_count` `reason="OUT_OF_RESOURCES"` ile 0'dan
  büyük ve bazı monitor'ler bağlantı zaman aşımıyla DOWN. Dinamik ayırma açıkken
  ya da `minPortsPerVm` 1024 iken düşme 0, monitor'lerin hepsi UP.
- Hangisini gördüğünüzü, `port_usage`'ın en yüksek değeriyle yazın. Ölçümden
  sonra 20 monitor'ü silebilirsiniz; adım 7 ve 8 onları kullanır.

### 7. Kube-dns ya da Cloud DNS sorgu hızı: önce ve sonra

0.52'den beri her kontrol hedefin adını yeniden çözer; pod'un `ndots:5`'i
yüzünden noktası beşten az bir dış ad önce arama alanlarıyla denenir. Önce
backend'in çözücüsünü okuyun:

```
kubectl -n kubeat exec deploy/kubeat-backend -- cat /etc/resolv.conf
```

Kube-dns'te sorgu sayacını iki kez, beş dakika arayla okuyun. Önce adım 6'nın
monitor'leri yokken (ya da Kubeat 0.51'deyken, yükseltmeden önce), sonra 20
monitor varken (ya da yükseltmeden sonra):

```
for p in $(kubectl -n kube-system get pods -l k8s-app=kube-dns -o name); do
  kubectl get --raw "/api/v1/namespaces/kube-system/pods/${p#pod/}:10054/proxy/metrics" |
    grep -E '^kubedns_dnsmasq_(hits|misses) '
done
kubectl -n kube-system top pods -l k8s-app=kube-dns
```

Hız, iki okumadaki toplamların farkının 300'e bölümüdür (sorgu/sn). Seri adı
kube-dns sürümüne göre değişebilir; çıktı boşsa `grep -i dnsmasq` ile bakıp
adını yazın. NodeLocal DNSCache açıksa sorguları önce node'daki önbellek karşılar;
onun sayacını (`node-local-dns` pod'larının 9253 portu,
`coredns_dns_requests_total`) okuyun. Cloud DNS for GKE'de kube-dns yoktur:
Cloud Monitoring'de Cloud DNS'in sorgu sayısını
(`dns.googleapis.com/query/response_count`) aynı iki aralıkta okuyun.

Beklenen:

- `resolv.conf`'ta `options ndots:5` ve arama alanları (GKE'de çoğunlukla
  altı).
- İki ölçüm arasındaki artış yaklaşık `N × (arama alanı + 1) × 2 / A`: altı
  arama alanı, 20 monitor ve 30 sn'de ~9 sorgu/sn (README "Bağlantı yükü
  (GKE)", kaba hesap; ölçülmedi).
- Kube-dns pod'larının CPU'su limitine yaklaşmıyor. Yaklaşıyorsa adım 10'u
  hemen koşun.

### 8. `kubeat_checks_in_flight` ve `KubeatWorkersSaturated`

Adım 6'nın 20 monitor'ü çalışırken Grafana Cloud'da:

```
max_over_time(kubeat_checks_in_flight{namespace="kubeat"}[10m])
increase(kubeat_scheduler_dispatch_skipped_total{namespace="kubeat"}[10m])
```

Alerting'de `KubeatWorkersSaturated`'a bakın.

Beklenen:

- `kubeat_checks_in_flight`'ın en yükseği `UPTIME_WORKERS`'ın (varsayılan 20)
  çok altında: 20 monitor'le birkaç kontrol. Ortalama dolu worker kabaca
  "monitor sayısı × ortalama kontrol süresi / aralık"tır
  ([yuk-testi.md](yuk-testi.md) "HTTP kontrolünün bağlantıları (0.52)").
- `kubeat_scheduler_dispatch_skipped_total` artmıyor, `KubeatWorkersSaturated`
  yanmıyor.
- NAT portları taştıysa (adım 6) kontroller zaman aşımına kadar sürer ve
  `kubeat_checks_in_flight` yükselir; bunu ayrıca yazın. Kural 15 dakika boyunca
  sırası gelen kontroller boş worker bulamazsa yanar; çare `UPTIME_WORKERS` ya
  da NAT'ın portlarıdır.

### 9. `helm upgrade` sırasında rozetin 502 süresi

Backend tek replica ve `Recreate`'tir; arayüzde `preStop` yoktur ve GCE NEG
arkasında yük dengeleyici biten pod'u uç noktalardan biraz geç çıkarabilir.
Bir monitor'ü yayındaki bir durum sayfasına koyun (README "Durum sayfaları"),
rozetini saniyede bir yoklayın:

```
while true; do
  printf '%s %s\n' "$(date +%T)" "$(curl -s -o /dev/null -m 5 -w '%{http_code}' https://kubeat.example.com/api/badge/<monitor-id>/status)"
  sleep 1
done | tee rozet.log
```

Başka bir terminalde bir önceki sürümden bu sürüme `helm upgrade` yapın (aynı
sürümdeyseniz `kubectl -n kubeat rollout restart deploy/kubeat-backend
deploy/kubeat-frontend`). Yoklamayı yükseltme bitip rozet `200`'e döndükten bir
dakika sonra durdurun:

```
grep -vc ' 200$' rozet.log
grep -v ' 200$' rozet.log | sed -n '1p;$p'
```

Beklenen:

- `200` olmayan satır sayısı ve ilk ile son zamanı. Sandbox Route'unda bir
  yükseltmede kötü pencere ~25 sn ölçüldü (backend ve arayüz, 2026-11-08; README
  "Durum sayfaları" → "Sınırlar").
- GCE NEG arkasında ayrıca yük dengeleyicinin `502`'si: arayüz pod'u
  değişirken pencere uzayabilir. `502` dışındaki kodları (ör. `503`, `000`)
  ayrı yazın.

### 10. Kube-dns'te `resolve_error`

Kapsama bağlı kontrollerde (kapsamı sınırlı kullanıcının ve annotation
monitor'lerinin) kapsam, kontrolün açtığı bağlantıların adını backend'in
çözücüsüyle çözer; çözülemeyen ad güvenli tarafta reddedilir ve `reason="resolve_error"` ile sayılır (README
"Hedef politikası" → "Egress ve DNS"). Kube-dns yük altında SERVFAIL ya da zaman
aşımı verirse bu sayaç artar. Adım 6–8 sürerken:

```
kubectl -n kubeat exec deploy/kubeat-backend -- server resolve <dış-hedef-host> yok.example.invalid
```

Grafana Cloud'da:

```
sum by (reason, source) (increase(kubeat_target_scope_denied_total{namespace="kubeat"}[1h]))
```

Beklenen:

- `<dış-hedef-host>: resolves to …, outside the cluster: in scope for anyone`
  ve `yok.example.invalid: NXDOMAIN, no such name for this pod's resolver`.
- `did not resolve (… server misbehaving)` ya da `no answer within 2s`
  görürseniz kube-dns dış adlara SERVFAIL ya da zaman aşımı veriyor: komutu
  birkaç kez yineleyin, adım 7'deki CPU'yla birlikte yazın.
- Sorguda `reason="resolve_error"` satırı yok ya da 0. Varsa monitor'leri
  `blocked_by_scope` ile DOWN olmuştur; hangi `source`'ta olduğunu yazın.
  0.53'te kurallar açıksa annotation monitor'lerinde bu durum
  `KubeatScopeResolveErrors` ile de duyurulur.

## Geri bildirim

Şablonu doldurup GitHub'da bir issue'yla iletin. Depo herkese açıktır: alan
adını, IP adreslerini, proje, cluster ve router adlarını maskeleyin; token,
parola ve anahtar yazmayın.

```
GKE sürümü; Standard ya da Autopilot; zonal ya da regional:
Ağ: Dataplane V2 / ağ politikası eklentisi; özel cluster ve Cloud NAT var mı:
DNS: kube-dns / NodeLocal DNSCache / Cloud DNS for GKE; arama alanı sayısı:
Arayüzün yolu: GCE Ingress (NEG) / ingress-nginx / GKE Gateway; hop değeri:
Kurallar: CRD + mimir.rules.kubernetes / mimirtool kopyası / yok:

| Adım | Sonuç (geçti / kaldı / koşulmadı / geçerli değil) | Ölçülen | Not |
| --- | --- | --- | --- |
| 1 ManagedCertificate ve https | | durum, http'nin kodu | |
| 2 Arayüz politikası | | negatif adımın sonucu | |
| 3 Hop 2 ve uydurma XFF | | iki satırın source'u (maskeli) | |
| 4 Hop 1, hops_too_low ve kural | | WARN, sorgu, alarm | |
| 5 ingress-nginx externalTrafficPolicy | | Local / Cluster | |
| 6 Cloud NAT | | NAT ayarları, en yüksek port_usage, düşen paket | |
| 7 DNS sorgu hızı | | önce ve sonra sorgu/sn, CPU | |
| 8 checks_in_flight ve WorkersSaturated | | en yüksek değer, alarm | |
| 9 Rozetin 502 süresi | | 200 olmayan satır, süre | |
| 10 resolve_error | | server resolve satırları, sayaç | |
```

Ve şu çıktılar:

```
kubectl version
helm list -n kubeat
kubectl -n kubeat get pods -o wide
kubectl -n kubeat logs deploy/kubeat-backend --since=1h | grep -E 'level":"(WARN|ERROR)"'
kubectl -n kubeat get ingress,managedcertificate,networkpolicy
```
