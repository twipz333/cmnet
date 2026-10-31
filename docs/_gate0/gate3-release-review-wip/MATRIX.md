# Gate 3.6 — PRODUCT-BAR release-review matrix

**Issue:** [#68](https://github.com/twipz333/cmnet/issues/68)  
**Orca Run:** `run_d7ae77d33f9b`  
**Review base:** `origin/master` @ `86544a9` (includes Gate 3.5 script #64, address-book contrast #65, seat Steam URL #67)  
**Desktop drop reviewed:** `C:\Users\dimag\Desktop\cmnet-native-0.4.0` (README: Git `86544a9`, built 2026-11-01; setup SHA256 `E5CF3A859D34CFF325FAFFD3124103FFD0B35B5A2A2C37D9CB1CA022B7B4628F`, Authenticode **NotSigned**)  
**Released:** **NO** — do not label v0.4.0-native as Released.

Status vocabulary (PRODUCT-BAR): **not started** / **in progress** / **verified** / **blocked**.

---

## Hard release gates

| Gate | Status | Evidence (PRs / NOTES / Desktop) | Honesty note |
|---|---|---|---|
| Product identity | **in progress** | 3B refs + sign-off [`../3b-reference/SIGN-OFF.md`](../3b-reference/SIGN-OFF.md); Scout #33 / Draft #37 / Gate 1 close-out #42; naming #55 (`gate3-naming-wip/`); Desktop drop branding CMNET 0.4.0-native | Craft deltas vs approved boards still deferred (gallery density, enemy-intel strip, icon polish). Independent product-owner visual OK not filed as final sign-off. |
| First run | **in progress** | Welcome / first-run #46 + #55; NSIS package #57 (`gate3-installer-wip/NOTES.md`); Desktop drop `cmnet-native-setup-0.4.0.exe` + `README.txt` | Scripted install/smoke works; **two unassisted friend first-runs still empty** (`gate3-friend-trial-wip/NOTES.md`). SmartScreen «Unknown publisher» expected while unsigned. |
| Functional parity | **in progress** | Gate 0B baseline [`../parity-and-gaps.md`](../parity-and-gaps.md); closed slices #45 Data, #46 Settings, #47 GSI, #51 History, #52 Friends, #53 Migration; Scout/Draft #33/#37/#42 | Matrix no longer «mostly missing», but residual Electron gaps remain (alerts/TTS, item rules, Discord, some Draft craft). Electron `v0.3.x` still production until native Released. |
| Data integrity | **verified** *(path)* | Migration backup/rollback + lock refuse #53 (`migration-2b-wip/NOTES.md`); uninstall preserves `%APPDATA%\cmnet` #57/#61 + Gate 3.5 script marker (`script-evidence.json`) | Concurrent Electron+WPF on one DB still forbidden by contract; friend-machine migration walkthrough not in human trial tables. |
| UI resilience | **in progress** | State kit Scout+Draft+chrome #42 (`gate1-closeout/`); GSI live/readonly #47 (`gsi-2b-wip/`); seat URL/contrast bugs #65/#67 | Live cancel-under-load + suspend/resume + real-monitor focus/scroll proof incomplete; Orca Computer Use still **blocked** for interactive QA. |
| Visual inspection | **in progress** | Capture matrices under `scout-3b-wip/`, `draft-3b-wip/`, `gate1-closeout/`, `ci-screenshots/` (geometry Count==33); refs in `3b-reference/` | Captures use simulated DPI / fixture SAMPLE labels. Real 1366×768@100%, 1920×1080@125%, 2560×1440@150% + keyboard/a11y on physical monitors not signed off. |
| Performance | **in progress** | Single-host baseline [`../perf-baseline.md`](../perf-baseline.md) (#41) @ `fba7271` | Baseline ≠ release SLA. Live-network scout timings / scroll FPS still gaps; no regression re-measure at `86544a9`. |
| Packaging | **in progress** | Installer #57; update channel fail-closed #59 (`gate3-updates-wip/`); diagnostics + NSIS data preserve #61 (`gate3-diagnostics-wip/`); clean-VM script #64; Desktop drop @ `86544a9` | **Authenticode unsigned** on Desktop setup (`Get-AuthenticodeSignature` → NotSigned; manifest `"unsigned": true`). Trusted friend download path incomplete. |
| Release QA | **blocked** | Script PASS `gate3-friend-trial-wip/script-evidence.json`; human Friend 1/2 tables empty (#62); this matrix (#68) | PRODUCT-BAR: friend unassisted session required. Inventing friend PASS forbidden. Independent release review not closed. |

---

## Six happy-path journeys

| # | Journey | Status | Evidence | Remaining gap |
|---|---|---|---|---|
| 1 | Install & open | **in progress** | #55/#57/#64; Desktop `cmnet-native-0.4.0` @ `86544a9`; script install→smoke→upgrade→uninstall PASS | Human friend trials **BLOCKED**; unsigned SmartScreen; not on GitHub Releases as Released |
| 2 | Before match / Scout | **in progress** | Scout 3B #33; close-out #42; seat contrast #65; Steam vanity seat assign #67; Desktop drop notes fix | Deferred craft vs `scout-approved.png`; live long-job cancel proof still Gate QA |
| 3 | During match / Draft | **in progress** | Draft 3B #37; state kit #42; GSI install/reconcile #47 | Live draft clock honesty gap; deferred gallery/intel craft; preds/why depend on core wire |
| 4 | After match | **in progress** | History UI #51 (`history-2b-wip/NOTES.md`) | Depth vs Electron decision log / live post-match stats when sources absent needs friend-session proof |
| 5 | Friends | **in progress** | Local CMNETPACK1 import/export #52 (`friends-2b-wip/`); relay fail-closed under `CMNET_LOCAL_ONLY` | Unassisted friend **install + share** session not filed; #62 human track BLOCKED |
| 6 | Maintain | **in progress** | Data progress/cancel/log #45; Settings+welcome #46; updates fail-closed #59; diagnostics #61 | Production update trust key + Authenticode ops not shipped; sync/train under real rate limits not friend-proven |

---

## Gate 3 packaging slice roll-up (context only)

| Slice | Merged evidence | Slice status vs Released |
|---|---|---|
| 3.1 Naming | PR [#55](https://github.com/twipz333/cmnet/pull/55) · `gate3-naming-wip/` | **verified** code/docs — not Released |
| 3.2 Installer | PR [#57](https://github.com/twipz333/cmnet/pull/57) · `gate3-installer-wip/` · Desktop drop | **verified** package path — unsigned |
| 3.3 Updates | PR [#59](https://github.com/twipz333/cmnet/pull/59) · `gate3-updates-wip/` | **verified** fail-closed — no production trust key |
| 3.4 Diagnostics | PR [#61](https://github.com/twipz333/cmnet/pull/61) · `gate3-diagnostics-wip/` | **verified** local redaction + uninstall contract |
| 3.5 Friend trials | PR [#64](https://github.com/twipz333/cmnet/pull/64) · `gate3-friend-trial-wip/` · issue [#62](https://github.com/twipz333/cmnet/issues/62) | Script **verified**; humans **BLOCKED** |
| 3.6 This review | issue [#68](https://github.com/twipz333/cmnet/issues/68) · this folder | **in progress** — Released = **NO** |

---

## Desktop drop fingerprint (`86544a9`)

| Item | Value |
|---|---|
| Folder | `C:\Users\dimag\Desktop\cmnet-native-0.4.0` |
| Git (README) | `86544a9` |
| Setup | `cmnet-native-setup-0.4.0.exe` · 51 241 508 bytes · SHA256 `E5CF3A859D34CFF325FAFFD3124103FFD0B35B5A2A2C37D9CB1CA022B7B4628F` |
| Portable zip | `cmnet-native-0.4.0-win-x64.zip` · SHA256 `AF75492ABFFAA9EE0ED4051A30AE8D57E788FDFEFE7CE82B63B8C944C9B7FECA` |
| Manifest | `cmnet-native-package.manifest.json` → `"unsigned": true`, `"released_claim": false` |
| Authenticode | **NotSigned** |

---

## Verdict

**Released = NO.** Hard gate **Release QA** is **blocked** on real friend trials (#62); **Packaging** remains **in progress** while Authenticode is unsigned; several journey gates stay **in progress** with deferred craft / live proofs. Agents must not equate merged Gate 3.x PRs or a green Desktop smoke with PRODUCT-BAR Released.
