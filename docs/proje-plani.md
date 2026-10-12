# OCP Uptime Monitoring Projesi: Başlangıç ve Sprint Planı

Oct 12, 2026 · @Yusuf Coşkun

Proje, OCP/Kubernetes ortamlarındaki servisleri izleyen, asıl görevi uptime monitoring olan, Go + Next.js + SQLite ile yazılacak bir araçtır. Önerilen plan: 1 keşif sprinti, 3 sprintte kullanılabilir bir MVP ve toplam 9 sprintte (2 haftalık sprintlerle yaklaşık 18 hafta) canlıya çıkış.

En kritik mimari karar: metrikleri sıfırdan toplamak yerine OCP'nin hazır Prometheus/Thanos altyapısından okumak, projenin asıl emeğini uptime kontrollerine ve güvenilir alarmlara vermek.

## Takım ve çalışma düzeni

Bu boyutta bir proje için 6–8 kişilik bir takım yeterlidir.

| Rol | Kişi | Sorumluluk |
| --- | --- | --- |
| Product Owner | 1 | Öncelikleri belirler; tercihen SRE/platform ekibinden |
| Tech lead / Scrum Master | 1 | Mimari kararlar, ADR'ler, toplantı ritmi |
| Go backend geliştirici | 2 | Scheduler, checker, alarm motoru, API, agent |
| Next.js frontend geliştirici | 1–2 | Dashboard'lar, alarm ve monitor ekranları |
| DevOps / SRE | 1 | OCP, Helm, CI/CD, RBAC, SCC |
| QA | 0,5 | Test senaryoları, yük testi, UAT |

Sprintler 2 hafta sürer. Her sprintte planning, daily, sprint ortasında backlog refinement, sprint sonunda review (demo) ve retrospective yapılır.

İşe başlamadan iki tanım yazılı olarak kabul edilmeli:

- **Definition of Ready:** Kabul kriterleri yazılmış, bağımlılıkları belli, takım tarafından tahminlenmiş iş sprint'e alınabilir.
- **Definition of Done:** Kod review'dan geçti, test yazıldı, CI yeşil, dev ortamına deploy edildi, dokümante edildi.

## Sprint 0: kod yazmadan önce

İlk 1–2 hafta keşif sprintidir; sonunda aşağıdaki çıktıların hepsi hazır olmalı.

- [ ] **Vizyon dokümanı (one-pager):** Problem, kullanıcılar (SRE, uygulama ekipleri, NOC) ve başarı ölçütü. Örnek: bir servis düştüğünde 1 dakika içinde doğru ekibe bildirim gitmesi.
- [ ] **Kapsam ve MVP tanımı:** İlk sürümde ne olacak, ne olmayacak. MVP = uptime + alarm; dashboard'lar sonra.
- [ ] **Fonksiyonel olmayan gereksinimler:** Cluster ve endpoint sayısı, check sıklığı (30 sn / 1 dk), veri saklama süresi, aracın kendi HA ihtiyacı, air-gapped ortam, kimlik doğrulama (LDAP/OIDC).
- [ ] **ADR'ler:** Bir sonraki bölümdeki her araştırma konusu için bir karar kaydı.
- [ ] **Mimari çizimler:** C4 diyagramları, sequence diyagramları, ERD, deployment diyagramı.
- [ ] **Risk listesi ve ilk backlog:** Epic'ler ve kullanıcı hikayeleri.
- [ ] **Walking skeleton:** Go API'nin health endpoint'i Next.js'te görünüyor, CI/CD ile OCP'ye deploy ediliyor.

## Araştırılması gerekenler

Bu konular projenin kaderini belirler; her biri bir spike ile araştırılıp bir ADR (Architecture Decision Record) olarak yazılmalı.

### Tekerleği yeniden icat etmemek

OCP; cluster monitoring ve user workload monitoring ile hazır Prometheus + Thanos Querier sunar. Metrikleri kendimiz toplayıp saklamak yerine Thanos Querier API'sinden PromQL ile okumak çok daha doğru. Ayrıca Uptime Kuma, Blackbox Exporter ve Gatus incelenip "bizim aracımız neden farklı" sorusu cevaplanmalı.

