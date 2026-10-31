# Gate 3.2 — Self-contained native Windows package (#56)

**Branch:** `twipz333/cmnet-gate32-installer`  
**Issue:** #56  
**Base:** `origin/master` @ `49f1167` (Gate 3.1 naming #55)  
**Status:** implementer recommends acceptance after CI + independent review. **Not Released.** Unsigned (Gate 3.3).

## User outcome

A non-developer friend can install/run native CMNET without SDK/CLI: double-click NSIS setup (or unzip portable), get Desktop/Start Menu «CMNET», land on first-run Welcome. Data stays in `%APPDATA%\cmnet`.

## Artifacts (builder machine / CI)

| Artifact | Path | Role |
|---|---|---|
| NSIS setup | `native/dist/cmnet-native-setup-0.4.0.exe` | Friend installer (per-user, Start Menu + Desktop) |
| Portable zip | `native/dist/cmnet-native-0.4.0-win-x64.zip` | Fallback / CI preview sibling |
| Manifest | `native/dist/cmnet-native-package.manifest.json` | Provenance stamp |
| Build script | `native/installer/Build-NativeInstaller.ps1` | Reproducible package from clean checkout |
| NSIS script | `native/installer/cmnet-native.nsi` | Packaging source |

**Naming is distinct from Electron** `cmnet-setup-*.exe` (`desktop.yml` v0.3.x). Native does **not** publish to GitHub Releases in this slice; CI uploads Actions artifact `cmnet-native-setup` only.

### Local build hashes (this workstation, 2026-10-31)

| File | Size | SHA256 |
|---|---:|---|
| `cmnet-native-setup-0.4.0.exe` | 51 219 242 | `41C94791C0B621F5D6267E8E19707D4C5857BAFBF687989BDEFF80EDA6C93625` |
| `cmnet-native-0.4.0-win-x64.zip` | 71 743 403 | `AEC4120BC5DEE175F5D9D762DF7107410030E6408F07F7E2A25E40C0E8070C24` |

Binaries are gitignored (`native/dist/`, `*.exe`); rebuild with the script below.

## Reproducible build (clean checkout)

```powershell
# Requires Go + .NET 10 SDK on the *builder* only. Friends need neither.
# NSIS 3.x: winget install NSIS.NSIS  (script can invoke winget if missing)
pwsh -File native/installer/Build-NativeInstaller.ps1
```

Produces setup + zip + manifest under `native/dist/`, then runs `Cmnet.Native.exe --smoke-ui` on the staged payload.

CI (`native.yml`): after existing publish/smoke/capture/zip, runs the same script with `-SkipPublish -SkipSmoke` and uploads `cmnet-native-setup` artifact. **`desktop.yml` untouched.**

## Friend install steps (demo)

1. Copy `cmnet-native-setup-0.4.0.exe` to the friend’s PC (USB / chat / Actions artifact).  
2. Double-click. Windows may warn **Unknown publisher** — expected: **unsigned** (Gate 3.3). Choose More info → Run anyway if SmartScreen blocks.  
3. Next → pick folder (default `%LOCALAPPDATA%\Programs\CMNET`) → Install.  
4. Finish with «Open CMNET» or use Desktop / Start Menu shortcut **CMNET**.  
5. First-run Welcome: choose data folder / «Начать с нуля». No terminal, no SDK.  
6. Uninstall via Settings → Apps → «CMNET (native)» or `Uninstall.exe`. **User data in `%APPDATA%\cmnet` is preserved.**

Portable alternative: unzip `cmnet-native-0.4.0-win-x64.zip` and run `Cmnet.Native.exe` next to `advisor.exe`.

## Smoke evidence (this run)

| Check | Result |
|---|---|
| `Build-NativeInstaller.ps1` publish + NSIS | PASS |
| Staged `Cmnet.Native.exe --smoke-ui` | exit 0 |
| Silent install `setup /S /D=%TEMP%\cmnet-native-smoke-install` | exit 0 |
| Payload contains `Cmnet.Native.exe` + `advisor.exe` | PASS |
| Desktop + Start Menu `CMNET.lnk` created | PASS |
| Installed `--smoke-ui` | exit 0 |
| Silent uninstall | install dir removed |
| `%APPDATA%\cmnet` after uninstall | **preserved** |

Branding from Gate 3.1: product **CMNET**, `cmnet.ico`, informational version **0.4.0-native**.

## Honesty / out of scope

- **UNSIGNED** — no Authenticode; SmartScreen warning expected. Signing + update channel = **Gate 3.3**.  
- Do **not** claim **Released**. Electron remains production friend path for v0.3.x until native gates close.  
- No paid APIs. No match-journey redesign (Scout/Draft/Data). Welcome copy only updated for installer honesty.  
- Guest SKU / auto-update / friend VM trials = later Gate 3 slices.

## Ready-for-acceptance

Implementer recommends acceptance after green `native-windows` CI and review of this NOTES + artifact names. Coordinator merges; worker does **not** merge.
