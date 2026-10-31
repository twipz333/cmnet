# Gate 1 Draft 3B WIP — capture notes

**Branch:** `twipz333/cmnet-gate1-draft-3b`  
**Issue:** #31  
**Date:** 2026-10-31  
**Evidence:** `dotnet publish` win-x64 self-contained + `--smoke-ui` + `--capture-ui` → this folder

## Before / after

| | Path |
|---|---|
| Approved 3B reference | `docs/_gate0/3b-reference/draft-approved.png` |
| Previous CI fixture (pre-Gate1) | `docs/_gate0/ci-screenshots/draft-1920x1080-layout100.png` |
| This WPF after | `draft.png`, `draft-1920x1080-layout100.png`, `draft-empty.png`, `draft-loading.png`, `draft-error.png` |

## What landed (D1–D6)

- **D1** Full CM board: Radiant + Dire pick slots and ban strips, center turn ownership (`Ход Dire · pick`), timer chrome. Live captains-mode pick clock is **not** on the wire DTO — live shows honest «таймер хода недоступен»; SAMPLE fixture may set `meta.previewDraftTimerSec` and labels it `SAMPLE · демо-таймер`.
- **D2** Recommendation rows: integer score (`0.92` → `92`) + Comfort/Synergy/Counter/Gameplay tags from explicit sample `tags` or honest why-token derivation (`из наших` → Комфорт). No invented live meta factors (Factors remain non-wire).
- **D3** Gallery filter chips: «Все роли» / Поз.1–5 + Str/Agi/Int/Uni. Attribute map is UI taxonomy only (`HeroAttributes.cs`), not strength.
- **D4** Selected-hero panel: roles, WR/popularity when present in rec/positionMeta/why, **почему** bullets, primary CTA **«Выбрать героя»**.
- **D5** Enemy-intel strip + scout-signals on Draft: observational from `team.enemy` + scout stats; honest empty/partial/error copy — no fake intel.
- **D6** Three-column IA retained (`DraftRecommendations` | `DraftCatalog` | `DraftContext`) under the board, with intel row beneath.
- Shared `Theme.xaml` tokens reused (Scout #33). Go recommendation engine untouched.
- Scout capture regression included in the same `--capture-ui` run.
- `draft-empty/loading/error.png` are captured for review evidence but **not** counted in `geometry.json` (CI matrix stays **33** = Draft×5×3 + Scout×5×3 + scout empty/loading/error).

## Remaining gaps vs `draft-approved.png`

1. ~~**D7 MATCH/SYSTEM nav**~~ — flat rail + `GO CORE · ЛОКАЛЬНО` in Gate 1 close-out (#38); still text+glyph (not icon-only mock). Residual craft **deferred**.
2. **Role chips** are a second filter row under search (mock used a dropdown); functional parity, not pixel-matched. **Deferred**.
3. **Hero gallery density / portrait crop** approximate; virtualized grid vs mock’s fixed tiles. **Deferred**.
4. **Enemy intel cards** are compact observational chips, not the full mock player-strip with percentage bars. **Deferred** (no invented bars).
5. **Timer** in live play cannot show 0:28 without a GSI draft-clock field — intentional honesty; SAMPLE only.
6. **Tags / why / WR / popularity** in live mode only appear when core wires `why` / positionMeta / rates — SAMPLE fixture fills the visual contract with labeled demo fields.
7. Independent visual review by product owner still required before merge.
8. **State kit** empty/loading/offline/partial/error/stale on Draft (+ shared shell) — **Fixed** in #38; see `docs/_gate0/gate1-closeout/`.

## Honesty notes

- SAMPLE DATA watermark on all captures.
- `previewDraftTimerSec`, sample `tags`, `whyLines`, `winRate`, `pickRate` exist only in visual fixtures / explicit JSON — never fabricated from empty live state.
- Scout layout beyond shared tokens left alone.
