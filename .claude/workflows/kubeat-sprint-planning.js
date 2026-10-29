export const meta = {
  name: 'kubeat-sprint-planning',
  description: 'Sprint planlaması: müşteri ve SRE delta keşfi, BA hikâyeleri, PO önceliği, PMO planı, süreç güncellemesi ve GitHub issue’ları',
  whenToUse: 'Sprint başında. args: number, date, branch (planlama dalı), sprintFile, prevSprintFile, prevDate, prevStoriesFile (önceki sprintin hikâyeleri JSON), prevTag, tag, upgradeSection, candidates, nextStoryId, productState. Döner: findings, stories, decision, plan.',
  phases: [
    { title: 'Keşif', detail: 'Müşteri A, Müşteri B ve SRE: önceki sprintin adayları, son sürümün değişiklikleri, yeni bulgular' },
    { title: 'Analiz', detail: 'BA hikâyeleri' },
    { title: 'Öncelik', detail: 'PO sprint hedefi ve en çok dört madde' },
    { title: 'Plan', detail: 'PMO sprint dosyası, süreç güncellemesi, backlog/yol haritası ve issue’lar' },
  ],
}

const S = args

const common = `Çalışma dizini Kubeat deposudur (yerel klon, dal ${S.branch}). docs/agents/sprint-takimi.md dosyasını oku; ilkeler, sınırlar ve tamamlanma tanımı orada. Kullanıcıya soru soramazsın; belirsizlikte varsayım yaz ve işaretle. Cluster, sandbox, kubectl/helm/oc ve gizli değerler yasak; backend/frontend çalıştırma. Çıktın Türkçe; sonucu şemaya göre döndür; kanıt olarak dosya:satır ya da README bölüm adı ver.
Ürünün bugünkü hali: ${S.productState}
Sprint ${S.number}, tarih ${S.date}. Önceki sprint dosyası: ${S.prevSprintFile} ("Yapılmayacaklar" altındaki "Sonraki sprint" adayları, "Sprint sonu" bölümündeki yeni müşteri bulguları ve "Retro"). Önceki sprintin bütün hikâyeleri, tam metin ve son durumlarıyla: ${S.prevStoriesFile}.`

const SEVERITY = ['engelleyici', 'kritik', 'yüksek', 'önemli', 'orta', 'hoş olur', 'düşük']

const FINDINGS = {
  type: 'object',
  properties: {
    role: { type: 'string' },
    summary: { type: 'string', description: 'Son sürümle senin işin açısından ne değişti, 2-4 cümle' },
    candidates: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          id: { type: 'string', description: 'Önceki sprintin hikâye ID’si (S10, S1, ...) ya da sprint sonu bulgusunun başlığı' },
          importance: { type: 'string', enum: [...SEVERITY, 'beni ilgilendirmiyor'] },
          comment: { type: 'string' },
        },
        required: ['id', 'importance', 'comment'],
      },
    },
    findings: {
      type: 'array',
      maxItems: 5,
      items: {
        type: 'object',
        properties: {
          title: { type: 'string' },
          job: { type: 'string', description: 'Yapmak istediğim iş ya da korumak istediğim şey' },
          today: { type: 'string', description: 'Bugün ne oluyor' },
          expected: { type: 'string', description: 'Beklediğim' },
          severity: { type: 'string', enum: SEVERITY },
          kind: { type: 'string', enum: ['bug', 'gap', 'request', 'risk', 'docs'] },
          evidence: { type: 'string', description: 'Dosya:satır, README bölümü ya da chart değeri' },
          smallest_fix: { type: 'string' },
        },
        required: ['title', 'job', 'today', 'expected', 'severity', 'kind', 'evidence'],
      },
    },
  },
  required: ['role', 'summary', 'candidates', 'findings'],
}

const STORIES = {
  type: 'object',
  properties: {
    stories: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          id: { type: 'string', description: 'Önceki hikâyeler kendi ID’siyle, yeniler args.nextStoryId’den başlar' },
          title: { type: 'string' },
          why: { type: 'string', description: 'Hangi bulgu (kimden, alıntı), hangi işe yarar' },
          acceptance: { type: 'array', items: { type: 'string' } },
          out_of_scope: { type: 'array', items: { type: 'string' } },
          areas: { type: 'array', items: { type: 'string', enum: ['backend', 'frontend', 'chart', 'ci', 'docs'] } },
          files: { type: 'array', items: { type: 'string' } },
          size: { type: 'string', enum: ['S', 'M', 'L'] },
          principle_fit: { type: 'string' },
          conflicts: { type: 'string', description: 'ADR/backlog/yol haritası çakışması; yoksa boş' },
          open_questions: { type: 'array', items: { type: 'object', properties: { question: { type: 'string' }, default: { type: 'string' } }, required: ['question', 'default'] } },
          sources: { type: 'array', items: { type: 'string' } },
          changed_since_last_sprint: { type: 'string', description: 'Önceki hikâyeyse 0.43 ile ne değişti; yeniyse boş' },
        },
        required: ['id', 'title', 'why', 'acceptance', 'out_of_scope', 'areas', 'files', 'size', 'principle_fit', 'conflicts', 'open_questions', 'sources', 'changed_since_last_sprint'],
      },
    },
    already_exists: { type: 'array', items: { type: 'object', properties: { finding: { type: 'string' }, where: { type: 'string' } }, required: ['finding', 'where'] } },
    not_a_story: { type: 'array', items: { type: 'object', properties: { finding: { type: 'string' }, reason: { type: 'string' } }, required: ['finding', 'reason'] } },
  },
  required: ['stories', 'already_exists', 'not_a_story'],
}

