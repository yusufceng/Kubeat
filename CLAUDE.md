# Kubeat

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues on `yusufceng/Kubeat` (public), managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five default triage roles, each label string equal to its role name (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `GLOSSARY.md` at the repo root and ADRs in `docs/adr/`. See `docs/agents/domain.md`.

### Sprint team

Roles (customer ×2, SRE, BA, PO, PMO, developer) live in `.claude/agents/`; the cycle, limits, issue template and definition of done in `docs/agents/sprint-takimi.md`; sprint files in `docs/sprints/`. The main session is the release manager: it pushes, opens PRs, merges and releases. Agents never touch a cluster.
