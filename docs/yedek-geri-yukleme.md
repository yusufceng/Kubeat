# Yedekten geri yükleme ve felaketten dönüş: runbook

Öz-sağlık (self-health) disk doluluğunu değil kaybolan sonuçları ölçer:
SQLite'ın WAL dosyası var olan alanı yeniden kullandığından dolu bir veri
volume'unda yazımlar bir süre düşmez, seyrek (az monitor'lü) bir kurulumda
dolu disk uzun süre sessiz kalabilir; bu yüzden PVC doluluğunu izlemeyi
platform alarmına bırakın (platformdan bağımsız kural için aşağıda "Dolu veri
volume'undan dönüş").

Bu belge yedek kopyadan geri yüklemeyi, node kaybından ve dolu volume'dan
dönüşü adım adım anlatır; ayrıca ne kadar veri kaybedilebileceğini (RPO),
geri dönüşün ne kadar sürdüğünü (RTO) ve bunun nasıl denendiğini kaydeder.
Yeni bir mekanizma getirmez: adımlar chart'ın bugünkü `Recreate` stratejisini
ve doğrulanmış-yedek yolunu (aşağıda "Yedek kopyaları cluster dışına almak")
kullanır. Komutlar release adının `kubeat` olduğunu varsayar; değilse
`kubeat-data`, `kubeat-backup` ve `kubeat-secret-key` adlarındaki ön eki
(dışarı alma bloğunda `RELEASE`'i) değiştirin. Yedeklemenin nasıl çalıştığı
ve kaldırmaya karşı korumanın ayarları README "Yedekleme ve geri
yükleme"dedir.

## Hedefler

| | Değer | Neye bağlı |
|---|---|---|
| **RPO** (kaybedilebilecek en uzun süre) | `UPTIME_BACKUP_INTERVAL`, varsayılan 24 saat | Kopya bu aralıkla alınır; son kopyadan sonraki monitor, kanal ve ayar değişiklikleri ile kontrol sonuçları kaybolur. Daha kısa RPO için aralığı düşürün (kopya çalışırken alınır, kontroller durmaz). |
| **RTO** (geri dönüş süresi) | Aynı kurulumda birkaç dakika; yeni kurulumda kurulum süresi + birkaç dakika | Provada yeni bir namespace'e kurulum dahil 76 saniye sürdü (imajlar önbellekteydi). Büyük veritabanında kopyanın taşınma süresi eklenir. |
| **Cluster kaybı** | Kopyalar ve anahtar dışarıda saklanmışsa geri dönülebilir | Yedek PVC'si aynı cluster'dadır. Kopyaları ve `kubeat-secret-key` içeriğini dışarıya taşımak kurulumu yapanın işidir. |

**Değişiklik yönetimi için.** Bir geri yükleme en kötü durumda son yedek
kopyadan bu yana geçen süre kadar (RPO, `UPTIME_BACKUP_INTERVAL`, varsayılan
24 saat) veri kaybeder. Riskli bir değişiklikten (yükseltme, taşıma, node
bakımı) önce aralığı geçici düşürerek ya da en yeni kopyayı aşağıdaki
"Yedek kopyaları cluster dışına almak" bloğuyla doğrulayıp dışarı alarak bu
pencere daraltılabilir.

Geri yüklemenin geri getirmedikleri:

- Kopyadan sonraki kontrol sonuçları ve olaylar. Bu sürede çıkan kesintiler
  geçmişte görünmez.
- Gönderilmiş bildirimler. Kopyadan sonra açılıp kapanmış bir olay yeniden
  bildirilmez; hâlâ süren bir kesinti kontroller başlayınca yeniden bulunur.
- Anahtarsız bir kopya. Gizli değerler (token'lar, parolalar, webhook
  adresleri) anahtar olmadan okunamaz. Yanlış anahtarla backend açılışta
  durur ve nedenini yazar.

## Geri yüklemeden önce: susturma ve GitOps

Geri yükleme backend'i bir süre sıfır replikaya çeker; bu sırada Kubeat
kontrol yapmaz. Planlı bir geri yüklemede:

- **Susturma.** Backend sıfıra inince `kubeat_self_healthy` 0 olur; chart'ın
  kuralları açıksa `KubeatDown` ve `KubeatUnhealthy` yanar, heartbeat varsa o
  da kesilir. Bakım penceresini Alertmanager'da susturun (`alertname=KubeatDown`
  ve `alertname=KubeatUnhealthy`, ilgili `namespace`/`instance` ile) ki planlı
  kesinti kimseyi sayfaya düşürmesin. Geri yüklemeden sonra Kubeat süren bir
  kesintiyi kontroller başlayınca yeniden bulur; kopyadan sonra kapanmış bir
  olay yeniden bildirilmez.
- **GitOps self-heal.** Kurulumu Argo CD ya da OpenShift GitOps yönetiyorsa
  otomatik eşitleme (self-heal) `replicas: 0`'ı hemen tekrar `1`'e çeker ve
  geri yüklemeyi bozar. Geri yükleme boyunca uygulamanın otomatik eşitlemesini
  durdurun (uygulamayı manuel sync moduna alın) ya da backend Deployment'ını
  eşitlemeden hariç tutun; bitince eşitlemeyi geri açın.

## Yedek kopyaları cluster dışına almak

Yedek PVC'si aynı cluster'dadır; cluster'ın tamamını kaybetmeye karşı
kopyaların ve şifreleme anahtarının dışarıya da taşınması gerekir. Backend
imajında `tar` olmadığından `kubectl cp` çalışmaz, dosya `exec` üzerinden
taşınır. Düz `exec … cat > dosya` ise büyük ikili dosyada akışı **sessizce ve
değişken noktada kesebilir** (uzak ya da hız sınırlı API uçlarında: Developer
Sandbox'ta ~5,7 MB'lık kopya art arda denemelerde 0,6–2,4 MB geldi,
[#117](https://github.com/yusufceng/Kubeat/pull/117)); kesik kopya ancak geri
yüklerken fark edilir. Bu yüzden kopya aşağıdaki blokla alınır. Blok
kurulumdaki en yeni kopyayı ve şifreleme anahtarını bulunduğunuz dizine
yazar:

- Kopyayı 512 KiB'lik parçalar hâlinde alır ve her parçanın sha256'sını
  pod'daki parçanınkiyle karşılaştırır; eşleşmeyen parça yeniden çekilir (en
  çok 8 deneme).
- En sonda tüm dosyanın sha256'sını pod'daki dosyanınkiyle, ilk 16 baytını
  `SQLite format 3` başlığıyla karşılaştırır ve anahtar dosyasının boş
  olmadığına bakar.
- Hepsi tutarsa kopyayı son adıyla yazar, sağlama toplamını yanına
  `<kopya>.sha256` olarak koyar ve **"yedek doğrulandı"** der. Doğrulanmayan
  kopya son adını hiç almaz: `.partial` olarak yazılır ve silinir, aynı adla
  daha önce doğrulanmış bir kopyanın üzerine de yazmaz.

**Blok "yedek doğrulandı" demiyorsa kopya geçersizdir:** saklamayın, ona
güvenerek yükseltme ya da kaldırma yapmayın, bloğu yeniden çalıştırın. Blok
`kubectl`'den ve `oc`'den bağımsızdır (`K=${KUBECTL:-kubectl}`) ve kendi
`sh`'ında koşar; bash'e de zsh'e de olduğu gibi yapıştırılır. İlk satırda
kendi değerlerinizi verin:

```sh
KUBECTL=kubectl NS=kubeat RELEASE=kubeat sh -s <<'EOF'
K=${KUBECTL:-kubectl}
BS=524288                                    # 512 KiB parça
umask 077                                    # kopya ve anahtar 0600 yazılır
# Yerel sağlama toplamı: Linux'ta sha256sum, macOS'ta shasum -a 256.
if command -v sha256sum >/dev/null 2>&1; then SUM=sha256sum; else SUM='shasum -a 256'; fi
# Yerel base64 çözme: GNU ve yeni macOS'ta -d, eski macOS'ta -D.
if [ "$(printf YQ== | base64 -d 2>/dev/null)" = a ]; then DEC='base64 -d'; else DEC='base64 -D'; fi
X(){ $K -n "$NS" exec "deploy/$RELEASE-backend" -- sh -c "$1" </dev/null; }
fail(){ [ -n "${F:-}" ] && rm -f "$F.partial" "$F.chunk" kubeat-secret.key.partial
        echo "YEDEK DOĞRULANMADI: $1; kopyayı saklamayın, bloğu yeniden çalıştırın"; exit 1; }

if [ -n "${COPY:-}" ]; then B=/backup/$COPY; else B=$(X 'ls -1 /backup/kubeat-*.db | tail -n 1'); fi
[ -n "$B" ] || fail "/backup'ta kopya yok (backup.enabled?)"
F=$(basename "$B")
SIZE=$(X "wc -c < $B" | tr -dc 0-9)
POD=$(X "sha256sum $B" | cut -d' ' -f1)
[ "${SIZE:-0}" -gt 0 ] && [ ${#POD} -eq 64 ] || fail "$B pod'da okunamadı"
N=$(( (SIZE + BS - 1) / BS ))

: > "$F.partial"; i=0
while [ "$i" -lt "$N" ]; do
  t=0; want=
  while :; do
    [ ${#want} -eq 64 ] || want=$(X "dd if=$B bs=$BS skip=$i count=1 2>/dev/null | sha256sum" | cut -d' ' -f1)
    X "dd if=$B bs=$BS skip=$i count=1 2>/dev/null | base64" | $DEC > "$F.chunk" 2>/dev/null
    [ ${#want} -eq 64 ] && [ "$($SUM "$F.chunk" | cut -d' ' -f1)" = "$want" ] && break
    t=$((t+1)); [ "$t" -lt 8 ] || fail "parça $i/$N 8 denemede alınamadı"
  done
  cat "$F.chunk" >> "$F.partial"; i=$((i+1))
done
rm -f "$F.chunk"

$K -n "$NS" get secret "$RELEASE-secret-key" -o jsonpath='{.data.key}' </dev/null | $DEC > kubeat-secret.key.partial
YEREL=$($SUM "$F.partial" | cut -d' ' -f1)
[ "$YEREL" = "$POD" ] || fail "$F'in sha256'sı pod'dakiyle eşleşmiyor"
[ "$(head -c 16 "$F.partial" | tr -d '\0')" = 'SQLite format 3' ] || fail "$F bir SQLite veritabanı değil"
[ -s kubeat-secret.key.partial ] || fail "$RELEASE-secret-key okunamadı"
mv kubeat-secret.key.partial kubeat-secret.key && mv "$F.partial" "$F" || fail "dosyalar yazılamadı"
printf '%s  %s\n' "$YEREL" "$F" > "$F.sha256"
echo "yedek doğrulandı: $F ($SIZE bayt, sha256 $YEREL) ve kubeat-secret.key"
EOF
```

- **`kubectl` ya da `oc`.** Blok `kubectl` ile çalışır; OpenShift'te ilk
  satıra `KUBECTL=oc` yazın. Belirli bir bağlam için
  `KUBECTL="kubectl --context <bağlam>"`: değer `sh` içinde kelimelere
  bölünür.
- **macOS ve Linux.** Yerel sağlama toplamı Linux'ta `sha256sum`, macOS'ta
  `shasum -a 256`'dır (yeni macOS'ta `sha256sum` da vardır); blok hangisi
  varsa onu kullanır. Yerel base64 çözme GNU coreutils'te ve yeni macOS'ta
  `base64 -d`, eski macOS'ta `base64 -D`'dir (orada `-d` çözmez); blok `-d`'yi
  dener, çözmezse `-D`'ye geçer. Pod tarafında backend imajının GNU araçları
  (`sha256sum`, `base64`, `dd`) çalışır, fark yoktur. Kopyayı sonradan elle
  denetlemek için: Linux'ta `sha256sum -c <kopya>.sha256`, macOS'ta
  `shasum -a 256 -c <kopya>.sha256`.
- **Hangi kopya.** Kopya adları zamana göre sıralanır, son satır en yeni
  kopyadır; yazılmakta olan kopya (`.partial`) listeye girmez. Belirli bir
  kopya için ilk satıra `COPY=kubeat-20261012T030000Z.db` ekleyin; kopyaları
  `kubectl exec deploy/kubeat-backend -- ls /backup` listeler. `/backup` boşsa
  ya da yoksa yedekleme kapalıdır (`backup.enabled`).
- **Süre.** Her parça iki `exec` çağrısıdır (sağlama toplamı ve veri); süre
  veritabanının boyutuyla büyür.
- **Backend pod'u yoksa** (ör. uyutulmuş bir kurulumda) önce
  `kubectl scale deploy/kubeat-backend --replicas=1` ve
  `kubectl rollout status deploy/kubeat-backend`. Blok backend Deployment'ı
  üzerinden çalışır; release kaldırıldıktan sonra çalışmaz, kopyayı
  kaldırmadan önce alın.
- **Düz `cat` ile alınmış eski kopyalar.** 0.49 ve öncesinde bu bölümde
  yazan `exec … cat` komutuyla alınmış bir kopya doğrulanmamıştır ve sessizce
  kesilmiş olabilir; cluster duruyorsa kopyayı bu blokla yeniden alın.

Anahtar dosyasını kopyalarla birlikte ama ayrı ve erişimi kısıtlı bir yerde
saklayın. Kopyadaki gizli değerler, kopya alındığı sırada kullanılan
`UPTIME_SECRET_KEY` ile şifrelidir; anahtarı olmayan bir kopya işe yaramaz.

## Yedek kopyadan geri yükleme

Geri yükleme bir yardımcı pod ile yapılır. Yardımcı pod backend imajını
kullanır, ek bir imaj çekilmez.

**Yardımcı pod'un fsGroup/UID'i backend ile aynı olmalıdır.** Pod
düzeyindeki `securityContext`, yardımcı pod'un volume'lara backend gibi
yazabilmesi içindir; kurulum komutundaki `podSecurityContext.fsGroup=1001`'in
karşılığıdır (README, "Cluster'a kurulum"). Olmazsa volume'un kökü root'a ait
bir cluster'da aşağıdaki kopyalama `/data`'ya yazamaz ve backend o sırada
sıfır replikada, yani izleme durmuş hâlde kalır.

- **OpenShift'te** `fsGroup` satırını çıkarın; UID ve fsGroup'u SCC atar.
- **Kurulumda başka bir `podSecurityContext` verdiyseniz** satırdaki değeri
  onunla değiştirin. Kurulumdaki değer:
  `kubectl get deploy kubeat-backend -o jsonpath='{.spec.template.spec.securityContext}'`
  (çıktı, ör. `{"fsGroup":1001}`, olduğu gibi yazılabilir).

```yaml
# kubeat-restore.yaml: image, kurulumdaki backend imajı olmalıdır
# (kubectl get deploy kubeat-backend -o jsonpath='{..image}')
apiVersion: v1
kind: Pod
metadata:
  name: kubeat-restore
spec:
  restartPolicy: Never
  securityContext: { fsGroup: 1001 }             # OpenShift'te bu satırı çıkarın
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

### Aynı kurulumda, yedek volume'undaki bir kopyaya dönmek

1. Backend'i durdurun ve pod'un gitmesini bekleyin:
   `kubectl scale deploy/kubeat-backend --replicas=0`. Deployment'ın stratejisi
   `Recreate`'tir: tek yazarlı SQLite için eski pod tamamen gitmeden yenisi
   volume'u bağlamamalıdır, bu yüzden pod'un `Terminating`'inin bitmesini
   bekleyin.
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

### Cluster ya da namespace kaybında, yeni bir kuruluma

1. Saklanan anahtarla bir Secret oluşturun ve chart'ı onunla kurun:
   `kubectl create secret generic kubeat-restored-key --from-file=key=kubeat-secret.key`,
   `helm install kubeat ... --set secretKey.existingSecret=kubeat-restored-key`.
   Yedeklemeyi de açın (`backup.enabled=true`).
2. Backend hazır olunca (boş bir veritabanı oluşturur) yukarıdaki 1. ve 2.
   adımları uygulayın; yardımcı pod'da `backup` volume'u gerekmez.
3. Bilgisayardaki kopyayı aşağıdaki blokla doğrulayarak yazın; ilk satırda
   namespace'i ve kopyanın dosya adını verin. Kopya tek akışla önce
   `/data/uptime.db.restore`'a yazılır; pod tarafında o dosyanın sha256'sı
   yerel kopyanınkiyle, ilk 16 baytı `SQLite format 3` başlığıyla
   karşılaştırılır. Yalnız ikisi de tutarsa `mv` ile `/data/uptime.db` olur
   ve blok **"geri yüklendi"** der. Tutmazsa `mv` yapılmaz, yarım dosya
   silinir, `/data/uptime.db` değişmez ve blok `GERİ YÜKLENMEDİ` ile neyin
   tutmadığını yazar; aynı bloğu yeniden çalıştırın.

```sh
KUBECTL=kubectl NS=kubeat F=kubeat-20261012T030000Z.db sh -s <<'EOF'
K=${KUBECTL:-kubectl}
if command -v sha256sum >/dev/null 2>&1; then SUM=sha256sum; else SUM='shasum -a 256'; fi
fail(){ echo "GERİ YÜKLENMEDİ: $1"; exit 1; }
YEREL=$($SUM "$F" | cut -d' ' -f1)
[ ${#YEREL} -eq 64 ] || fail "$F okunamadı"
if [ -f "$F.sha256" ]; then
  [ "$(cut -d' ' -f1 "$F.sha256")" = "$YEREL" ] || fail "$F, dışarı alındığındaki sha256'yla ($F.sha256) eşleşmiyor"
else
  echo "UYARI: $F.sha256 yok; kopyanın dışarı alınırken doğrulandığı bilinmiyor"
fi
$K -n "$NS" exec -i kubeat-restore -- sh -c 'cat > /data/uptime.db.restore' < "$F"
$K -n "$NS" exec kubeat-restore -- sh -c '
  got=$(sha256sum /data/uptime.db.restore 2>/dev/null | cut -d" " -f1)
  hdr=$(head -c 16 /data/uptime.db.restore 2>/dev/null | tr -d "\0")
  if [ "$got" = "$1" ] && [ "$hdr" = "SQLite format 3" ]; then
    mv /data/uptime.db.restore /data/uptime.db && echo "geri yüklendi: /data/uptime.db (sha256 $got)"
  else
    rm -f /data/uptime.db.restore
    echo "GERİ YÜKLENMEDİ: yüklenen dosya yerel kopyayla eşleşmiyor (sha256 ${got:-yok}, beklenen $1); /data/uptime.db değişmedi, yeniden deneyin"
    exit 1
  fi' sh "$YEREL" </dev/null
EOF
```

- **Önce yerel kopya.** Kopyanın yanında dışarı alma bloğunun yazdığı
  `<kopya>.sha256` varsa yerel kopya önce ona karşı denetlenir: kopya
  dışarı alındığından beri değiştiyse yükleme hiç başlamaz. Dosya yoksa
  (kopya bu blokla alınmadıysa) blok uyarır ve sürer; pod'a yazılan dosya
  yine yerel kopyayla karşılaştırılır, ama yerel kopyanın kendisinin
  sağlam olduğu bilinmez.
- **Yükleme parçalı değildir.** Kesilen bir akışı pod'daki denetim yakalar
  ve `mv` yapılmaz; yükleme art arda tutmuyorsa bir
  [issue](https://github.com/yusufceng/Kubeat/issues) açın.
- **`kubectl` ya da `oc`, macOS ya da Linux.** Farklar dışarı alma
  bloğundakiyle aynıdır (yukarıda "Yedek kopyaları cluster dışına
  almak"); yükleme yerelde base64 kullanmaz.

4. Yukarıdaki 4. ve 5. adımlar.

Kopyadan sonraki kontrol sonuçları ve değişiklikler kaybolur. Eski bir
sürümün kopyası yeni sürümde açılır ve şeması yükseltilir; yeni bir sürümün
kopyası eski sürümde açılmaz. Anahtar yanlışsa backend başlamaz.

Aynı release adıyla **yeniden kurulum** (ör. Terraform destroy/apply) geri
yükleme gerektirmez: `helm uninstall` veri ve yedek PVC'lerini, şifreleme
anahtarını ve chart'ın ürettiği `<release>-admin`, `<release>-api-token` ve
`<release>-metrics-token` Secret'larını (`backend.persistence.keepOnUninstall`
açıkken) bırakır, aynı adla yeniden kurulum hepsini sahiplenir; yönetici
parolası ve token'lar eskisi gibi kalır (README "Yedekleme ve geri yükleme",
"Kazayla kaldırmadan dönüş").

## Node kaybından dönüş

Veri PVC'si tek pod'a bağlanan (ReadWriteOnce) blok depolama ister. Node
çökünce backend pod'u `Terminating`'de kalabilir ve yeni pod volume'u
bağlayamaz (`Multi-Attach error`); Kubeat kendiliğinden dönmeyebilir ve
`KubeatDown` yanar. Veri kaybı yoktur, volume sağlamdır; sorun eski pod'un
bağının (attach) bırakılmamasıdır.

1. Durumu görün: `kubectl get pods -o wide` eski pod'u çöken node'da
   `Terminating`, yeni pod'u `ContainerCreating`'de gösterir; yeni pod'un
   olayları (`kubectl describe pod`) `Multi-Attach error for volume` yazar.
2. Node geri gelecekse bekleyin: node `NotReady`'den çıkınca kubelet eski
   pod'u temizler, bağ bırakılır ve yeni pod volume'u bağlar.
3. Node geri gelmeyecekse (kalıcı kayıp) takılı pod'u zorla silin:
   `kubectl delete pod <eski-pod> --grace-period=0 --force`. Bağ, depolama
   denetleyicisi node'un gittiğini anlayıp volume'u ayırınca bırakılır ve yeni
   pod bağlar. Tek yazarlı SQLite için bunu yalnızca eski pod'un gerçekten
   çalışmadığından emin olunca yapın: aynı volume'a iki backend yazarsa
   veritabanı bozulur.
4. Yeni pod `Ready` olunca izleme kaldığı yerden sürer. Volume de kaybolduysa
   (node yerel diski ya da reclaim'lenen PV) yukarıdaki "Yedek kopyadan geri
   yükleme" ile en yeni kopyadan dönün.

Ağ dosya sistemleri (NFS, CephFS, EFS, Azure Files) iki pod'un aynı anda
yazıp SQLite'ı bozmasına yol açabileceğinden veri PVC'si için kullanılmamalıdır
([yol-haritasi.md](yol-haritasi.md) "Bilinen sınırlar").

## Dolu veri volume'undan dönüş

Veri volume'u dolunca kontrol sonuçları yazılamaz ve kaybolur (bellekte
tutulmaz): olay açılmaz, bildirim gitmez. Baştaki uyarı burada da geçerli;
öz-sağlık disk doluluğunu değil kaybolan sonuçları ölçtüğü için dolu disk, WAL
var olan alanı kullandığı sürece bir süre sessiz kalır. PVC doluluğunu bir
platform alarmı dolmadan önce yakalamalıdır.

Bu alarm platformdan bağımsızdır. `KubePersistentVolumeFillingUp`
[kubernetes-mixin](https://github.com/kubernetes-monitoring/kubernetes-mixin)
kuralıdır: OpenShift user workload monitoring'de, kube-prometheus-stack'te ve
GKE'de Grafana Cloud'un Kubernetes entegrasyonu (ya da Google Managed
Prometheus'un kubernetes-mixin kuralları) ile hazır gelir; `<release>-data`
PVC'sini `persistentvolumeclaim` etiketiyle kapsar. Hiçbiri yoksa aynı
`kubelet_volume_stats_*` metriklerinden elle bir kural yazılır (eşik, mixin'in
uyarı eşiği olan %15 boş alan):

```yaml
- alert: KubeatDataVolumeFillingUp
  expr: |
    kubelet_volume_stats_available_bytes{persistentvolumeclaim="kubeat-data"}
      / kubelet_volume_stats_capacity_bytes{persistentvolumeclaim="kubeat-data"}
      < 0.15
  for: 15m
  labels:
    severity: warning
  annotations:
    summary: "Kubeat'in veri volume'u doluyor"
```

(kubernetes-mixin ayrıca `predict_linear` ile dört gün içinde dolacak
volume'u da uyarır; yukarıdaki oran en küçük hâlidir.) `<release>` chart'ın
release adıysa PVC adı `<release>-data`'dır; metrik sunulmuyorsa
kube-state-metrics / kubelet volume metriklerinin scrape edildiğinden emin
olun.

Volume dolduğunda:

1. Yeri neyin aldığına bakın: veritabanının kendisi mi büyüdü (saklama
   ayarları ve monitor sayısı; README "Geçmiş verisi") yoksa paylaşılan bir
   PVC'yi ya da thin-provisioned havuzu başka bir kiracı mı doldurdu.
2. En kalıcı çözüm PVC'yi büyütmektir. StorageClass `allowVolumeExpansion:
   true` ise:
   `kubectl patch pvc kubeat-data -p '{"spec":{"resources":{"requests":{"storage":"<yeni-boyut>"}}}}'`;
   çoğu CSI sürücüsü dosya sistemini çevrimiçi genişletir, gerekirse backend
   pod'unu yeniden başlatın. PVC kendiliğinden büyümez ([backlog](backlog.md)
   "Dolan veri volume'u").
3. Büyütme mümkün değilse yer açın: saklama süresini kısaltın
   (`UPTIME_RETENTION`, `UPTIME_ROLLUP_RETENTION`). Backend'in sıkıştırması
   (VACUUM) geçici olarak veritabanı kadar daha yer ister; çok dolu bir
   volume'da VACUUM yer bulamayabilir, bu yüzden büyütme tercih edilir.
4. Veritabanı bu sırada bozulduysa "Yedek kopyadan geri yükleme" ile en yeni
   kopyadan dönün; yedek ayrı bir volume'da olduğundan veri volume'u dolsa da
   kopyalar durur.

## Prova: 2026-10-26, minikube, 0.41.0

Senaryo: namespace'in tamamı kaybolur. Elde yalnızca dışarı alınmış son
kopya ve anahtar vardır. Geri yükleme sıfırdan yeni bir kuruluma yapılır.

1. `kubeat-rh-a` namespace'ine `backup.enabled=true` ve
   `backup.interval=2m` ile kuruldu. API üzerinden 3 HTTP monitor ve
   adresi şifreli saklanan bir webhook kanalı eklendi.
2. Son kopya `kubectl exec ... cat` ile, anahtar Secret'tan bilgisayara
   alındı. Kopya `PRAGMA integrity_check` = ok, şema 26, 3 monitor ve 9
   kontrol sonucu içeriyordu. (O günün yolu; düz `cat` büyük kopyayı
   sessizce kesebildiği için bugün kopya "Yedek kopyaları cluster dışına
   almak"taki parçalı ve sha256 doğrulamalı blokla alınır.)
3. Release kaldırıldı ve namespace silindi. Bununla veri ve yedek PVC'leri
   de gitti.
4. `kubeat-rh-b` namespace'ine saklanan anahtarla
   (`secretKey.existingSecret`) yeni kurulum yapıldı. Backend sıfıra
   ölçeklendi, yardımcı pod ile kopya `kubeat-data`'ya yazıldı, backend
   yeniden başlatıldı. (Yazım o gün doğrulamasızdı; bugün "Cluster ya da
   namespace kaybında" 3. adımdaki blok pod'daki dosyayı sha256 ve başlıkla
   denetlemeden `mv` etmez.)
5. Sonuç: 3 monitor ve webhook kanalı geri geldi. Kanalın şifreli adresi
   anahtarla çözüldü. Dosya izni açılışta `0600`'a çekildi.

Toplam süre, namespace oluşturmadan backend'in hazır olmasına kadar
**76 saniye**.

Provada görülenler, README'ye işlendi:

- Backend imajında `tar` yok, bu yüzden `kubectl cp` çalışmaz. Dosyalar
  `exec` üzerinden taşınır. Düz `cat` büyük ikili dosyayı sessizce
  kesebildiği için ([#117](https://github.com/yusufceng/Kubeat/pull/117))
  0.50'den beri dışarı alma 512 KiB parçalı ve sha256 doğrulamalı, geri
  yükleme ise pod tarafında sha256 ve `SQLite format 3` başlığıyla
  doğrulamalıdır ("Yedek kopyaları cluster dışına almak", "Cluster ya da
  namespace kaybında" 3. adım).
- Backend düzgün kapandığında `-wal` ve `-shm` dosyası kalmaz. Bunları
  silmek yalnızca backend çökmüşse gerekir.
- Yardımcı pod backend imajıyla çalışabilir, ek bir imaj gerekmez. OpenShift
  `restricted-v2` SCC altında kullanıcıyı namespace aralığından alır ve aynı
  namespace'teki backend ile aynı UID'yi kullanır.

## Prova: 2026-10-29, minikube, 0.44.0 (kazayla kaldırma)

Senaryo: yanlış release'i kaldırmak ya da takılı bir release'i
"kaldır ve yeniden kur" ile düzeltmek. Kurulum yayımlanan 0.44.0 chart'ı ve
imajlarıyla yapıldı, `backup.enabled=true`, varsayılan
`keepOnUninstall=true`.

1. Kurulum, iki kanal ve üç monitor. Kurulum Secret'ındaki yönetici
   parolasıyla giriş: 200.
2. `helm uninstall`: Helm "These resources were kept due to the resource
   policy" altında `kubeat-data`, `kubeat-backup` ve `kubeat-secret-key`'i
   saydı; üçü de kaldı.
3. Aynı ad ve namespace'le `helm install`: hatasız bitti, iki PVC aynı
   volume'larla yeni release'e bağlandı. Kanallar ve monitor'lar geri geldi.
   Bir döngü daha aynı sonucu verdi.
4. Giriş: **eski** yönetici parolası geçti (200). Yeniden üretilen
   `kubeat-admin` Secret'ındaki, yani kurulum notlarının gösterdiği parola
   geçmedi (401). API ve metrik anahtarları da yeniden üretildi; backend
   bunları ortamdan okuduğu için yenileri geçerli.
5. Notlardaki `kubectl -n <namespace> delete pvc kubeat-data kubeat-backup`
   yalnızca bu iki PVC'yi sildi. Release kaldırıldıktan sonra notlar
   `helm get notes` ile okunamıyor; komut README'de de yazılı.

Görülenler: veri ve yedek kaldırmadan sağ çıkıyor (RPO etkilenmiyor),
yeniden kurulum bir dakikadan kısa sürüyor. Bu provanın açtığı eksik
(yeniden kurulumda kurulum notlarının geçmeyen bir parola göstermesi) 0.47.0'da
kapandı: `<release>-admin`, `<release>-api-token` ve `<release>-metrics-token`
Secret'ları artık `backend.persistence.keepOnUninstall` ile korunur, yeniden
kurulum eski değerleri sahiplenir ve kurulum notları silme komutunda üçünü de
sayar (S39).

## Prova: 2026-10-29, minikube CSI (fsGroupPolicy: File), 0.45.0

Senaryo: fsGroup'u gerçekten uygulayan bir depolama sürücüsünde (minikube
`csi-hostpath-driver`, `hostpath.csi.k8s.io`, `fsGroupPolicy: File`) README
"Cluster'a kurulum" komutuyla kurulum ve README'deki yardımcı pod'la geri
yükleme. Varsayılan minikube hostpath sağlayıcısı fsGroup uygulamadığı için
önceki provalar bunu sınamıyordu. Yayımlanan 0.45.0 chart'ı ve imajları,
`backup.interval=1m`.

1. Kurulum `--set podSecurityContext.fsGroup=1001` ile: kurulum notlarında
   fsGroup uyarısı yok. Backend UID 1001, ek grup 1001; `/data` ve `/backup`
   `root:1001` sahipliğinde ve setgid'li geldi, veritabanı yazıldı.
2. Bir monitor eklendi, dakikalık yedek alındı; yedekten sonra bir monitor
   daha eklendi.
3. Negatif: pod düzeyinde `securityContext` olmayan yardımcı pod
   kopyalayamadı (`cp: cannot create regular file '/data/uptime.db.restore':
   Permission denied`).
4. README'deki yardımcı pod (`securityContext: { fsGroup: 1001 }`) ile
   adımlar: backend sıfıra, kopya `/data/uptime.db`'ye, `-wal`/`-shm`
   silindi, backend bire. Geri yüklemeden sonra yalnızca yedekteki monitor
   vardı; yedekten sonra eklenen gitti.
5. Negatif: aynı sürücüde fsGroup'suz kurulum. Kurulum notları fsGroup
   uyarısını gösterdi; backend uyarının söylediği `open database failed`
   (`unable to open database file (14)`) ile CrashLoopBackOff'a düştü.

Görülenler: README'deki fsGroup satırı hem kurulum hem geri yükleme için
gerekli ve yeterli; kurulum notlarının uyarısı doğru durumda çıkıyor.

## Yeniden prova

Sürüm şemayı değiştirdiğinde ya da yedekleme kodu değiştiğinde prova
tekrarlanır. Bu belgeye tarih, sürüm ve süre eklenir. OpenShift'te (sandbox)
geri yükleme provası henüz koşulmadı; sprint 5'in "Kullanıcıda kalanlar"
listesinde.

Parçalı dışarı alma bloğunun `oc` ile yazılmış önceki biçimi 2026-11-06'da
Developer Sandbox'ta doğrulanmış yedeği aldı
([#117](https://github.com/yusufceng/Kubeat/pull/117)). Bu belgedeki biçim
(`KUBECTL`, `RELEASE`, `.partial`) ve doğrulamalı geri yükleme henüz bir
cluster'da koşulmadı: minikube'da `kubectl` ile prova ve bozuk bir kopyanın
`mv` edilmediğinin denenmesi
[sprint 8](sprints/2026-11-06-sprint-08.md#kullanıcıda-kalanlar)'in
"Kullanıcıda kalanlar" listesinde, GKE'de `kubectl` ile koşu kullanıcıda.
