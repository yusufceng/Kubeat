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
  [release.yml](.github/workflows/release.yml) imajları ve chart'ı yayımlar.
- Yayınlanmış bir sürüme düzeltme gerekirse o etiketten `release/vX.Y` açılır,
  `hotfix/` branch'i oraya merge edilir ve düzeltme `main`'e de taşınır.

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
- release.yml elle de çalıştırılabilir (`workflow_dispatch`, ör.
  `gh workflow run release.yml --ref <dal>`): bu bir kuru denemedir,
  denetimler koşar, imajlar ve chart derlenir, hiçbir şey itilmez ve hiçbir
  etikete dokunulmaz. GitHub elle çalıştırmayı yalnızca varsayılan dalda
  tanımlı bir iş akışında açar.

#### Sabit araç sürümleri

| Araç | Sürüm | Nerede |
| --- | --- | --- |
| Helm | v4.3.1 | ci.yml chart işi ("chart") ve release.yml chart işi; `azure/setup-helm`'in `version`'ı |
| Helm 3 | v3.22.1 | ci.yml chart işinin ikinci koşusu ("chart (Helm 3)") |

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