const PO = {
  type: 'object',
  properties: {
    sprint_goal: { type: 'string' },
    selected: {
      type: 'array',
      maxItems: 4,
      items: { type: 'object', properties: { story_id: { type: 'string' }, order: { type: 'integer' }, rationale: { type: 'string' }, reversible: { type: 'string' } }, required: ['story_id', 'order', 'rationale', 'reversible'] },
    },
    deferred: {
      type: 'array',
      items: { type: 'object', properties: { story_id: { type: 'string' }, decision: { type: 'string', enum: ['sonraki sprint', 'backlog', 'yapılmayacak'] }, reason: { type: 'string' } }, required: ['story_id', 'decision', 'reason'] },
    },
    release: { type: 'string', description: 'Sprint sonunda sürüm çıkacak mı (ör. 0.44.0), neden' },
    backlog_additions: { type: 'array', items: { type: 'object', properties: { title: { type: 'string' }, reason: { type: 'string' } }, required: ['title', 'reason'] } },
    roadmap_changes: { type: 'array', items: { type: 'string' } },
    notes: { type: 'string' },
  },
  required: ['sprint_goal', 'selected', 'deferred', 'release', 'backlog_additions', 'roadmap_changes', 'notes'],
}

const PLAN = {
  type: 'object',
  properties: {
    sprint_file: { type: 'string' },
    items: {
      type: 'array',
      items: { type: 'object', properties: { order: { type: 'integer' }, story_id: { type: 'string' }, title: { type: 'string' }, issue: { type: 'integer' }, branch: { type: 'string' }, size: { type: 'string' }, reviewer: { type: 'string', enum: ['sre', 'ba'] }, depends_on: { type: 'array', items: { type: 'integer' } } }, required: ['order', 'story_id', 'title', 'issue', 'branch', 'size', 'reviewer', 'depends_on'] },
    },
    merge_order: { type: 'array', items: { type: 'integer' } },
    changed_files: { type: 'array', items: { type: 'string' } },
    notes: { type: 'string' },
  },
  required: ['sprint_file', 'items', 'merge_order', 'changed_files', 'notes'],
}

phase('Keşif')
const discovery = [
  { role: 'musteri-kurumsal', label: 'müşteri A (kurumsal)', extra: '' },
  { role: 'musteri-startup', label: 'müşteri B (startup)', extra: '' },
  { role: 'sre', label: 'SRE', extra: 'Testleri koşabilirsin (cd backend && go test ./...), backend’i çalıştıramazsın.' },
]
const findings = (await parallel(discovery.map(d => () =>
  agent(`${common}
Görev: Sprint ${S.number} keşfi. Önceki keşif ${S.prevDate}'de yapıldı; hikâyeleri ${S.prevStoriesFile} içinde. Aynı bulguları yeniden üretme.
1. ${S.prevSprintFile} dosyasını ve docs/backlog.md'nin ${S.prevDate} ve sonrası girdilerini oku.
2. Son sürümün değişikliklerini incele: \`git log --oneline ${S.prevTag}..${S.tag}\`, ilgili diff'ler ve README "${S.upgradeSection}".
3. candidates: "Sonraki sprint" adaylarını (${S.candidates}) ve önceki sprint sonunun yeni müşteri bulgularını kendi işin açısından değerlendir; seni ilgilendirmeyeni "beni ilgilendirmiyor" diye işaretle.
4. findings: en çok 5 YENİ bulgu: önceki sprint dosyasında ve backlog'da olmayan ya da yeni kanıtı olan; özellikle son sürümün getirdiği yeni riskler ve geçen sefer bakılmayan alanlar. ${d.extra}`,
    { label: `keşif:${d.label}`, phase: 'Keşif', schema: FINDINGS, agentType: d.role })
))).filter(Boolean)
log(`Keşif: ${findings.map(f => `${f.role} ${f.candidates.length} aday, ${f.findings.length} yeni bulgu`).join(' | ')}`)

