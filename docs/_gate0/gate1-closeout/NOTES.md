# Gate 1 close-out — state kit + shared chrome (#38)

**Branch:** `twipz333/cmnet-gate1-closeout`  
**Issue:** #38  
**Base:** `origin/master` @ `fba7271` (Scout #33 + Draft #37)  
**Evidence:** `dotnet publish` win-x64 self-contained + `--smoke-ui` + `--capture-ui` → this folder  
**geometry.json:** **33** entries (unchanged CI matrix). Extra honesty PNGs require `--capture-honesty` (not in default CI capture; keeps wall clock under 180s).

## Outcome

1. Whole-app **Evidence state kit** (`native/StateKit.cs`) on Scout + Draft + connection chrome:  
   `empty` / `loading` / `offline` / `partial` / `error` / `stale` use warning/danger/muted tones — never success green as “all good”.
2. Degraded Draft recommendation lists show an explicit placeholder card (not a blank list that reads as “model found nothing worth picking”).
3. D7 nav chrome: flat rail (no MATCH/SYSTEM inventing IA), footer `GO CORE · ЛОКАЛЬНО`; same five destinations.
4. Scout ban module renamed to **«Наблюдаемый бан-кандидат»** (honesty P2 from Scout REVIEW).

## Captures (SAMPLE DATA labeled)

| Surface | Files |
|---|---|
| Happy / DPI | `scout.png`, `draft.png`, `*-1920x1080-layout100.png`, full matrix in `geometry.json` |
| Scout states | `scout-empty.png`, `scout-loading.png`, `scout-error.png`, `scout-partial.png`, `scout-stale.png`, `scout-offline.png` |
| Draft states | `draft-empty.png`, `draft-loading.png`, `draft-error.png`, `draft-partial.png`, `draft-stale.png`, `draft-offline.png` |

## NOTES triage (Scout + Draft WIP)

### Fixed (high impact)

| Gap | Action |
|---|---|
| State kit not whole-app; failures could look quiet/success | Shared banners + connection chrome + Draft rec placeholders |
| Scout REVIEW P2 ban title oversell | Renamed module + empty copy |
| D7 nav MATCH/SYSTEM vs 3B flat rail | Flattened; no new destinations |
| Draft empty/loading/error left prior rec rows (fixture refresh bug) | Force `recommendationKey` reset on scenario load |

### Deferred (explicit, low impact / out of Gate 1)

| Gap | Rationale |
|---|---|
| Pixel-identical sidebar glyphs / metric icons / bottom player strip | Craft polish; honesty > pixel chase (Scout NOTES #1, Draft #1 leftover density) |
| Role chips as second filter row vs mock dropdown | Functional parity OK (Draft NOTES #2) |
| Enemy intel compact chips vs mock player-strip bars | Observational only; no invented bars (Draft NOTES #4) |
| Live GSI draft clock | No wire field; SAMPLE timer only (Draft NOTES #5) |
| Live cancel proof under long scout job | Fixture-proven; live proof = Gate QA (Scout NOTES #4) |
| S4 quick actions «профиль / заметки» | Would invent destinations; keep toast stubs (Scout REVIEW P2) |
| Live WR/tags when core omits why | Honest omission (Draft NOTES #6) |
| Independent product-owner visual OK | Required before merge; this PR does not self-approve |

## Honesty

- All captures watermarked `SAMPLE DATA`.
- Offline badge: gold `SAMPLE · офлайн`, never success green.
- Error+retained heroes remain labeled кэш/устарело (Scout #33 follow-up preserved).
- No Go recommendation engine changes.

## Verify

```text
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out
native/out/Cmnet.Native.exe --smoke-ui
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/gate1-closeout
# optional Gate1 honesty PNGs (outside geometry.json / CI matrix):
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/gate1-closeout --capture-honesty
```
