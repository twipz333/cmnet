# Gate 1 Scout 3B WIP — capture notes

**Branch:** `cmnet-gate1-scout-3b`  
**Issue:** #29  
**Date:** 2026-10-30  
**Evidence:** `dotnet publish` + `--smoke-ui` + `--capture-ui` PASS → this folder

## Before / after

| | Path |
|---|---|
| Approved 3B reference | `docs/_gate0/3b-reference/scout-approved.png` |
| Previous CI fixture (pre-Gate1) | `docs/_gate0/ci-screenshots/scout.png` |
| This WPF after | `scout.png`, `scout-1920x1080-layout100.png`, `scout-empty.png`, `scout-loading.png`, `scout-error.png` |

## What landed

- Theme tokens locked to SIGN-OFF (`#0B1117` / `#121A23` / `#172128` / `#3DD9C4` / `#67D9B8` / `#F06A6A` / `#F3F7FB`).
- Metric strip: найдено / данные / скрыто / ошибки.
- Opponent cards: status badge + observed WR/games (aggregate of source `topHeroes` only).
- Threat modules: signature heroes, pool intersections, key risks, observational ban candidate — each with honest empty copy when evidence is missing.
- Selected-player insights + quick actions; cancel control wired to `/api/ops/cancel` (enabled only while scout ops are active).
- Go core unchanged; UI still renders snapshot/stats only.

## Honesty P1 follow-up (error + retained topHeroes)

When `scoutStatus` is `error` or `stale` but prior `topHeroes` remain, opponent cards and the selected panel now label WR/heroes as **кэш / устарело / не свежие** (and suppress fresh-style insight bullets). Threat modules still use only `ok` reports. Re-captured `scout-error.png` after the change.

## Remaining gaps (honest)

1. **Not pixel-identical to the mock** — sidebar glyphs, metric icons, and bottom full-width player strip from the board are approximated (side split kept for viewport acceptance at 1280@150%).
2. **“Опасные пересечения” ≠ invented combos** — only shared heroes across pools; mock-style duo names are not fabricated.
3. **“Рекомендованный бан” is observational** (≥12 games in confirmed pools), not a draft-model ban score.
4. **Cancel** is present and API-backed; not proven under a live long scout job in this PR (fixture shows disabled when idle).
5. **Draft IA** intentionally untouched beyond shared tokens (parallel Gate 1 Draft issue).
6. **Independent review** landed in `REVIEW.md` (ACCEPT_WITH_GAPS); P1 honesty labeling addressed in this follow-up — still needs user visual OK before merge.
7. Full DPI matrix PNGs in this folder are local WIP; git keeps a small sample via `.gitignore` exceptions.