phase('Analiz')
const stories = await agent(`${common}
Görev: Sprint ${S.number} hikâyeleri. Girdiler:
(a) Önceki sprintlerin hikâyeleri ${S.prevStoriesFile} içinde: "status" alanı teslim edilenleri, "sonraki sprint" ve "backlog" kararlarını gösterir.
(b) ${S.prevSprintFile} "Sprint sonu" bölümündeki yeni müşteri bulguları ve "Retro"daki ürün işi olan öneriler (ör. kuralların promtool ile denetimi).
(c) Bu turun keşif çıktısı (aşağıda).
Önceki hikâyelerin ID'sini koru; metinlerini ve kabul ölçütlerini son sürüme göre depoda doğrulayıp güncelle ("changed_since_last_sprint"). Yeni hikâyelere ${S.nextStoryId}'den başlayarak ID ver. Rol dosyandaki adımlarla birleştir, doğrula, kararlarla karşılaştır. Backlog'da tetikleyicisi gelmemiş bir hikâyeyi yeni kanıt yoksa yeniden açma; yalnızca bu sprint için aday olabilecekleri yaz.

Keşif çıktısı (JSON):
${JSON.stringify(findings, null, 1)}`,
  { label: 'analiz:BA', phase: 'Analiz', schema: STORIES, agentType: 'ba' })
log(`BA: ${stories.stories.length} hikâye, ${stories.already_exists.length} zaten var, ${stories.not_a_story.length} hikâye değil`)

phase('Öncelik')
const decision = await agent(`${common}
Görev: Sprint ${S.number} için sprint hedefini ve en çok DÖRT maddelik sıralı listeyi seç; kalan her hikâye için karar ver ve gerekçele. Önceki sprintin retrosu ve PMO'nun sprint önerisi ${S.prevSprintFile} "Retro" bölümünde: girdi say, bağlayıcı değil. Retro kararları: sprint takvimle değil teslimatla kapanır; sürüm PR'ı README'de yükseltme notu taşır; doğrulanamayan şey sprint dosyasında "Kullanıcıda kalanlar"a yazılır. release.yml'i değiştiren bir madde seçersen doğrulaması bu sprintin sürüm etiketiyle olur; bunu gerekçeye ve riske yaz. "release" alanında sprint sonunda sürüm çıkıp çıkmayacağını söyle.

Hikâyeler (JSON):
${JSON.stringify(stories, null, 1)}`,
  { label: 'öncelik:PO', phase: 'Öncelik', schema: PO, agentType: 'po' })
log(`PO: hedef "${decision.sprint_goal}", ${decision.selected.length} madde`)

phase('Plan')
const plan = await agent(`${common}
Görev (planlama modu): Sprint ${S.number} planı.
1. \`${S.sprintFile}\` dosyasını rol dosyandaki biçimde yaz (Türkçe; tarih ${S.date}); ${S.prevSprintFile} biçim örneğidir. "Yükseltme notu" (davranış değiştiren her madde için bir cümle; developer raporlarından doldurulacak) ve "Kullanıcıda kalanlar" başlıklarını da aç.
2. Önceki retronun süreç kararlarını docs/agents/sprint-takimi.md'ye kısa olarak işle: tamamlanma tanımına "davranış değiştiren madde yükseltme notuna bir cümle verir; sürüm PR'ı README'deki yükseltme bölümünü taşır", döngüye "sprint takvimle değil teslimatla (merge, sürüm, müşteri puanı) kapanır", "doğrulanamayan maddeler sprint dosyasında Kullanıcıda kalanlar listesine yazılır".
3. Seçilen her madde için \`gh issue create\` ile şablona uygun bir issue aç: etiketler \`ready-for-agent\` ve türü (\`bug\`, \`enhancement\`, \`documentation\`). Gövde Türkçe; sprint dosyasının yolunu yaz; şirket, kişi ve host adı yazma. Issue numaralarını sprint dosyasına işle.
4. PO'nun "backlog_additions" maddelerini docs/backlog.md biçiminde ekle; "roadmap_changes" varsa docs/yol-haritasi.md'yi düzenle (29. satır sprint durumunu gösterir).
5. Dal adlarını (feat/, fix/, docs/, chore/, ci/ + kısa İngilizce kebab-case) ve her madde için inceleyiciyi (ops etkisi olana "sre", belge ve kabul ölçütü ağırlıklı olana "ba") ver. Aynı dosyalara dokunan maddeler için bağımlılık ve merge sırası yaz.
Push, PR, merge ve git commit yapma; release manager yapacak.

PO kararı (JSON):
${JSON.stringify(decision, null, 1)}

Hikâyeler (JSON):
${JSON.stringify(stories.stories, null, 1)}`,
  { label: 'plan:PMO', phase: 'Plan', schema: PLAN, agentType: 'pmo' })
log(`PMO: ${plan.items.length} issue, dosya ${plan.sprint_file}, merge sırası ${plan.merge_order.join(' → ')}`)

return { findings, stories, decision, plan }
