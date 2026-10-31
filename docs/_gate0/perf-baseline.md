# Gate 0.6 — Windows performance baseline

**Status:** measured (local host)  
**Issue:** [#39](https://github.com/twipz333/cmnet/issues/39)  
**Orca Run:** `run_d7ae77d33f9b`  
**Measured:** 2026-10-31 17:42 +03:00  
**Build SHA:** `fba7271b9a3b5fe534d3442b459c684695a1f235` (`master` / PR #37 tip at measure time)  
**Raw artifact:** [`logs/perf-baseline/perf-baseline.raw.json`](./logs/perf-baseline/perf-baseline.raw.json)  
**Harness:** [`scripts/Measure-PerfBaseline.ps1`](./scripts/Measure-PerfBaseline.ps1), [`tools/perfbaseline`](../../tools/perfbaseline)

No product UI/behavior changes. Measurement-only + tiny local scout fixture harness.

---

## Machine

| Item | Value |
|---|---|
| Host | `HOMESTATION` |
| OS | Microsoft Windows NT 10.0.26200.0 |
| CPU | AMD Ryzen 9 7950X 16-Core Processor (16C/32T) |
| RAM | 63.1 GiB |
| .NET SDK | `10.0.403` |
| Go | `go1.27.1 windows/amd64` |

These numbers are a **single-machine baseline**, not a release SLA. Re-run the harness on other PCs before claiming regressions.

---

## Commands used

```powershell
# From repo root
$env:CGO_ENABLED = '0'
go build -trimpath -ldflags "-s -w" -o native/out/advisor.exe ./cmd/advisor
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out -p:PublishSingleFile=true

# Full measure (rebuild + timings)
pwsh docs/_gate0/scripts/Measure-PerfBaseline.ps1

# Or reuse an existing native/out:
pwsh docs/_gate0/scripts/Measure-PerfBaseline.ps1 -SkipPublish

# Scout fixture-only:
go run ./tools/perfbaseline
```

Published binaries at measure time:

| Binary | Approx size |
|---|---|
| `native/out/Cmnet.Native.exe` | ~126 MiB (self-contained single-file) |
| `native/out/advisor.exe` | ~26 MiB |

---

## 1) Startup to interactive (native publish exe)

**Definition used:** process start → WPF `MainWindowHandle` non-zero (shell visible).  
**Core proxy:** process start → child `advisor.exe` spawned by `BootAsync` / `AdvisorCore.StartAsync` (not full `/api/v1/state` ready — that would need IPC introspection).

Isolated temp `--data-dir` per run (no shared Electron DB contention).

| Metric | n | min | median | max | mean |
|---|---:|---:|---:|---:|---:|
| To MainWindow (ms) | 3 | 587.3 | **712.2** | 763.0 | 687.5 |
| To advisor.exe child (ms) | 3 | 842.5 | **901.1** | 913.3 | 885.6 |

**Interpretation:** shell is interactive in ~0.6–0.8 s on this host; Go sidecar appears ~0.2 s later. Full “core answered `/api/v1/state`” was **not** instrumented externally (port is ephemeral).

### Related: `--smoke-ui` (no Show / no Go)

UI construction + navigation assert on STA thread, then exit.

| Metric | n | min | median | max | mean |
|---|---:|---:|---:|---:|---:|
| Wall clock (ms) | 3 | 768.9 | **771.4** | 2143.8 | 1228.0 |

First run ~2.1 s (cold process / AV / disk); subsequent ~0.77 s.

---

## 2) Scout path — warm vs cold (local fixtures only)

### 2a) Local assembly fixture (`tools/perfbaseline`)

**Methodology:** in-memory `scout.Source` with 5 resolved players + 2 event drafts. **No HTTP, no OpenDota, no STRATZ, no paid APIs.**

| Path | Result |
|---|---|
| Cold (first `Fetch` in process) | **528 µs** (~0.53 ms) |
| Warm (mean per-call over 500 batched `Fetch`) | **26.5 µs** (~0.026 ms) |

**Label honestly:** this is CPU/assembly cost of layers 2–3 over stubs. It is **not** live scout latency. Product `opScout` always calls `scout.NewOpenDotaSource()` and does **not** short-circuit `Fetch` through SQLite affinity cache — so a “warm” live scout still pays network.

### 2b) Not measured (and why)

| Item | Why skipped |
|---|---|
| Live OpenDota cold/warm scout | Free API exists, but Gate 0.6 harness avoids unbounded `/players/{id}/heroes` traffic; results would also be network-noise-dominated |
| STRATZ scout | Requires token (not free/local-only for this worker) |
| UI “Разведать соперников” end-to-end | Needs running core + real/fixture HTTP ops job; `--capture-ui` uses SAMPLE DATA only |
| Cancellation under live scout | Needs live job |

---

## 3) Memory (rough Working Set)

| Scenario | Native WS | Advisor WS | Notes |
|---|---:|---:|---|
| Idle after interactive startup (~5 s settle), median | **137.4 MiB** | **87.7 MiB** | Run 3 advisor sample 28.5 MiB — likely early sample; treat ~88 MiB as typical |
| Peak during `--capture-ui` | **295.5 MiB** peak WS (290.8 private) | n/a (preview mode, no Go) | Full Draft+Scout PNG matrix + honesty fixtures |

`--capture-ui` wall clock on this host: **88.9 s** (exit 0). Dominated by multi-resolution PNG rasterization, not scout network.

---

## 4) UI responsiveness / redraw

**Not measured.** PRODUCT-BAR also asks redraw/scroll responsiveness; that needs hands-on WPF interaction or Computer Use. Orca Computer Use permissions were previously blocked for interactive WPF QA on this program — left as a gap.

---

## 5) Gaps / next measurements

1. Instrument optional `--bench-startup` (or readiness file) for true “core HTTP ready” without changing default UX.
2. One carefully rate-limited live OpenDota scout of a public account (cold vs immediate re-scout) when explicitly authorized.
3. Record scroll/list virtualization FPS on Scout roster under load.
4. Re-run this harness after packaging/installer changes and on a second machine before Release QA.

---

## Acceptance mapping (issue #39)

| Acceptance | Evidence |
|---|---|
| Scripted or documented measurement commands | This file + `docs/_gate0/scripts/Measure-PerfBaseline.ps1` |
| Numbers committed with machine notes | Sections above + `logs/perf-baseline/perf-baseline.raw.json` |
| No code behavior change required | Only measurement harness under `tools/perfbaseline` + docs/scripts |
