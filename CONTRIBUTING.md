# Katkı Rehberi

## Branch stratejisi

Trunk-based, kısa ömürlü branch'ler (GitHub Flow).

- `main` her zaman yeşil ve deploy edilebilir durumdadır. Doğrudan push yapılmaz;
  her değişiklik PR ile girer.
- Her iş `main`'den açılan kısa ömürlü bir branch'te yapılır ve en geç birkaç gün
  içinde merge edilir. Uzun yaşayan `develop` branch'i yoktur.
- Yarım kalan özellikler uzun branch'te bekletilmez; küçük parçalar halinde
  merge edilir, gerekirse feature flag arkasına alınır.

### Branch isimleri

`<tip>/<kısa-açıklama>`, issue varsa `<tip>/<issue-no>-<kısa-açıklama>`.

| Tip | Kullanım | Örnek |
| --- | --- | --- |
| `feat/` | Yeni özellik | `feat/42-http-checker` |
| `fix/` | Hata düzeltme | `fix/57-scheduler-jitter` |
| `chore/` | Build, CI, bağımlılık | `chore/ci-go-cache` |
| `docs/` | Doküman, ADR | `docs/adr-0003-sqlite` |
| `refactor/` | Davranış değiştirmeyen düzenleme | `refactor/store-interface` |
| `spike/` | Araştırma; merge edilmez, çıktısı ADR olur | `spike/thanos-querier` |
| `hotfix/` | Yayınlanmış sürüme acil düzeltme | `hotfix/v1.2.1-alert-dedup` |

### Merge

- PR, CI'daki beş check (`backend`, `frontend`, `chart`, `chart (Helm 3)`,
  `govulncheck`) yeşil olmadan merge edilemez. Bunu deponun `main` kural seti
  zorunlu kılar; yönetici dahil kimse kırmızıyla merge edemez, `main`'e
  doğrudan push, force push ve silme reddedilir. Kararsız bir testin
  kırmızısı da kırmızıdır: test düzeltilir, yeniden koşturup geçirmek
  yetmez. Tek bakımcılı depoda onay sayısı zorunlu değildir; inceleme
  sprint ekibinin SRE ya da BA'sı tarafından yapılır
  ([docs/agents/sprint-takimi.md](docs/agents/sprint-takimi.md)).
- Squash merge kullanılır; `main` geçmişi lineer kalır. PR başlığı squash
  commit mesajı olur, bu yüzden Conventional Commits formatındadır.
- Merge edilen branch silinir.

### Sürümler

`v*` etiketlerini yalnızca depo yöneticisi oluşturabilir (etiket kural seti
"yalnız yönetici oluşturur", yönetici bypass'ıyla; push çıktısındaki
"Cannot create ref due to creations being restricted" uyarısı normaldir,
ref yine oluşur); bir etiket sonradan kimse tarafından taşınamaz ve
silinemez (ikinci kural seti "taşınmaz ve silinemez", bypass'sız). Kural
seti `main`'i ve `release/v*` dallarını aynı biçimde korur.

Deponun "Immutable releases" ayarı (Settings → General) açık tutulur:
yayımlanmış bir GitHub Release'in adı, notu ve ekleri (SBOM'lar) sonradan
kimse tarafından değiştirilemez ve silinemez. Etiket değişmezliğiyle
birlikte bu, bir sürümün yayımlandıktan sonra olduğu gibi kaldığını söyler;
yanlış bir sürüm geri alınmaz, yalnızca ileriye yeni bir yama sürümüyle
düzeltilir (0.46.1 gibi). Ayarı yalnızca depo yöneticisi değiştirebilir.

- Sürümler `main` üzerinde `vMAJOR.MINOR.PATCH` etiketiyle (SemVer) çıkar:
  `make release` etiketi `Makefile`'daki `VERSION` ile atar ve iter,
  [release.yml](.github/workflows/release.yml) imajları ve chart'ı imzalı ve
  SBOM'lu yayımlar ve etiketin GitHub Release'ini açar ("Yayım iş akışı").
  Etiket, CI'ın o commit'teki push koşusu bitince atılır: yayımın kapısı
  CI'ı yeşil olmayan commit'i yayımlamaz.
