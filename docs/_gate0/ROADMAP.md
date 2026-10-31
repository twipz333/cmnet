# CMNET — roadmap to production-quality native release

**Program:** complete product, not MVP shortcuts (`docs/DELIVERY-BOARD.md`).  
**Orca Run:** `run_d7ae77d33f9b`  
**Baseline:** `3b849b4` (was `c75ac6f` at Gate 0 audit)  
**Constraint:** Gate 0.5 **verified** — implement against `docs/_gate0/3b-reference/`; do not invent geometry beyond signed boards.  
**Companion:** [`GATE0-REPORT.md`](./GATE0-REPORT.md), [`3b-reference/SIGN-OFF.md`](./3b-reference/SIGN-OFF.md)

Status vocabulary: **not started** / **in progress** / **verified** / **blocked**.

---

## Phase 0 — Establish truth (Gate 0)

| Item | Status | Notes / evidence |
|---|---|---|
| 0A Build/test/CI screenshots | **verified** | `build-evidence.md`; go test **not** green locally |
| 0B Electron↔WPF parity matrix | **verified** | `parity-and-gaps.md` |
| 0C Synthesis + roadmap | **verified** | `GATE0-REPORT.md`, this file |
| 0.5 Approved 3B Draft+Scout images + sign-off | **verified** | User boards 2026-10-30 → `3b-reference/` + `SIGN-OFF.md` |
| 0.6 Performance baseline timings | **verified** (single-host) | `perf-baseline.md` + harness; live network scout / scroll FPS still gaps |

**Phase 1 unlocked.** Do not invent 3B geometry beyond signed references.

---

## Phase 1 — Visual craft + tactical experience (Gate 1)

Order matters: lock shared shell/theme once; only one worktree touches shell/theme at a time. Max **two** implementation worktrees; stop after two failed correction attempts per task.

| # | Task | Depends | Acceptance (observable) | Status |
|---|---|---|---|---|
| 1.1 | Tokenize shell to signed 3B (nav, surfaces, type, semantic team colors) | 0.5 | Side-by-side screenshots vs reference at 1366/1920 | **in progress** (NEXT) |
| 1.2 | **Scout vertical slice** end-to-end (roster, detail, empty/loading/error/partial, cancel, no focus steal) | 1.1 | Real WPF exe walkthrough + screenshots; honest source states | **not started** |
| 1.3 | Shared components freeze (hero tile, player card, status, empty) | 1.2 | Reused without drift; independent review | **blocked** |
| 1.4 | **Draft vertical slice** (board, gallery, apply, undo/redo gates, GSI status surface, preds + why) | 1.3 | Matches reference composition + Electron critical loops | **blocked** |
| 1.5 | Whole-app state kit wired on Scout+Draft | 1.4 | Offline/partial/error never look like success | **blocked** |

Maps to PRODUCT-BAR journeys **Scout** + **Draft** (before / during match).

---

## Phase 2 — Complete experience & parity (Gate 2)

| # | Task | Depends | PRODUCT-BAR journey | Status |
|---|---|---|---|---|
| 2.1 | Data: progress, cancel, log, history; no raw JSON wall | 1.5 | Maintain | **not started** |
| 2.2 | Settings: remaining useful tabs OR explicit cut-list with UX copy; welcome/first-run | 1.5 | Maintain / Install | **not started** |
| 2.3 | GSI: spawn path, install UX, readonly/live reconciliation | 1.4 | Draft (live) | **not started** |
| 2.4 | After-match / decision history UI | 1.4 | After match | **not started** |
| 2.5 | Friends: secure local-only share **or** agreed import/export (no public relay) | PRODUCT-BAR decision | Friends | **in progress** (PR for #49) |
| 2.6 | Electron→native data migration + backup/rollback demo | LOCAL-FIRST-V1 | Data integrity | **not started** |
| 2.7 | Keyboard/a11y/DPI 100/125/150 real monitors; suspend/resume; cancel under load | 1.5 | UI resilience | **not started** |

Each ticket: one user-visible outcome, non-overlapping ownership, automated acceptance where possible.

---

## Phase 3 — Ship trusted desktop (Gate 3)

| # | Task | Depends | Status |
|---|---|---|---|
| 3.1 | Naming, icon, versioning, polished first-run | 2.x journeys | **verified** (merged #55) |
| 3.2 | Self-contained Windows installer (no SDK for friends) | 3.1 | **in progress** (#56 evidence `gate3-installer-wip/`) |
| 3.3 | Update channel: manifest, signature, failure recovery, rollback | 3.2 | **in progress** (#58 / this PR) — fail-closed unsigned; not Released |
| 3.4 | Crash/diagnostics without secrets; uninstall preserves data | 3.2 | **in PR** (#60) |
| 3.5 | Clean-VM install/upgrade/uninstall script + **two friend tests** | 3.3 | **not started** |
| 3.6 | Release review vs full PRODUCT-BAR matrix | 3.5 | **not started** |

Maps to PRODUCT-BAR journey **Install & open** + Release QA. Do **not** declare Released until 3.6 **verified**.

---

## Orca task list (create in dependency order)

| Order | Proposed Orca task title | Creates when | Status |
|---:|---|---|---|
| 1 | Gate 0.5 — file 3B references + SIGN-OFF | Human provided | **verified** |
| 2 | Gate 1.1+1.2 — shell tokens + Scout vertical slice | After 0.5 | **in progress** |
| 3 | Gate 1.3 — shared component freeze | After Scout slice | **not started** |
| 4 | Gate 1.4 — Draft vertical slice | After 1.3 | **blocked** |
| 5 | Gate 1.5 — state kit Scout+Draft | After 1.4 | **blocked** |
| 6+ | Gate 2.x parity tickets (one journey each) | After 1.5 | **not started** |
| n | Gate 3 packaging + friend trials | After 2.x pass | **not started** |

Keep Electron `v0.3.x` as production until Gate 3 packaging is **verified**.

---

## Parallel hygiene (non-blocking for UI craft, still required)

| Item | Status |
|---|---|
| Stabilize Windows `go test` TempDir/SQLite cleanup flakes | **not started** |
| Extend `--capture-ui` to Data + Settings pages | **not started** |
| Record performance baseline (startup, cold/warm scout, memory) | **verified** (local fixtures; see `perf-baseline.md`) |
| Enable Orca Computer Use permissions for interactive WPF QA | **blocked** (OS permissions unsupported in current session) |
| Keep Electron v0.3.x production until native packaging verified | policy |

---

## Explicit non-goals / forbidden moves

- Rewrite Go recommendation core without measured necessity  
- Replace WPF with another web shell  
- Parallel conflicting UI frameworks  
- Paid/cloud mandatory APIs  
- Auto-merge / silent PR deletion  
- Declaring MVP or preview as Released  
- Inventing Minimal Tactical 3B layout without approved screenshots  

---

## Immediate action (ONE next step)

1. **User (human approval):** provide approved Minimal Tactical **3B** Draft + Scout images.  
2. **Coordinator:** file under `docs/_gate0/3b-reference/`, write `SIGN-OFF.md`, mark Gate 0.5 **verified**, create Orca tasks 1.1–1.2.  
3. **Do not** start broad WPF redesign or Gate 1 implementation wave before step 2.
