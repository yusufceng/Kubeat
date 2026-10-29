export const meta = {
  name: 'kubeat-sprint-fix-round',
  description: 'Ret alan bir madde için ek düzeltme turu ve hedefli inceleme',
  whenToUse: 'Bir madde yine ret alınca. args: issue, story, round, reviewer, worktree, branch, sprintFile, storiesFile, historyFile (dev ve review JSON), coauthor, extra, reviewExtra. Döner: fixed, review.',
  phases: [
    { title: 'Düzeltme', detail: 'Developer inceleyicinin bulgularını kapatır' },
    { title: 'İnceleme', detail: 'İnceleyici yalnızca düzeltmelere ve testlere bakar, tüm testleri koşar' },
  ],
}

const A = args

const DEV = {
  type: 'object',
  properties: {
    worktree: { type: 'string' },
    branch: { type: 'string' },
    base: { type: 'string' },
    commits: { type: 'array', items: { type: 'string' } },
    files: { type: 'array', items: { type: 'string' } },
    tests: { type: 'array', items: { type: 'object', properties: { command: { type: 'string' }, result: { type: 'string' } }, required: ['command', 'result'] } },
    acceptance: { type: 'array', items: { type: 'object', properties: { criterion: { type: 'string' }, met: { type: 'boolean' }, note: { type: 'string' } }, required: ['criterion', 'met', 'note'] } },
    upgrade_note: { type: 'string' },
    user_checks: { type: 'array', items: { type: 'string' } },
    deferred: { type: 'array', items: { type: 'string' } },
    open_points: { type: 'array', items: { type: 'string' } },
    summary: { type: 'string' },
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

const rules = `Kurallar: docs/agents/sprint-takimi.md'yi oku. Kullanıcıya soru soramazsın; belirsizlikte en küçük makul yorumu uygula ve raporda işaretle. kubectl, helm install/upgrade, oc, minikube yok; cluster ve sandbox yok; gizli değer yok; backend ya da frontend'i çalıştırma, yalnızca test ve build. gh yalnızca okuma için. Push, PR, merge yapma. Çıktın Türkçe; sonucu şemaya göre döndür.
Sprint dosyası: ${A.sprintFile}. Hikâye: ${A.storiesFile} içinde id ${A.story}. Önceki developer raporu ve son inceleme (bulgular dahil): ${A.historyFile} ("dev" ve "review" alanları).`

const testing = `Testler: \`cd backend && gofmt -l . && go vet ./... && go test -race ./...\`; chart'a dokunduysan \`helm lint deploy/helm/kubeat && deploy/helm/test-chart.sh\`.`

phase('Düzeltme')
const fixed = await agent(`${rules}
Görev: issue #${A.issue} için inceleyici ${A.round}. incelemede yine ret verdi. Worktree: \`cd ${A.worktree}\`; dal ${A.branch} (açık). ${A.historyFile} içindeki "review.findings" bulgularının her birini düzelt ya da raporda gerekçeyle reddet. İnceleyicinin önerdiği satırlar onun kopyasında denenmiş; yine de her bulgu için düzeltme olmadan düşen, düzeltmeyle geçen ayırt edici bir test yaz. Yükseltme notu cümlesini inceleyicinin istediği biçimde düzelt. ${A.extra || ''} ${testing} Commit'le (push yok); her commit mesajı şu satırla biter: "${A.coauthor}". Güncel raporu şemaya göre ver (commits tüm dal commit'lerini içersin).`,
  { label: `düzeltme${A.round}:#${A.issue}`, phase: 'Düzeltme', schema: DEV, agentType: 'developer' })
if (!fixed) return { fixed: null, review: null }

phase('İnceleme')
const review = await agent(`${rules}
Görev (inceleme modu, ${A.round + 1}. inceleme): issue #${A.issue}. Worktree: \`cd ${fixed.worktree}\`; dal ${fixed.branch}. Önceki incelemenin bulguları ${A.historyFile} "review.findings" içinde. Yalnızca bu bulguların düzeltmelerine ve yeni testlere bak: \`git log --oneline main..HEAD\`, son commit'lerin diff'i. Tüm testleri kendin koş (${testing}). Her önceki bulgu için kapandı mı (kanıt: dosya ve satır), düzeltme yeni bir kayıp yolu açtı mı. Yeni bir gerçek sorun yoksa kabul ver. ${A.reviewExtra || ''} Developer'ın güncel raporu:
${JSON.stringify(fixed, null, 1)}`,
  { label: `inceleme${A.round + 1}:#${A.issue}`, phase: 'İnceleme', schema: REVIEW, agentType: A.reviewer })
log(`#${A.issue}: ${review ? review.verdict : 'inceleme yok'}`)
return { fixed, review }