- Yayınlanmış bir sürüme düzeltme gerekirse o etiketten `release/vX.Y` açılır,
  `hotfix/` branch'i oraya merge edilir ve düzeltme `main`'e de taşınır.
  `make release` bu daldan da etiketler; bu yalnızca v0.45.0 ve sonrasından
  açılan dallarda geçerlidir, çünkü etiket push'u etiketlenen commit'teki
  release.yml'i koşar ve öncekiler imzalamaz. Bu bir yetenektir, destek sözü
  değildir: güvenlik düzeltmesini yalnızca son minor alır
  ([SECURITY.md](SECURITY.md)); eski bir minor'a yama ayrı bir kararla çıkar.
  Böyle bir yamanın GitHub Release'i deponun "Latest"i olmaz.
- SECURITY.md'deki bildirim yolu deponun "Private vulnerability reporting"
  ayarıyla açılır (Settings → Code security).

#### Sürüm yükseltme listesi

Sürüm PR'ı sürümü şu yerlerin hepsinde birlikte yükseltir:

- `deploy/helm/kubeat/Chart.yaml`: `version` ve `appVersion` (imaj etiketinin
  varsayılanı `appVersion`'dır).
- `Makefile`: `VERSION` (`make release` etiketi bundan atar).
- `README.md`: "Hızlı başlangıç"taki kurulum ve heartbeat komutlarının,
  "Cluster'a kurulum", "OpenShift" ve "Aracın kendi sağlığı"ndaki (heartbeat)
  komutların `--version`'ı.
- `docs/pilot.md`: "İmajları aynalamak" (imzalı yolda `oras cp -r`'ın iki
  imaj ve chart etiketi, `cosign verify` kimliğindeki `refs/tags/v<sürüm>`;
  imzasız yolun notunda iki imaj etiketi, `helm pull` `--version`'ı,
  `kubeat-<sürüm>.tgz`) ve "Kurulum" komutu.

Yükseltme notları [docs/yukseltme.md](docs/yukseltme.md)'dedir; README'de
yalnızca ona bağlantı vardır. Sürüm PR'ı sprint dosyasının "Yükseltme
notu" tablosundan yeni sürümün bölümünü yazar:

- Başlık `## <sürüm>'e yükseltme` biçimindedir, Türkçe ekiyle (`0.45'e`,
  `0.46'ya`); bölüm atlamalı yolun altına, öteki sürümlerin üstüne girer.
- Dört alt başlığı vardır: `### Yapılacak`, `### Davranış değişikliği`,
  `### Geri dönüş` ve `### Alarm kuralları: değişti` ya da
  `### Alarm kuralları: değişmedi` (chart'ın PrometheusRule'unun bir önceki
  sürüme göre gerçek diff'i).
- Başlangıçtaki "Atlamalı yol" yeni sürüme göre güncellenir.
- Dosya sürüm denetiminin dışındadır: eski sürümlerin komutları bilerek
  eskidir.

`scripts/check-versions.sh` README ve docs/pilot.md'deki sürümleri
`Chart.yaml`'daki `version` ile karşılaştırır ve farklı olan her yeri
`dosya:satır` ile yazar:

- Yalnızca komut kalıplarına bakar (`--version X.Y.Z`,
  `kubeat-backend:X.Y.Z`, `kubeat-frontend:X.Y.Z`, `kubeat-X.Y.Z.tgz`,
  `charts/kubeat:X.Y.Z`, `release.yml@refs/tags/vX.Y.Z`).
  Düzyazıdaki sürüm anışları ve `<sürüm>` gibi yer tutucular denetlenmez;
  README'de bir başlık hiçbir şeyi denetimden çıkarmaz. Komutlardan biri
  başka bir dosyaya taşınırsa betikteki dosya listesi de güncellenir; hiç
  sürümlü komut kalmayan dosya da, sürümlü komutu kalmayan "Hızlı başlangıç"
  da denetimi düşürür.
- CI'ın chart işi her PR'da çalıştırır; sürüm PR'ında eksik kalan yer orada
  yakalanır. Yerelde `scripts/check-versions.sh` ya da `make lint`; betiğin
  kendi testi `scripts/test-check-versions.sh` (`make test`).
- release.yml etiketle birlikte ilk işinde (`versions`) çalıştırır ve
  etiketin `v` + `Chart.yaml`'daki `version` olmasını da ister. Uyuşmazlıkta
  sonraki işler koşmaz: imajlar ve chart itilmez.
- release.yml elle de çalıştırılabilir (`workflow_dispatch`): bu bir kuru
  denemedir ve sürümü `Chart.yaml`'dan alır ("Yayım iş akışı").

#### Yayım iş akışı

Etiket itilince [release.yml](.github/workflows/release.yml) şu işleri sırayla
koşar:

1. `versions`: önce sürüm denetimi (yukarıda), sonra yayım kapısı
   ([scripts/release-gate.sh](scripts/release-gate.sh)); biri geçmezse
   sonraki işler koşmaz ve hiçbir şey itilmez. Kapı iki şeye bakar:
   - Etiketlenen commit `main`'den ya da bir `release/v*` dalından
     erişilebilir.
   - CI'ın o commit'teki **push** koşusunda beş zorunlu check ("Merge")
     başarılıdır; commit'te birden çok push koşusu varsa her biri. Yalnızca
     `success` sayılır: atlanan (`skipped`) bir check kural setinde geçmiş
     sayılır, kapıda sayılmaz. PR koşusu sayılmaz: `main` kural setinde
     "strict" kapalı olduğundan squash commit'i PR'ın sınadığı ağaç
     değildir. Bu yüzden ci.yml `release/v*` dallarına push'ta da koşar.
   - CI sürüyorsa ya da koşu henüz başlamadıysa kapı 30 saniyede bir sorar,
     en çok 30 dakika bekler, sonra düşer. Başarısız bir check kapıyı hemen
     düşürür.
   - Kapı GitHub API'sini `gh api` ile okur; `actions: read` (CI'ın
     koşularını ve işlerini okumak için) yalnızca bu iştedir. Zorunlu
     check'lerin listesi betiktedir: kural seti değişirse betik ve bu
     dosya da değişir.
   - Kararı `scripts/test-release-gate.sh` sahte bir API'yle sınar (yeşil,
     kırmızı, atlanan check, sürüyor, PR koşusu, dalda değil; `make test`,
     CI'ın chart işi).
2. `images`: iki imajı yalnızca sürüm etiketiyle iter, `latest` itilmez.
   Index BuildKit'in SPDX SBOM'unu ve SLSA provenance'ını (`mode=max`)
   taşır; platform başına SBOM'lar iş akışı artefaktı olarak saklanır.
3. `chart`: chart'ı paketler ve iter; `helm push`'un yazdığı digest'i alır.
4. `sign`: iki imajın ve chart'ın digest'ini cosign ile keyless imzalar
   (GitHub OIDC, Sigstore). `id-token: write` yalnızca bu iştedir ve iş
   cosign'dan başka bir şey koşmaz: üçüncü taraf action yoktur, cosign sabit
   sürümüyle indirilir ve sağlama toplamı denetlenir.
5. `verify`: imzaları README "Tedarik zinciri"ndeki komutlarla ve yalnızca
   okuma izniyle doğrular; müşterinin hava boşluklu yolunu
   ([docs/pilot.md](docs/pilot.md) "İmajları aynalamak") her sürümde koşar:
   - Önce çevrimiçi.
   - İşin kendi registry servis konteynerine (`registry:2.8.3`,
     `localhost:5000`) iki imajı ve chart'ı digest'lerinden `oras cp -r`
     ile, sürüm etiketiyle aynalar; etiketin digest'i imzalanan digest
     olmalıdır.
   - Ağ kesikken `--trusted-root` ile: aynalanan referansları (etiket ve
     digest'le), sonra `cosign save` ile diske yazılan iki imajı ve chart'ı
     (`--local-image`). Ağ kesintisini bütün bağlantıları reddeden bir vekil
     sağlar; Go vekili localhost'a uygulamadığından yalnızca aynaya
     ulaşılır.
   - Ardından Release notunu `scripts/release-notes.sh` ile yazar; betiğin
     testi `scripts/test-release-notes.sh` (`make test`, CI'ın chart işi).
6. `release`: etiketin GitHub Release'ini açar: sürümün yükseltme notuna
   bağlantı (etiketteki `docs/yukseltme.md`'de sürümün bölümü), üç digest,
   doğrulama komutu ve backend ile frontend için SPDX SBOM'lar.
   `contents: write` yalnızca bu iştedir. Bağlantının çapası bölüm
   başlığından ("0.46'ya yükseltme" → `#046ya-yükseltme`) gelir; sürüm PR'ı
   `Chart.yaml`'ı yükseltirken minor'ın bu biçimdeki bölümünü de yazar, yoksa
   `scripts/test-release-notes.sh` düşer.

İş akışı düzeyindeki varsayılan izin en küçük ayrıcalık ilkesiyle yalnızca
`contents: read`'tir. Daha fazlası yalnızca gereken işte verilir:
`packages: write` paketleri iten işlerde (`images`, `chart`) ve `sign`'da;
`id-token: write` (keyless imza için) yalnızca `sign`'da; `contents: write`
(GitHub Release'i açmak için) yalnızca `release`'te; `actions: read` (CI'ın
koşularını okumak için) yalnızca `versions` (kapı) işinde. Paket iten işler
(`images`, `chart`, `sign`) ve bütün sonraki işler `versions` kapısına
`needs` ile bağlıdır: kapı düşmeden hiçbir şey itilmez.

Üçüncü taraf GitHub action'ları (release.yml, ci.yml, security.yml) ve temel
imajlar (backend/frontend Dockerfile'ları) taşınabilir bir etikete değil bir
digest'e (`@sha256:...`) sabitlenir; okunabilirlik için sürüm etiketi yorumda
ya da etiketle birlikte kalır. Dependabot bu digest'leri haftalık PR'larla
günceller ([.github/dependabot.yml](.github/dependabot.yml): `github-actions`
ve iki `docker` girdisi). release.yml'deki `registry` servis konteyneri de
digest'e sabitlenir ama Dependabot onu görmez; cosign ve ORAS gibi elle
yükseltilir ("Sabit araç sürümleri"). Böylece bir yayım tam olarak neyle
kurulduğunu söyler ve bir etiketin sessizce taşınması yayımı etkilemez.

Kapı düşerse hiçbir şey itilmemiştir:

- CI 30 dakikada bitmediyse CI bitince yayım koşusunda "Re-run failed jobs".
- CI kırmızıysa o commit yayımlanmaz. Etiket taşınmaz: düzeltme yeni bir
  commit'le ve yeni bir yama sürümüyle gelir.
- Commit `main`'de ya da `release/v*`'ta değilse etiket yanlış commit'e
  atılmıştır; doğru commit yeni bir sürüm numarasıyla etiketlenir.
- GitHub API'sine ulaşılamadıysa "Re-run failed jobs".

İmajlar ya da chart itildikten sonra bir iş kırılırsa etiket yeniden
yazılmaz. Geçici bir hatada kırılan iş "Re-run failed jobs" ile yeniden
koşturulur; aynı digest'i yeniden imzalamak ikinci bir imza ekler, zararı
yoktur. Release yarım açıldıysa önce silinir. Hata iş akışının kendisindeyse
düzeltme yeni bir yama sürümüyle çıkar.

Kuru deneme (`gh workflow run release.yml --ref <dal>`) aynı adımları yayım
paketlerine dokunmadan dener:

- Kapı dalı denetlemez; CI'ın dalın HEAD'indeki koşusuna bakar. Bu, dalın
  PR koşusudur: kuru denemeden önce PR açılır, CI sürüyorsa kapı bekler.
- İmajlar ve chart kendi paketlerine itilir: `ghcr.io/yusufceng/kubeat-dry-run/`
  altında `kubeat-backend`, `kubeat-frontend` ve `charts/kubeat`; etiketleri
  `Chart.yaml`'daki sürümdür. Yayım paketlerine, sürüm etiketlerine ve
  `latest`'e dokunulmaz, GitHub Release açılmaz.
- İmza ve doğrulama gerçektir. Sertifikadaki kimlik
  `.../release.yml@refs/heads/<dal>`'dır.
- Release notu ve SBOM'lar koşunun özetinde ve `release-<sürüm>`
  artefaktındadır (`gh run download <koşu> -n release-<sürüm>`).
- Deneme paketleri ilk itişte özel (private) oluşabilir; onları yerelde
  doğrulamak için `ghcr.io`'ya giriş gerekir. Denemeden sonra üç paket de
  silinir (GitHub → Packages → paket → Package settings).
- GitHub elle çalıştırmayı yalnızca varsayılan dalda tanımlı bir iş akışında
  açar; `--ref` verilen dalın iş akışı o dalda koşar.

#### Sabit araç sürümleri

| Araç | Sürüm | Nerede |
| --- | --- | --- |
| Helm | v4.3.1 | ci.yml chart işi ("chart") ve release.yml chart işi; `azure/setup-helm`'in `version`'ı |
| Helm 3 | v3.22.1 | ci.yml chart işinin ikinci koşusu ("chart (Helm 3)") |
| cosign | v3.1.4 | release.yml `sign` ve `verify` işleri; `COSIGN_VERSION` ve `COSIGN_SHA256` |
| ORAS | v1.3.6 | release.yml `verify` işi; `ORAS_VERSION` ve `ORAS_SHA256` |
| registry (Distribution) | 2.8.3 | release.yml `verify` işinin servis konteyneri, `registry:2.8.3@sha256:...`; etiket ve digest'le sabit |
| govulncheck | v1.9.0 | ci.yml `govulncheck` işi ve security.yml; `golang.org/x/vuln/cmd/govulncheck@<sürüm>` |

Helm'i yükseltmek:

- **Helm 4.** ci.yml'daki `chart` satırını ve release.yml'daki `version`'ı
  aynı PR'da, birlikte yükseltin; chart işi yeni sürümle `test-chart.sh`'ı
  geçmeden release.yml'a girmez. Bu, sürüm PR'ından ayrı bir `chore(ci)`
  PR'ıdır. Dependabot `azure/setup-helm`'in kendi sürümünü yükseltir,
  kurduğu Helm'in `version`'ını değil: yeni Helm sürümlerine
  [helm/helm sürümlerinden](https://github.com/helm/helm/releases) elle
  bakılır.
- **Helm 3.** İkinci koşu Helm 3'ün en son yamasıyla koşar, çünkü GitOps
  araçları (Argo CD gibi) chart'ı kendi gömdükleri Helm ile render eder ve
  README `--reset-then-reuse-values` için Helm 3.14+ der. Yeni bir Helm 3
  yaması çıkınca `chart (Helm 3)` satırı yükseltilir. Bu koşu kırılırsa ya
  chart düzeltilir ya da README'deki Helm 3 desteği aynı PR'da daraltılır.
  Helm 3'ü bırakmak ayrı bir karardır.

cosign, ORAS, registry ve govulncheck'i yükseltmek (yeni sürümlere
[cosign sürümlerinden](https://github.com/sigstore/cosign/releases),
[ORAS sürümlerinden](https://github.com/oras-project/oras/releases),
[Docker Hub'daki `registry` imajından](https://hub.docker.com/_/registry) ve
[pkg.go.dev](https://pkg.go.dev/golang.org/x/vuln?tab=versions)'den elle
bakılır; Dependabot cosign'ı, ORAS'ı, registry imajını ve govulncheck'i
görmez):

- **cosign.** `COSIGN_VERSION` ile `COSIGN_SHA256` birlikte değişir; sağlama
  toplamı o sürümün `cosign_checksums.txt` dosyasındaki `cosign-linux-amd64`
  satırıdır. README "Tedarik zinciri"ndeki komutlar yeni sürümün bayraklarına
  karşı okunur (cosign v4 kullanımdan kalkan bayrakları kaldıracak) ve
  değişiklik etiketten önce bir kuru denemeyle sınanır. Sürüm PR'ından ayrı
  bir `chore(ci)` PR'ıdır.
- **ORAS ve registry.** `ORAS_VERSION` ile `ORAS_SHA256` birlikte değişir;
  sağlama toplamı o sürümün `oras_<sürüm>_checksums.txt` dosyasındaki
  `oras_<sürüm>_linux_amd64.tar.gz` satırıdır. `oras cp -r` ORAS'ta hâlâ
  önizleme (Preview) olarak işaretli; ORAS'ın ya da registry imajının yeni
  sürümü, cosign gibi, sürüm PR'ından ayrı bir `chore(ci)` PR'ında bir kuru
  denemeyle sınanır. registry imajı yükseltilirken etiketiyle birlikte
  `@sha256:...` digest'i de güncellenir (digest'i okumanın bir yolu
  `docker buildx imagetools inspect registry:<sürüm>`). README "İmzalarla
  aynalamak" ve docs/pilot.md'deki `oras` komutları yeni sürümün bayraklarına
  karşı okunur.
- **govulncheck.** ci.yml'daki ve security.yml'daki `@vX.Y.Z` aynı PR'da
  yükseltilir. Yeni sürüm daha yeni bir Go isteyebilir (`go.mod`'undaki `go`
  satırı); CI Go'yu `backend/go.mod`'dan aldığı için gerekirse önce toolchain
  yükseltilir.

#### Güvenlik taraması

[security.yml](.github/workflows/security.yml) her pazartesi 05:23'te (UTC)
ve elle koşar: backend'de `govulncheck`, frontend'de
`npm audit --omit=dev --audit-level=high`. PR'larda koşmaz; yeni yayımlanan
bir açık PR'ları değil bu iş akışını kırmızıya çevirir.

- GitHub zamanlanmış bir koşunun başarısızlığını iş akışını oluşturan ya da
  cron satırını son değiştiren kullanıcıya bildirir; bildirimin gelmesi için
  o kullanıcının Actions bildirimleri açık olmalıdır.
- Herkese açık bir depoda 60 gün etkinlik olmazsa GitHub zamanlamayı kapatır;
  Actions sekmesinden yeniden açılır.
- Bulgu Kubeat'i etkiliyorsa ve kritik ya da yüksekse SECURITY.md'deki 7
  günlük hedef başlar: bağımlılık yükseltilir (çoğu zaman Dependabot'un PR'ı)
  ve yeni bir yama sürümü çıkar.

## Commit mesajları

[Conventional Commits](https://www.conventionalcommits.org/):

```
<tip>(<kapsam>): <özet>
```

Tipler: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`.
Kapsamlar: `backend`, `frontend`, `agent`, `helm`, `ci`.

Örnek: `feat(backend): add health endpoint`

## Yapılandırma

Adres, port, yol, süre, eşik ve kimlik bilgisi gibi ortama göre değişen
değerler koda gömülmez. Backend'de `internal/config` üzerinden ortam
değişkeni olarak okunur, makul bir varsayılanı olur ve README'deki tabloya
eklenir. Gizli bilgiler (token, parola) repoya girmez; varsayılanı yoktur.

## Definition of Done

- Kod review'dan geçti
- Test yazıldı
- CI yeşil
- Dokümante edildi (gerekiyorsa ADR)
