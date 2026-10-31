# Gate 0B — Electron ↔ WPF parity and gaps

**Audit date:** 2026-10-30  
**Target:** `master` @ `c75ac6f3ab89c3b3fc3b37ece7b066aa5d2254a7`  
**Scope:** read-only product audit vs `docs/PRODUCT-BAR.md`, `docs/DELIVERY-BOARD.md`, `native/DESIGN.md`, `docs/LOCAL-FIRST-V1.md`  
**Method:** code + README + CI workflow evidence. No app redesign. Runtime friend-test / live GSI / installer UX on a clean VM = **unknown** unless noted.  
**Production client today:** Electron (`desktop/` + `internal/web/static/ui/`). WPF (`native/`) is an explicit **preview** (`native/README.md`).

### Status legend

| Status | Meaning |
|---|---|
| **working** | Wired to real Go APIs / shell; no intentional dead control |
| **partial** | Present but missing important Electron behaviors or edge states |
| **stub** | UI visible; primary action disabled or non-functional by design |
| **missing** | No native surface / journey |
| **unknown** | Needs hands-on Windows verification; code alone insufficient |

---

## 1. Minimal Tactical 3B reference — BLOCKING

**Status: BLOCKING — approved reference images not found.**

| Searched | Result |
|---|---|
| This repo (`**/*.{png,jpg,jpeg,webp,gif}`) | **0** image files |
| Paths / names containing `3B`, `tactical`, `reference`, `mockup` under repo | Code comments / docs only (`native/TacticalViews.cs`, `native/ScoutingView.cs`, product docs) |
| Sibling worktrees under `cmnet/` | CI fixture captures only (see below) — not approved originals |
| AgentMemory prior decision | Explicit: *«no approved original Minimal Tactical 3B image reference — BLOCKING»* |

**What exists but is NOT the approved 3B reference**

- CI artifact name `cmnet-wpf-3b-screenshots` (`.github/workflows/native.yml`) — WPF **render captures**, not a signed design spec.
- Fixture matrix writes `"sampleData": true` and `"dpiMode": "simulated layout and raster scale; not native monitor DPI"` (`native/UiPreview.cs`).
- Sibling copies under `cmnet-productization/docs/_gate0/ci-screenshots/` (same fixture class).

`docs/PRODUCT-BAR.md` and `docs/DELIVERY-BOARD.md` require the **actual approved screenshots** before broad visual implementation and visual sign-off. Label «Minimal Tactical 3B» in source comments is **not** a visual contract.

**Required unblock:** place user-approved Draft + Scout reference images in a reviewable path (suggested: `docs/_gate0/3b-reference/`) and record sign-off. Do not invent geometry from prose.

---

## 2. Feature parity matrix

Electron column = production UI under `internal/web/static/ui/` (+ `desktop/` shell). WPF = `native/*.cs`.

### 2.1 Draft

| Capability | Electron | WPF | Status | Evidence |
|---|---|---|---|---|
| Board picks/bans both sides | Yes — stage + ribbon | Yes — portrait strips | **partial** | Electron: `ui/draft.js`, `index.html`. WPF: `MatchVisuals.RefreshDraftBoard`. WPF lacks phase ribbon / step timeline |
| Hero portrait gallery + search | Yes + role filter | Yes search; **no role filter** | **partial** | Electron `#role-filter`. WPF `heroSearchBox` in `TacticalViews.cs` |
| Recommendations list | Yes + why/reasons | Top scores only; **no why** | **partial** | Electron `reasons(r.why)`. WPF `RecommendationRow` score only |
| Apply pick/ban | Yes | Yes (`/api/apply`) | **working** | `TacticalViews.CanApplyHero` + Apply button |
| Undo / redo | Gated by `canUndo`/`canRedo`; hidden in GSI | Always-clickable buttons | **partial** | Electron disables. WPF buttons always call `/api/undo|redo` without meta gates |
| New draft | Yes | Yes | **working** | `TacticalViews` → `/api/new-draft` |
| First-pick / side switch | Header controls | **missing** | **missing** | Electron `#first-pick`, side buttons. No WPF equivalent |
| Enemy next-move predictions | Yes (`preds` cards) | **missing** | **missing** | Electron `draft.js` preds. No WPF consumer of `result.preds` |
| Draft strength / win bar | Yes (`#strength`) | **missing** | **missing** | `index.html` strength block |
| Matchups on hero click | Dialog | **missing** (selection detail only) | **missing** / **partial** | Electron `#matchups`. WPF selection panel shows name+score |
| GSI live/readonly UX | Banner, readonly picker, hide history | **No GSI status UI**; `CanApplyHero` ignores `gsiMode` | **missing** / **partial** | Electron `meta.gsiMode`/`gsiSeen`. WPF apply gate omits GSI |
| Keyboard (search/undo) | Rich (`draft.js`) | Search Ctrl+F tooltip; limited | **partial** | WPF shell Ctrl+1…5 only (`NativeWindow`) |
| Decision / outcome logging | Outcome buttons when `meta.logging` | **missing** | **missing** | Electron `/api/outcome`. Absent in native |

