export const meta = {
  name: 'kubeat-sprint-review-items',
  description: 'İnceleme turu: developer raporu hazır olan maddeleri SRE ya da BA inceler; kesilen bir geliştirme koşusunun incelemelerini tamamlamak için',
  whenToUse: 'Developer bitirmiş ama inceleme koşmamışsa. args: sprintFile, storiesFile, items (her öğe issue, story, reviewer, worktree, branch, base, historyFile: dev raporu JSON, "dev" anahtarında; isteğe bağlı extra: inceleyiciye ek not). Döner: her madde için item, review.',
  phases: [{ title: 'İnceleme', detail: 'SRE/BA kabul ölçütlerine göre diff ve testler' }],
}

const A = args

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

const rules = `Kurallar: docs/agents/sprint-takimi.md'yi oku. Kullanıcıya soru soramazsın; belirsizlikte en küçük makul yorumu uygula ve raporda işaretle. kubectl, helm install/upgrade, oc, minikube yok; cluster ve sandbox yok; kubectl/oc/helm'i taklit eden test düzeneğinde önce KUBECONFIG'i boş bir tuzak dosyasına çevir; sahteyi PATH gölgelemesiyle değil KUBECTL gibi bir değişkeni sahte betiğin mutlak yoluna ayarlayarak ver; taklit ettiğin her ad için PATH'in başına çağrılınca kayıt yazıp hata veren bir koruma betiği koy ve 'command -v kubectl oc helm' bu koruma betiklerini göstermeden test koşma; inceleyici developer'ın düzeneğini koşmadan önce bu ikisini arar; gizli değer yok; backend ya da frontend'i çalıştırma, yalnızca test ve build. gh yalnızca okuma için (gh issue view). Push, PR, merge yapma. Çıktın Türkçe; sonucu şemaya göre döndür.
Sprint dosyası: ${A.sprintFile}. Hikâyeler: ${A.storiesFile} içinde "issue" alanıyla.`

const testing = `Testleri kendin koş: backend \`cd backend && gofmt -l . && go vet ./... && go test -race ./...\`; frontend \`cd frontend && npm ci && npm run lint && npm test && npm run build\`; chart \`helm lint deploy/helm/kubeat && deploy/helm/test-chart.sh\` (promtool için PROMTOOL=${A.promtool || ''} verilebilir); .github/workflows değiştiyse YAML'ı \`python3 -c 'import sys,yaml; yaml.safe_load(open(sys.argv[1]))' <dosya>\` ile ayrıştır ve adımları tek tek oku (iş akışı ancak etiketle ya da PR'da koşar; doğrulanamayanı notes'a yaz).`

const prompt = (item) => `${rules}
Görev (inceleme modu): issue #${item.issue} (hikâye ${item.story}) için developer'ın dalını incele. Developer raporu ${item.historyFile} dosyasının "dev" alanında (worktree, dal, commit'ler, testler, kabul ölçütleri, yükseltme notu, kullanıcı kontrolleri). Worktree: \`cd ${item.worktree}\`; dal ${item.branch}, base ${item.base}. Diff: \`git log --oneline ${item.base}..HEAD\` ve \`git diff ${item.base}...HEAD\`. Hikâyenin kabul ölçütlerini tek tek işaretle (kanıt: dosya ve satır). ${testing} Süreye bağlı testler varsa sahte saat kullandıklarına ve raporda \`-race -count=20\` koşusuna bak. Ops etkisi, varsayılanlar, geri alma yolu, iki dil, README/docs güncelliği ve yükseltme notu cümlesinin doğruluğu. Yalnızca gerçek sorunlar (yanlış davranış, eksik kabul ölçütü, yarış, veri kaybı, güvenlik, eksik doküman) ret sebebidir; üslup değildir.${item.extra ? '\n' + item.extra : ''}`

phase('İnceleme')
const results = await parallel(A.items.map(item => () =>
  agent(prompt(item), { label: `inceleme:#${item.issue}`, phase: 'İnceleme', schema: REVIEW, agentType: item.reviewer })
    .then(review => { log(`#${item.issue}: ${review ? review.verdict : 'inceleme yok'}`); return { item, review } })
))
return results.filter(Boolean)
