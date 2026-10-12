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