### 2.2 Scout

| Capability | Electron | WPF | Status | Evidence |
|---|---|---|---|---|
| 5v5 roster seats | Yes + drag/drop/chooser | Yes seat edit dialog (no drag/drop) | **partial** | Electron `team.js`. WPF `BuildSide` / `EditSeatAsync` |
| Add by Steam ID / link | Yes | Yes textbox | **working** | `NativeWindow` add player + `/api/ops/scout*` |
| Ally + enemy scout jobs | Yes | Yes | **working** | `/api/ops/scout`, `/api/ops/scout-allies` |
| Address book / known players | Yes | Yes virtualized list | **working** | `UpdateKnownPlayers`, `KnownPlayers` |
| Player detail (heroes, rates) | Yes | Yes when selected | **partial** | `RenderScoutDetails`; depth vs Electron needs runtime compare → **unknown** fidelity |
| Honest scoutStatus (ok/private/error/stale/…) | Yes | Labels for ok/error/stale/no_stats/unresolved/empty | **working** (labels) | `MatchVisuals.RefreshScoutSignals` |
| Progress without full remount | SSE-aware partial update | Roster retained; reload on timer | **partial** | Comments in `NativeWindow`; still reloads stats every ~9s on Scout page |
| Cancel in-flight scout | Via ops cancel in Data/jobs UX | **No cancel control on Scout** | **partial** / **missing** | Electron jobs cancel. WPF Data shows activity text only |
| Empty / loading / error states | Page-level empty | Fixture scenarios in preview; live empty hints exist | **partial** | `bookEmptyHint`, toast on timeout; full PRODUCT-BAR state set not verified live |

### 2.3 Data

| Capability | Electron | WPF | Status | Evidence |
|---|---|---|---|---|
| Corpus metrics | Rich corpus + meta + daily chart | Four metric tiles + top picks list | **partial** | Electron `data.js`. WPF `BuildDataView` / `LoadDataAsync` |
| Sync / train triggers | Yes | Yes | **working** | `/api/ops/sync`, `/api/ops/train` |
| Live job phase + last line | Yes | One-line activity from SSE | **partial** | `UpdateLiveState` → `dataActivity` |
| Job history | Recent list | **missing** | **missing** | Electron `job-history` |
| Streaming job log | `/api/jobs/log` | **missing** | **missing** | Electron `openLog`/`pullLog` |
| Cancel job | Confirmable stop | **missing** | **missing** | Electron `/api/ops/cancel` |
| Source / auto-collection status | Shown | **missing** dedicated UI | **missing** / **partial** | Electron `data-sources`, auto phase |
| Charts / corpus explorer | Yes | **missing** | **missing** | `native/README.md` explicitly lists gap |

### 2.4 Settings

| Capability | Electron | WPF | Status | Evidence |
|---|---|---|---|---|
| App config.yaml editor | Typed sections + save | Typed fields + override checkboxes + save | **working** (core config) | Electron `settings.js` app tab. WPF `BuildSettingsView` |
| Open data directory | Via desktop menu / about | Explicit button | **working** | WPF explorer launch |
| Alerts / TTS timers | Full tab | **missing** (alert-* keys skipped) | **missing** | WPF skips `alert-` keys; Electron `/api/alerts` |
| Item rules editor | Tab | **missing** | **missing** | Electron rules tab |
| Discord bot config | Tab (gated) | Core started with `--discord=false` | **missing** (intentional for native?) | `AdvisorCore` args |
| About + auto-update UI | Desktop updater status | **missing** | **missing** | Electron `renderAbout` + `electron-updater` |
| First-run data-dir picker | `welcome.html` / `welcome.js` | Reuses Electron `desktop.json` / AppData discovery only | **partial** | `AdvisorCore.FindDataDirectory`; no welcome wizard |