### SQLite'ın sınırları

SQLite tek yazıcı (single writer) ile çalışır. 500 endpoint 30 saniyede bir kontrol edilirse günde yaklaşık 1,4 milyon satır oluşur. Gerekenler:

- WAL modu ve tek bir writer goroutine ile batch insert
- Retention: ham veri 7 gün, dakikalık özet 30 gün, saatlik özet 1 yıl (örnek değerler)
- Yedekleme için Litestream benzeri bir araç
- Tek replica + PVC; veri katmanı bir repository interface arkasında, ileride PostgreSQL'e geçiş kolay olsun

### Merkezi bağlantı mı, agent mı?

Birden fazla cluster ve ayrık ağlar varsa, her cluster'a hafif bir Go agent koyup sonuçları merkeze push etmek, merkezden kubeconfig ile bağlanmaktan daha güvenli ve sağlamdır.

### OCP'ye özgü konular

- Route nesneleri (`route.openshift.io`)
- SCC kısıtları: container root olmadan ve rastgele UID ile çalışabilmeli
- ServiceAccount + ClusterRole: pods, services, routes, endpoints için get/list/watch
- Thanos Querier'a bearer token ile erişim

### Alarm mantığı

Flapping'e karşı "art arda N başarısız kontrol" kuralı, cluster içinden ve dışından doğrulama, tekilleştirme (dedup), bakım pencereleri, susturma ve eskalasyon. Yanlış alarm, hiç alarm olmamasından daha hızlı güven kaybettirir.

### Frontend

Next.js App Router; büyük zaman serileri için ECharts veya uPlot (Recharts büyük veride zorlanır); canlı durum için SSE veya WebSocket.

## Mimari ve çizilmesi gerekenler

Önerilen mimaride her cluster'daki agent sonuçları merkezi Go backend'ine gönderir; backend metrikleri Thanos'tan okur, alarmları bildirim kanallarına iletir.

&#91;embedded content: Container seviyesinde önerilen mimari\]

Scheduler check'leri planlar, checker pool çalıştırır ve sonuçları SQLite'a yazar; alarm motoru bu sonuçlardan olay (incident) üretir.

Sprint 0'da çizilmesi gerekenler:

- **C4 modeli:** Context (sistem ve çevresi), Container (backend, frontend, agent, DB), kritik parçalar için Component
- **Sequence diyagramları:** Bir check'in ve bir alarmın yaşam döngüsü
- **ERD:** Monitor, check sonucu, olay, alarm kuralı, bildirim kanalı tabloları
- **Deployment diyagramı:** Namespace, PVC, Route, ServiceAccount
- **OpenAPI dokümanı:** Backend–frontend API sözleşmesi

Repo yapısı olarak monorepo önerilir:

```
backend/
  cmd/server/
  cmd/agent/
  internal/checker/
  internal/scheduler/
  internal/store/
  internal/alert/
  internal/k8s/
frontend/
deploy/helm/
docs/adr/
```

SQLite sürücüsü olarak CGO gerektirmeyen `modernc.org/sqlite` seçilirse UBI tabanlı minimal image üretmek kolaylaşır.

## Sık düşülen tuzaklar

- **İzleyeni kim izleyecek?** Araç kendi `/metrics` endpoint'ini açmalı ve dışarıdaki bir dead man's switch'e düzenli heartbeat göndermeli; araç çökerse de haber gitmeli.
- **Her monitor için ayrı goroutine:** Küçük ölçekte çalışır, büyüdükçe sorun çıkarır. Scheduler + sınırlı worker pool + her check'e context timeout daha sağlamdır.
- **Jitter eksikliği:** Check zamanlarına jitter eklenmezse yüzlerce check aynı saniyede çalışır.
- **Dashboard'a erken odaklanmak:** Asıl değer güvenilir alarmdadır; öncelik hep orada olmalı.

Sonraki adım: Sprint 0 için ADR şablonları, SQLite veri modeli ve Sprint 1 user story'leri.
