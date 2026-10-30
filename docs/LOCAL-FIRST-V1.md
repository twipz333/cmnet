# CMNET local-first v1 compatibility contract

This is an additive rollout. Production Electron remains on the existing /api/* endpoints; WPF uses /api/v1/state and /api/v1/events and can continue using legacy mutation paths. All data, inference, and model files are local.

## Database

Migration 8 is applied by the existing versioned SQLite ladder on the next startup. The complete executable SQL lives in [migration_008.sql](../internal/cache/migration_008.sql). It creates source-observation provenance, frozen team snapshots, model registry, draft checkpoints and decision events without dropping or renaming legacy tables. Existing cached players, matches and hero statistics remain intact. This is not a schema downgrade: before opening the database with an older Electron build after migration, use a backup of the original database.

A consistent online backup must use SQLite VACUUM INTO / SQLite backup API, not copying cmnet.db during WAL writes. Store.Backup() provides VACUUM INTO plus integrity verification. Do not point simultaneous native and Electron processes at the same data directory.

## IPC security

WPF generates a cryptographically random 256-bit token per advisor.exe child process, passes it via CMNET_IPC_TOKEN in the process environment, and sends Authorization: Bearer on every request (including SSE). Tokens are never placed in argv or a settings file. The web listener only accepts loopback binds; Host, Origin and Sec-Fetch-Site are checked on the live listener for every request. Native clients must use matching advisor.exe and .NET 10 binaries.

Standalone legacy browser/Electron processes without CMNET_IPC_TOKEN retain the older tokenless loopback API; they are still protected by loopback binding, Host/Origin and browser fetch-metadata checks, but local same-user processes retain access. This transitional mode is not equivalent to authenticated native IPC.

## State contract

The snapshot contains schemaVersion=1, sessionId (one per Go process), revision (monotonic within that process), and modelUpdatePending. Every SSE frame contains a complete snapshot; clients reject revisions that are not newer within the same session and take a fresh snapshot after reconnect. Invalid schema versions fail explicitly in the native client.

The active model remains pinned once a manual pick is registered or a GSI stream is observed; updated models are staged. The next manual /api/new-draft activates a staged model. GSI-mode sessions retain their model until process restart, rather than resetting a live match.

## Quick scout

Enemy and ally scouting now share an overall 15-second deadline, not a new 15 seconds at each stage. Already gathered partial evidence survives a deadline expiry; cancellation still discards work. The UI continues displaying the prior local snapshot while the asynchronous queue runs. No public relay or paid API is introduced by these changes.

## Verification

```shell
go test ./internal/web ./internal/session ./internal/cache ./internal/scout ./cmd/advisor
go vet ./...
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true
native/out/Cmnet.Native.exe --smoke-ui
```

In addition to CI, a Windows release requires an actual match smoke test, Electron-to-WPF data migration and rollback trial, installer signature verification and UI DPI 100/125/150% checks. A green compile does not prove those release gates.
