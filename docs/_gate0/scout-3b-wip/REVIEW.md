# Independent review — PR #33 Scout 3B vertical slice

**Reviewer:** Cursor (separate from implementer; Codex unavailable)  
**PR:** https://github.com/twipz333/cmnet/pull/33 (`cmnet-gate1-scout-3b` @ `28d4d1d`)  
**Date:** 2026-10-30  
**Scope:** Visual compare `scout-3b-wip/scout.png` vs `3b-reference/scout-approved.png`; Gate 0.5 `DELTAS.md` (S1–S6); read-only audit of `ScoutingView.cs`, `MatchVisuals.cs`, `Theme.xaml`, `NativeWindow.cs`. No code fixes; no merge.

## Verdict: **ACCEPT_WITH_GAPS**

Gate 1 Scout IA and locked 3B tokens are in place with honest empty/partial threat modules and cancel wiring to `/api/ops/cancel`. Remaining work is craft polish, one honesty labeling gap on error+retained `topHeroes`, and unproven live cancel — not a redesign blocker and not invented intel.

---

## Evidence reviewed

| Artifact | Result |
|---|---|
| `docs/_gate0/scout-3b-wip/scout.png` | Populated SAMPLE DATA: metric strip 5/5 · 3/5 · 1 · 1; five opponent cards; threat grid; selected NightFall |
| `scout-empty.png` / `scout-loading.png` / `scout-error.png` | Honest empty copy; loading enables cancel via active ops; error flips badges |
| `docs/_gate0/3b-reference/scout-approved.png` + `SIGN-OFF.md` | Canonical 3B column + tokens |
| `docs/_gate0/3b-reference/DELTAS.md` | **Not on this PR branch** — read from `docs/gate0-evidence-and-3b-ref` (S1–S6 Scout rows) |
| Native diffs vs `master` | Theme tokens; Scout metrics/cards/threat modules; cancel enablement; selected-player insights |

---

## Visual vs `scout-approved.png`

**Aligned (directionally 3B):**
- Dark canvas / surface / teal accent hierarchy reads as Minimal Tactical 3B, not Soft 3A / Premium 3C.
- Four-card metric strip (найдено / данные / скрыто / ошибки).
- Five opponent cards with status + WR/games emphasis.
- Threat block with signature / intersections / risks / ban candidate.
- Selected-player panel with observations + actions.
- SAMPLE DATA / PREVIEW watermarks prevent mistaking fixtures for live intel.

**Still divergent (expected craft gaps — also in NOTES.md):**
1. Sidebar chrome (MATCH/SYSTEM groups, Local Client footer) ≠ mock’s flat nav + «Go Core» foot; acceptable per DELTAS shared D7/S note.
2. No mock «Quick Scout» toggle / «updated N min ago» chrome (good — those would be fake freshness if invented).
3. Opponent cards use initials/letter avatars when artwork missing; mock shows portraits + hero chip row on the card (chips moved to selected panel / threat modules).
4. «Опасные пересечения» shows honest empty («не выдумываем связки») instead of mock duo bars (Pudge+Warlock…) — **correct** product choice.
5. Ban card is observational (Tusk · «Наблюдаемый приоритет»), not mock «High Priority Ban» marketing.
6. Layout is side-split intel (threat \| selected) rather than mock’s denser bottom component strip; NOTES gap #1.
7. Capture viewport / DPI sample ≠ pixel-identical board crop.

---

## DELTAS.md (Scout S1–S6) closure

| # | Pre-Gate1 delta | PR #33 status |
|---|---|---|
| **S1** | Four status cards missing | **Closed** — `ScoutMetricStrip` + counts |
| **S2** | Threat modules missing | **Mostly closed** — four modules present; combos = pool overlaps only; ban = observational ≥12 games, not draft-model score |
| **S3** | Weaker player cards | **Mostly closed** — status pills + WR/games; portraits/hero chips on card still thinner than mock |
| **S4** | No selected insights + quick actions | **Partial** — observations yes; actions are «Повторить разведку» / «Составы» toast, not mock «открыть профиль / аналитика / заметки» |
| **S5** | Flat «Аналитика состава» bullets | **Closed** — replaced by threat grid |
| **S6** | Partial roster in old fixture | **Closed** when roster filled (empty scenario still shows five empty seats) |

Draft rows D1–D7 correctly out of scope for this PR (shared tokens only).

---

## Honesty (no fake intel)

