# Gate 0 — build / test / screenshot evidence

**Status:** evidence collected (docs-only). **Not** a visual 3B product approval.  
**Collected:** 2026-10-30 19:12 +03:00  
**Worker:** Claude/Cursor (Codex unavailable)  
**Worktree:** `C:\Users\dimag\orca\workspaces\cmnet\cmnet-gate0a-build`  
**Branch:** `cmnet-gate0a-build`  
**HEAD:** `c75ac6f3ab89c3b3fc3b37ece7b066aa5d2254a7` (matches TARGET)

## Environment

| Item | Value |
|---|---|
| OS | Microsoft Windows NT 10.0.26200.0 (`win32`) |
| Host | HOMESTATION |
| Go | `go1.27.1 windows/amd64` |
| .NET SDK | `10.0.403` |
| GitHub CLI | `gh 2.103.3` |
| `TMP`/`Go` temp | Failures below reference `F:\Go\tmp\...` |

No application source, workflows, migrations, Electron, or Go product code were modified. Ownership limited to `docs/_gate0/**`.

---

## 1) `go test ./...`

**Command:** `go test ./...`  
**Working directory:** `C:\Users\dimag\orca\workspaces\cmnet\cmnet-gate0a-build`  
**Exit code:** `1`  
**Full log:** `C:\Users\dimag\orca\workspaces\cmnet\cmnet-gate0a-build\docs\_gate0\logs\go-test.txt`

### Package outcomes (summary)

Most packages `ok`. Packages with **no test files:** `cmd/advisor-discord`, `cmd/advisor-gsi-fake`, `dota/macrolabel/adapter/current_capture`, `dota/macrolabel/audit`, `sources/stratz`, `startup`, `tui`.

### Failures (exact)

Both failures are **post-test TempDir cleanup** on Windows (`testing.go` RemoveAll), not assertion messages in the test body:

```
--- FAIL: TestCorpusDailyIsPerMatchAndCMIsASubset (4.89s)
    testing.go:1617: TempDir RemoveAll cleanup: unlinkat F:\Go\tmp\TestCorpusDailyIsPerMatchAndCMIsASubset914987308\001: The directory is not empty.
FAIL
FAIL	cmnet/internal/cache	152.631s

--- FAIL: TestPurgeDeletesWholeDrafts (3.00s)
    testing.go:1617: TempDir RemoveAll cleanup: unlinkat F:\Go\tmp\TestPurgeDeletesWholeDrafts1798599188\001: The directory is not empty.
FAIL
FAIL	cmnet/internal/declog	26.391s
```

**Overall:** `FAIL` (exit 1). Do **not** treat the suite as green on this host/run.

---

## 2) `go vet ./...`

**Command:** `go vet ./...`  
**Exit code:** `0`  
**Stdout/stderr:** empty (no reported findings)  
**Log:** `C:\Users\dimag\orca\workspaces\cmnet\cmnet-gate0a-build\docs\_gate0\logs\go-vet.txt` (empty file)

---

## 3) `dotnet publish` (native WPF, self-contained win-x64)

**Command:**

```text
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out
```

**Exit code:** `0`  
**Log:** `C:\Users\dimag\orca\workspaces\cmnet\cmnet-gate0a-build\docs\_gate0\logs\dotnet-publish.txt`

Key lines:

```text
Cmnet.Native -> ...\native\bin\Release\net10.0-windows\win-x64\Cmnet.Native.dll
Cmnet.Native -> ...\native\out\
```

**Published EXE:** `C:\Users\dimag\orca\workspaces\cmnet\cmnet-gate0a-build\native\out\Cmnet.Native.exe`

---

## 4) Local UI smoke / capture (publish succeeded)

### `--smoke-ui`

**Command:** `native\out\Cmnet.Native.exe --smoke-ui`  
**Exit code:** `0`  
**Log:** `C:\Users\dimag\orca\workspaces\cmnet\cmnet-gate0a-build\docs\_gate0\logs\smoke-ui.txt` (no stdout; process exited cleanly)

### `--capture-ui`

**Command:** `native\out\Cmnet.Native.exe --capture-ui <dest>`  
**Destination:** `C:\Users\dimag\orca\workspaces\cmnet\cmnet-gate0a-build\docs\_gate0\local-capture`  
**Exit code:** `0`  
**Log:** `C:\Users\dimag\orca\workspaces\cmnet\cmnet-gate0a-build\docs\_gate0\logs\capture-ui.txt`  
**checks.txt:**

```text
PASS: hero/player selection, retained draft selection, taken hero, model readiness, three columns and Apply viewport containment.
DPI cases simulate logical viewport/raster scaling; native monitor DPI requires separate validation.
Actual WPF SAMPLE DATA fixtures; no Go process or user database.
```

**Note:** Running the EXE without `--smoke-ui` / `--capture-ui` (e.g. bare `--help`) launches the GUI and hangs; killed during investigation. Flags confirmed present in `native/Program.cs` (read-only).

---

## 5) CI artifact `cmnet-wpf-3b-screenshots`

