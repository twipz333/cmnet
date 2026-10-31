# Gate 0 — CMNET baseline audit (synthesis)

**Gate 0 overall:** **verified** for audit evidence; visual craft **unblocked** after 3B sign-off 2026-10-30 (`docs/_gate0/3b-reference/SIGN-OFF.md`)  
**Audit date:** 2026-10-30  
**Orca Run:** `run_d7ae77d33f9b` — *CMNET Production-Quality Native Release*  
**Baseline SHA:** `c75ac6f3ab89c3b3fc3b37ece7b066aa5d2254a7` (`master`, PR #26)  
**Open PRs:** none  
**Latest release:** Electron `v0.3.3` (2026-10-29) — only friend-installable package  
**Native CI:** [native.yml run 41084058862](https://github.com/twipz333/cmnet/actions/runs/41084058862) — **success** on same SHA  

**Language:** Implemented ≠ Verified ≠ Released (`docs/PRODUCT-BAR.md`). This report is evidence, **not** a release claim. No application code was changed.

**Sources synthesized:** Gate 0A (`task_cc8db2136c83`) → `build-evidence.md`; Gate 0B (`task_58001ed3106e`) → `parity-and-gaps.md` + `NOTES.md`; coordinator worktree copies under `docs/_gate0/`.

---

## A. Six Gate 0 deliverables (DELIVERY-BOARD checklist)

| # | Deliverable | Status | Evidence paths |
|---|---|---|---|
| 1 | Exact `master` SHA, open issues, last release, WPF build status | **verified** | SHA above; `gh` release `v0.3.3`; CI run 41084058862 success; local `dotnet publish` exit 0 in `build-evidence.md` |
| 2 | Windows CI 33-case geometry + Draft/Scout screenshots + `--smoke-ui` | **verified** (rendering only) | `docs/_gate0/ci-screenshots/` (33 cases + aliases); `docs/_gate0/local-capture/`; both `checks.txt` PASS; **sampleData fixtures**, not premium fidelity |
| 3 | `go test` / `go vet` / `dotnet publish` with exact failures | **verified** (honest: tests not green) | `build-evidence.md`; `docs/_gate0/logs/go-test.txt`; `docs/_gate0/logs/dotnet-publish.txt`; `go-test.log` / `dotnet-publish.log` mirrors. `go vet` exit 0; `go test ./...` exit **1** (Windows TempDir cleanup in `cache`/`declog`) |
| 4 | Electron → native parity matrix | **verified** | `docs/_gate0/parity-and-gaps.md` (Draft/Scout/Data/Settings/GSI/History/Friends/Install) |
| 5 | Approved 3B Draft+Scout reference images + visual sign-off | **verified** | User-supplied boards; `docs/_gate0/3b-reference/scout-approved.png`, `draft-approved.png`, `SIGN-OFF.md` |
| 6 | Measurable performance baseline (startup, cold/warm scout, UI, memory) | **verified** (single-host fixtures) | `perf-baseline.md`; live network scout + scroll FPS still open |

**Gate 0 stop rule:** deliverable **5** must become **verified** before Gate 1 UI craft. Do not invent 3B geometry from prose.

---

## B. Six PRODUCT-BAR user journeys (native WPF target)

| # | Journey | Status | Summary | Evidence |
|---|---|---|---|---|
| 1 | **Install & open** | **blocked** / **not started** for native | Electron NSIS works for friends; WPF = zip preview only, no Start Menu / updater | parity §2.8, §4; `native.yml` zip |
| 2 | **Before match / Scout** | **in progress** | Roster, Steam add, scout ops, status labels wired; no cancel on Scout; live fidelity **unknown**; fixture empty/loading/error PNGs exist | parity §2.2; `ci-screenshots/scout-*.png` |
| 3 | **During match / Draft** | **in progress** | Board/gallery/apply/new wired; **missing** preds, why, first-pick/side, strength, matchups, GSI UI; Undo/Redo ungated | parity §2.1, §2.5; `ci-screenshots/draft-*.png` |
| 4 | **After match** | **not started** | No outcome UI / decision history page in WPF | parity §2.6 |
| 5 | **Friends** | **blocked** (stub) | Share open/receive disabled under `CMNET_LOCAL_ONLY=1`; no LAN/import alternative | parity §2.7; `AdvisorCore` / `BuildShareView` |
| 6 | **Maintain** | **in progress** / **partial** | Sync/train + core settings exist; **missing** job cancel/log/history, alerts/rules/about-update, welcome first-run | parity §2.3–2.4 |

**Friend-ready as native product?** **No.** Electron `v0.3.3` remains the production client (`native/README.md`).

---

## 1. Real state of CMNET (with proof)

| Area | Verdict | Evidence |
|---|---|---|
| Git baseline | Clean `master` @ `c75ac6f` | `git rev-parse`; PR #26 merged |
| Go engine | Builds; `go vet` clean; **local `go test ./...` not green on this Windows host** | `build-evidence.md`, `logs/go-test.txt` |
| WPF native preview | **Publish + `--smoke-ui` + `--capture-ui` pass** | `dotnet publish` 0; smoke/capture 0 |
| Electron | Production through v0.3.3 | Releases; parity §4 |
| Visual 3B contract | **blocked** — no approved reference images | parity §1; zero design PNGs |
| Packaging (native) | **not started** | Zip preview only |

### PRODUCT-BAR hard release gates (snapshot)

| Gate | Status |
|---|---|
| Product identity (3B) | **blocked** |
| First run (native) | **not started** / **blocked** for friends |
| Functional parity | **in progress** |
| Data integrity | **not started** (contract only) |
| UI resilience | **unknown** live; fixture smoke only |
| Visual inspection | **blocked** (no reference); Draft/Scout fixtures **verified** as rendering |
| Performance | **not started** |
| Packaging (native) | **not started** |
| Release QA | **not started** |

### Orca task outcomes

| Task | ID | Dispatch | Verdict |
|---|---|---|---|
| Gate 0A build/test/CI | `task_cc8db2136c83` | `ctx_f34f2bfd3d90` | **verified** (honest: tests fail locally) |
| Gate 0B parity/gaps | `task_58001ed3106e` | `ctx_724e6e414ab9` | **verified** |
| Gate 0C synthesis | `task_f70568b5cc2f` | `ctx_e90554b19605` | **verified** on write of this report |

**Agents:** Cursor workers (Claude launcher unconfigured; Codex usage-limited). Computer Use: screenshot capability advertised; OS accessibility/screenshot permissions reported `unsupported` — interactive WPF Computer Use not relied on.

---

## 2. Screenshot references

**Important:** all captures are **WPF SAMPLE DATA fixtures** (`checks.txt`: no Go process, no user DB). They prove geometry/rendering, **not** 3B fidelity or live scout honesty.

### Canonical locations (this worktree)

| Set | Path |
|---|---|
| CI artifact (SHA-matched) | `docs/_gate0/ci-screenshots/` |
| Local re-capture after publish | `docs/_gate0/local-capture/` |
| Geometry matrix | 33 cases in `geometry.json` (Draft 5×3 + Scout 5×3 + empty/loading/error) |
| Meta | `ci-screenshots/checks.txt`, `local-capture/checks.txt` (identical PASS text) |

### Representative frames

| Screen | File | Observation |
|---|---|---|
| Draft 1920×1080 | `ci-screenshots/draft-1920x1080-layout100.png` | Sidebar + board + recommendations + gallery; sample picks |
| Scout 1920×1080 | `ci-screenshots/scout-1920x1080-layout100.png` | Opponent cards + analytics; 5th card can clip at some widths |
| Scout empty | `ci-screenshots/scout-empty.png` | Empty seats + empty profile pane |
| Scout error | `ci-screenshots/scout-error.png` | Per-player ERROR + honest source-error copy |
| Data / Settings pages | — | **No page PNGs** (nav bounds only in `geometry.json`) |

CI: https://github.com/twipz333/cmnet/actions/runs/41084058862 — artifact `cmnet-wpf-3b-screenshots`.

---

## 3. Issues ranked by product impact

Full matrix: [`parity-and-gaps.md`](./parity-and-gaps.md).

| Rank | Issue | Impact | Status |
|---:|---|---|---|
| 1 | **No approved Minimal Tactical 3B reference images** | Blocks Gate 0 visual sign-off and all Gate 1 craft | **blocked** / BLOCKING |
| 2 | No native installer / signed update channel | Friends cannot «install & open» on WPF | **not started** / missing |
| 3 | Friends/share stubbed (`CMNET_LOCAL_ONLY`); no LAN/import alternative | PRODUCT-BAR friends unmet on native | **blocked** / stub |
| 4 | GSI not first-class in native (no spawn flag / UI / Apply gating) | Live during-match path incomplete | **not started** / missing |
| 5 | Draft parity holes: preds, why, first-pick/side, strength, matchups, outcome | Captain loop thinner than Electron | **in progress** / partial |
| 6 | Data jobs: no cancel / log stream / history / charts | Maintain trust for long jobs | **in progress** / partial |
| 7 | Settings incomplete + no WPF welcome/first-run | First-run / maintain | **not started** / partial |
| 8 | After-match / decision history absent in WPF | After-match journey | **not started** |
| 9 | Electron→WPF migration/backup/rollback not demonstrated | Data integrity | **not started** / unknown |
| 10 | Treating CI `sampleData` captures as visual acceptance | False confidence | process risk |
| 11 | Local `go test ./...` fails (Windows TempDir cleanup) | CI confidence / agent loops | environmental |
| 12 | Ungated Undo/Redo and similar honesty issues | UX trust | **in progress** / partial |

---

## 4. Build / test facts (do not round up to «green»)

| Command | Exit | Notes |
|---|---:|---|
| `go vet ./...` | 0 | Clean |
| `go test ./...` | 1 | TempDir cleanup fails in `internal/cache`, `internal/declog` — suite **not** verified green on this host |
| `dotnet publish … -o native/out` | 0 | Self-contained win-x64 |
| `Cmnet.Native.exe --smoke-ui` | 0 | |
| `Cmnet.Native.exe --capture-ui` | 0 | PASS `checks.txt`; sampleData |

Details: [`build-evidence.md`](./build-evidence.md).

---

## 5. Roadmap to production release

See [`ROADMAP.md`](./ROADMAP.md). Mandatory order:

0. **Unblock:** deposit approved 3B Draft+Scout references + human sign-off (**no mass UI before this**).
1. **Gate 1:** cohesive 3B shell → Scout vertical slice → Draft (shared theme lock).
2. **Gate 2:** Data/Settings/GSI/history/friends-or-import + migration + resilience.
3. **Gate 3:** installer, updates, friend tests, full PRODUCT-BAR sign-off.

Electron remains production until native installer + parity tickets land.

---

## 6. Tracked Orca work

| ID | Title | Status |
|---|---|---|
| `run_d7ae77d33f9b` | CMNET Production-Quality Native Release | **in progress** |
| `task_cc8db2136c83` | Gate 0A evidence | **verified** |
| `task_58001ed3106e` | Gate 0B parity | **verified** |
| `task_f70568b5cc2f` | Gate 0C synthesis | **verified** (this report) |
| *(next)* | Gate 0.5 — place & sign off 3B references | **blocked** until human provides images; then executable |
| *(queued)* | Gate 1 Scout vertical slice | **blocked** on 0.5 |
| *(queued)* | Gate 1 Draft vertical slice | **blocked** on Scout shell contract |
| *(queued)* | Parity tickets ranks 3–8 | after visual lock |
| *(queued)* | Installer / update / friend trial | after journeys pass |
| *(hygiene)* | Stabilize Windows `go test` TempDir flakes | **not started** (non-blocking for UI) |
| *(hygiene)* | Performance baseline measurements | **verified** locally — `perf-baseline.md` / #39 |

---

## 7. ONE next executable task (human approval before implementation wave)

### NEXT: Gate 0.5 — Obtain and lock Minimal Tactical 3B visual reference

**User story:** As product owner I provide the approved Draft and Scout screenshots so agents stop inventing geometry.

**Change:** Place signed-off images under `docs/_gate0/3b-reference/` (`draft-approved.png`, `scout-approved.png`) plus a one-paragraph `SIGN-OFF.md` (who approved, date, resolution). Optionally attach source conversation/export.

**Acceptance:**

- Files exist and are human-legible full-screen compositions (**not** CI fixtures).
- Coordinator (or user) records visual sign-off comparing **one** current WPF Draft and **one** Scout fixture against the references, listing deltas **without** rewriting UI yet.
- Only then create Gate 1 Scout vertical-slice Orca task.

**Constraints:** No inventing 3B layout from prose. No broad WPF redesign until sign-off. No Go core rewrite. No auto-merge. No declaring release-ready.

**Owner:** Human (user) provides images → coordinator files them and opens Gate 1.

**Ask for human approval:** Do **not** start Gate 1 implementation worktrees until Gate 0.5 is **verified**.

---

## 8. Hand-off block

```text
TASK: Gate 0 / baseline truth
STATUS: BLOCKED (3B reference) — build/parity/synthesis evidence VERIFIED
BASE: c75ac6f3ab89c3b3fc3b37ece7b066aa5d2254a7
CHANGED: docs/_gate0/** only (no app code)
PROOF: build-evidence.md; parity-and-gaps.md; GATE0-REPORT.md; ROADMAP.md; CI run 41084058862; ci-screenshots/; local-capture/; logs/
DESIGN: approved 3B images ABSENT — BLOCKING
DATA/ROLLBACK: untouched user DB; audit read-only for app
GAPS: Gate 0 items 5–6 open; Data/Settings screenshots missing; go test not green locally; Computer Use permissions unsupported
SIX JOURNEYS: install blocked; scout/draft in progress; after-match not started; friends blocked; maintain partial
NEXT: Gate 0.5 — user supplies approved 3B Draft+Scout references into docs/_gate0/3b-reference/ (human approval before Gate 1)
```
