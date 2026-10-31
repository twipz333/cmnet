# Gate 2.3 — GSI install UX + live reconciliation (#44)

**Branch:** `twipz333/cmnet-gate23-gsi`  
**Issue:** #44  
**Base:** `origin/master` @ `0f5e937` (Gate 2.2 Settings #46)  
**Evidence:** `dotnet publish` win-x64 self-contained + `--smoke-ui` + `--capture-ui` (+ `--capture-gsi`) → this folder  
**geometry.json:** **33** entries (unchanged CI matrix). GSI honesty PNGs are **outside** geometry (same pattern as Data `--capture-data` / Settings `--capture-settings`). Default CI `--capture-ui` must stay under 180s: GSI kit is opt-in (`--capture-gsi` / `--capture-gsi-only`).

## Outcome

1. **Install UX** — Settings → вкладка **GSI**: статус пути/файла, «Установить конфиг», browse папки `gamestate_integration`, открыть в Проводнике, честные инструкции (вкл. `gsi` + перезапуск CMNET + перезапуск Dota). API: `GET /api/gsi/status`, `POST /api/gsi/install` (обёртка над `gsi.Install`).
2. **Честный статус** — Draft badge + StateKit: ручной / GSI жду событие / GSI live (только чтение) / офлайн. Никогда не рисуем «live», если `meta.gsiSeen=false`.
3. **Reconciliation** — `draftBoardKey` больше не зависит от ephemeral `status`; HeroCatalog не пересобирает одинаковый список (scroll/selection); Apply/New/Undo/Redo gated на `gsiMode`.
4. **Ручной режим** — при `gsiMode=false` apply/undo/redo/new-draft работают как раньше.

## Captures (SAMPLE DATA labeled)

| Surface | Files |
|---|---|
| Settings GSI | `gsi.png`, `gsi-missing.png`, `gsi-installed.png`, `gsi-waiting.png`, `gsi-live.png`, `gsi-offline.png` |
| Draft GSI chrome | `gsi-draft-manual.png`, `gsi-draft-waiting.png`, `gsi-draft-live.png` |
| CI matrix (unchanged) | Draft×5×3 + Scout×5×3 + scout empty/loading/error → `geometry.json` Count==33 |

## Verify

```text
go build -o native/out/advisor.exe ./cmd/advisor
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out
native/out/Cmnet.Native.exe --smoke-ui
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/gsi-2b-wip --capture-gsi
# optional fast GSI re-shot (does not rewrite geometry.json):
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/gsi-2b-wip --capture-gsi-only
```

## Honesty

- Все GSI captures помечены `SAMPLE DATA · …`.  
- Preview install не пишет реальный Dota cfg.  
- Live GSI на чистой VM с настоящей Dota — отдельная ручная проверка (не заявлена verified здесь).  
- Scout/Draft/Data/Settings 3B не передизайнены.

## Deferred / out of Gate 2.3

| Gap | Rationale |
|---|---|
| Hot-enable GSI без перезапуска ядра | Слушатель поднимается при старте (`ShellConfig.GSI`) |
| Signed installer / auto-update | Gate 3 |
| Independent product-owner visual OK | Required before merge; this PR does not self-approve |

## Ready-for-acceptance

Implementer recommends acceptance after independent review of GSI Settings + Draft badge + screenshots. Do **not** auto-merge.
