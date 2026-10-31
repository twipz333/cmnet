# Gate 3.4 — Crash diagnostics (no secrets) + data-preserving uninstall (#60)

**Branch:** `twipz333/cmnet-gate34-diagnostics`  
**Issue:** #60  
**Base:** `origin/master` @ `2a58b20` (Gate 3.3 updates #59)  
**Status:** implementer recommends acceptance after CI + independent review. **Not Released.** Local-first; no paid crash cloud.

## User outcome

Crashes leave useful **local** breadcrumbs under `%APPDATA%\cmnet\diagnostics` with tokens / API keys / labeled account ids redacted. Settings → О программе shows the path and opens the folder honestly. NSIS uninstall still removes only the install tree — `%APPDATA%\cmnet` (data + diagnostics) stays.

## Code

| Piece | Path |
|---|---|
| Redaction | `native/SecretRedactor.cs` |
| Paths + README | `native/DiagnosticsPaths.cs` |
| Handlers + write | `native/CrashBreadcrumbs.cs` |
| Self-test | `native/DiagnosticsSelfTest.cs` (`--self-test-diagnostics`) |
| About UX | `native/SettingsView.cs` («Папка диагностики») |
| Capture fixture | `about-diagnostics` via `--capture-diagnostics[-only]` |
| NSIS preserve | `native/installer/cmnet-native.nsi` Uninstall section (install tree only) |

## Redaction (best-effort)

Replaces with `[REDACTED]`:

- PEM private key blocks  
- `Authorization` / Bearer  
- `CMNET_IPC_TOKEN`, STRATZ / OpenDota / generic API key env forms  
- JSON / YAML `token` / `api_key` / `password` fields  
- JWT-shaped blobs  
- ≥32-char hex tokens  
- Labeled `account_id` / `steam_id` (numeric)

Ordinary draft text (`hero_id=1 pick=Anti-Mage`) passes through unchanged.

## Uninstall data preserve (Gate 3.2 contract re-verified)

NSIS `Section "Uninstall"`:

1. Deletes Desktop / Start Menu shortcuts.  
2. `RMDir /r "$INSTDIR"` only.  
3. Clears uninstall registry keys.  
4. **Does not** `RMDir` / `Delete` `$APPDATA\cmnet` (data, `desktop.json`, `diagnostics\`, backups).

Finish page already links: «User data stays in %APPDATA%\cmnet (not removed on uninstall)».

Self-test assertion `nsis-preserve-contract` scans the `.nsi` for any active (non-comment) `RMDir`/`Delete` targeting APPDATA\cmnet — must fail if someone adds a wipe.

Friend VM install/uninstall script remains **Gate 3.5**. This gate documents + statically guards the contract and keeps diagnostics inside the preserved tree.

## Repro commands

```powershell
dotnet build native/Cmnet.Native.csproj -c Release -o native/out-diag
.\native\out-diag\Cmnet.Native.exe --self-test-diagnostics docs/_gate0/gate3-diagnostics-wip
.\native\out-diag\Cmnet.Native.exe --capture-ui docs/_gate0/gate3-diagnostics-wip --capture-diagnostics-only
```

## Evidence (this run)

| Check | Result |
|---|---|
| `--self-test-diagnostics` (17 assertions) | PASS → `self-test-diagnostics.txt` / `checks.txt` |
| Sample breadcrumb redaction | `sample-crash-redacted.txt` (IPC token + account_id scrubbed) |
| `%APPDATA%\cmnet\diagnostics\README.txt` | created on first write |
| NSIS preserve contract (static) | PASS |
| About capture | `about-diagnostics.png` (local; gitignored binary) |

## Honesty / out of scope

- **Not Released.** No SaaS crash reporter.  
- Redaction is best-effort — still treat the folder as private.  
- Friend clean-VM install/upgrade/uninstall + two human trials = **Gate 3.5**.  
- No match-journey redesign (Theme.xaml / StateKit only as already used).

## Ready-for-acceptance

Implementer recommends acceptance after green `native-windows` CI and review of this NOTES + `self-test-diagnostics.txt`. Coordinator merges; worker does **not** merge.
