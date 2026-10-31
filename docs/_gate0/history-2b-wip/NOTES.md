# Gate 2.4 — After-match / decision history UI (#48)

**Branch:** `twipz333/cmnet-gate24-history`  
**Issue:** #48  
**Base:** `origin/master` @ `3e13ba0` (Gate 2.3 GSI #47)  
**Evidence:** `dotnet publish` win-x64 self-contained + `--smoke-ui` + `--capture-ui` (+ `--capture-history`) → this folder  
**geometry.json:** **33** entries (unchanged CI matrix). History honesty PNGs are **outside** geometry (same pattern as Data/Settings/GSI). Default CI `--capture-ui` must stay under 180s: History kit is opt-in (`--capture-history` / `--capture-history-only`).

## Outcome

1. **WPF «История»** — шестая точка навигации (Ctrl+4; Настройки/Друзья → Ctrl+5/6). Список сохранённых драфтов + детальный decision log (совет #1 vs выбранный герой, причина, предикт врага, сила доски — только из лога).
2. **API** — `GET /api/declog/history` (список, `?last=N`) и `GET /api/declog/history?id=` (деталь). Читает `decisions.jsonl` рядом с кэшем через `declog.Load`. Не выдумывает outcome/score/reason.
3. **StateKit** — empty / loading / partial / error / offline / ready. Partial когда нет outcome или provenance (bundle/patch/snapshot).
4. **Тема** — Theme.xaml + Surface/Evidence banner; Scout/Draft/Data/Settings/GSI не передизайнены.

## Captures (SAMPLE DATA labeled)

| Surface | Files |
|---|---|
| Happy | `history.png`, `history-ready.png` |
| States | `history-empty.png`, `history-partial.png`, `history-loading.png`, `history-error.png`, `history-offline.png`, `history-detail.png` |
| CI matrix (unchanged) | Draft×5×3 + Scout×5×3 + scout empty/loading/error → `geometry.json` Count==33 |

## Verify

```text
go build -o native/out/advisor.exe ./cmd/advisor
go test ./internal/web/ -count=1 -run DeclogHistory
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out
native/out/Cmnet.Native.exe --smoke-ui
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/history-2b-wip --capture-history
# optional fast History re-shot (does not rewrite geometry.json):
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/history-2b-wip --capture-history-only
```

Measured locally: capture wall clock ~34s; geometry Count==33.

## Honesty

- Все History captures помечены `SAMPLE DATA · history-<scenario>`.
- Preview не читает user `decisions.jsonl` и не стартует Go.
- Нет выдуманных winrate/confidence; «исход не записан» / «причина не записана» явные.
- Post-draft survey (win/loss chips на Драфте) — не в scope этого gate; исход по-прежнему пишется через существующие `/api/outcome` / GSI.

## API gaps addressed

| Gap before | Minimal fix |
|---|---|
| Только `/api/declog/review` (текущий драфт) | `/api/declog/history` list+detail поверх `declog.Load` |
| CLI `advisor log` only | Native UI + same file path (`declog.PathFor` / `Session.LogPath`) |

## Deferred / out of Gate 2.4

| Gap | Rationale |
|---|---|
| In-Draft post-match survey UI (Electron parity) | Separate UX; logging APIs already exist |
| Export / forget-player from WPF | CLI `advisor log --export/--forget` remains |
| Signed installer | Gate 3 |
| Independent product-owner visual OK | Required before merge; this PR does not self-approve |

## Ready-for-acceptance

Implementer recommends acceptance after independent review of History journey + screenshots. Do **not** auto-merge.