### 2.5 GSI

| Capability | Electron | WPF | Status | Evidence |
|---|---|---|---|---|
| Live draft from GSI (engine) | Supported when core run with GSI | Same Go core **can** do GSI if configured | **unknown** (native launch path) | Core: `cmd/advisor`, `internal/web` GSI pump. Native spawn: `--web-addr … --open=false --discord=false` only — **no `--gsi` flag** in `AdvisorCore.cs` |
| GSI install UX | CLI `advisor gsi-install`; no dedicated modern-ui button found | **missing** | **missing** | No `/api/gsi-install` in web; CLI only |
| In-UI GSI connected / waiting | Yes | **missing** | **missing** | Electron source/GSI notes |
| Readonly board under GSI | Yes | Apply not gated on `gsiMode` | **partial** / risk | Server may reject; UI may look editable |

### 2.6 History / after-match

| Capability | Electron | WPF | Status | Evidence |
|---|---|---|---|---|
| Post-draft outcome (win/loss) | `/api/outcome` UI | **missing** | **missing** | `draft.js` |
| Decision reasons / survey | Classic + modern logging paths | **missing** | **missing** | classic `app.js` survey; modern outcome only partial |
| Browse past decisions | CLI `advisor log`; limited UI | **missing** | **missing** | README CLI; no WPF history page |
| Manual draft checkpoint restore | Engine persistence | Engine shared; **no dedicated UI** | **unknown** / **partial** | `LOCAL-FIRST-V1.md` persistence; WPF relies on core silently |

### 2.7 Friends / share

| Capability | Electron | WPF | Status | Evidence |
|---|---|---|---|---|
| Host share start (public relay) | Working in Electron host | Button **disabled** + tooltip | **stub** | `BuildShareView` `open.IsEnabled = false`; `CMNET_LOCAL_ONLY=1` |
| Guest receive by code | Working in Electron | Button **disabled** | **stub** | Same |
| Stop share | Yes | Enabled (calls stop) | **partial** | Useful only if session existed |
| Status display | Yes | Shows share meta from state | **partial** | Mostly empty under local-only |
| LAN / import-export alternative | Not finished | Explicitly **not** implemented | **missing** | `LOCAL-FIRST-V1.md` fail-closed boundary |
| Guest edition installer | `electron-builder.guest.js` + release assets | **missing** native guest SKU | **missing** | `desktop.yml` guest publish |

### 2.8 Install / update / packaging

| Capability | Electron | WPF | Status | Evidence |
|---|---|---|---|---|
| Friend-installable package | NSIS `cmnet-setup-*.exe` via `desktop.yml` | Zip preview artifact only | Electron **working** (unsigned — see §3); native **missing** installer | `desktop/package.json` nsis; `native.yml` zip |
| Desktop shortcut / Start Menu | NSIS flags | N/A for zip | Electron **working**; native **missing** | `package.json` `createDesktopShortcut` |
| Auto-update + rollback channel | `electron-updater` + `latest.yml` | **missing** | Electron **partial**/working; native **missing** | `desktop/main.js`; signing off (`CSC_IDENTITY_AUTO_DISCOVERY: false`) |
| Preserve data on uninstall | `deleteAppDataOnUninstall: false` | N/A | Electron **working** intent | `package.json` |
| Self-contained runtime (no SDK) | Yes for Electron users | `PublishSingleFile` self-contained preview | **partial** | Still requires correct `advisor.exe` sidecar adjacency |
| Code signing / authenticity | Disabled in CI | N/A | **partial** / **missing** vs PRODUCT-BAR | `desktop.yml` |

### 2.9 Shell / cross-cutting

