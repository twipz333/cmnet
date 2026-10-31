# Gate 2.2 — Settings + first-run (#43)

**Branch:** `twipz333/cmnet-gate22-settings`  
**Issue:** #43  
**Base:** `origin/master` @ `fa49dcc` (Gate 2.1 Data #45)  
**Evidence:** `dotnet publish` win-x64 self-contained + `--smoke-ui` + `--capture-ui` (+ `--capture-settings`) → this folder  
**geometry.json:** **33** entries (unchanged CI matrix). Settings honesty PNGs are **outside** geometry (same pattern as Data `--capture-data`). Default CI `--capture-ui` must stay under 180s: Settings kit is opt-in (`--capture-settings` / `--capture-settings-only`); HeroCatalog freeze / warmup discipline unchanged.

## Product cut-list (confirmed)

**KEEP**
1. Typed `config.yaml` editor with validation + save confirmation + open data folder  
2. About: version / data path / model / restart note — **without** auto-update  
3. In-app first-run / welcome for choose-or-create data dir (no console / SDK)

**CUT** (honest «Вне объёма» copy, not dead controls)
- Discord bot (`--discord=false`)  
- Alerts / TTS timers (no separate tab; `alert-*` keys remain skipped)  
- Item rules editor  
- Auto-update channel (Gate 3)

## Outcome

1. **Settings surface** — tabs Приложение / О программе / Вне объёма; StateKit evidence banner; field help; int/float/duration validation before `/api/config/save`; visible save confirmation strip.  
2. **First-run welcome** — Electron-compatible `%APPDATA%\cmnet\desktop.json` (`dataDir` + `setupDone`); bounded scan for existing `cmnet.db`; browse folder / file; start fresh under default data dir.  
3. **Shared chrome** — `Theme.xaml` tokens + StateKit empty/loading/error/offline/ready (no new design system).  
4. **No Scout/Draft/Data regression** — smoke navigates all pages; geometry Count==33.

## Captures (SAMPLE DATA labeled)

| Surface | Files |
|---|---|
| Happy | `settings.png`, `settings-ready.png` |
| States | `settings-empty.png`, `settings-loading.png`, `settings-error.png`, `settings-offline.png` |
| Tabs | `settings-about.png`, `settings-deferred.png` |
| First-run | `settings-welcome.png` |
| CI matrix (unchanged) | Draft×5×3 + Scout×5×3 + scout empty/loading/error → `geometry.json` Count==33 |

## Verify

```text
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out
native/out/Cmnet.Native.exe --smoke-ui
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/settings-2b-wip --capture-settings
# optional fast Settings re-shot (does not rewrite geometry.json):
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/settings-2b-wip --capture-settings-only
# If publish fails with file lock: Stop-Process -Name Cmnet.Native -Force
```

## Honesty

- All Settings captures watermarked `SAMPLE DATA · settings-<scenario>`.  
- Preview config / welcome never writes user `desktop.json` in capture paths that only render fixtures (welcome capture builds overlay UI without `SaveChoice`).  
- Live first-run persists to the same `desktop.json` Electron uses.  
- No invented Discord/Alerts/Rules/update affordances.  
- Scout/Draft/Data not redesigned.

## Deferred / out of Gate 2.2

| Gap | Rationale |
|---|---|
| GSI install UX | Separate Gate 2 item |
| Signed installer / auto-update | Gate 3 |
| Alerts / Rules / Discord | Explicit cut-list |
| Independent product-owner visual OK | Required before merge; this PR does not self-approve |

## Ready-for-acceptance

Implementer recommends acceptance after independent review of Settings + first-run journey + screenshots. Do **not** auto-merge.
