# Gate 3.5 — Clean-VM install script + two friend trials (#62)

**Branch:** `twipz333/cmnet-gate35-friend-trial`  
**Issue:** #62  
**Base:** `origin/master` @ `9fe383a` (Gate 3.4 diagnostics #61)  
**Status:** scripted path implementable; **human friend trials BLOCKED / pending** until two real friends report. **Not Released.** Do not claim Gate 3.6.

## User outcome

1. A reproducible PowerShell script proves silent install → `--smoke-ui` → optional upgrade overwrite → uninstall while **`%APPDATA%\cmnet` is preserved**.
2. Two non-developer friends can install from USB/chat **without developer help** and file results below.
3. Electron `cmnet-setup-*.exe` / `desktop.yml` channel stays untouched.

## Automatable evidence (agent)

| Artifact | Path | Role |
|---|---|---|
| Clean-VM script | `native/installer/Test-CleanVmFriendInstall.ps1` | Silent install / smoke / upgrade / uninstall / APPDATA assert |
| Package builder | `native/installer/Build-NativeInstaller.ps1` | Produces `cmnet-native-setup-<ver>.exe` when not pre-supplied |
| Script evidence JSON | `docs/_gate0/gate3-friend-trial-wip/script-evidence.json` | Written by the script on each run (`pass`, hashes, steps) |
| This NOTES | `docs/_gate0/gate3-friend-trial-wip/NOTES.md` | Human checklist + result slots |

### Run on a Windows builder / clean VM

```powershell
# Option A — build setup then trial (needs Go + .NET 10 SDK; NSIS via winget if missing)
pwsh -File native/installer/Test-CleanVmFriendInstall.ps1 -Build

# Option B — consume an already-built / CI-downloaded setup
pwsh -File native/installer/Test-CleanVmFriendInstall.ps1 `
  -SetupPath path\to\cmnet-native-setup-0.4.0.exe

# Skip the second silent install (upgrade overwrite) if needed
pwsh -File native/installer/Test-CleanVmFriendInstall.ps1 -SetupPath .\native\dist\cmnet-native-setup-0.4.0.exe -SkipUpgrade
```

What the script asserts:

1. Resolves or builds `cmnet-native-setup-0.4.0.exe` (distinct from Electron `cmnet-setup-*.exe`).
2. Silent install: `setup.exe /S /D=%TEMP%\cmnet-gate35-cleanvm-install`.
3. Installed `Cmnet.Native.exe --smoke-ui` → exit 0.
4. Optional upgrade: silent re-install over the same `INSTDIR`, then smoke again.
5. Writes marker `%APPDATA%\cmnet\gate35-cleanvm-marker.txt` **before** uninstall.
6. Silent uninstall: `Uninstall.exe /S`; payload removed from install dir.
7. Marker still present with identical SHA256 → **data preserved**.
8. Snapshots/restores pre-existing HKCU uninstall keys + Desktop/Start Menu shortcuts (builder safety). Prefer a clean VM for the canonical run.

**Honesty:** script PASS ≠ friend success. Unsigned package → SmartScreen «Unknown publisher» expected until Authenticode ops. No paid APIs.

## Human checklist — TWO friend installs (no developer help)

Distribute **only** `cmnet-native-setup-0.4.0.exe` (USB stick, chat file, or CI artifact `cmnet-native-setup`). Friends must **not** be walked through by a developer during the trial. Product owner / coordinator records answers; agents must **not invent** success.

### Shared prep (coordinator)

- [ ] Setup EXE SHA256 recorded: `________________________________`
- [ ] Friends told: unsigned → SmartScreen may block; More info → Run anyway is OK for this preview
- [ ] Friends told: uninstall must **not** wipe their `%APPDATA%\cmnet` if they already used CMNET
- [ ] Channel: USB / chat / other: `________________` (not GitHub Releases claim of Released)

### Friend 1

| Field | Value |
|---|---|
| Handle / initials | |
| Windows version | |
| Date (local) | |
| Received via | USB / chat / other: |
| SmartScreen / Unknown publisher? | Yes / No / notes: |
| Install completed without help? | Yes / No |
| Desktop or Start Menu «CMNET» appeared? | Yes / No |
| First-run Welcome usable? | Yes / No / notes: |
| Data folder / «Начать с нуля» OK? | Yes / No / N/A |
| Any crash / blank window? | |
| Uninstall tried? | Yes / No |
| After uninstall, `%APPDATA%\cmnet` still present? | Yes / No / not checked |
| Would install again? | Yes / No |
| **Verdict** | **PASS / FAIL / BLOCKED** |
| Notes | |

### Friend 2

| Field | Value |
|---|---|
| Handle / initials | |
| Windows version | |
| Date (local) | |
| Received via | USB / chat / other: |
| SmartScreen / Unknown publisher? | Yes / No / notes: |
| Install completed without help? | Yes / No |
| Desktop or Start Menu «CMNET» appeared? | Yes / No |
| First-run Welcome usable? | Yes / No / notes: |
| Data folder / «Начать с нуля» OK? | Yes / No / N/A |
| Any crash / blank window? | |
| Uninstall tried? | Yes / No |
| After uninstall, `%APPDATA%\cmnet` still present? | Yes / No / not checked |
| Would install again? | Yes / No |
| **Verdict** | **PASS / FAIL / BLOCKED** |
| Notes | |

## Trial results status (coordinator fills)

| Track | Status | Evidence |
|---|---|---|
| Scripted clean-VM / builder | **PASS** (2026-10-31, consumed CI setup SHA256 `E877B529…A4066E`) | `script-evidence.json` (`pass: true`; install+smoke+upgrade+uninstall; APPDATA marker preserved) |
| Friend 1 unassisted | **BLOCKED — pending real friend** | table above |
| Friend 2 unassisted | **BLOCKED — pending real friend** | table above |
| Released claim | **FORBIDDEN until Gate 3.6** | PRODUCT-BAR / DELIVERY-BOARD |

## Out of scope

- Code-signing certificate purchase / Authenticode (ops; Gate 3.3 noted separately).
- Full PRODUCT-BAR release matrix (Gate 3.6).
- Inventing or auto-checking friend PASS.
- Changing Electron desktop packaging.

## Ready-for-acceptance (this PR)

- [x] Script + NOTES landed, linked to #62.
- [x] Script run on Windows builder with `script-evidence.json` `pass: true`.
- [ ] Two friend tables filled by humans — until then mark Gate 3.5 human track **BLOCKED**.
- Implementer does **not** merge and does **not** claim Released.
