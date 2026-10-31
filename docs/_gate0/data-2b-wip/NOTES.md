# Gate 2.1 — Data surface (#40)

**Branch:** `twipz333/cmnet-gate21-data`  
**Issue:** #40  
**Base:** `origin/master` @ `338fbc5` (Gate1 close-out StateKit)  
**Evidence:** `dotnet publish` win-x64 self-contained + `--smoke-ui` + `--capture-ui` (+ `--capture-data`) → this folder  
**geometry.json:** **33** entries (unchanged CI matrix). Data honesty PNGs are **outside** geometry (same pattern as Gate1 `--capture-honesty`). Default CI `--capture-ui` must stay under 180s: Data kit is opt-in (`--capture-data` / `--capture-data-only`); catalog column rebuild is frozen during the size/scale matrix.

## Outcome

Captain-facing Maintain / Data screen replaces the previous metric tiles + one-line activity dump:

1. **Human model + corpus status** — patch / bundle version / ready-or-not copy; corpus tiles with period; no raw JSON wall.
2. **Sync / train jobs** — start via `/api/ops/sync` and `/api/ops/train`; live row shows phase, last line, waitingOn, duration.
3. **Cancel / retry** — stop via `/api/ops/cancel` (with job `id` when present); failed recent jobs get **Повторить**.
4. **Readable log strip** — `/api/jobs/log` when core is live; SAMPLE fixture lines in preview.
5. **Evidence StateKit** — empty / loading / error / offline / partial / stale / ready on the Data banner (shared chrome with Scout/Draft).

## Captures (SAMPLE DATA labeled)

| Surface | Files |
|---|---|
| Happy | `data.png`, `data-ready.png` |
| States | `data-empty.png`, `data-loading.png`, `data-error.png`, `data-partial.png`, `data-stale.png`, `data-offline.png` |
| CI matrix (unchanged) | Draft×5×3 + Scout×5×3 + scout empty/loading/error → `geometry.json` Count==33 |

## Verify

```text
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out
native/out/Cmnet.Native.exe --smoke-ui
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/data-2b-wip --capture-data
# optional fast Data re-shot (does not rewrite geometry.json):
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/data-2b-wip --capture-data-only
# If publish fails with file lock: Stop-Process -Name Cmnet.Native -Force
```

## Honesty

- All Data captures watermarked `SAMPLE DATA · data-<scenario>`.
- Preview corpus / job log never reads user `cmnet.db` or starts Go.
- No invented training accuracy metrics; failure/empty copy is explicit.
- Scout/Draft not redesigned; Theme.xaml + StateKit reused.

## Deferred / out of Gate 2.1

| Gap | Rationale |
|---|---|
| Live cancel proof under long sync | Fixture-proven; live proof = Gate QA |
| Sources activity table (`/api/sources`) | Useful secondary; not required for #40 acceptance |
| Corpus daily charts | Electron charts.js parity — later polish |
| Independent product-owner visual OK | Required before merge; this PR does not self-approve |

## Ready-for-acceptance

Implementer recommends acceptance after independent review of Data journey + screenshots. Do **not** auto-merge.
