#Requires -Version 7.0
<#
.SYNOPSIS
  Gate 0.6 Windows performance baseline measurements for CMNET native publish exe.

.DESCRIPTION
  Measurement-only. Does not change product behavior.
  Records: startup-to-window, smoke-ui, capture-ui, idle/capture memory,
  and invokes tools/perfbaseline for local scout fixture cold/warm timings.

.NOTES
  Issue #39 / PRODUCT-BAR Performance gate. Free/local only.
#>
[CmdletBinding()]
param(
    [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path,
    [string]$OutDir = '',
    [int]$StartupRuns = 3,
    [int]$SmokeRuns = 3,
    [switch]$SkipPublish
)

$ErrorActionPreference = 'Stop'
if (-not $OutDir) {
    $OutDir = Join-Path $RepoRoot 'docs\_gate0\logs\perf-baseline'
}
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$nativeOut = Join-Path $RepoRoot 'native\out'
$exe = Join-Path $nativeOut 'Cmnet.Native.exe'
$advisor = Join-Path $nativeOut 'advisor.exe'
$sha = (git -C $RepoRoot rev-parse HEAD).Trim()
$machine = [pscustomobject]@{
    CsName   = $env:COMPUTERNAME
    Os       = [System.Environment]::OSVersion.VersionString
    Cpu      = (Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name).Trim()
    RamGb    = [math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory / 1GB, 1)
    Dotnet   = (dotnet --version)
    Go       = (go version)
    BuildSha = $sha
    MeasuredAt = (Get-Date).ToString('o')
}

function Write-Step([string]$msg) { Write-Host "==> $msg" -ForegroundColor Cyan }

function Invoke-TimedProcess {
    param(
        [string]$FilePath,
        [string[]]$ArgumentList = @(),
        [int]$TimeoutSec = 180,
        [switch]$SampleMemory
    )
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $p = Start-Process -FilePath $FilePath -ArgumentList $ArgumentList -PassThru -WorkingDirectory $nativeOut
    $peakWs = 0L
    $peakPrivate = 0L
    try {
        while (-not $p.HasExited) {
            if ($SampleMemory) {
                try {
                    $p.Refresh()
                    if ($p.WorkingSet64 -gt $peakWs) { $peakWs = $p.WorkingSet64 }
                    if ($p.PrivateMemorySize64 -gt $peakPrivate) { $peakPrivate = $p.PrivateMemorySize64 }
                } catch { }
            }
            if ($sw.Elapsed.TotalSeconds -gt $TimeoutSec) {
                Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
                throw "Timeout after ${TimeoutSec}s: $FilePath $($ArgumentList -join ' ')"
            }
            Start-Sleep -Milliseconds 50
        }
    } finally {
        $sw.Stop()
    }
    return [pscustomobject]@{
        ExitCode       = $p.ExitCode
        ElapsedMs      = [math]::Round($sw.Elapsed.TotalMilliseconds, 1)
        PeakWorkingSetMb = [math]::Round($peakWs / 1MB, 1)
        PeakPrivateMb  = [math]::Round($peakPrivate / 1MB, 1)
    }
}

function Measure-InteractiveStartup {
    param([int]$Runs)
    $rows = @()
    for ($i = 1; $i -le $Runs; $i++) {
        Write-Step "interactive startup run $i/$Runs"
        $dataDir = Join-Path $env:TEMP ("cmnet-perf-data-" + [guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
        $sw = [System.Diagnostics.Stopwatch]::StartNew()
        $p = Start-Process -FilePath $exe -ArgumentList @('--data-dir', $dataDir) -PassThru -WorkingDirectory $nativeOut
        $toWindowMs = $null
        $toAdvisorMs = $null
        $idleWsMb = $null
        $idlePrivateMb = $null
        $advisorWsMb = $null
        try {
            $deadline = (Get-Date).AddSeconds(45)
            while ((Get-Date) -lt $deadline) {
                $p.Refresh()
                if ($p.HasExited) { throw "Cmnet.Native exited early: $($p.ExitCode)" }
                if ($p.MainWindowHandle -ne [IntPtr]::Zero -and -not $toWindowMs) {
                    $toWindowMs = [math]::Round($sw.Elapsed.TotalMilliseconds, 1)
                }
                $child = Get-CimInstance Win32_Process -Filter "Name='advisor.exe'" -ErrorAction SilentlyContinue |
                    Where-Object { $_.ParentProcessId -eq $p.Id } |
                    Select-Object -First 1
                if ($child -and -not $toAdvisorMs) {
                    $toAdvisorMs = [math]::Round($sw.Elapsed.TotalMilliseconds, 1)
                }
                if ($toWindowMs -and $toAdvisorMs) { break }
                Start-Sleep -Milliseconds 50
            }
            if (-not $toWindowMs) { throw 'MainWindowHandle never appeared within 45s' }
            # Idle settle: shell visible + core child spawned (or timed out waiting for child).
            Start-Sleep -Seconds 5
            $p.Refresh()
            $idleWsMb = [math]::Round($p.WorkingSet64 / 1MB, 1)
            $idlePrivateMb = [math]::Round($p.PrivateMemorySize64 / 1MB, 1)
            $adv = Get-Process -Name advisor -ErrorAction SilentlyContinue |
                Where-Object { try { $_.Parent.Id -eq $p.Id } catch { $false } } |
                Select-Object -First 1
            if (-not $adv) {
                # Fallback: any advisor started after our launch near our PID tree.
                $cim = Get-CimInstance Win32_Process -Filter "Name='advisor.exe'" -ErrorAction SilentlyContinue |
                    Where-Object { $_.ParentProcessId -eq $p.Id } | Select-Object -First 1
                if ($cim) {
                    $adv = Get-Process -Id $cim.ProcessId -ErrorAction SilentlyContinue
                }
            }
            if ($adv) {
                $adv.Refresh()
                $advisorWsMb = [math]::Round($adv.WorkingSet64 / 1MB, 1)
            }
        } finally {
            $sw.Stop()
            Get-CimInstance Win32_Process -Filter "Name='advisor.exe'" -ErrorAction SilentlyContinue |
                Where-Object { $_.ParentProcessId -eq $p.Id } |
                ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
            if (-not $p.HasExited) { Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue }
            Start-Sleep -Milliseconds 300
            Remove-Item -Recurse -Force $dataDir -ErrorAction SilentlyContinue
        }
        $rows += [pscustomobject]@{
            Run                = $i
            ToMainWindowMs     = $toWindowMs
            ToAdvisorChildMs   = $toAdvisorMs
            IdleNativeWsMb     = $idleWsMb
            IdleNativePrivateMb = $idlePrivateMb
            IdleAdvisorWsMb    = $advisorWsMb
            Note               = 'interactive = MainWindow visible; core ready approximated by advisor.exe child spawn (BootAsync)'
        }
    }
    return $rows
}

function Stats([double[]]$values) {
    if (-not $values -or $values.Count -eq 0) { return $null }
    $sorted = $values | Sort-Object
    [pscustomobject]@{
        n      = $sorted.Count
        min    = [math]::Round(($sorted | Measure-Object -Minimum).Minimum, 1)
        max    = [math]::Round(($sorted | Measure-Object -Maximum).Maximum, 1)
        median = [math]::Round($sorted[[math]::Floor(($sorted.Count - 1) / 2)], 1)
        mean   = [math]::Round(($sorted | Measure-Object -Average).Average, 1)
    }
}

# --- build ---
if (-not $SkipPublish) {
    Write-Step 'go build advisor.exe'
    $env:CGO_ENABLED = '0'
    & go build -trimpath -ldflags '-s -w' -o $advisor ./cmd/advisor
    if ($LASTEXITCODE -ne 0) { throw 'go build advisor failed' }

    Write-Step 'dotnet publish Cmnet.Native'
    & dotnet publish (Join-Path $RepoRoot 'native\Cmnet.Native.csproj') -c Release -r win-x64 --self-contained true -o $nativeOut -p:PublishSingleFile=true -p:PublishTrimmed=false
    if ($LASTEXITCODE -ne 0) { throw 'dotnet publish failed' }
}

if (-not (Test-Path $exe)) { throw "Missing $exe" }
if (-not (Test-Path $advisor)) { throw "Missing $advisor" }

# --- smoke-ui ---
$smoke = @()
for ($i = 1; $i -le $SmokeRuns; $i++) {
    Write-Step "smoke-ui run $i/$SmokeRuns"
    $smoke += Invoke-TimedProcess -FilePath $exe -ArgumentList @('--smoke-ui') -TimeoutSec 60
}

# --- capture-ui ---
$captureDest = Join-Path $OutDir 'capture-ui'
if (Test-Path $captureDest) { Remove-Item -Recurse -Force $captureDest }
New-Item -ItemType Directory -Force -Path $captureDest | Out-Null
Write-Step 'capture-ui (memory sampled)'
$capture = Invoke-TimedProcess -FilePath $exe -ArgumentList @('--capture-ui', $captureDest) -TimeoutSec 300 -SampleMemory

# --- interactive startup ---
$startup = Measure-InteractiveStartup -Runs $StartupRuns

# --- local scout fixture ---
Write-Step 'tools/perfbaseline (local scout fixture)'
$scoutJsonPath = Join-Path $OutDir 'scout-local.json'
Push-Location $RepoRoot
try {
    & go run ./tools/perfbaseline | Tee-Object -FilePath $scoutJsonPath
    if ($LASTEXITCODE -ne 0) { throw 'perfbaseline failed' }
} finally {
    Pop-Location
}
$scoutLocal = Get-Content -Raw $scoutJsonPath | ConvertFrom-Json

$result = [ordered]@{
    machine = $machine
    commands = @{
        go_build       = 'CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o native/out/advisor.exe ./cmd/advisor'
        dotnet_publish = 'dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out -p:PublishSingleFile=true'
        smoke_ui       = 'native\out\Cmnet.Native.exe --smoke-ui'
        capture_ui     = 'native\out\Cmnet.Native.exe --capture-ui <dir>'
        interactive    = 'native\out\Cmnet.Native.exe --data-dir <temp>'
        scout_local    = 'go run ./tools/perfbaseline'
        measure_script = 'pwsh docs/_gate0/scripts/Measure-PerfBaseline.ps1'
    }
    smoke_ui = @{
        runs = $smoke
        elapsed_ms = Stats @($smoke | ForEach-Object { [double]$_.ElapsedMs })
    }
    capture_ui = $capture
    interactive_startup = @{
        runs = $startup
        to_main_window_ms = Stats @($startup | ForEach-Object { [double]$_.ToMainWindowMs })
        to_advisor_child_ms = Stats @($startup | Where-Object { $_.ToAdvisorChildMs } | ForEach-Object { [double]$_.ToAdvisorChildMs })
        idle_native_ws_mb = Stats @($startup | ForEach-Object { [double]$_.IdleNativeWsMb })
        idle_advisor_ws_mb = Stats @($startup | Where-Object { $_.IdleAdvisorWsMb } | ForEach-Object { [double]$_.IdleAdvisorWsMb })
    }
    scout_local_fixture = $scoutLocal
    not_measured = @(
        'Live OpenDota network scout cold/warm (avoid unbounded free-API load in Gate 0.6 harness)',
        'STRATZ scout (requires token)',
        'Redraw/scroll FPS under user input (needs interactive Computer Use / manual QA)',
        'Cancellation latency under live scout'
    )
}

$jsonPath = Join-Path $OutDir 'perf-baseline.raw.json'
($result | ConvertTo-Json -Depth 8) | Set-Content -Encoding utf8 $jsonPath
Write-Host "Wrote $jsonPath" -ForegroundColor Green
return $result
