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

- PR en az 1 onay ve yeşil CI ile merge edilir.
- Squash merge kullanılır; `main` geçmişi lineer kalır. PR başlığı squash
  commit mesajı olur, bu yüzden Conventional Commits formatındadır.
- Merge edilen branch silinir.

### Sürümler

- Sürümler `main` üzerinde `vMAJOR.MINOR.PATCH` etiketiyle (SemVer) çıkar:
  `make release` etiketi `Makefile`'daki `VERSION` ile atar ve iter,
  [release.yml](.github/workflows/release.yml) imajları ve chart'ı imzalı ve
  SBOM'lu yayımlar ve etiketin GitHub Release'ini açar ("Yayım iş akışı").
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
- `README.md`: "Cluster'a kurulum" ve "OpenShift"teki kurulum komutlarının
  `--version`'ı.
- `docs/pilot.md`: "İmajları aynalamak" (iki imaj etiketi, `helm pull`
  `--version`'ı, `kubeat-<sürüm>.tgz`) ve "Kurulum" komutu.

`scripts/check-versions.sh` bunları `Chart.yaml`'daki `version` ile
karşılaştırır ve farklı olan her yeri `dosya:satır` ile yazar:

- README ve docs/pilot.md'de yalnızca komut kalıplarına bakar
  (`--version X.Y.Z`, `kubeat-backend:X.Y.Z`, `kubeat-frontend:X.Y.Z`,
  `kubeat-X.Y.Z.tgz`). Yükseltme notları ("... yükseltme" başlıklı
  bölümler), düzyazıdaki sürüm anışları ve `<sürüm>` gibi yer tutucular
  denetlenmez. Komutlardan biri başka bir dosyaya taşınırsa betikteki dosya
  listesi de güncellenir; hiç sürümlü komut kalmayan dosya denetimi düşürür.
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

1. `versions`: sürüm denetimi (yukarıda); geçmezse hiçbir şey itilmez.
2. `images`: iki imajı yalnızca sürüm etiketiyle iter, `latest` itilmez.
   Index BuildKit'in SPDX SBOM'unu ve SLSA provenance'ını (`mode=max`)
   taşır; platform başına SBOM'lar iş akışı artefaktı olarak saklanır.
3. `chart`: chart'ı paketler ve iter; `helm push`'un yazdığı digest'i alır.
4. `sign`: iki imajın ve chart'ın digest'ini cosign ile keyless imzalar
   (GitHub OIDC, Sigstore). `id-token: write` yalnızca bu iştedir ve iş
   cosign'dan başka bir şey koşmaz: üçüncü taraf action yoktur, cosign sabit
   sürümüyle indirilir ve sağlama toplamı denetlenir.
5. `verify`: imzaları README "Tedarik zinciri"ndeki komutlarla ve yalnızca
   okuma izniyle doğrular: önce çevrimiçi, sonra `cosign save` ile diske
   yazılanı ağ kesikken `--trusted-root` ile. Ardından Release notunu
   `scripts/release-notes.sh` ile yazar; betiğin testi
   `scripts/test-release-notes.sh` (`make test`, CI'ın chart işi).
6. `release`: etiketin GitHub Release'ini açar: üç digest, doğrulama komutu
   ve backend ile frontend için SPDX SBOM'lar. `contents: write` yalnızca bu
   iştedir.

İş akışı düzeyindeki izinler `contents: read` ve `packages: write`'tır.

İmajlar ya da chart itildikten sonra bir iş kırılırsa etiket yeniden
yazılmaz. Geçici bir hatada kırılan iş "Re-run failed jobs" ile yeniden
koşturulur; aynı digest'i yeniden imzalamak ikinci bir imza ekler, zararı
yoktur. Release yarım açıldıysa önce silinir. Hata iş akışının kendisindeyse
düzeltme yeni bir yama sürümüyle çıkar.

Kuru deneme (`gh workflow run release.yml --ref <dal>`) aynı adımları yayım
paketlerine dokunmadan dener:

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

cosign ve govulncheck'i yükseltmek (Dependabot ikisini de görmez; yeni
sürümlere [cosign sürümlerinden](https://github.com/sigstore/cosign/releases)
ve [pkg.go.dev](https://pkg.go.dev/golang.org/x/vuln?tab=versions)'den elle
bakılır):

- **cosign.** `COSIGN_VERSION` ile `COSIGN_SHA256` birlikte değişir; sağlama
  toplamı o sürümün `cosign_checksums.txt` dosyasındaki `cosign-linux-amd64`
  satırıdır. README "Tedarik zinciri"ndeki komutlar yeni sürümün bayraklarına
  karşı okunur (cosign v4 kullanımdan kalkan bayrakları kaldıracak) ve
  değişiklik etiketten önce bir kuru denemeyle sınanır. Sürüm PR'ından ayrı
  bir `chore(ci)` PR'ıdır.
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