| Capability | Electron | WPF | Status | Evidence |
|---|---|---|---|---|
| Navigation Draft/Scout/Data/Settings/Friends | Hash router | Sidebar | **working** | Both |
| Offline / reconnect messaging | Offline bar | Connection badge + boot errors | **partial** | Needs live verify |
| Demo / sample labeled offline experience | Preview fixtures only in CI | No first-run demo product path | **missing** vs PRODUCT-BAR first-run | PRODUCT-BAR §1; UiPreview sampleData is CI-only |
| Simultaneous Electron+WPF same DB | Warned against | Warned against | **risk documented** | `native/README.md`, `LOCAL-FIRST-V1.md` |
| IPC token (native) | Tokenless loopback legacy | Bearer `CMNET_IPC_TOKEN` | **working** (native) | `AdvisorCore`, `LOCAL-FIRST-V1.md` |

---

## 3. Broken or simulated user journeys

### Dead / stub controls (user sees UI that cannot complete the job)

1. **Friends → «Открыть сессию» / «Получить драфт»** — always disabled; tooltips admit public relay off and LAN not ready (`NativeWindow.BuildShareView`). Journey for «share with friends» is **blocked** on native.
2. **Data → no Cancel / no log viewer** — user can start sync/train but cannot stop or inspect streaming log in WPF (Electron can). Long jobs feel fire-and-forget.
3. **Settings → no Alerts / Rules / Discord / About-update tabs** — captain cannot finish Electron-equivalent maintain path.

### Misleading or incomplete journeys

4. **Draft under potential GSI** — WPF does not surface GSI state and does not disable Apply on `gsiMode`; risk of «looks editable / fails oddly» (**unknown** without runtime with GSI-enabled core; native spawn currently omits `--gsi`).
5. **Undo/Redo always enabled visually** — may no-op or error depending on server; Electron gates this.
6. **Draft missing preds / why / first-pick / strength** — board looks like a draft cockpit but omits the Electron «why this pick» and enemy-intent loop that PRODUCT-BAR calls out.
7. **Scout primary view hides roster editor** behind «Составы и адресная книга» — workable, but empty enemy seats + toast «Добавь противников…» is easy to miss if editor collapsed (`ScoutingView`).
8. **CI / `--capture-ui` screenshots use sampleData** — useful for geometry smoke, **not** proof of premium fidelity or real scout honesty (`UiPreview.cs`).

### Missing empty / error / offline product states (vs PRODUCT-BAR)

9. No first-run welcome / choose-or-create data dir in WPF (Electron has `welcome.html`).
10. No labeled demo corpus for zero-data friends install on native.
11. No dedicated after-match history empty state (feature missing entirely).
12. Friends page shows collaboration chrome while actions are disabled — honest tooltips, but still a **simulated collaboration** surface.

---

## 4. Installer / update / data migration readiness (non-developer friend)

| Gate question | Verdict | Evidence |
|---|---|---|
| Single reputable Windows installer for **native** WPF? | **No** | Preview zip via Actions (`native.yml`); README says Electron remains production |
| Electron friend path exists? | **Yes (host + guest NSIS)** | Releases through `v0.3.3` (`gh release list`); `desktop.yml` |
| Signed binaries / authenticity verification? | **Not in CI** | `CSC_IDENTITY_AUTO_DISCOVERY: 'false'` |
| Native auto-update + rollback? | **Missing** | No updater in `native/` |
| Electron auto-update? | **Implemented** | `electron-updater` in `desktop/main.js` / settings About |
| Shared data directory discovery? | **Partial** | WPF reads `%APPDATA%\cmnet\desktop.json` / `cmnet.db` (`AdvisorCore.FindDataDirectory`) |
| Tested Electron→WPF migration + backup/rollback? | **Not evidenced in-repo as a shipped test** | Contract described in `LOCAL-FIRST-V1.md` (`Store.Backup`, migration 8); PRODUCT-BAR gate still open |
| Concurrent Electron + WPF on one DB? | **Unsafe / forbidden** | Explicit warnings; not mechanically prevented |
| Clean uninstall preserving data? | Electron NSIS intent yes; native N/A | `deleteAppDataOnUninstall: false` |
| Friend can finish session with **native** unassisted? | **No** (preview) | Needs unzip, sidecar layout, no update channel, Friends stub, GSI install CLI-only |

**Bottom line for friends:** Electron v0.3.x is the only near-friend path; it still fails PRODUCT-BAR packaging items (signing, full release QA). Native is **not** ready for non-developer distribution.

---

## 5. Ranked product-impact issues

Impact = blocks PRODUCT-BAR happy path or release gate. Severity is product, not code aesthetics.

