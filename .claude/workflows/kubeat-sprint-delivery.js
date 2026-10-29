export const meta = {
  name: 'kubeat-sprint-delivery',
  description: 'Sprint geliştirme: her issue kendi worktree’sinde bir developer ajanıyla yapılır, SRE ya da BA inceler, ret alan madde için bir düzeltme turu',
  whenToUse: 'Plan main dalına girince. args: sprintFile, storiesFile (seçilen hikâyeler JSON), coauthor (commit atıf satırı), chains (paralel zincirler; her öğe issue, story, branch, base, reviewer). Döner: her madde için item, dev, review, rounds.',
  phases: [
    { title: 'Geliştirme', detail: 'Developer ajanları, ayrı worktree’ler; zincirli maddeler öncekinin worktree’sinde' },
    { title: 'İnceleme', detail: 'SRE/BA kabul ölçütlerine göre diff ve testler' },
    { title: 'Düzeltme', detail: 'Ret alan maddeler için bir tur ve ikinci inceleme' },
  ],
}

const A = args

const DEV = {
  type: 'object',
  properties: {
    worktree: { type: 'string', description: 'Worktree’nin mutlak yolu (pwd)' },
    branch: { type: 'string' },
    base: { type: 'string' },
    commits: { type: 'array', items: { type: 'string' }, description: 'kısa hash + konu' },
    files: { type: 'array', items: { type: 'string' } },
    tests: { type: 'array', items: { type: 'object', properties: { command: { type: 'string' }, result: { type: 'string' } }, required: ['command', 'result'] } },
    acceptance: { type: 'array', items: { type: 'object', properties: { criterion: { type: 'string' }, met: { type: 'boolean' }, note: { type: 'string' } }, required: ['criterion', 'met', 'note'] } },
    upgrade_note: { type: 'string', description: 'Davranış değiştiyse yükseltme notu için bir cümle (Türkçe); değişmediyse boş' },
    user_checks: { type: 'array', items: { type: 'string' }, description: 'Ajan sınırı yüzünden doğrulanamayan, kullanıcının gerçek ortamda bakması gerekenler' },
    deferred: { type: 'array', items: { type: 'string' }, description: 'Kapsam dışı bırakılan ya da ertelenen parçalar (backlog adayı)' },
    open_points: { type: 'array', items: { type: 'string' } },
    summary: { type: 'string', description: 'Ne yapıldı ve neden, 3-6 cümle; PR açıklamasına girecek' },
  },
  required: ['worktree', 'branch', 'base', 'commits', 'files', 'tests', 'acceptance', 'upgrade_note', 'user_checks', 'deferred', 'open_points', 'summary'],
}

const REVIEW = {
  type: 'object',
  properties: {
    verdict: { type: 'string', enum: ['kabul', 'ret'] },
    acceptance: { type: 'array', items: { type: 'object', properties: { criterion: { type: 'string' }, met: { type: 'boolean' }, evidence: { type: 'string' } }, required: ['criterion', 'met', 'evidence'] } },
    findings: { type: 'array', items: { type: 'object', properties: { file: { type: 'string' }, line: { type: 'string' }, severity: { type: 'string', enum: ['kritik', 'yüksek', 'orta', 'düşük'] }, issue: { type: 'string' }, requested_change: { type: 'string' } }, required: ['file', 'line', 'severity', 'issue', 'requested_change'] } },
    tests_run: { type: 'array', items: { type: 'string' } },
    notes: { type: 'string' },
  },
  required: ['verdict', 'acceptance', 'findings', 'tests_run', 'notes'],
}

const rules = `Kurallar: docs/agents/sprint-takimi.md'yi oku. Kullanıcıya soru soramazsın; belirsizlikte en küçük makul yorumu uygula ve raporda işaretle. kubectl, helm install/upgrade, oc, minikube yok; cluster ve sandbox yok; gizli değer yok; backend ya da frontend'i çalıştırma, yalnızca test ve build. gh yalnızca okuma için (gh issue view). Push, PR, merge yapma. Çıktın Türkçe; sonucu şemaya göre döndür.
Sprint dosyası: ${A.sprintFile} (PO kararları, sıra, bağımlılıklar ve riskler orada). Hikâyeler (kabul ölçütleri, kapsam dışı, dosyalar): ${A.storiesFile} içinde "id" alanıyla.`

const testing = `Dokunduğun alanların testlerini koş: backend \`cd backend && gofmt -l . && go vet ./... && go test -race ./...\`; frontend \`cd frontend && npm ci && npm run lint && npm test && npm run build\`; chart \`helm lint deploy/helm/kubeat && deploy/helm/test-chart.sh\`; .github/workflows değiştiyse YAML'ı \`python3 -c 'import sys,yaml; yaml.safe_load(open(sys.argv[1]))' <dosya>\` ile ayrıştır ve adımları tek tek oku (iş akışı ancak etiketle ya da PR'da koşar; doğrulanamayanı user_checks'e yaz).`

