export const meta = {
  name: 'kubeat-sprint-review',
  description: 'Sprint sonu: iki müşteri teslimatı puanlar, PMO sprint dosyasının sprint sonu, yükseltme notu, kullanıcıda kalanlar ve retro bölümlerini doldurur',
  whenToUse: 'Sürüm çıkınca. args: sprintFile, deliveryFile (teslimat özeti JSON) ve deliveryNote, ya da delivery (nesne). Döner: scores, retro.',
  phases: [
    { title: 'Değerlendirme', detail: 'Müşteri A ve B teslimatı puanlar' },
    { title: 'Retro', detail: 'PMO sprint dosyasını kapatır' },
  ],
}

const A = args

const SCORES = {
  type: 'object',
  properties: {
    role: { type: 'string' },
    items: { type: 'array', items: { type: 'object', properties: { issue: { type: 'integer' }, score: { type: 'integer', minimum: 1, maximum: 5 }, comment: { type: 'string' } }, required: ['issue', 'score', 'comment'] } },
    new_findings: { type: 'array', maxItems: 3, items: { type: 'object', properties: { title: { type: 'string' }, job: { type: 'string' }, today: { type: 'string' }, expected: { type: 'string' }, severity: { type: 'string' }, evidence: { type: 'string' } }, required: ['title', 'job', 'today', 'expected', 'severity', 'evidence'] } },
    overall: { type: 'string', description: 'Sprint hedefi senin işin açısından tutuldu mu, 2-4 cümle' },
  },
  required: ['role', 'items', 'new_findings', 'overall'],
}

const RETRO = {
  type: 'object',
  properties: {
    sprint_file: { type: 'string' },
    done: { type: 'array', items: { type: 'string' } },
    not_done: { type: 'array', items: { type: 'string' } },
    went_well: { type: 'array', items: { type: 'string' } },
    to_change: { type: 'array', items: { type: 'string' } },
    next_sprint: { type: 'string' },
    changed_files: { type: 'array', items: { type: 'string' } },
  },
  required: ['sprint_file', 'done', 'not_done', 'went_well', 'to_change', 'next_sprint', 'changed_files'],
}

const rules = `Çalışma dizini Kubeat deposudur (main, teslimat merge edilmiş). docs/agents/sprint-takimi.md'yi oku. Kullanıcıya soru yok; cluster, sandbox, gizli değer yok; backend/frontend çalıştırma. Çıktın Türkçe, şemaya göre.
Sprint dosyası: ${A.sprintFile}. Teslimat özeti (release manager'dan): ${A.deliveryFile ? `${A.deliveryFile} dosyasında (JSON; her madde için PR, merge commit, inceleme özeti, upgrade_note, user_checks, deferred). ${A.deliveryNote || ''}` : JSON.stringify(A.delivery, null, 1)}`

phase('Değerlendirme')
const scores = (await parallel(['musteri-kurumsal', 'musteri-startup'].map(role => () =>
  agent(`${rules}
Görev (sprint sonu): her teslim edilen madde için \`git show --stat <merge commit>\` ve \`git show <merge commit>\` ile diff'i, README'deki ilgili bölümleri ve sprint dosyasını oku; "işimi görüyor mu" puanı (1-5) ve bir cümle ver. Yeni bulgun varsa keşif biçiminde ekle (en çok 3). Sprint hedefi senin açından tutuldu mu, yaz. Gerçek ortamda doğrulanmamış bir şeye "doğrulandı" deme.`,
    { label: `puan:${role}`, phase: 'Değerlendirme', schema: SCORES, agentType: role })
))).filter(Boolean)
log(`Puanlar: ${scores.map(s => `${s.role.slice(0, 20)}: ${s.items.map(i => `#${i.issue}=${i.score}`).join(' ')}`).join(' | ')}`)

phase('Retro')
const retro = await agent(`${rules}
Görev (sprint sonu modu): sprint dosyasında
1. "Maddeler" tablosunun Durum sütununu güncelle;
2. "Yükseltme notu" bölümünü teslimat özetindeki upgrade_note cümleleriyle doldur (README'deki sürüm bölümüyle tutarlı);
3. "Kullanıcıda kalanlar" bölümünü teslimat özetindeki user_checks maddeleriyle doldur (yinelenenleri birleştir);
4. "Sprint sonu" (ne bitti: PR, merge commit, sürüm; ne kaldı ve neden; inceleme sonuçları; müşteri puanları ve yorumları; yeni müşteri bulguları) ve "Retro" (üç "iyi gitti", üç "değişecek", sonraki sprint önerisi; sprint dosyasındaki sonraki sprint sırasını ve yeni bulguları dikkate al) bölümlerini doldur.
Developer'ların "ertelendi" dediği parçaları docs/backlog.md'ye kısa birer madde olarak ekle (yoksa). Yol haritası 29. satırı güncelle. Kapanmamış issue varsa gh issue comment ile durum yaz. git commit ve push yapma.
Müşteri puanları:
${JSON.stringify(scores, null, 1)}`,
  { label: 'retro:PMO', phase: 'Retro', schema: RETRO, agentType: 'pmo' })

return { scores, retro }
