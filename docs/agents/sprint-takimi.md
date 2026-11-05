# Sprint ekibi

Kubeat'i geliştiren ajan ekibi ve çalışma düzeni. Roller `.claude/agents/`
altındadır; her ajan işe başlarken kendi rol dosyasını ve bu dosyayı okur.

## Neden

Şirket ve ekip yok: ürünü iki kişi, kullanıcı ve Claude, ilerletiyor.
Müşteri ve SRE bakışını, hikâye yazmayı, önceliklendirmeyi ve planlamayı
ayrı ajanlar üstlenir; developer ajanlar kodu yazar. Claude'un ana oturumu
**release manager**'dır: dalları iter, PR açar, CI'yı bekler, squash-merge
eder, gerekirse sürüm çıkarır ve sandbox'ı yükseltir. Kullanıcı sprint
sonunda bakar ve yön verir.

## Roller

| Rol | Dosya | İşi |
| --- | --- | --- |
| Müşteri A, kurumsal | `musteri-kurumsal.md` | Bankanın OpenShift platform ekibi gözüyle değerlendirir |
| Müşteri B, küçük ekip | `musteri-startup.md` | Yönetilen Kubernetes'teki küçük SaaS ekibi gözüyle değerlendirir |
| SRE | `sre.md` | İşletilebilirlik, güvenilirlik ve güvenlik bulguları; ops PR'larını inceler |
| BA | `ba.md` | Bulguları kabul ölçütlü hikâyelere çevirir; kabul ölçütlerini inceler |
| PO | `po.md` | Sprint hedefi, sıralı backlog, yapılmayacaklar; kararın sahibi |
| PMO | `pmo.md` | Sprint planı, GitHub issue'ları, bağımlılıklar, retro |
| Developer | `developer.md` | Bir issue'yu kendi dalında uçtan uca yapar |

## Döngü: bir sprint

1. **Keşif** (paralel): Müşteri A, Müşteri B ve SRE ürünü değerlendirir;
   bulgular kanıtlı ve bir işe bağlıdır.
2. **Analiz**: BA bulguları birleştirir, hikâyelere çevirir, kabul
   ölçütlerini ve kapsam dışını yazar, ilke ve ADR çakışmalarını işaretler.
   Kurulum notlarına (NOTES) dokunan hikâyede, NOTES'un gösterdiği README
   bölümünde önerdiği değerin geçmesi kabul ölçütüdür. Cross-tenant ya da
   SSRF gibi güvenlik maddelerinde kabul ölçütü ve reviewer sorusu ad
   çözümünün biçimlerini tek tek sayar (çıplak IP, kısa ad, iki etiketli
   `servis.namespace`, tam `.svc`/FQDN, API sunucusu adı) ve pod'un DNS arama
   alanını (`ndots`) hesaba katar; negatif testler bu biçimleri içerir.
3. **Öncelik**: PO sprint hedefini, en çok dört maddelik sıralı listeyi ve
   gerekçeli "yapılmayacaklar"ı verir. Bir hikâyenin bilinen yan etkisi
   sonraki bir hikâyeye bırakılıyorsa o hikâye yedekte değil, kesin madde
   olarak planlanır.
4. **Plan**: PMO `docs/sprints/<tarih>-sprint-<n>.md` dosyasını yazar ve
   her madde için `ready-for-agent` etiketli bir GitHub issue açar. Aynı
   sprintte aynı dosyaya yazan maddeler komşu satıra değil kendi alt
   başlığına ya da satırına yazar (git'in üç satırlık bağlamı bitişik
   değişiklikleri çakıştırır); PMO planda bu riski dosya ve bölüm adıyla
   işaretler.
