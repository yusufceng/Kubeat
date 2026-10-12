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

- Sürümler `main` üzerinde `vMAJOR.MINOR.PATCH` etiketiyle (SemVer) çıkar.
- Yayınlanmış bir sürüme düzeltme gerekirse o etiketten `release/vX.Y` açılır,
  `hotfix/` branch'i oraya merge edilir ve düzeltme `main`'e de taşınır.

## Commit mesajları

[Conventional Commits](https://www.conventionalcommits.org/):

```
<tip>(<kapsam>): <özet>
```

Tipler: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`.
Kapsamlar: `backend`, `frontend`, `agent`, `helm`, `ci`.

Örnek: `feat(backend): add health endpoint`

## Definition of Done

- Kod review'dan geçti
- Test yazıldı
- CI yeşil
- Dokümante edildi (gerekiyorsa ADR)