const where = (item, wt) => wt
  ? `Çalışma yeri: \`cd ${wt}\` (git worktree; şu an ${item.base} dalındasın). Orada \`git checkout -b ${item.branch}\` ile dalını aç.`
  : `Çalışma yeri: bulunduğun dizin bir git worktree'sidir (main'den açıldı). \`git checkout -b ${item.branch}\` ile dalını aç; raporda worktree yolunu \`pwd\` ile ver.`

const devPrompt = (item, wt) => `${rules}
Görev: issue #${item.issue} (hikâye ${item.story}): \`gh issue view ${item.issue} --json title,body\` ile gövdeyi, ${A.storiesFile} içinden hikâyeyi oku. ${where(item, wt)}
Kabul ölçütlerinin tamamını karşıla; kapsam dışına çıkma; sprint dosyasındaki PO kararlarına uy. Başka bir maddenin dalındaki değişikliğe ihtiyaç duyarsan cherry-pick yapma, open_points'e yaz. Davranış değiştiyse test yaz. ${testing}
README'nin ilgili Türkçe bölümünü ve gerekiyorsa .env.example, values.yaml yorumu, docs/yol-haritasi.md ve docs/backlog.md'yi güncelle. Conventional Commits ile İngilizce commit(ler); her commit mesajı şu satırla biter: "${A.coauthor}". Bitince raporu şemaya göre ver: "summary" PR açıklamasına, "upgrade_note" sürümün yükseltme notuna, "user_checks" sprint dosyasının "Kullanıcıda kalanlar" listesine girecek.`

const reviewPrompt = (item, dev) => `${rules}
Görev (inceleme modu): issue #${item.issue} (hikâye ${item.story}) için developer'ın dalını incele. Worktree: \`cd ${dev.worktree}\`; dal ${dev.branch}, base ${dev.base}. Diff: \`git log --oneline main..HEAD\` ve \`git diff main...HEAD\` (zincirli maddede base dalına göre: \`git diff ${item.base}...HEAD\`). Hikâyenin kabul ölçütlerini tek tek işaretle (kanıt: dosya ve satır). Testleri kendin koş (${testing}). Ops etkisi, varsayılanlar, geri alma yolu, iki dil, README güncelliği ve yükseltme notu cümlesinin doğruluğu. Yalnızca gerçek sorunlar (yanlış davranış, eksik kabul ölçütü, yarış, veri kaybı, güvenlik, eksik doküman) ret sebebidir; üslup değildir. Developer'ın raporu:
${JSON.stringify(dev, null, 1)}`

const fixPrompt = (item, dev, review) => `${rules}
Görev: issue #${item.issue} için inceleyici ret verdi. Worktree: \`cd ${dev.worktree}\`; dal ${dev.branch} (zaten açık). Her bulguyu düzelt ya da raporda gerekçeyle reddet. ${testing} Commit'le (push yok); her commit mesajı şu satırla biter: "${A.coauthor}". Güncel raporu şemaya göre ver (commits alanı tüm commit'leri içersin).
İnceleyici bulguları:
${JSON.stringify(review.findings, null, 1)}
İnceleyici notları: ${review.notes}
Önceki raporun:
${JSON.stringify(dev, null, 1)}`

async function build(item, wt) {
  const opts = { label: `dev:#${item.issue}`, phase: 'Geliştirme', schema: DEV, agentType: 'developer' }
  if (!wt) opts.isolation = 'worktree'
  let dev = await agent(devPrompt(item, wt), opts)
  if (!dev) return { item, dev: null, review: null, rounds: 0 }
  let review = await agent(reviewPrompt(item, dev), { label: `inceleme:#${item.issue}`, phase: 'İnceleme', schema: REVIEW, agentType: item.reviewer })
  let rounds = 1
  if (review && review.verdict === 'ret') {
    log(`#${item.issue}: ret (${review.findings.length} bulgu), düzeltme turu`)
    const fixed = await agent(fixPrompt(item, dev, review), { label: `düzeltme:#${item.issue}`, phase: 'Düzeltme', schema: DEV, agentType: 'developer' })
    if (fixed) dev = fixed
    const again = await agent(reviewPrompt(item, dev) + `\nBu ikinci incelemedir. İlk incelemenin bulguları:\n${JSON.stringify(review.findings, null, 1)}`, { label: `inceleme2:#${item.issue}`, phase: 'Düzeltme', schema: REVIEW, agentType: item.reviewer })
    if (again) review = again
    rounds = 2
  }
  log(`#${item.issue}: ${review ? review.verdict : 'inceleme yok'} (${rounds} tur), dal ${dev.branch}`)
  return { item, dev, review, rounds }
}

async function runChain(chain) {
  const out = []
  let wt = null
  for (const item of chain) {
    const r = await build(item, wt)
    out.push(r)
    if (!r.dev) { log(`#${item.issue}: developer sonuç vermedi, zincir durdu`); break }
    wt = r.dev.worktree
  }
  return out
}

const results = (await parallel(A.chains.map(chain => () => runChain(chain)))).filter(Boolean).flat()
return results