| Rank | Issue | Impact | Status |
|---:|---|---|---|
| 1 | **Approved Minimal Tactical 3B reference images absent** | Blocks Gate 0 visual sign-off and Gate 1 craft; any «3B polish» without them invents design | **BLOCKING** |
| 2 | **No native installer / update / signed release channel** | Friends cannot meet «install & open» on WPF target | **missing** |
| 3 | **Friends/share journey stubbed under `CMNET_LOCAL_ONLY` with no LAN/import alternative** | PRODUCT-BAR friends path unmet on native; Electron still uses public relay (migration tension) | **stub** / **missing** alternative |
| 4 | **GSI not first-class in native** (no `--gsi` in spawn, no install UX, no status, Apply not GSI-aware) | During-match live path incomplete | **missing** / **partial** |
| 5 | **Draft parity holes** (no enemy preds, why/explain, first-pick/side, strength, matchups, outcome log) | Core captain loop thinner than Electron | **partial** / **missing** |
| 6 | **Data job ops incomplete** (no cancel, log stream, history, charts) | Maintain/sync trust broken for long jobs | **partial** |
| 7 | **Settings incomplete** (alerts/rules/discord/about-update) + **no welcome/first-run** | Maintain + first-run gates | **missing** / **partial** |
| 8 | **After-match / decision history absent in WPF** | PRODUCT-BAR «after match» unmet | **missing** |
| 9 | **Migration/backup/rollback not demonstrated as release evidence**; dual-client DB risk | Data integrity gate | **unknown** / documented risk |
| 10 | **CI geometry captures ≠ visual acceptance**; sampleData fixtures | False confidence if treated as 3B proof | process risk |
| 11 | Electron production still **unsigned** in CI | Trust bar for «~$10 product» | **partial** |
| 12 | Undo/Redo and other ungated controls | Small UX honesty issues | **partial** |

---

## 6. Honest unknowns (labeled)

- Interactive Windows walkthrough of this SHA’s WPF exe (scroll jumps, DPI 125/150 on real monitors, focus steal) — **unknown** here; CI smoke + fixture captures only.
- Whether Go core rejects `/api/apply` when `gsiMode` despite WPF enabling Apply — **unknown** without live GSI session.
- Depth parity of scout player intelligence cards vs Electron — **unknown** without side-by-side runtime.
- Real cold/warm scout timings and memory — **unknown** (Gate 0 baseline not measured in this task).
- Whether latest GitHub Actions `cmnet-wpf-3b-screenshots` for `c75ac6f` were downloaded and human-reviewed — **unknown** in this worktree (sibling has copies; not authoritative acceptance).
- Electron friend test unassisted on clean VM for v0.3.3 — **unknown** (release exists; PRODUCT-BAR friend test still unchecked on board).

---

## 7. What already looks comparatively solid (do not overclaim «verified»)

Code-backed, not friend-verified:

- Native shell navigation + smoke (`AssertNavigationWorks`, `--smoke-ui` in `native.yml`).
- Scout roster / address book / status labels / Steam add / scout ops posting.
- Draft board portraits, gallery, search, recommendations scores, apply/new/undo/redo API wiring.
- Data sync/train triggers + basic corpus metrics.
- Settings core config save + open data folder.
- Local-only fail-closed share (safer than silently calling relays).
- Deterministic Go core remains single calculation owner (architecture intact).

---

## 8. Recommended next Gate 0 / Gate 1 inputs (no design invented)

1. **Unblock:** obtain and check in approved 3B Draft + Scout reference images under `docs/_gate0/3b-reference/` with explicit user sign-off note.
2. Keep Electron as production until native installer + parity tickets land.
3. Open narrowly scoped tickets from §5 ranks 3–8 with acceptance = real WPF exe + empty/error paths — after references exist.
4. Treat CI screenshots as geometry regression only until compared to approved references at target DPI.

---

## 9. Audit provenance

| Item | Value |
|---|---|
| Git HEAD | `c75ac6f3ab89c3b3fc3b37ece7b066aa5d2254a7` |
| Latest known release (query) | `v0.3.3` |
| App code modified by this audit | **None** (read-only) |
| Files written | `docs/_gate0/parity-and-gaps.md`, `docs/_gate0/NOTES.md` |
