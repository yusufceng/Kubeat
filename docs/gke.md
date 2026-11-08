# GKE'ye kurulum: values-gke.yaml

GKE'nin kendi Ingress'iyle (GCE) kopyalanabilir bir kurulum.
[README](../README.md#clustera-kurulum) "Cluster'a kurulum" → "GKE" bölümünün
değerleri tek dosyada:

- container-native yük dengeleme (NEG) ile GCE Ingress;
- Google'ın yönettiği sertifika (`ManagedCertificate`), TLS yük dengeleyicide
  biter;
- giriş throttle'ı için hop `2` (istemci ve yük dengeleyicinin adresi);
- arayüzün ağ politikası, kaynak olarak yük dengeleyicinin iki aralığı.

**Ölçülmedi.** Değerler GKE'nin belgelenmiş davranışından türetildi; gerçek bir
GKE'de kurulmadı (prova kullanıcıda). CI'da sınanan: `deploy/helm/test-chart.sh`
aşağıdaki bloğu bu belgeden olduğu gibi çıkarıp Helm 3 ve Helm 4 ile render
eder. Kurulum notunda hop, TLS ya da arayüz politikası uyarısı çıkmadığını ve
README "Kurulum notları görünmez"deki CI tarifinin bu değerlerle sıfır
`WARNING` saydığını sınar.

## 1. Önce

1. Alan adının yük dengeleyicinin adresine çözülmesi. ManagedCertificate
   ancak alan adı yük dengeleyicinin IP'sine çözülünce `Active` olur. Adres
   Ingress yeniden oluşturulunca değişmesin diye global bir statik IP ayırıp
   DNS'te `kubeat.example.com`'un A kaydını ona verin (önerilir):

   ```
   gcloud compute addresses create kubeat-ip --global
   gcloud compute addresses describe kubeat-ip --global --format='value(address)'
   ```

   Sonra values dosyasında `kubernetes.io/ingress.global-static-ip-name`
   satırını açın.

2. Namespace ve ManagedCertificate. Chart ManagedCertificate üretmez:

   ```yaml
   apiVersion: networking.gke.io/v1
   kind: ManagedCertificate
   metadata:
     name: kubeat
     namespace: kubeat
   spec:
     domains:
       - kubeat.example.com
   ```

   ```
   kubectl create namespace kubeat
   kubectl apply -f managedcertificate.yaml
   ```

3. Heartbeat. Kubeat'in kendisi düşerse cluster dışındaki bir alıcı haber
   verir (README "Aracın kendi sağlığı"). GKE'de PrometheusRule CRD'si
   çoğunlukla yoktur; kuralları Grafana Cloud ya da Mimir'de çalıştırmak için
   README "Operator'süz cluster".

   ```
   kubectl -n kubeat create secret generic kubeat-heartbeat --from-literal=url=https://<heartbeat-adresi>
   ```

4. Ağ politikasının uygulanması. Cluster NetworkPolicy uygulamıyorsa arayüzün
   politikası sessizce etkisiz kalır; Dataplane V2 ya da ağ politikası
   eklentisi gerekir (README "Hesaplar ve giriş" → "Politikanın uygulandığını
   doğrulamak"):

   ```
   gcloud container clusters describe <cluster> --location <konum> --format='value(networkConfig.datapathProvider,networkPolicy.enabled)'
   ```

## 2. values-gke.yaml

```yaml
# GKE'nin kendi Ingress'i (GCE), NEG ve ManagedCertificate ile.
# README "Cluster'a kurulum" -> "GKE"; ölçülmedi.
podSecurityContext:
  fsGroup: 1001                 # imajlar 1001 ile çalışır, volume'un kökü root'a ait olabilir
publicURL: https://kubeat.example.com   # TLS yük dengeleyicide biter; chart http:// türetmesin
ingress:
  enabled: true
  host: kubeat.example.com
  annotations:
    kubernetes.io/ingress.class: gce                  # GCE ingressClassName'e bakmaz
    networking.gke.io/managed-certificates: kubeat    # 1. adımdaki ManagedCertificate
    # kubernetes.io/ingress.global-static-ip-name: kubeat-ip   # önerilir, 1. adım
  tls:
    enabled: false              # sertifika ManagedCertificate'te; Ingress TLS Secret'ı istemesin
frontend:
  service:
    annotations:
      cloud.google.com/neg: '{"ingress": true}'       # container-native (NEG)
  networkPolicy:
    enabled: true
    from:                       # yük dengeleyicinin ve sağlık kontrollerinin aralıkları
      - ipBlock:
          cidr: 35.191.0.0/16
      - ipBlock:
          cidr: 130.211.0.0/22
backend:
  extraEnv:
    UPTIME_TRUST_PROXY_HOPS: "2"   # X-Forwarded-For: istemci, yük dengeleyicinin adresi
heartbeat:
  urlSecret:
    name: kubeat-heartbeat      # 1. adımdaki Secret
backup:
  enabled: true
```

Değerlerin gerekçesi README'de:

- sınıfın annotation'la verilmesi, NEG ve ManagedCertificate'in üç değeri:
  "Cluster'a kurulum" → "GKE";
- hop `2`: "Hesaplar ve giriş" → "Vekil-hop rehberi";
- `from`'daki iki `ipBlock`: "Cluster'a kurulum" → "GKE kontrol listesi".
  Frontend Service'ini NEG yerine `NodePort` yaparsanız kaynak node olabilir,
  node'ların aralığını da ekleyin.

## 3. Kurulum

Sürüm README "Hızlı başlangıç"taki komutunkidir:

```
helm upgrade --install kubeat oci://ghcr.io/yusufceng/charts/kubeat --version <sürüm> \
  -n kubeat -f values-gke.yaml
```

Kurulum notunda beklenenler:

- "TLS ends at GKE's load balancer" satırı;
- arayüz politikasının uygulandığını doğrulama isteği
  (`frontend.networkPolicy is on: check that the cluster enforces it`);
- hop, TLS ya da arayüz politikası için `WARNING` yok.

Values'u değiştirdiğinizde çelişkiyi kurulum notu söyler: sınıf `gce` iken hop
`2` değilse, ManagedCertificate varken `ingress.tls.enabled` açıksa ya da
`from` iki aralığı birden içermiyorsa bir `WARNING` satırı çıkar.

## 4. Kurulumdan sonra

1. Sertifika: `kubectl -n kubeat describe managedcertificate kubeat`.
   `Certificate Status: Active` olana kadar (alan adı doğruysa genelde 15–60
   dakika) tarayıcı sertifika hatası görür.
2. Yük dengeleyicinin arka ucu:
   `kubectl -n kubeat describe ingress kubeat`. `ingress.kubernetes.io/backends`
   annotation'ında arka uçlar `HEALTHY` olmalı. `UNHEALTHY` kalıyorsa
   politikanın `from`'u ya da hazırlık probe'u (`/login`) yük dengeleyicinin
   sağlık kontrolünü geçirmiyordur.
3. Giriş throttle'ı ve arayüz politikası: README "Cluster'a kurulum" → "GKE
   kontrol listesi" (görülen kaynak, uydurma başlıkla negatif adım, başka bir
   namespace'ten zaman aşımı).
4. Bağlantı yükü: özel cluster'da Cloud NAT ve DNS, README "Cluster'a kurulum"
   → "Bağlantı yükü (GKE)".
