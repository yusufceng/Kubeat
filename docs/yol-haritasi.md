# Yol haritası

[Proje planı](proje-plani.md) üzerine, MVP'den sonra kararlaştırılan sıra.
Ertelenen fikirler [backlog.md](backlog.md) dosyasındadır.

| # | Adım | Durum |
| --- | --- | --- |
| 0 | Temel: scheduler, HTTP/TCP kontrolleri, SQLite, olay ve webhook, arayüz, Helm chart | Tamamlandı |
| 1 | Keşif: Service/Ingress/Route'ları bulma, namespace görünümü | Tamamlandı |
| 2 | Arayüz derinliği: grafik, uzun dönem uptime, düzenleme/durdurma, HTTP seçenekleri, sertifika süresi | Tamamlandı |
| 3 | Alarm olgunluğu: Slack/Teams/e-posta, bakım pencereleri, susturma, hatırlatma | Tamamlandı |
| 4 | Aracın kendi sağlığı: `/metrics`, heartbeat | Tamamlandı; sprint 1 (S9): düşen sonuç, başarısız bildirim, dolu worker, duran ve yaşlanan yedek, biten lisans PrometheusRule'da alarm (bkz. 19); sprint 2 (S32): heartbeat sonuçlar yazılamazken de kesilir (sonuç gelmeyen aralık sağlıklı sayılır), veri PVC'si için boyut kuralı README "Geçmiş verisi"nde; sprint 3 (S38, #50): heartbeat'in kararı `kubeat_self_healthy` gauge'unda ve heartbeat'siz kurulumda da `critical` yanan `KubeatUnhealthy` kuralında (bkz. 19), `UPTIME_NOTIFY_QUEUE_SIZE` varsayılanı 1000 ve etkin monitor sayısı kuyruğu aşınca bir kez WARN, heartbeat de kurallar da kapalıysa ve yedek kapalıysa kurulum notunda uyarı |
| 5 | Cluster metrikleri: monitor'un arkasındaki pod'lar, Prometheus/Thanos'tan CPU ve bellek | Tamamlandı |
| 6 | Agent ve çoklu cluster: her cluster'da hafif agent, merkeze push | Ertelendi: şimdilik tek cluster yeterli (2026-10-12) |
| 7 | Üretime hazırlık: API koruması, yük testi, yedekleme, yönetici parolasıyla giriş | Tamamlandı |
| 8 | Kontrol motorları: HTTP genişletmeleri, TLS'li TCP, ping, DNS, push, gRPC, WebSocket; tekrar aralığı, ters mod, kopyalama | Tamamlandı |
| 9 | Çalışma alanı arayüzü: kenar çubuğunda aranabilir monitor listesi, genel bakış, zengin monitor sayfası, tüm tipler için ekleme/düzenleme formu, tema | Tamamlandı |
| 10 | Etiketler, gruplar ve toplu işlemler | Tamamlandı |
| 11 | Durum sayfaları ve rozetler | Tamamlandı |
| 12 | Tekrarlayan bakım pencereleri | Tamamlandı |
| 13 | Bildirim sağlayıcıları: Telegram, Discord, PagerDuty, Mattermost, Google Chat, ntfy, Gotify; kanal düzenleme ve namespace'e göre yönlendirme | Tamamlandı |
| 14 | Veritabanı kontrolleri: PostgreSQL, MySQL/MariaDB, Redis | Tamamlandı |
| 14b | Kubernetes workload tipi: hazır pod sayısı, keşifte workload'lar | Tamamlandı |
| 14c | MongoDB, Kafka, RabbitMQ kontrolleri | Planlandı |
| 16 | Gizli değerlerin veritabanında şifrelenmesi | Tamamlandı |
| 15 | Kişisel hesaplar, LDAP girişi, namespace bazlı roller, değişiklik kaydı | Tamamlandı |
| 15b | Ayarlar ekranı, kişi başına API anahtarları | Planlandı |
| 17 | Annotation ile sürekli keşif: `kubeat.io/monitor` taşıyan nesneler kendiliğinden izlenir | Tamamlandı |
| 18 | Pod bazında kontrol: Service'in arkasındaki her hazır pod'a aynı kontrol (`each_pod`) | Tamamlandı |
| 19 | Cluster Alertmanager'ı ile alarm: chart'ta PrometheusRule | Tamamlandı; sprint 1 (S9) öz-sağlık kurallarını ekledi: `KubeatResultsDropped`, `KubeatNotificationsFailing`, `KubeatWorkersSaturated`, `KubeatBackupFailing`, `KubeatBackupStale`, `KubeatLicenseExpiring`; sprint 2 (S29): `KubeatDown` backend pod'u hiç yokken de yanar, `KubeatMonitorDown` ve `KubeatCertificateExpiring` 10 dakikaya kadar süren scrape boşluğunda çözülmez ve Recreate'te tek alarm kalır, `KubeatBackupFailing` yedek düzelene kadar açık kalır; kurallar promtool ile sınanır (`test-chart.sh`, CI); sprint 3 (S38, #50): `KubeatUnhealthy` (`critical`, `kubeat_self_healthy` 5 dakika 0); (S37, #50): `metrics.prometheusRule.clusterLabel` ile kurallar birden çok cluster'ın tek deposunda (Grafana Cloud/Mimir, global Thanos Ruler) cluster başına değerlendirilir, `KubeatDown` 24 saatlik yokluk penceresiyle; boşken 0.44'teki gibi; iki cluster'lı ikinci promtool senaryo dosyası; (S26, #49): `KubeatNotificationsFailing` kanal başına (`channel`, `channel_type`) ve `for: 5m` ile yanar, kanalın son denemesi başarısızken ya da almadığı bir kesinti yeniden gönderilmeyi beklerken açık kalır, scrape boşluğunda çözülmez (`kubeat_channel_last_delivery_ok`, `kubeat_channel_undelivered_outages`), `clusterLabel` verilince cluster ve kanal başına toplanır |
| 20 | Olay bağlamı: monitor sayfasında pod'ların ve workload'un cluster olayları | Tamamlandı |
| 21 | Ekip raporları: namespace başına uptime ve kesinti özeti, CSV | Tamamlandı |
| 22 | Alarm yönlendirme: kanalda etiket ve olay süzgeci, namespace başına gruplama, monitor sayfasında "alarm nereye gider"; workload monitor'da rollout toleransı | Tamamlandı |
| 23 | SSO: OpenID Connect ile giriş (PKCE), gruplardan rol | Tamamlandı |
| 24 | Yayım: sürüm etiketiyle GHCR'ye çok mimarili imajlar ve OCI chart; gerçek cluster kurulumunun ön koşulu | Tamamlandı |
| 25 | İki dil: arayüz (tarayıcıya göre, değiştirilebilir) ve bildirimler İngilizce/Türkçe | Tamamlandı |
| 26 | Namespace'le sınırlı keşif: cluster-admin olmadan kurulum (Role), Thanos'un namespace ucu | Tamamlandı |
| 27 | Open-core: Kubeat Enterprise lisansı, çevrimdışı imzalı lisans dosyası ([ADR-0002](adr/0002-open-core-lisans.md)) | Tamamlandı |
| 28 | Pilot: gerçek bir cluster'da üç hafta kullanım ([pilot.md](pilot.md)) | Developer Sandbox'ta (2026-10-27'den itibaren; 2026-10-28'de 0.43.0, doğrulanmış yedekten sonra 2026-10-29'da 0.45.0 ve 2026-11-04'te 0.46.0 (Helm revizyon 32), sonra 0.47.0 ve 0.48.0 (revizyon 33, 34) ve 2026-11-06'da doğrulanmış yedekle 0.49.0 (revizyon 35); ardından `backend.extraEnv` ile `UPTIME_TRUST_PROXY_HOPS=1` kalıcı eklendi (revizyon 36); 2026-11-07'de yeni doğrulamalı blokla (`oc`) alınan yedekten sonra 0.50.0 (revizyon 37–39, hops=1 korundu) ve aynı gün doğrulanmış yedekten sonra 0.51.0 (revizyon 40, hops=1 ve lisans korundu; ön kontrol kapsamı sınırlı kullanıcı buldu, açılışta 2 annotation ve kaydı olmayan 9 monitor namespace'ine bağlandı, 11 monitor'ün durumu değişmedi, `blocked_by_scope` 0); sprint 3'ün alarm provası (2) sandbox UWM'de, sprint 4'ün kimlik ve bekletme provası (1) sandbox'ta geçti; sprint 7'nin S77, S72 ve S63 provaları 2026-11-06'da sandbox'ta koşuldu ([sprints/2026-11-05-sprint-07.md](sprints/2026-11-05-sprint-07.md) "Provaların sonucu"); sprint 8'in S78+S79 provası 2026-11-07'de sandbox Route'unda gerçek tarayıcı form yolundan geçti ([sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md) "Provaların sonucu"); sprint 9'un durum sayfası kilidi ve parola değiştirme provası (iki kaynak, hops=1'de uydurma başlık) 2026-11-07'de sandbox Route'unda geçti ([sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md) "Provaların sonucu"); pilot değerleri; karar 2026-11-27); şirket cluster'ı ve ekip olmadığından geri bildirim sprint ekibinin müşteri ve SRE ajanlarından |
| 29 | Sprint ekibi: müşteri, SRE, BA, PO, PMO ve developer ajanlarıyla düzenli sprintler ([agents/sprint-takimi.md](agents/sprint-takimi.md), [sprints/](sprints/)) | Sprint 1 (2026-10-27–28) tamamlandı, 0.43.0: bildirim teslimatı (S18, S17), yedek dayanıklılığı (S19), öz-sağlık alarmları (S9); puanlar 4,0/5 ve retro [sprints/2026-10-27-sprint-01.md](sprints/2026-10-27-sprint-01.md); sprint 2 (2026-10-28–29) tamamlandı, 0.44.0: Kubeat'in kendi arızası sessiz kalmaz ve kayıp yaratmaz (S29 öz-sağlık kuralları + CI'da promtool, S25 kesinti bildirimi kaybı ve ölü makine kanalı, S31 kaldırmada PVC koruması, S32 sonuç yazılamazken heartbeat); sandbox 0.44.0'da; gerçek ortam provaları (firing, kaldırma ve yeniden kurulum, SIGTERM, dolu volume) kullanıcıda; puanlar 3,5/5 ve retro [sprints/2026-10-28-sprint-02.md](sprints/2026-10-28-sprint-02.md); sprint 3 (2026-10-29) tamamlandı, 0.45.0: ilk imzalı ve SBOM'lu yayım (S1 #48), OpenShift dışında da yazılabilir volume'la kuran ve sürümü CI'da denetlenen README komutu (S10+S11 #47), 4xx'te duraklamayan ve bildirim alamayan kanalı adıyla alarmlayan teslimat (S26 #49), heartbeat'siz kurulumda da critical yanan ve tek depoda cluster başına değerlendirilen öz-sağlık kuralları (S38+S37 #50); imza ve fsGroup'lu kurulum provaları geçti, sandbox 0.45.0'da, alarm provası geçti; puanlar 3,75/5 ve retro [sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md); sprint 4 (2026-10-29–11-04) tamamlandı, 0.46.0: README'de tek komutluk hızlı başlangıç ve `docs/yukseltme.md`'de toplanan yükseltme notları, operator'süz cluster için öz-sağlık yolu, izlenen namespace'lerde NetworkPolicy izni, iç içe AD grupları ve lisans isteği (doküman paketi S12, S15, S30, S46, S51; #59, #60), PagerDuty ve webhook'ta kurulum kimliği ile kısa RETRY_MAX'ta durmayan açılış (S28+S36 #61), ulaşılamayan insan kanalının sayfaları geciktirmemesi (S55 #62), yalnız CI'ı yeşil main commit'inden çıkan yayım ve her sürümde CI'da ağsız doğrulanan imzalı aynalama (S52 #63); sandbox 0.46.0'da, üç prova geçti (sandbox'ta kimlik ve bekletme, minikube'da hızlı başlangıç ve körleşme); puanlar 4,1/5 ve retro [sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md); sprint 5 (2026-11-04) tamamlandı, 0.47.0: geri yükleme, node kaybı ve dolu volume için runbook ([yedek-geri-yukleme.md](yedek-geri-yukleme.md)) ve aynı adla yeniden kurulumda korunan admin/API/metrik Secret'ları (S41+S39 #82), digest'e sabitlenmiş action'larla koşan ve v0.47.0 etiketinde ilk kez sınanan değişmez sürüm/Release (S54+S53 #83), monitor kontrol hatasındaki iç cluster IP'sinin maskelenmesi (S62 #85) ve çok kiracılı cluster'da kullanıcı başına (namespace bazlı) hedef kısıtı (S64 #84); 0.47.0 ayrıca #77–81 güvenlik düzeltmelerini kamuya açar (0.46.1 + advisory ayrı, kullanıcıda; 2026-11-07 kullanıcı kararıyla düştü); sandbox 0.47.0'da (Helm revizyon 33), S39 (Secret keep) ve S62 (IP maskesi) canlı doğrulandı, çok kiracılı prova ve OpenShift geri yükleme kullanıcıda; S64 ana vektörleri kapattı ama iki etiketli kısa adla (`servis.namespace`) aşılabildiği (Müşteri A bulgusu) için çekirdek cross-tenant takibi sprint 6'ya; puanlar 3,9/5 (A 4,5; B 3,25) ve retro [sprints/2026-11-04-sprint-05.md](sprints/2026-11-04-sprint-05.md); sprint 6 (2026-11-05) tamamlandı, 0.48.0: kapsamı sınırlı bir kullanıcının iç hedeflerini çözüm anında (dial-time) denetleyerek iki etiketli kısa ad (`servis.namespace`) ve iç adrese çözülen genel adla kapsam aşımını kapatan hedef kapsamı (S67 #95), kanal geri gelince susturulmuş/bakımdaki monitor için gece bildirimini önleyen, kanal testi/kaydının duraklatmayı kaldırdığı ve insan kanallarına ayrı `UPTIME_NOTIFY_HUMAN_RETRY_MAX` tanıyan teslimat (S27 #96), Kubeat'i infra node'a yerleştiren chart değerleri ile GKE'nin kendi Ingress'iyle kurulum belgesi ve dolu veri diski için platformdan bağımsız alarm yolu (S3+S60 #97), giriş kilidini hesap-başına sert kilitten kaynak IP-başına throttle'a çeviren [ADR-0004](adr/0004-giris-kilitleme-stratejisi.md)'lü düzeltme (S65 #98); sandbox 0.48.0'da (Helm revizyon 34), S67 admin yolu regresyonsuz (iç-cluster create 201) ve S62 IP maskesi canlı doğrulandı, S67 ZORUNLU çok kiracılı kapsam provası + S65 vekil-zinciri throttle + S27 gerçek teslimat yolu + GKE hızlı başlangıç kullanıcıda; S65'in opt-in koruması public Ingress'te varsayılanla devrede değil (B bulgusu, sprint 7'ye) ve S67 kapsamı var olan monitor'leri yeniden denetlemiyor (A bulgusu, sprint 7'ye); puanlar 4,0/5 (A 4,0; B 4,0) ve retro [sprints/2026-11-05-sprint-06.md](sprints/2026-11-05-sprint-06.md); sprint 7 (2026-11-05–06) tamamlandı, 0.49.0: 0.48'de yayımlanan giriş throttle'ını gerçek vekil/NAT topolojisinde savunulur kılan, `X-Forwarded-For` görülürken `UPTIME_TRUST_PROXY_HOPS=0` iken ya da zincir kısayken çalışma WARN'ı veren ve hops gerçek zincirden uzunsa görülen kaynağı istemci-kontrollü girdiden güvenli eşe (peer) düşüren düzeltme (S72 #106, #111), çözüm-anı kapsam denetimini DNS/çözüm hatasında (NXDOMAIN dahil) fail-safe'e çeviren, çözüme ayrı zaman aşımı (`UPTIME_TARGET_RESOLVE_TIMEOUT`) koyan ve iç-adres oracle'ını tek genel mesaja daraltan düzeltme (S77 #107, #112), giriş/dizin erişilebilirliğini ve giriş throttle/lockout'u enum sızdırmayan `reason`/`kind` etiketli metriklerle (`kubeat_signin_failures_total`, `kubeat_signin_throttled_total`) görünür kılan (S63 #108, #113) ve kapsam/yetki dışı hedef reddini bir denetim satırı (`deny`/`target-scope`, 90 gün) ve `kubeat_target_scope_denied_total{reason}` sayacıyla gözleyen (S68 #109, #114) maddeler; dört madde de ilk incelemede kabul (S77 bir düzeltme turuyla), merge sırası #111→#112→#113→#114, S68 rebase'indeki `metrics.go`/`api.go` çakışması release manager tarafından developer turu açılmadan çözüldü; sandbox 2026-11-06'da doğrulanmış yedekle 0.49.0'a yükseltildi (Helm revizyon 35) ve provalar koşuldu: S77'de 403/201 ayrımı ve NXDOMAIN dalı geçti (sandbox tek namespace olduğundan hedef namespace'ler adla temsil edildi; CoreDNS zaman aşımı, hava boşluklu egress, arayüzdeki mesaj ve denetim sayfası görseli koşulmadı), S72 port-forward ve elle yazılmış XFF ile geçti (hops=0 ve revizyon 36'da hops=1), arayüz yolu ölçülmedi ve sprint 8 keşfinde etkisiz olduğu bulundu (S78), S63'te `reason=wrong_password`, `reason=directory_unreachable` ve `kind=source` arttı, `kind=account` denenmedi; puanlar 3,9/5 (A 4,25; B 3,5) ve retro [sprints/2026-11-05-sprint-07.md](sprints/2026-11-05-sprint-07.md); sprint 8 (2026-11-06–07) tamamlandı, 0.50.0 (güvenlik sürümü): arayüzden gelen girişte istemci adresinin backend'e taşınması ve hop rehberinin bu yola göre düzeltilmesi (S78 [#118](https://github.com/yusufceng/Kubeat/issues/118), #126), `X-Forwarded-For`'a yalnız arayüzün sırrını (`X-Frontend-Token`) sunan istekte güvenilmesi ve opt-in `frontend.networkPolicy` (S79 [#119](https://github.com/yusufceng/Kubeat/issues/119), #128), yedeğin cluster dışına parçalı ve sha256 doğrulamalı alınması, doğrulanmamış kopyanın geri yüklenmemesi ve dolu-disk reçetesinin `<release>-data` etiketi (S84+S74 [#120](https://github.com/yusufceng/Kubeat/issues/120), #124), vekilli kurulumda kapsamı sınırlı kullanıcının yerelde NXDOMAIN alan dış hedefi için opt-in `UPTIME_TARGET_SCOPE_TRUST_PROXY` (S81 [#121](https://github.com/yusufceng/Kubeat/issues/121), #125); sprint dışı zamanlayıcının kararsız testinin kök düzeltmesi (#127); dördü de kabul (S78 ve S81 üç, S84 iki, S79 bir incelemeyle), merge sırası #126→#124→#125→#128; sandbox 2026-11-07'de yeni blokla doğrulanmış yedekten sonra 0.50.0'a yükseltildi (Helm revizyon 37–39, hops=1) ve üç zorunlu prova geçti: S78+S79 sandbox Route'unda gerçek tarayıcı form yolundan (`source` istemcinin adresi, cluster içindeki pod uydurma XFF ile admin'i kilitleyemedi, fazla hop değerinin atlatılabildiği gerçek yolda görüldü), S84 minikube'da `kubectl` ile (bozuk kopyaların üçü de reddedildi), S81 minikube'da iki namespace ve cluster dışı vekille; ölçülen tek hop satırı düz Route, GKE satırları, WAF'lı Route ve `frontend.networkPolicy` koşulmadı; önceden var olan bir güvenlik boşluğu gizli triage'da, sprint 9 ilk güvenlik maddesi; puanlar 3,5/5 (A 3,75; B 3,25) ve retro [sprints/2026-11-06-sprint-08.md](sprints/2026-11-06-sprint-08.md); sprint 9 (2026-11-07) tamamlandı, 0.51.0 (güvenlik sürümü, advisory yok; sürüm PR'ı #140): kapsamı sınırlı kullanıcının monitor'lerinde hedef kapsamının kontrolün her bağlantısında uygulanması (S86 [#131](https://github.com/yusufceng/Kubeat/issues/131), #139), kapsam denetiminde operatörün verdiği kurum ağı aralıklarının cluster dışı sayılması için opt-in `UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS` (S80 [#132](https://github.com/yusufceng/Kubeat/issues/132), #137), hop değerini taşıyan hızlı başlangıç, GKE ve OpenShift komutları ile banka pilot reçetesi ve sınıfa ve router politikasına göre öneren kurulum notu (S87 [#133](https://github.com/yusufceng/Kubeat/issues/133), #136), durum sayfası kilidi ile parola değiştirmede kaynak adrese göre sınır (S76 [#134](https://github.com/yusufceng/Kubeat/issues/134), #138; S75'in "Sınırlar" bloğu ve S88'in dizin kilidi belgesi dahil; S69'un SECURITY.md cümlesi sürüm PR'ında); dördü de kabul (S86 üç, S80 iki, S87 ve S76 bir incelemeyle), merge sırası #136→#137→#138→#139, S86'nın rebase'i range-diff ile developer turu açılmadan; üç zorunlu prova geçti: `ingress-nginx` yolu ve `frontend.networkPolicy` Calico'lu minikube'da (hop `1` ilk kez ölçüldü), kurum aralıkları ve her bağlantıdaki kapsam iki namespace'li minikube'da arayüz formundan, durum sayfası kilidi ve parola değiştirme sandbox Route'unda iki kaynakla; sandbox 0.51.0'da (revizyon 40), 11 monitor'ün durumu değişmedi; GKE, WAF/F5 önlü Route, vekilli kurumsal cluster ve gerçek AD ölçülmedi (kullanıcıda); puanlar 3,63/5 (A 4,0; B 3,25) ve retro [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md); sprint 10 (2026-11-07) planlandı, 0.52.0: kapsam reddinin çözüm hatasından ve iptalden ayrılması, API ile kontrolün çözüm süresinin ayrılması, denetim satırlarının dakika sınırı ve kalan red yolları (S82 [#142](https://github.com/yusufceng/Kubeat/issues/142), [#143](https://github.com/yusufceng/Kubeat/issues/143)), HTTP kontrolünün kontroller arasında bağlantı tutmaması (S92 [#144](https://github.com/yusufceng/Kubeat/issues/144)), kaynak başına, adlardan bağımsız giriş tavanı (S88 [#145](https://github.com/yusufceng/Kubeat/issues/145)) ve kaynak ayrımı gerileme sayacı ile chart kuralı (S85 dilim 1 [#146](https://github.com/yusufceng/Kubeat/issues/146)), okunamayan namespace'te keşfin sürmesi (S43 [#147](https://github.com/yusufceng/Kubeat/issues/147)); PMO ve release manager önerisinden fark: S89 sprint 11'e kaydı, 0.51'in gerilemesi S92 eklendi ([sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)); sprint 11 taslağı S73 ile S91, S96 ile cluster ağı beyanı, S95 ile S94, S66 ve belge turu, ardından kararlı sürüm kararı (satır 32) |
| 30 | Tedarik zinciri: cosign ile imzalı imaj ve chart, SBOM/provenance, SECURITY.md | Tamamlandı. Sprint 3'te S1 (#48) 0.45.0 ile yayımlandı: iki imajın ve chart'ın digest'i cosign v3 ile keyless imzalanır (GitHub OIDC; `id-token: write` yalnızca imzalayan işte), imajlar SPDX SBOM ve `mode=max` provenance taşır, `latest` itilmez; iş akışı imzaları README "Tedarik zinciri"ndeki komutlarla çevrimiçi ve ağsız doğrular, etiketin GitHub Release'i digest'leri ve SBOM'ları taşır; SECURITY.md (yalnızca son minor, kritik/yüksek açık için 7 günlük hedef), haftalık `govulncheck` ve `npm audit` (security.yml), govulncheck sabit sürümde; `make release` `release/v*` dalından da etiketler (yetenek, söz değil). Kuru deneme `kubeat-dry-run/` altındaki kendi paketlerine iter. İmza v0.45.0 etiketinde ve başka bir makinede doğrulandı ([sprints/2026-10-29-sprint-03.md](sprints/2026-10-29-sprint-03.md) "Provaların sonucu" 1). Sprint 4'te S52 (#63): yayımın ilk işi etiketlenen commit'in `main`'den ya da `release/v*`'tan erişilebildiğini ve CI'ın o commit'teki push koşusunda beş zorunlu check'in yeşil olduğunu denetler, CI sürüyorsa en çok 30 dk bekler (karar `scripts/release-gate.sh`'ta, sahte API'li testi `make test`'te; ci.yml `release/v*` push'unda da koşar); verify işi iki imajı ve chart'ı iş içindeki bir registry'ye (`registry:2.8.3`) `oras cp -r` ile aynalayıp ağ kesikken `--trusted-root` ile doğrular, diskten doğrulama frontend'i de kapsar; docs/pilot.md'nin aynalaması imzalı yolda; Release metni sürümün `docs/yukseltme.md` bölümüne bağlanır; Chart.yaml'da `home` ve `sources`. Kuru denemede ve v0.46.0 etiketinde doğrulanır (release manager); etikette geçince sprint 3'ün 4. provası (hava boşluklu yol) kapanır. Sprint 5'te S54+S53 (#83, 0.47.0) tamamlandı: üç iş akışındaki üçüncü taraf action'lar, iki Dockerfile'ın taban imajları ve `registry:2.8.3` çok mimarili digest'e sabitlendi, iş düzeyindeki `packages: write` kalkıp yalnız iten işlere (images/chart/sign) verildi, images/chart yayım kapısına bağlı kaldı ve "Immutable releases" açıldı; v0.47.0 yeni kural setiyle (v* güncelleme/silme bypass'sız) atılan ilk değişmez etikettir ve "taşınamaz/silinemez" sözü ilk kez burada sınandı (kuru deneme run 41929844294, yayım koşusu run 41931078847, Release `immutable=true`; [sprints/2026-11-04-sprint-05.md](sprints/2026-11-04-sprint-05.md)). Açık kalanlar: destek penceresi kararlı sürüm (1.0) kararına bırakıldı (kullanıcı kararı 2026-11-07: satılmış sürüm yok; satır 32, [backlog](backlog.md)) |
| 31 | Hızlı başlangıç ve kurulum belgeleri: güncel kurulum komutu, OpenShift dışı fsGroup notu, yükseltme notları docs/yukseltme.md, lisansın nasıl istendiği, operator'süz cluster için öz-sağlık kuralları (S30) | Sprint 3'te S10+S11 geldi (#47, 0.45.0 ile yayımlandı; fsGroup uygulayan bir CSI sürücüsünde kurulum ve geri yükleme provası geçti, GKE'de koşulmadı): kurulum komutunda ve geri yükleme pod'unda `fsGroup=1001` (OpenShift'te atlanır), OpenShift dışında `podSecurityContext` boşken NOTES uyarısı; README ve docs/pilot.md'deki kurulum ve aynalama komutlarının sürümünü `scripts/check-versions.sh` CI chart işinde ve yayımın imajlardan önceki ilk işinde denetler (etiket = Chart.yaml); Helm iki iş akışında v4.3.1'e sabit, CI chart işi ayrıca Helm 3 (v3.22.1) ile koşar; release.yml'in `workflow_dispatch` kuru denemesi yayım paketlerine ve etiketlere dokunmaz. Sprint 4'te #59 (S12; S34'ün README, S61'in belge kısmı): README'nin ilk ekranında hızlı başlangıç (Ingress `ingress.className` ve cert-manager ile tek komut, fsGroup ve heartbeat satırları), desteklenen sürüm satırı ve GKE zonal notu; 0.38–0.45 yükseltme notları [yukseltme.md](yukseltme.md)'de (her sürümde Yapılacak, Davranış değişikliği, Geri dönüş, Alarm kuralları; başta yükseltmeden önce yedek, atlamalı yol ve API'yle betiklenen ön kontroller), README'de bağlantı; "GitOps ile kurulum"da kurulum notlarının uyarıları ve kapatan değerleri; README "API"de cluster dışından erişim; `check-versions.sh` hızlı başlangıç komutunu da denetler. #60 (S30, S15, S46, S51; S34 ile S56'nın belge kısımları; S45, S49 ve S59'un sınır cümleleri): README "Aracın kendi sağlığı"nda operator'süz cluster için öz-sağlık yolu (PrometheusRule CRD'si + Grafana Alloy `mimir.rules.kubernetes` ile chart'la yükselen kurallar; kopyalama yolu `mimirtool rules sync` ve her yükseltmede koşan adım, GMP `Rules`; elle scrape'in job adı ve `backendIngressFrom`; CRD'siz cluster'da iki değerin kurulumu düşürmesi), kurulum notunda heartbeat'in iki satırı, CRD yokken kural önerilmemesi, heartbeat'in bildirim alamayan kanalı duymadığı ve API anahtarıyla port-forward satırları (`test-chart.sh` `--api-versions` ile ve onsuz); "Ağ erişimi"nde izlenen namespace'lerin varsayılan-ret politikası ve `allow-from-kubeat` (proje şablonu, var olan namespace'ler, AdminNetworkPolicy); "LDAP"ta iç içe AD grupları için zincir süzgeci; lisansın nasıl istendiği ve aynı dosyanın birden çok cluster'da geçtiği; HTTPRoute, bakımda uptime ve TLS'in router'da bitmesi sınır cümleleri. Alloy yolu, GKE, `allow-from-kubeat` ve AD koşulmadı: o sprintin "Kullanıcıda kalanlar" 4, 6 ve 7 ([sprints/2026-10-29-sprint-04.md](sprints/2026-10-29-sprint-04.md)) |
| 32 | Kararlı sürüm (1.0) kararı | Planlandı: sprint 11 sonunda. Destek penceresi, eski minor desteği ve geri taşıma bu kararla düşünülür (kullanıcı kararı 2026-11-07: satılmış sürüm yok, eski minor'lara geri taşıma yok; [sprints/2026-11-07-sprint-09.md](sprints/2026-11-07-sprint-09.md)). Kararın girdileri (sprint 10 planı, [sprints/2026-11-07-sprint-10.md](sprints/2026-11-07-sprint-10.md)): Müşteri A'nın her güvenlik düzeltmesine GHSA ve CVE kaydı ile destek penceresi (en az bir önceki minor'a yama) isteği; kullanıcı kararları (a) eski sürüm desteği yok ve (b) kendi bulunan açıklara advisory yok, ikisi de 1.0'a kadar; kullanıcıdaki GKE uçtan uca ve kurumsal OpenShift provaları; sprint 11'e sığmayan maddeler (ör. S89, S93, S94). Karar "henüz değil" de olabilir |

Yük testi sonuçları: [yuk-testi.md](yuk-testi.md); 0.51'de kapsama bağlı HTTP
kontrollerinin kontroller arasında tuttuğu bağlantıların ölçümü ve 0.52'deki
düzeltmesi (S92, [#144](https://github.com/yusufceng/Kubeat/issues/144)) orada
"HTTP kontrolünün bağlantıları" bölümünde.

## Uptime Kuma ile karşılaştırma

8. adımdan itibaren hedef, [Uptime Kuma](https://github.com/louislam/uptime-kuma)'nın
işlevselliğini bu projenin amacına (OCP/Kubernetes servisleri) uyan kısmıyla
karşılamak. Kuma'nın kaynağı (3.0 beta, 2026-10) incelenerek çıkarıldı.

| Kuma'da | Kubeat'te |
| --- | --- |
| HTTP: metot, gövde, header, kabul edilen kodlar, yönlendirme sınırı, keyword (ve tersi), JSON sorgusu, basic/bearer | Var. NTLM, OAuth2 ve mTLS yok |
| TCP port, ping, DNS, push, gRPC, WebSocket | Var. gRPC'de keyword yerine standart sağlık protokolü |
| Tekrar sayısı ve tekrar aralığı, ters mod, sertifika süresi bildirimi | Var |
| Veritabanları (PostgreSQL, MySQL, SQL Server, MongoDB, Redis, Oracle), MQTT, Kafka, RabbitMQ, SMTP, SNMP, SFTP, NTP, RADIUS | PostgreSQL, MySQL/MariaDB ve Redis var. MongoDB, Kafka ve RabbitMQ 14c adımında; kalanı yok |
| Docker container, PM2, sistem servisi | Karşılığı var: Kubernetes workload tipi (hazır pod sayısı) ve pod paneli |
| Gerçek tarayıcı, oyun sunucuları, Steam, Tailscale, Globalping | Kapsam dışı |
| Kenar çubuğunda liste, arama ve filtre, toplu seçim | Var |
| Monitor sayfası: kalp atışı çubuğu, yanıt süresi grafiği, 24 sa / 30 gün / 1 yıl uptime, sertifika bilgisi, olay tablosu | Var |
| Açık, koyu ve sistem teması | Var |
| Gruplar ve etiketler | Var: namespace ya da serbest grup, renkli etiketler. Etiketlerin değeri ve iç içe gruplar yok |
| Durum sayfaları (herkese açık, olay duyurusu, RSS), rozetler | Var: sayfa, duyuru, günlük uptime geçmişi, son kesintiler, durum ve uptime rozetleri. RSS, özel CSS ve özel alan adı yok |
| Bakım: tek seferlik, tekrarlayan, cron | Tek seferlik ve günlük/haftalık/aylık tekrar var, saat dilimiyle. Cron ifadesi ve haftanın birden çok günü yok (her gün için ayrı pencere açılır) |
| 110 bildirim sağlayıcısı | 11 tanesi var: Slack, Teams, Telegram, Discord, Mattermost, Google Chat, PagerDuty, ntfy, Gotify, e-posta, webhook. Opsgenie eklenmedi: Atlassian ürünü sonlandırıyor |
| Monitor başına bildirim seçimi | Kanal başına namespace ya da grup kapsamı var; tek tek monitor seçimi yok |
| Kullanıcı, 2FA, API anahtarları, ayarlar ekranı | Hesaplar, LDAP ve namespace bazlı roller var (Kuma tek kullanıcılıdır). 2FA, kişi başına API anahtarı ve ayarlar ekranı yok |
| Proxy, uzak tarayıcı, Cloudflare tüneli | Kapsam dışı |

Kuma'da olmayıp burada olanlar: cluster'dan keşif (Service, Ingress, Route),
monitor'un arkasındaki pod'lar ile CPU/bellek, Helm chart ve OpenShift
kısıtlarına uygun imajlar, `/metrics` ve heartbeat ile kendi kendini izleme.

## OpenShift

2026-10-13'te gerçek bir OpenShift'te doğrulandı: Red Hat Developer Sandbox
(ROSA üzerinde OpenShift 4.21.30, Kubernetes 1.34), cluster-admin olmadan,
tek bir namespace'e kurulumla. Ondan önce yalnızca minikube üzerindeki
benzetim vardı (`make minikube-openshift-sim`).

Gerçek cluster'da doğrulananlar:

- `restricted-v2` SCC: rastgele UID (1010760000), GID 0, SCC'nin atadığı
  fsGroup ile PVC'ye yazma, salt okunur kök dosya sistemi
- Router'ın Route'u taşıması ve host ataması; edge TLS, HTTP→HTTPS
  yönlendirme, oturum çerezinin `Secure` gelmesi
- GHCR'den imaj çekme ve cluster'ın iç registry'sinden imaj çekme
- HTTP (Service ve Route üzerinden), TLS'li TCP, DNS ve ping kontrolleri;
  ping RHCOS'ta ek ayar gerektirmeden çalışıyor
- Namespace'le sınırlı keşif (ClusterRole olmadan): Route, Service ve
  workload adayları, annotation eşitlemesi
- Pod bazlı kontrol: dondurulan pod Ready kalırken adıyla yakalandı
- Workload monitor ve rolling update sırasında sessiz kalması
- Pod paneli: pod'lar, node'lar, cluster olayları
- Pod CPU/belleği: Thanos Querier'ın namespace ucu (9092), servis hesabı
  token'ı ve pod'a kendiliğinden gelen service CA ile
- User workload monitoring'in ServiceMonitor'ü toplaması ve PrometheusRule
  ile `KubeatMonitorDown` alarmının firing olması

Gerçek cluster'ın gösterdiği ve düzeltilen: OpenShift `namespace` etiketini
zorla scrape edilen namespace yapıyor (`honorLabels` etkisiz); monitor'un
namespace'i artık `monitor_namespace` etiketinde.

Hâlâ doğrulanmayanlar (cluster-admin gerektirir):

- Cluster çapında keşif (ClusterRole) ve `cluster-monitoring-view` ile
  Thanos'un 9091 ucu
- NetworkPolicy'nin backend'i gerçekten yalıtması (Sandbox'ın kendi
  "aynı namespace'e izin ver" kuralı sonucu gizliyor); izlenen namespace'in
  varsayılan-ret politikasında `allow-from-kubeat`'in Service ve `each_pod`
  kontrollerini yeşile döndürmesi de (aşağıda "Bilinen sınırlar", S51
  satırı)
- Alarmın Alertmanager alıcısına (e-posta, Slack) kadar gitmesi
- OIDC/LDAP girişinin kurumsal bir sağlayıcıyla denenmesi

## Bilinen sınırlar

- Backend tek replica çalışır; güncelleme sırasında kontroller durur
  (gerçek OpenShift'te ölçülen boşluk 23–27 sn).
- Kapanışta (yükseltme, pod taşıma) yarıda kalan ters monitor kontrolü açık
  kesintiyi kapatıp sahte bir düzelme bildirimi gönderebilir; açılışta kesinti
  yeniden açılır (S89, sprint 11 adayı).
- Veri PVC'si attach'i uygulayan blok depolama ister. NFS, CephFS, EFS,
  Azure Files gibi ağ dosya sistemlerinde iki pod aynı anda yazıp SQLite'ı
  bozabilir. Node çökünce backend pod'u Terminating'de kalabilir ve yeni pod
  volume'u bağlayamayabilir (Multi-Attach); Kubeat kendiliğinden dönmeyebilir
  ve `KubeatDown` yanar. Veri kaybı yoktur, volume sağlamdır; dönüş adımları
  (node'u beklemek ya da takılı pod'u zorla silmek)
  [yedek-geri-yukleme.md](yedek-geri-yukleme.md) "Node kaybından dönüş"te
  ([#82](https://github.com/yusufceng/Kubeat/issues/82)).
- Veri volume'u dolunca kontrol sonuçları yazılamaz ve kaybolur (bellekte
  tutulmaz): olay açılmaz, bildirim gitmez. Kubeat bunu kendi bildirim
  kanallarından duyuramaz. 0.45'ten beri sonuç kaybı başlayınca
  `kubeat_self_healthy` 0 olur ve chart'ın kuralları açıksa `KubeatUnhealthy`
  5 dakikada `critical` yanar, heartbeat olmadan da; heartbeat varsa o da
  kesilir. Ama `KubeatUnhealthy` disk doluluğunu değil kaybolan sonuçları
  ölçer: SQLite'ın WAL'ı var olan alanı kullandığından seyrek (az monitor'lü)
  bir kurulumda dolu disk uzun süre sessiz kalabilir, bu yüzden PVC doluluğu
  için platform alarmı gerekir (OpenShift'te `KubePersistentVolumeFillingUp`,
  `kubelet_volume_stats_*`). İkisi de kapalıysa kimse duymaz; kurulum notu
  bunu uyarır. Volume kendiliğinden büyümez; boyut kuralı README "Geçmiş
  verisi"nde, dönüş adımları [yedek-geri-yukleme.md](yedek-geri-yukleme.md)
  "Dolu veri volume'undan dönüş"te ([backlog](backlog.md) "Dolan veri
  volume'u: büyütme, kotaya göre saklama, yazılamayan sonuçlar").
- Giriş kilidi sayacı backend belleğindedir; yeniden başlatınca sıfırlanır.
  Tek replica olduğu için şimdilik sorun değildir; çok replicalı dağıtık
  throttle için paylaşılan durum yoktur. 0.48'de kilit kullanıcı-adı bazlı
  sert kilitten kaynak-adres bazlı throttle'a döndü (CWE-307, S65,
  [#98](https://github.com/yusufceng/Kubeat/issues/98),
  [ADR-0004](adr/0004-giris-kilitleme-stratejisi.md)): adını bilen biri tek bir
  kaynaktan bir hesabı herkes için kilitleyemez, meşru sahip başka bir adresten
  girer. Kalan sınırlar: (a) kaynak adresi bir Ingress/yük dengeleyici
  arkasında ancak `UPTIME_TRUST_PROXY_HOPS` arayüzün önündeki vekillerin
  `X-Forwarded-For`'a yazdığı girdi sayısına (hiçbiri yazmıyorsa `1`;
  0.48–0.49'da "gerçek vekil sayısı") ayarlanınca doğru okunur, yoksa tüm
  istemciler vekilin tek adresini paylaşıp ada-başına
  tek bir kilide geriler; (b) çok sayıda ayrı kaynaktan gelen dağıtık bir
  tahmin, gevşek hesap tavanını (`UPTIME_LOGIN_ACCOUNT_MAX_FAILURES`, 50)
  tetikleyip hesabı yine kilitleyebilir (kabul edilen taviz; tek kaynaktan ~15
  istek yerine en az beş kaynaktan ~50). Kilitli bir hesap için API anahtarı,
  SSO ya da backend'i yeniden başlatmak kalır (README, "Kilitli hesaba
  girmek"). Dizin erişilebilirlik metriği (S63) aynı `auth.go`/Throttle
  yüzeyine dokunduğu için S65'ten ayrılıp sprint 7'ye bırakıldı. 0.49.0
  (sprint 7) bu yüzeye iki madde ekledi: S72
  ([#106](https://github.com/yusufceng/Kubeat/issues/106)) `X-Forwarded-For`
  görülürken `UPTIME_TRUST_PROXY_HOPS=0` iken ya da zincir kısayken çalışma
  anında (dakika-sınırlı) WARN verir ve hops gerçek zincirden uzunsa görülen
  kaynağı güvenli adrese (peer) düşürür, paylaşılan NAT/vekil durumunu ve tavan
  ayarını ADR-0004/README'de açıklar; S63
  ([#108](https://github.com/yusufceng/Kubeat/issues/108)) giriş/dizin ve
  throttle/lockout'u enum sızdırmayan `reason`/`kind` etiketli metriklerle
  (`kubeat_signin_failures_total{reason}`, `kubeat_signin_throttled_total{kind}`)
  görünür kıldı; istemci yanıtı (401/429) değişmedi. **Arayüz yolu
  (0.48–0.49, bilinen sınır):** arayüz istemci adresini backend'e taşımıyordu.
  Route/Ingress arkasında giriş, parola değiştirme ve durum sayfası kilidi arayüz
  pod'unun adresine anahtarlanıyor, yani tarayıcıdan gelen girişte kaynak-başına
  throttle ad-başına tek paylaşılan kilide geriliyordu. hops=0 iken WARN
  düşmüyordu; hops=1 iken `xff-hops-too-high` WARN'ı düşüyor ama kaynak yine
  arayüz pod'u oluyordu. S78 ([#118](https://github.com/yusufceng/Kubeat/issues/118),
  0.50.0) arayüzün bu üç çağrıda gelen `X-Forwarded-For`'u aynen taşımasıyla
  düzeltir: hops=0 iken WARN arayüz yolunda da düşer, `sign-in refused` log
  satırı anahtarlanan kaynağı (`source`) yazar ve hop rehberi arayüz yoluna göre
  yeniden türetildi (README "Hesaplar ve giriş"; bu yolda ölçülen satırlar
  düz Route, sandbox 2026-11-07, ve `ingress-nginx` `1`, Calico'lu minikube, sprint
  9; ötekiler ölçülmedi). Chart varsayılanı 0 kaldı;
  kurulum notu Route/Ingress açıkken değeri önerir. Fazla hop değeri artık
  güvenli tarafa düşmez: başlığa ekleyen bir vekilin (Route, GCLB) arkasında
  istemci başlığı uzatıp kaynağını seçebilir ve WARN düşmez (0.49'da arayüz
  yolunda kaynak eşe düşüyordu); README ve kurulum notu emin olunmayan durumda
  küçük değeri önerir. S79
  ([#119](https://github.com/yusufceng/Kubeat/issues/119), 0.50.0) hops>0 iken
  `X-Forwarded-For`'u yalnız arayüzün sırrını (`X-Frontend-Token`; chart
  `<release>-frontend-token`) sunan istekte okur: backend'e doğrudan gelen pod
  uydurma başlıkla kaynak seçemez, kendi adresine anahtarlanır ve hesap
  tavanını dolduramaz; sır tanımsızsa açılışta WARN düşer ve başlığa hiçbir
  eşten güvenilmez (ADR-0004 "Güvenilir eş: arayüz", allowlist'in ilk dilimi).
  Kalanlar: isteğe bağlı arayüz politikası (`frontend.networkPolicy.enabled`,
  varsayılan kapalı) kapalıyken cluster içindeki bir pod arayüz Service'i
  üzerinden (arayüz `NodePort` ise node portuna node üzerinden gelen bir istemci
  de) uydurma `X-Forwarded-For` ile kaynak seçebiliyor; politikanın `ingress-nginx`
  seçicisi Calico'lu minikube'da ölçüldü (sprint 9), OpenShift router seçicileri
  ölçülmedi; cluster içi uydurma başlık provası sandbox'ta geçti (sprint 8). S76
  ([#134](https://github.com/yusufceng/Kubeat/issues/134), 0.51.0) parola
  değiştirme ve durum sayfası kilidini de kaynağa göre yapar: bir kaynağın
  yanlış denemeleri yalnız o kaynağı kilitler (`UPTIME_LOGIN_MAX_FAILURES`),
  sayfa ve hesap başına gevşek tavan girişinkidir
  (`UPTIME_LOGIN_ACCOUNT_MAX_FAILURES`, yeni ayar yok); değer `0` ya da
  eksikse veya sır eksikse bu iki yüzey de tek paylaşılan kilide geriler
  (eksik değer WARN vermez), fazla değerde girişteki gibi başlığını uzatan
  istemci kaynağını seçebilir; WARN yüzeyi adlandırır. Bu iki yüzeyin 429'ları
  yalnız dakikalık WARN'da görünür, sayacı yoktur (S85 dilim 1, sprint 10,
  [#146](https://github.com/yusufceng/Kubeat/issues/146)). Gerçek
  tarayıcı yolunda iki kaynaklı prova sandbox Route'unda geçti (sprint 9). Dizin
  (LDAP/AD) hesap kilidiyle etkileşim belgelendi (README "LDAP" → "Dizin hesap
  kilidi", ADR-0004): dizinde bulunan bir adın her yanlış parolası dizine bir
  bind'dir; ara önlem `UPTIME_LOGIN_MAX_FAILURES`'ı dizinin eşiğinin altında
  tutmak. S88 ([#145](https://github.com/yusufceng/Kubeat/issues/145), 0.52.0)
  bir kaynağın bütün adlardaki hatalı girişlerine opt-in bir tavan ekler
  (`UPTIME_LOGIN_SOURCE_MAX_FAILURES`, `kind="source_total"`): varsayılan `0`
  kapalıdır, çünkü NAT ya da PAC vekili arkasındaki kullanıcılar tek kaynaktır;
  LDAP tanımlıyken ayar verilmemişse açılışta `WARN`, açıkça `0` ise `INFO`
  düşer. Kalanlar: tavan kapalıyken tek kaynaktan sprey sürer, çok kaynağa
  yayılan sprey (IPv6'da tek `/64` dahil) kaynak tavanını aşmadan sürebilir,
  README'nin önerdiği değer gerçek bir AD/OIDC dizininde ölçülmedi (sprint 10
  "Kullanıcıda kalanlar"); kapsam yalnız giriştir. Paylaşılan kurumsal NAT ayrımı
  backlog'da ([backlog](backlog.md) "Giriş throttle için güvenilir-vekil IP
  allowlist").
- `/metrics` monitor adlarını, namespace'lerini, sertifika bitişlerini ve
  bildirim kanallarının adlarını verir ve kullanıcı rollerinin namespace
  sınırını tanımaz; bu yüzden
  `UPTIME_METRICS_TOKEN` ile ayrı bir anahtar ister (chart onu varsayılan
  olarak üretir). Anahtar ayarlanmazsa açıktır ve backend'e kimlerin
  ulaşabileceğini NetworkPolicy belirler. NetworkPolicy `/metrics`'i push ve
  API'den ayıramaz: üçü aynı porttadır.
- Yayımlanmış bir durum sayfasının her görüntülenmesi tüm monitor'ların
  durumunu okur; önbellek yoktur. Çok ziyaret edilen bir sayfa için önüne
  bir önbellek konmalıdır. Okuma havuzunda bağlantı sınırı da yoktur: çok
  sayıda eşzamanlı istek backend'i bellek limitine itebilir (S21,
  [backlog](backlog.md) "Okuma havuzuna bağlantı sınırı ve durum sayfasına
  eşzamanlılık sınırı"). Sayfa izlenen cluster ve Kubeat ile birlikte düşer;
  Kubeat yükseltilirken ya da backend pod'u taşınırken bir iki dakika
  okunamaz, rozetler 502 verir (README "Durum sayfaları" → "Sınırlar"; son
  görünümü önbellekten göstermek [backlog](backlog.md)'ta).
- Gizli değerler `UPTIME_SECRET_KEY` ile şifrelenir; anahtar değiştirme
  (rotation) yoktur ve push token'ları şifrelenmez (S58, [backlog](backlog.md)
  "Şifreleme anahtarını döndürmek ve anahtar sızınca izlenecek yol").
- TLS router'da ya da Ingress controller'da biter: router → frontend ve
  frontend → backend trafiği pod ağında düz HTTP'dir. LDAP parolası bu
  yoldan geçer; OVN-Kubernetes IPsec açık değilse pod ağında şifresizdir.
  Route yalnızca edge'dir, reencrypt seçeneği yoktur (S59,
  [backlog](backlog.md) "Route'ta reencrypt ve arayüz ile backend arasında
  cluster içi TLS").
- Kontroller backend pod'undan çıkar: izlenen namespace'te gelen trafiği
  varsayılan olarak reddeden bir NetworkPolicy varsa (OpenShift proje
  şablonunun `default-deny`'ı) Service, `each_pod`, TCP, gRPC, veritabanı ve
  WebSocket kontrolleri `connect_timeout` ile düşer ve yanlış kesinti açar;
  Route/Ingress (router'dan geçer) ve `workload` (cluster API'si)
  etkilenmez, `ping` ve `dns` ağ eklentisine bağlıdır. Kubeat hedef
  namespace'in politikasını okuyup uyarmaz; çaresi izlenen namespace'e
  Kubeat'in backend pod'larına izin veren `allow-from-kubeat` politikası,
  proje şablonuyla ya da cluster çapında AdminNetworkPolicy ile (S51, README
  "Ağ erişimi", İzlenen namespace'ler; gerçek bir varsayılan-ret
  namespace'te koşulmadı, yukarıda "Hâlâ doğrulanmayanlar").
- Hedef politikası yalnızca loopback, link-local (bulut metadata servisi) ve
  multicast adreslerini kapatır; özel ağlar bilerek açıktır. 0.47.0'da
  kullanıcı başına kapsam eklendi (ADR-0003 c, S64,
  [#84](https://github.com/yusufceng/Kubeat/issues/84),
  [sprints/2026-11-04-sprint-05.md](sprints/2026-11-04-sprint-05.md)): kapsamı
  sınırlı bir kullanıcı artık yalnızca görebildiği namespace'lerin cluster içi
  adreslerine (`servis.namespace.svc`) monitor ya da kanal webhook'u kurabilir;
  API sunucusu, başka ekiplerin Service'leri ve namespace'i okunamayan çıplak
  ClusterIP/pod IP'leri reddedilir. Kalan sınır: her namespace'i gören bir
  kullanıcı (yönetici) her cluster içi adrese ulaşabilir ve deny-CIDR,
  ClusterIP aralığını paylaştığı için bu kullanıcılar arasında cross-tenant'ı
  ayıramaz — hassas aralıkları yine `UPTIME_TARGET_DENY_CIDRS` daraltır.
  0.47'nin iki etiketli kısa ad boşluğu 0.48.0'da kapatıldı (S67,
  [#95](https://github.com/yusufceng/Kubeat/issues/95),
  [sprints/2026-11-05-sprint-06.md](sprints/2026-11-05-sprint-06.md)): kapsam
  denetimi artık adı dış görünen bir hedefi de çözer (çözüm/bağlantı anında,
  dial-time) ve çözülen adres cluster aralığındaysa sınırlı kullanıcı için
  reddeder; iki etiketli kısa ad (`kubernetes.default`, `api.payments`) ve iç
  adrese çözülen genel ad (`172-30-0-1.sslip.io`) de kapanır. Kalan: adı çözümle
  bağlantı arasında değişen hedef (DNS rebinding, TOCTOU) bu kapının dışındadır
  (0.51'de kapsamı sınırlı monitor'lerin doğrudan bağlantılarında kapandı, (d))
  ve çok kiracılı prova kullanıcıdadır — deny-CIDR ve prova derinlemesine
  savunmadır. Vekil (`HTTP_PROXY`) üzerinden giden istekte hedef backend'de
  çözülüp denetlenir, ama vekil adı yeniden çözer; vekilin nereye ulaştığı onun
  ağına bağlıdır. 0.49.0 (sprint 7) bu yüzeye iki madde ekledi: S77
  ([#107](https://github.com/yusufceng/Kubeat/issues/107)) kapsam denetimini
  DNS/çözüm hatasında sınırlı kullanıcı için güvenli tarafa alır (reddeder),
  çözüme ayrı zaman aşımı koyar ve iç-adres oracle'ını daraltır; S68
  ([#109](https://github.com/yusufceng/Kubeat/issues/109)) kapsam/yetki dışı
  hedef reddini bir denetim satırı (`deny`/`target-scope`) ve `reason` etiketli
  sayaçla görünür kılar. Kalan sınırlar: (a) kapsamı sınırlı kullanıcı için
  kurum ağına (özel aralıklar: 10/8, 172.16/12, 192.168/16, 100.64/10) çözülen
  ad ve özel adrese çözülen on-prem Route adı varsayılan olarak cluster içi
  sayılıp reddediliyor (0.48'den beri; çıplak kurum IP'si 0.47'den beri);
  0.51'de opt-in `UPTIME_TARGET_SCOPE_EXTERNAL_CIDRS` operatörün verdiği kurum
  aralıklarını (çıplak IP ve çözülen ad) kapsam için cluster dışı sayıyor (S80,
  sprint 9, [#132](https://github.com/yusufceng/Kubeat/issues/132)). API
  sunucusunun ya da backend'in kendi adresini içeren aralık backend'i
  başlatmıyor; koruma pod'un o açılıştaki adresine baktığı için pod başka bir
  node'a taşınınca (drain) ilk açılışta geçen bir aralık backend'i
  başlatmayabilir; ortama bağlı denetimlerde ayarı yok sayıp izlemeyi sürdüren
  geri düşüş sprint 11 adayı (S96; S80'in "backend başlamaz" kararı bu
  denetimler için değişti, biçim hataları yine başlatmaz); pod ve Service ağının geri kalanı, node ağı ve API VIP'iyle
  çakışma operatörün sorumluluğunda, router VIP'i yalnız bilerek (node
  ağındaysa `/32` ile) giriyor ve o VIP'teki her Route'u açıyor. Keşiften
  içe aktarma yalnız namespace'e baktığı için aralık Route'un VIP'ini
  içermiyorsa bu hedefi yine alıyor, klonu ve hedef
  değişikliği reddediliyor; minikube provası geçti (sprint 9, arayüz formu); (b) vekilli kurulumda yerelde NXDOMAIN alan dış ad 0.49'da
  reddediliyor; 0.50'de opt-in `UPTIME_TARGET_SCOPE_TRUST_PROXY` (yalnız vekil
  cluster dışındaysa) vekile giden HTTP/WebSocket monitor'ü ve webhook'ta
  NXDOMAIN'i reddetmiyor (S81 [#121](https://github.com/yusufceng/Kubeat/issues/121),
  sprint 8). Cluster içine çözülen ad, SERVFAIL ve zaman aşımı yine reddediliyor;
  cluster DNS'i dış adlara SERVFAIL ya da zaman aşımıyla yanıt veriyorsa ayar
  yardım etmiyor; ayar açıkken kapsamın sınırı vekilin ulaştığı yer (iç adrese
  işaret eden genel ad backend'de NXDOMAIN alıp vekile gidiyor), bu yüzden node
  IP'lerini, VIP'leri ve kurum aralıklarını vekilin ACL'i kapatmalı;
  minikube provası geçti (sprint 8), gerçek hava boşluklu
  cluster provası kullanıcıda; (c) denetim ve sayaç çözüm hatasını gerçek cross-tenant reddinden
  ayırmıyor ve denetim satırlarının sınırı yok (S82, sprint 10,
  [#142](https://github.com/yusufceng/Kubeat/issues/142),
  [#143](https://github.com/yusufceng/Kubeat/issues/143)); (d)
  0.51'de kapsam yalnız hedef belirlenirken değil, monitor'ün her bağlantısında
  uygulanıyor (S86, sprint 9, [#131](https://github.com/yusufceng/Kubeat/issues/131);
  ADR-0003 c "Her bağlantıda kapsam"): kayıt monitor'ün ayarlarında (şema 26),
  yönetici ve `*` etkilenmiyor, kaydı olmayan eski monitor namespace'ine
  bağlanıyor (kapsamı sınırlı editör yoksa her namespace'in kaydını alıyor),
  annotation monitor'ü her kurulumda nesnesinin namespace'ine bağlı, kayıt
  daha geniş bir kapsama yalnız kontrolün bağlandığı yeri adlandıran bir ayar
  değişince geçiyor, kaydın namespace'lerinin hepsini tutmayan biri kontrolün
  herhangi bir ayarını değiştirince onu daraltıyor, reddedilen kontrol
  DOWN ve `reason=connection` ile sayılıyor. Kalan: kayıt
  yazıldığı anın görüntüsü, kapsam daralınca var olan monitor'ler eski kapsamla
  çalışıyor (S73, backlog, sprint 11 adayı); kanallar yalnız oluşturma ve
  değiştirmede denetleniyor (backlog); vekile giden kontrolde çözüm hatası
  gerçek bir reddinden ayrılmıyor (S82, sprint 10); kaydı olmayan monitor'ün
  açılış kararı her açılışta yeniden veriliyor, bu yüzden kapsamı daraltan bir
  rol ya da grup değişikliğinden sonraki bir açılışta kayıt genişleyebilir
  (S91, sprint 11 adayı, S73 ile); minikube provası geçti (sprint 9), vekilli
  ve hava boşluklu kurumsal cluster provası kullanıcıda.
- Keşif sayfasından içe aktarma tek seferliktir; sürekli izleme için nesneye
  `kubeat.io/monitor` annotation'ı eklenir.
- Annotation eşitlemesi her turda Service, Pod, EndpointSlice, Ingress,
  Route ve workload listelerini (cluster geneli keşifte tüm cluster'ınkini)
  sayfalamadan belleğe alır. SRE'nin yerel sentetik ölçümüne göre
  varsayılan 256Mi bellek limiti 6–7 bin pod civarında aşılabilir;
  OOMKill'den sonraki açılış yine eşitlemeyle başladığı için döngü kurulur
  ve kontroller durur. Aralığı uzatmak belleği değiştirmez: büyük cluster'da
  `backend.resources.limits.memory` artırılmalı ya da keşif
  `discovery.namespaces` ile sınırlanmalıdır (S42, [backlog](backlog.md)
  "Keşif eşitlemesinde bellek sınırı ve OOMKill döngüsü (S42)").
- Annotation eşitlemesinin hatası yalnızca WARN olarak loglanır; metriği ve
  alarmı yoktur. Namespace sınırlı keşifte listedeki tek bir namespace
  okunamazsa (silinmiş ya da 403) bütün tur bırakılır: öteki
  namespace'lerde yeni annotation'lar izlenmez, kaldırılanların monitor'leri
  yerinde kalır (S43, sprint 10, [#147](https://github.com/yusufceng/Kubeat/issues/147);
  S42'den önce).
- Gateway API HTTPRoute okunmaz; keşif adayı ya da annotation hedefi
  değildir. Ingress'ten HTTPRoute'a geçerken Ingress silinince monitor
  geçmişiyle birlikte kalkar; geçişten önce elle monitor oluşturun (S49,
  [backlog](backlog.md) "Gateway API HTTPRoute keşfi ve annotation
  hedefi").
- Bakım sırasındaki başarısız kontroller durum sayfasında ve raporda
  uptime'a dahildir. Bakımda olay açılmadığından rapor CSV'sinde aynı
  kontroller `uptime_percent`'e girer, `downtime_seconds`'a girmez; SLA için
  `downtime_seconds` kullanılmalıdır (S45, [backlog](backlog.md) "Duyurulan
  bakımı durum sayfası ve rapor uptime'ından ayırmak").
- Raporlar son N günü UTC'de ve o anki saatle biten bir dönem için verir;
  takvim ayı alınamaz (S44, sprint 5 kuyruğu).
- Cluster API'ye monitor'ün başarısızlık eşiği kadar kontrol boyunca
  ulaşılamazsa `workload` ve `each_pod` monitor'leri kesinti açar ve
  bildirim gönderir; API kesintisi hedefin kendi hatasından ayrılmaz (S20,
  [backlog](backlog.md) "Cluster API'ye ulaşılamayan kontrolü hedef hatası
  saymamak"). Zonal bir GKE cluster'ında control plane yükseltmesi buna yol
  açabilir; önlemi README "Cluster'a kurulum"daki GKE notunda.
- Bildirim kanalları birbirinden bağımsız teslim edilir; başarısız kesinti
  bildirimi olay kapanana kadar yeniden denenir, düzelme (`up`) bildirimi
  en çok `UPTIME_NOTIFY_RETRY_FOR` boyunca. Bekleme hatanın sınıfına
  bağlıdır (README "Alarm", hata sınıfları): ulaşılamayan kanal 1, 2, 4…
  dk (üst sınır `UPTIME_NOTIFY_RETRY_MAX`), reddeden (4xx) kanal doğrudan
  `UPTIME_NOTIFY_RETRY_MAX`, meşgul (429) kanal `Retry-After` kadar
  bekletilir; meşgul ve reddeden alıcıya kısa tekrar yapılmaz. 4xx dönen
  kanal canlıdır ve duraklamaz: öteki bildirimler beklemeden gider; kanal
  düzeltilince reddedilmiş bildirim sıradaki denemesini (en çok
  `UPTIME_NOTIFY_RETRY_MAX`) bekler; başarılı bir kanal testi ya da kanalı
  kaydetmek bu beklemeyi öne alır (bekleyenler kanalın sıradaki geçişinde
  gider). Bunun iki bedeli var: geçici bir 4xx (alıcı yeniden yayımlanırken 404) de
  reddetme sayılır ve o bildirimi `UPTIME_NOTIFY_RETRY_MAX` kadar
  geciktirir; her şeyi reddeden bir makine kanalı (süresi dolmuş token,
  kaldırılmış alıcı) bekleyen her bildirim için `UPTIME_NOTIFY_RETRY_MAX`'ta
  bir istek alır. Birlikte reddedilenler tek geçişte sırayla gider ve
  teslimat döngüsünü istek sayısı × yanıt süresi kadar tutar (500 kesinti,
  300 ms: her 15 dakikada ~2,5 dk); bu sırada kuyruk okunmaz. Çare kanalı
  düzeltmek ya da kapatmaktır, hangi kanal olduğunu
  `KubeatNotificationsFailing` söyler ([backlog](backlog.md) "Teslimat hata
  sınıflarının kalanları").
  Ulaşılamayan ya da meşgul bir kanal, makine (webhook, PagerDuty) ya da
  insan kanalı (Slack, e-posta…), yeni bildirimleri ve grupları kendi
  yeniden deneme zamanına bekletir, gruplama kapalıyken
  (`UPTIME_NOTIFY_GROUP_WAIT=0`) de: kanal dönünce yeni mesajı en çok o
  anki bekleme süresi kadar (üst sınır `UPTIME_NOTIFY_RETRY_MAX`) gecikir.
  Başarılı bir kanal testi ya da kanalı kaydetmek bu duraklatmayı kaldırır
  ve bekleyenleri sıradaki geçişte öne alır. İnsanların okuduğu kanalların
  üst sınırı `UPTIME_NOTIFY_HUMAN_RETRY_MAX` ile ayrıca kısaltılabilir
  (varsayılan `0` = `UPTIME_NOTIFY_RETRY_MAX`; webhook ve PagerDuty hep
  `UPTIME_NOTIFY_RETRY_MAX`'ta kalır).
  Yeniden denenmeyen bir bildirimin (hatırlatma, sertifika, yavaşlık)
  ilk denemesinde ulaşamadığı kanal, yeniden denenecek başka bir bildirimi
  yoksa bekletilmez: birden çok namespace'te aynı anda gelen böyle grupların
  her biri ona denenir ve döngüyü bir deneme süresi tutar ([backlog](backlog.md)
  "Teslimat hata sınıflarının kalanları"). Tek bir monitor'un
  bildirimlerine sürekli 5xx dönen bir alıcı kanalı her geçişte yeniden
  duraklatır, yeni bildirimler en çok `UPTIME_NOTIFY_RETRY_MAX` gecikir.
  `Retry-After` yalnızca başlıktan okunur (saniye ya da HTTP tarihi);
  gövdedeki `retry_after` alanları okunmaz, sağlayıcı başına istemci
  tarafı hız sınırlayıcı yoktur. SMTP'nin kalıcı yanıtları (5xx SMTP
  kodları) ulaşılamadı sayılır.
  Deneme listesi bellektedir: yeniden
  başlatmada açık kesintininki, hiç denenmemiş olanlar (kuyrukta ya da grup
  beklemesinde kalanlar) dahil, bildirim kaydından kurulur (kaydı
  `UPTIME_DELIVERY_RETENTION` ile silinmiş açık kesinti bir kez daha
  gider); bekleyen düzelme bildirimi düşer, kanal almadan yeni bir kesinti
  açılırsa da düşer. Yeniden başlatmada monitor o anda bakımdaysa teslim
  edilememiş kesinti yeniden gönderilmez: bakım penceresi onu, gecikmesi
  bakımda dolan bir kesintiyi sustururken olduğu gibi susturur (gece
  bildirimi gitmez).
  Kapanışta insan kanallarının grupları kuyruktan önce gider ve kapanışın
  tümü `UPTIME_SHUTDOWN_TIMEOUT` ile sınırlıdır; yetişmeyen düzelme
  bildirimi kaybolur. Teslim edilemeyen bildirimlerin ayrı bir görünümü
  (dead-letter) yoktur; bildirim kaydı,
  kanal başına `kubeat_channel_last_delivery_ok` ve
  `kubeat_channel_undelivered_outages` (chart'ın kanal başına
  `KubeatNotificationsFailing` alarmı bunlara bakar),
  `kubeat_notification_attempts_total{result="failed"}` ve
  `kubeat_notifications_total{result="failed"}` gösterir ([backlog](backlog.md)
  "Teslim edilemeyen bildirimler için dead-letter görünümü ve kalıcı
  yeniden deneme kuyruğu").
- Bildirim sağlayıcıları gerçek servislere karşı değil, isteği kaydeden
  yerel sunuculara karşı denendi; gövdeler sağlayıcıların belgelerine göre
  yazıldı.
- PagerDuty'nin `dedup_key`'i kurulum kimliğini taşır (`UPTIME_INSTANCE_NAME`,
  boşsa `UPTIME_PUBLIC_URL`'ün host'u) ama kayda yazılmaz, her gönderimde
  kimlikten kurulur: kimliği ya da `publicURL`'ü değiştirmeden önce, kimliksiz
  bir sürüme dönmeden önce de, açık PagerDuty olaylarını kapatın; eski
  anahtarla açılmış alarm Kubeat'ten kapanmaz. Kimliği olmayan kurulumların
  anahtarı eskisi gibidir (`kubeat-incident-<n>`): aynı servisi paylaşan
  kimliksiz kurulumların aynı numaralı olayları yine tek alarmda birleşir;
  0.45 ve öncesindeki kurulumlar da kimliksizdir, ortak servisteki filo
  0.46'ya geçip her kurulum kimlik alana kadar ayrı servis kullanın
  (S28, #61; README "Bildirim kanalları", **Kurulum kimliği**).
- Chart'ın alarmları backend'i cluster'ın Prometheus'u scrape edebildiği
  sürece görür: Kubeat 10 dakikadan uzun düşerse kesinti, sertifika ve yedek
  alarmları çözülür. README'deki susturma kuralı, alarmları alan
  Alertmanager'a eklenirse (OpenShift'in ve kube-prometheus-stack'in
  varsayılanında yoktur), bunları `KubeatDown` yanarken susturur; bedeli,
  Kubeat 25 dakikadan uzun düşükken biten bir kesintinin düzelmesinin
  Alertmanager'dan gitmeyebilmesidir (Kubeat'in kendi kanalları bildirir).
  Cluster'ın tamamı düşerse yalnızca dışarıdaki heartbeat haber verir
  (README, "Aracın kendi sağlığı").
- Ping, çekirdeğin yetkisiz ICMP soketine izin vermesine bağlıdır; minikube'da
  ve gerçek RHCOS'ta ek ayar gerektirmeden çalıştı. Bir Service adına ping
  atılamaz: ClusterIP sanal bir adrestir ve ICMP'ye yanıt vermez; pod IP'si
  ya da cluster dışı bir adres verilmelidir.