| Field | Value |
|---|---|
| Workflow | `native.yml` / run name `native-windows` |
| Run ID | `41084058862` |
| Conclusion | `success` |
| `head_sha` | `c75ac6f3ab89c3b3fc3b37ece7b066aa5d2254a7` (same as local HEAD) |
| Artifact ID | `12991146473` |
| Artifact size | 11450951 bytes |
| Download | `gh run download 41084058862 --repo twipz333/cmnet --name cmnet-wpf-3b-screenshots --dir docs/_gate0/ci-screenshots/extracted` → exit `0` |
| Extracted dir | `C:\Users\dimag\orca\workspaces\cmnet\cmnet-gate0a-build\docs\_gate0\ci-screenshots\extracted` |
| Run URL | https://github.com/twipz333/cmnet/actions/runs/41084058862 |

No newer successful `native.yml` run beyond `41084058862` was required (it is the latest success on this SHA).

---

## 6) Screenshot inventory

### Geometry matrix (33 cases)

Source of truth: `geometry.json` entries = **33** (CI and local capture both).

Composition from `native/Program.cs` capture path:

| Group | Count | Formula |
|---|---:|---|
| Draft × sizes × scales | 15 | 5 resolutions × 3 scales (`100`/`125`/`150`) |
| Scout × sizes × scales | 15 | same |
| Scout scenarios | 3 | `empty`, `loading`, `error` |
| **Total geometry cases** | **33** | |

Resolutions: `1920x1080`, `1600x900`, `1440x900`, `1366x768`, `1280x720`.  
Aliases (not extra matrix rows): `draft.png`, `scout.png` (copies of 1440×900 @ scale 1).

PNG file count on disk per capture set: **35** (= 33 cases + 2 aliases).

### Draft / Scout / Data / Settings coverage

| Screen | Dedicated PNG screenshots? | Notes |
|---|---|---|
| **Draft** (`Драфт`) | **Yes** — 15 matrix + `draft.png` | Absolute paths under CI/local dirs below |
| **Scout** (`Разведка`) | **Yes** — 15 matrix + 3 scenarios + `scout.png` | |
| **Data** (`Данные`) | **No** | Navigation control bounds only appear inside `geometry.json` `bounds["Navigation Данные"]`; no page capture |
| **Settings** (`Настройки`) | **No** | Same — nav bounds only; no page capture |

This is an inventory fact about the current `--capture-ui` harness, not a visual 3B approval.

### Absolute paths — CI screenshots (canonical for Gate 0)

Base: `C:\Users\dimag\orca\workspaces\cmnet\cmnet-gate0a-build\docs\_gate0\ci-screenshots\extracted\`

**Meta**

- `...\checks.txt`
- `...\geometry.json`

**Draft**

- `...\draft.png`
- `...\draft-1280x720-layout100.png`
- `...\draft-1280x720-layout125.png`
- `...\draft-1280x720-layout150.png`
- `...\draft-1366x768-layout100.png`
- `...\draft-1366x768-layout125.png`
- `...\draft-1366x768-layout150.png`
- `...\draft-1440x900-layout100.png`
- `...\draft-1440x900-layout125.png`
- `...\draft-1440x900-layout150.png`
- `...\draft-1600x900-layout100.png`
- `...\draft-1600x900-layout125.png`
- `...\draft-1600x900-layout150.png`
- `...\draft-1920x1080-layout100.png`
- `...\draft-1920x1080-layout125.png`
- `...\draft-1920x1080-layout150.png`

**Scout**

- `...\scout.png`
- `...\scout-empty.png`
- `...\scout-loading.png`
- `...\scout-error.png`
- `...\scout-1280x720-layout100.png`
- `...\scout-1280x720-layout125.png`
- `...\scout-1280x720-layout150.png`
- `...\scout-1366x768-layout100.png`
- `...\scout-1366x768-layout125.png`
- `...\scout-1366x768-layout150.png`
- `...\scout-1440x900-layout100.png`
- `...\scout-1440x900-layout125.png`
- `...\scout-1440x900-layout150.png`
- `...\scout-1600x900-layout100.png`
- `...\scout-1600x900-layout125.png`
- `...\scout-1600x900-layout150.png`
- `...\scout-1920x1080-layout100.png`
- `...\scout-1920x1080-layout125.png`
- `...\scout-1920x1080-layout150.png`

### Absolute paths — local re-capture (same machine, post-publish)

Base: `C:\Users\dimag\orca\workspaces\cmnet\cmnet-gate0a-build\docs\_gate0\local-capture\`  
Same 35 PNG filenames + `checks.txt` + `geometry.json` as CI set (byte sizes differ; both exit 0 / PASS checks.txt).

---

## Honest blockers / gaps (evidence, not product approval)

1. **`go test ./...` exit 1** on this Windows host: `internal/cache` and `internal/declog` fail at TempDir cleanup (`directory is not empty` under `F:\Go\tmp\...`). Suite is **not** green here.
2. **No Data/Settings page screenshots** in CI or local capture harness (Draft + Scout only).
3. **No claim of Minimal Tactical 3B visual acceptance** — this report is reproducible build/test/artifact evidence only.
4. Native monitor DPI is explicitly **not** validated by the capture harness (`checks.txt`).

---

## Artifact index under `docs/_gate0/`

```text
docs/_gate0/build-evidence.md          ← this report
docs/_gate0/logs/go-test.txt
docs/_gate0/logs/go-vet.txt
docs/_gate0/logs/dotnet-publish.txt
docs/_gate0/logs/smoke-ui.txt
docs/_gate0/logs/capture-ui.txt
docs/_gate0/ci-screenshots/extracted/  ← CI artifact (33 geometry + aliases)
docs/_gate0/local-capture/             ← local --capture-ui re-run
```
