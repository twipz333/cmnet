# Gate 2.6 — Electron→native data migration (#50)

**Branch:** `twipz333/cmnet-gate26-migration`  
**Issue:** #50  
**Base:** `origin/master` @ `2997252` (Gate 2.5 Friends)  
**Evidence:** Go tests for backup/migrate/rollback + lock probe; optional WPF `--capture-migration` (outside geometry.json Count==33)

## Outcome

Existing Electron user can open native WPF on their local data after a tested path that:

1. **Detects** Electron vs native data dirs (`desktop/datadirs.js` / `native/DesktopSettings.cs` / `internal/datamigrate`).
2. **Creates** a timestamped `migration-backup/<stamp>/` with SQLite `VACUUM INTO` + copies of existing `bundle.json` / `snapshot.json` / `config.yaml` / `team.json` / `assets/` (only what exists — nothing invented).
3. **Migrates/copies** into the destination folder (or same-folder adoption = backup only).
4. **Rollback** restores from `MANIFEST.json` + backup files.
5. **Refuses** when `cmnet.db` is locked (quit Electron / other cmnet first).
6. **Honest surface:** `advisor migrate …` CLI + Settings → «Миграция» (filesystem status + CLI commands; no fake success while the live core holds the DB).

## Demo script (Windows PowerShell)

```powershell
# 0) Build
go build -o native/out/advisor.exe ./cmd/advisor

# 1) Synthetic Electron-like folder
$root = Join-Path $env:TEMP ("cmnet-mig-" + [guid]::NewGuid().ToString("n"))
$electron = Join-Path $root "electron"
$native = Join-Path $root "native"
New-Item -ItemType Directory -Force -Path $electron | Out-Null
# Minimal DB via migrate tests is preferred; or copy a real data dir:
# Copy-Item -Recurse $env:APPDATA\cmnet\data\* $electron\

# Automated proof (preferred):
go test ./internal/datamigrate/ ./internal/cache/ -count=1 -run "Backup|Migrate|Probe|Refuse|Inspect|SameFolder" -timeout 90s

# 2) Manual CLI against real folders (Electron + native CLOSED):
.\native\out\advisor.exe migrate status --from $electron --to $native
.\native\out\advisor.exe migrate run --from $electron --to $native --update-desktop
.\native\out\advisor.exe migrate status --from $electron --to $native
# Rollback:
# .\native\out\advisor.exe migrate rollback --backup "$native\migration-backup\<stamp>" --to $native

# 3) Native UI (optional captures; does NOT rewrite geometry Count==33)
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out
.\native\out\Cmnet.Native.exe --smoke-ui
.\native\out\Cmnet.Native.exe --capture-ui docs/_gate0/migration-2b-wip --capture-migration-only
```

## Rollback instructions

1. Close Electron and native CMNET (both must release `cmnet.db`).
2. `advisor migrate status` — list `migration-backup\…` paths.
3. `advisor migrate rollback --backup <path\to\migration-backup\stamp> --to <data-dir>`.
4. Re-open native (or Electron) against that data dir. Schema note: if native already applied migration 8, older Electron may need the pre-native backup DB; see `docs/LOCAL-FIRST-V1.md`.

## Concurrent DB rule

- Native `AdvisorCore.StartAsync` and Welcome adoption call `advisor migrate status` lock probe before opening the DB.
- Settings «Миграция» does **not** open SQLite while the Go core is live (would false-positive “locked by us”); it shows paths/backups from the filesystem and prints CLI commands for offline mutate ops.

## Verify commands (this worktree)

```text
go test ./internal/datamigrate/ ./internal/cache/ -count=1 -run "Backup|Migrate|Probe|Refuse|Inspect|SameFolder"
go build -o native/out/advisor.exe ./cmd/advisor
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out
native/out/Cmnet.Native.exe --smoke-ui
```

## Honesty / out of scope

| Item | Note |
|---|---|
| No invented scout/model rows | Only copy existing artifacts |
| Gate 3 installer signing | Out of scope |
| Scout/Draft/Friends/History redesign | Not touched |
| geometry.json | Remains Count==33; migration PNGs are opt-in |

## Ready-for-acceptance

Implementer recommends acceptance after independent review of CLI demo + Settings tab. Do **not** auto-merge.