5. **Geliştirme** (paralel, ayrı worktree): her developer bir issue'yu kendi
   dalında yapar, test eder, commit'ler; push etmez. Release manager
   teslimatın başında sandbox oturumunu denetler; düşmüşse kullanıcıdan
   sürümden önce giriş ister. Teslimat başlamadan önce sprint dosyasının
   "Sprint başında, slot almadan" maddeleri yapılır ve `gh api` çıktısı o
   başlığa yazılır; çıktı yoksa teslimat başlamaz. Kesilen bir teslimat
   scratchpad'deki durum dosyasından değil GitHub'dan devam eder (dalın son
   commit'i, `gh pr list`, PR inceleme yorumu): incelemesi kabul almış madde
   yeniden incelenmez, developer'ı bitmiş madde yeniden koşmaz; teslimat iş
   akışı bir madde bitince bu üçünü (son commit, PR, inceleme özeti yorumu)
   yazdığını garanti eder.
6. **İnceleme**: SRE ya da BA diff'i kabul ölçütlerine göre doğrular;
   release manager CI'yı bekler, squash-merge eder, issue kapanır. Kırmızı
   check'le merge edilmez (main kural seti de zorlar); kararsız bir testin
   kırmızısı da kırmızıdır: yeniden koşturup geçirilmez, düzeltilir. Yalnız
   bağlamsal (komşu satır) bir çakışmayı release manager rebase ile kendi
   çözer ve developer turu açmaz; rebase turu tam inceleme değildir, reviewer
   `git range-diff` ve patch-id ile yalnız çakışma çözümünü okur. Davranış
   değiştiren maddelerin yükseltme notu cümleleri her merge'de değil, sürüm
   PR'ında developer raporlarından "Yükseltme notu" tablosuna işlenir; BA
   tabloyu okur ve okumasını sürüm PR'ına yorum olarak yazar.
7. **Sprint sonu**: release manager sürümü çıkarır. Sandbox'ı yükseltmeden
   önce en yeni yedeği ve şifreleme anahtarını dışarı alıp kopyayı pod'daki
   dosyayla karşılaştırır ([pilot.md](../pilot.md) "Yükseltmeden önce
   yedek"); karşılaştırma tutmazsa yükseltmez. Müşteri puanından önce
   "Kullanıcıda kalanlar"dan en çok üç provayı koşar ve sonucu sprint
   dosyasına yazar; sandbox yükseltmesi ve sandbox provası sürümden sonraki
   ilk provadır. Sonra müşteriler teslimatı puanlar, PMO retro yazar;
   bunlar sonraki sprintin girdisidir. Sprint takvimle değil teslimatla
   kapanır: issue'lar merge edilir, sürüm çıkar, müşteriler puanlar. Ajan
   sınırı içinde doğrulanamayan maddeler (gerçek cluster, sandbox, gerçek
   Prometheus) sprint dosyasında "Kullanıcıda kalanlar" listesine yazılır;
   sonraki sprintin keşfi bu listenin sonucuyla ve koşulmayan maddeleriyle
   başlar. Bir hikâye bir müşterinin asıl kurulum ya da aynalama yolunu
   belgeliyorsa o yol ya CI'da koşar ya da bu listeye zorunlu prova olarak
   yazılır; "isteğe bağlı" yalnızca yan yollar içindir. Gerçek ya da çok
   kiracılı cluster isteyen bir güvenlik iddiası da aynıdır: sandbox provası
   sürümden önce zorunlu prova olarak planlanır (yalnız `go test` yetmez) ya
   da sürüm metni iddiayı temkinli yazar ("ana vektörler kapandı, prova
   kullanıcıda"); plan iddiayı tek sürümde "tam kapandı" gibi sunmaz.

Ret alan bir madde en çok üç düzeltme turu görür. Dördüncü inceleme
sondur: yine ret gelirse madde sürümden çıkar ve sonraki sprinte kalır.

Adımlar `.claude/workflows/` altındaki iş akışlarıyla koşar:
`kubeat-sprint-planning` (1–4), `kubeat-sprint-delivery` (5–6),
`kubeat-sprint-fix-round` (ret alan madde için ek tur),
`kubeat-sprint-review-items` (kesilen bir koşunun incelemelerini tamamlamak
için) ve `kubeat-sprint-review` (7). Argümanları her dosyanın `whenToUse`
alanında. Bir maddenin `extra` notu bir turun bağlamını taşır (ör. "rebase
turu, kod değişmedi") ki kesilen koşu yeniden başlarken o tur doğru okunsun.

## Ürün ilkeleri: karar ölçütleri

- **Cluster-native fark.** Cluster'ın içinden görülen ve dışarıdan bakan
  araçların göremediği şeyler önceliklidir. UptimeRobot tamamlayıcıdır;
  Uptime Kuma referanstır, parite hedef değildir: "Kuma'da var" tek başına
  gerekçe olmaz.
- **Şişirme yok.** Her madde bir müşteri işine ya da bir SRE riskine
  bağlanır; bağlanmayan `docs/backlog.md` dosyasına yazılır.
- **Ayarlar koddan değil.** `backend/internal/config`, README "Yapılandırma"
  tablosu, `.env.example` ve chart values birlikte güncellenir.
- **İki dil.** Arayüz metinleri `frontend/lib/i18n/messages`, bildirim
  metinleri `backend/internal/alert/texts.go`; ikisi de Türkçe ve İngilizce.
- **Dil.** Dokümantasyon Türkçe (README, docs, issue'lar); kod, yorumlar,
  commit ve PR İngilizce. Conventional Commits; commit sonunda
  `Co-Authored-By` satırı.
- **İzleme asla durmaz.** Lisans, hata ya da ayar eksikliği kontrolleri
  durdurmaz.
- **Verilmiş kararlar.** `docs/adr/`, `docs/backlog.md` (ertelenenler) ve
  `docs/yol-haritasi.md` okunmadan kapsam önerilmez. Bir karara aykırı
  öneri açıkça yazılır: "ADR-0002'ye aykırı, çünkü...".

## Sınırlar

- Hiçbir ajan `kubectl`, `helm`, `oc` çalıştırmaz, cluster'a ya da
  sandbox'a dokunmaz; değerlendirme depo, dokümanlar ve lokal testlerle
  yapılır. Makinedeki kubeconfig gerçek iş cluster'larını içerir.
- Release manager provaları ve sandbox yükseltmesini yalnızca açıkça
  verilen bağlamla yapar: minikube'da `--context minikube` (helm'de
  `--kube-context minikube`). Varsayılan bağlam kullanılmaz.
- Backend ya da frontend lokalde çalıştırılmaz; kullanıcının kendi `make dev`
  süreçleri ve veritabanı vardır. Yalnızca testler koşulur.
- Gizli değer (token, parola, anahtar) okunmaz ve yazdırılmaz; `.dev/`,
  `~/.kube` ve anahtar zincirine dokunulmaz.
- GitHub'a yalnızca `gh issue` ile yazılır (PMO ve BA); push, PR ve merge
  release manager'ındır. Depo herkese açık: şirket, kişi ve host adı yazılmaz.
- Lokal portlar 18300–18399; iş bitince süreçler kapatılır.
- Kullanıcıya soru sorulmaz; belirsizlikte makul varsayım yazılır ve
  işaretlenir.

## Issue şablonu

Başlık: fiil + nesne, Türkçe. Gövde:

```
## Neden
<müşteri ya da SRE bulgusu, kısa alıntı; hangi işe yarar>

## Kabul ölçütleri
- [ ] ...

## Kapsam dışı
- ...

## Teknik notlar
<dosyalar, ayar adları, chart değerleri, i18n anahtarları>

## Dokümantasyon
<README bölümü, docs dosyası>
```

Etiketler: `ready-for-agent` ve `bug` / `enhancement` / `documentation`.

## Tamamlanma tanımı

- `make test` ve `make lint` geçer: backend `go test -race`, frontend
  `node --test`, chart `deploy/helm/test-chart.sh`; `gofmt`, `go vet`,
  `eslint`, `helm lint`.
- `alert` ve `scheduler` paketlerinde süreye bağlı yeni ya da değişen
  testler duvar saatiyle sınır koymaz; sahte saat ve olay tabanlı bekleme
  kullanır. Developer bu testleri rapordan önce `go test -race -count=20`
  ile ve bir kez `-cpu=1` ile koşar; reviewer bunu raporda arar.
- Kabul ölçütleri reviewer tarafından diff üzerinden doğrulandı.
- README, docs ve iki dil güncel; yeni ayar varsa README tablosu,
  `.env.example` ve chart values de. Davranış ya da yayım değiştiren maddenin
  belge kabul ölçütü kullanıcının okuduğu yüzeyi (README) de sayar, yalnız
  CONTRIBUTING ya da iç belgeyi değil.
- Ertelenen parçalar backlog'da, yol haritası satırı güncel.
- Davranış değiştiren madde, developer raporunda sprint dosyasının
  "Yükseltme notu" başlığına bir cümle verir. Sürüm PR'ı yükseltme
  bölümünü taşır; 0.46'dan itibaren bölüm `docs/yukseltme.md`'dedir
  ("<sürüm>'e yükseltme"), README'de bağlantı kalır. Bölümü o başlıktaki
  tablodan ve "ayrıca" listesinden yazar, PR açıklamasında madde madde
  işaretler; BA bölümü sprint dosyasına karşı okur, release manager
  okumayı sürüm PR'ına yorum olarak yazar.
- README'de bir paragrafı aşan davranış açıklaması alt başlıklarla ve
  maddeli yazılır.
- CI yeşil, squash-merge edildi, issue kapandı.

## Sprint dosyası

`docs/sprints/<yyyy-mm-dd>-sprint-<n>.md`: hedef, maddeler (issue, durum,
dal), yapılmayacaklar ve gerekçeleri, riskler, yükseltme notu, kullanıcıda
kalanlar, sprint sonu puanları, retro.