**Pass — intentional design:**
- File headers and UI subtitle: «без выдуманной аналитики» / «No estimates or invented scouting».
- Threat hero math uses only `scoutStatus == "ok"` reports (`readyReports`).
- Intersections: shared hero IDs across pools; disclaimer when populated; `HonestEmpty` when none.
- Ban candidate: max observed comfort with `Games >= 12`; empty copy explicitly refuses to substitute a ban model; pill «Наблюдаемый приоритет».
- Risks: empty roster / hidden / source errors / thin samples / core smurf labels only.
- Insights derive from `topHeroes` aggregates; thin-pool note denies skill verdict.
- Empty/error threat modules stay empty of fabricated combos/bans (verified on `scout-empty.png`, `scout-error.png`).

**Gap — error scenario retained heroes (P1 honesty labeling):** ~~open~~ **Closed (follow-up on this PR).**
- `UiPreview` error path still retains prior `topHeroes` (honest residual evidence).
- Cards now show muted WR + `кэш · N игр` / `устарело · N игр`; selected panel prefixes `Кэш ·` / `(не свежие)`, captions `ГЕРОИ · КЭШ` / `ГЕРОИ ИСТОЧНИКА · КЭШ`, and replaces fresh insight bullets with an explicit saved-sample note.
- Verified on re-captured `scout-error.png` (SAMPLE DATA · error).

**Naming oversell (P2, mitigated):**
- Module titles «Опасные пересечения» / «Рекомендованный бан» inherit mock language; body copy and observational pill mitigate. Prefer renaming ban module to «Наблюдаемый бан-кандидат» in a follow-up if captains misread it as model advice.

---

## Cancel wiring

| Check | Finding |
|---|---|
| Control present | `cancelScoutButton` in `ScoutingView.cs` |
| Endpoint | `POST /api/ops/cancel` via `RunAction` |
| Enablement | `NativeWindow.UpdateLiveState`: enabled iff `ops.active` has `kind` `scout` or `scout-allies` |
| Idle fixture | Cancel disabled / dimmed (`scout.png`, `scout-empty.png`) |
| Loading fixture | Active scout op → Cancel enabled (`scout-loading.png`) |
| Live proof | **Not in PR** — NOTES #4; fixture-only. Acceptable for ACCEPT_WITH_GAPS; Gate acceptance of cancel UX still needs one manual/long-job check |

No Go recommendation engine changes in native slice (matches PR claim).

---

## Token fidelity (`Theme.xaml` vs SIGN-OFF)

| Token | SIGN-OFF | Theme | |
|---|---|---|---|
| Background | `#0B1117` | `CanvasBrush` `#0B1117` | OK |
| Surface | `#121A23` | `PanelBrush` `#121A23` | OK |
| Surface 2 | `#172128` | `RaisedBrush` `#172128` | OK |
| Accent | `#3DD9C4` | `AccentBrush` `#3DD9C4` | OK |
| Success | `#67D9B8` | `SuccessBrush` `#67D9B8` | OK |
| Warning | `#F3B562` | `WarningBrush` `#F3B562` | OK |
| Danger | `#F06A6A` | `ErrorBrush` `#F06A6A` | OK |
| Text | `#F3F7FB` | `TextBrush` `#F3F7FB` | OK |

Extra shell brushes (`SidebarBrush` `#0D141C`, `PrimaryBrush`, `HoverBrush`, …) are coherent derivatives, not alternate brand accents. Accent moved from `#30D9C4` → `#3DD9C4` as required.

---

## Remaining gaps (ordered)

1. ~~**P1 — Error + retained `topHeroes`**~~ **Closed** — cached/stale labeling on cards + selected panel; see honesty section and `scout-error.png`.
2. **P2 — Live cancel proof:** one real in-flight scout cancel (or automation against ops.active).
3. **P2 — S4 quick actions:** either implement real profile/notes links or drop mock parity claim; current «Составы» toast is a stub.
4. **P3 — Visual craft:** portraits/glyphs, metric icons, denser selected-player strip vs board; keep honesty over pixel chase.
5. **P3 — Docs:** land `DELTAS.md` on the evidence branch / PR set so reviewers need not cross-branch; update S1–S6 status table after this review.
6. **Out of scope:** Draft Gate 1 (D1–D7); Go core; merge without user approval.

---

## What would flip the verdict

- **→ REQUEST_CHANGES:** shipping invented duo «combos», fake ban model scores, or removing observational disclaimers.
- **→ BLOCK:** silent Go/engine changes under a UI PR, or fixtures presented as live intel without SAMPLE markers.
- Neither observed.

---

## Recommendation to coordinator

Treat PR #33 as **acceptable Gate 1 Scout craft with documented gaps**. Do **not** merge until user visual OK; prefer a tiny honesty follow-up (P1) before or immediately after. Independent review obligation from NOTES #6 / PR test plan is satisfied by this file.
