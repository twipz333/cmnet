<#
.SYNOPSIS
  Gate 3.5 — scripted clean-VM install / smoke / upgrade / uninstall for native CMNET.

.DESCRIPTION
  Reproducible friend-path proof on a Windows builder or clean VM:
    1) Resolve or build cmnet-native-setup-<ver>.exe
    2) Silent install to a TEMP directory (/S /D=...)
    3) Run installed Cmnet.Native.exe --smoke-ui
    4) Optional second silent install (upgrade overwrite of same INSTDIR)
    5) Write a marker under %APPDATA%\cmnet BEFORE uninstall
    6) Silent uninstall; assert install dir gone and marker preserved

  Does NOT claim Released. Does NOT invent human friend-trial success.
  Electron channel (desktop.yml / cmnet-setup-*.exe) is untouched.

  Side effects: mutates per-user NSIS uninstall keys and Desktop/Start Menu
  «CMNET» shortcuts while the trial runs. The script snapshots and restores
  those HKCU keys + shortcuts when they already existed (builder safety).
  Prefer a clean VM / spare Windows profile for the canonical run.

.PARAMETER SetupPath
  Existing cmnet-native-setup-*.exe. If omitted, uses native/dist when present,
  otherwise builds via Build-NativeInstaller.ps1 (requires Go + .NET SDK + NSIS).

.PARAMETER Build
  Force rebuild of the setup via Build-NativeInstaller.ps1 even if dist exists.

.PARAMETER SkipBuild
  Fail if setup is missing instead of building.

.PARAMETER SkipUpgrade
  Skip the second silent install (upgrade overwrite) step.

.PARAMETER Version
  Setup version segment (default 0.4.0) — matches Build-NativeInstaller.ps1.

.PARAMETER InstallDir
  Silent-install target. Default: %TEMP%\cmnet-gate35-cleanvm-install

.PARAMETER RepoRoot
  Repository root (default: parent of native/).

.PARAMETER EvidenceDir
  Where to write script-evidence.json (default: docs/_gate0/gate3-friend-trial-wip).

.EXAMPLE
  pwsh -File native/installer/Test-CleanVmFriendInstall.ps1 -Build

.EXAMPLE
  pwsh -File native/installer/Test-CleanVmFriendInstall.ps1 -SetupPath D:\drop\cmnet-native-setup-0.4.0.exe
#>
[CmdletBinding()]
param(
    [string]$SetupPath = '',
    [switch]$Build,
    [switch]$SkipBuild,
    [switch]$SkipUpgrade,
    [string]$Version = '0.4.0',
    [string]$InstallDir = '',
    [string]$RepoRoot = '',
    [string]$EvidenceDir = ''
)

$ErrorActionPreference = 'Stop'

function Write-Step([string]$Message) {
    Write-Host ""
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Resolve-RepoRoot {
    if ($RepoRoot) { return (Resolve-Path $RepoRoot).Path }
    $here = $PSScriptRoot
    return (Resolve-Path (Join-Path $here '..\..')).Path
}

function Ensure-Dir([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) {
        New-Item -ItemType Directory -Force -Path $Path | Out-Null
    }
}

function Invoke-QuietProcess {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [Parameter(Mandatory = $true)][string[]]$ArgumentList,
        [int]$TimeoutMs = 300000,
        [string]$Label = 'process'
    )
    if (-not (Test-Path -LiteralPath $FilePath)) {
        throw "$Label missing: $FilePath"
    }
    Write-Host "  $Label : $FilePath $($ArgumentList -join ' ')"
    $proc = Start-Process -FilePath $FilePath -ArgumentList $ArgumentList `
        -WindowStyle Hidden -PassThru -Wait:$false
    if (-not $proc.WaitForExit($TimeoutMs)) {
        try { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue } catch { }
        throw "$Label timed out after ${TimeoutMs}ms"
    }
    return [int]$proc.ExitCode
}

function Export-RegKeyIfPresent([string]$RegPath, [string]$OutFile) {
    if (Test-Path -LiteralPath $RegPath) {
        # PowerShell uses HKCU:\...; reg.exe wants HKCU\...
        $regExport = $RegPath -replace '^HKCU:\\', 'HKCU\' -replace '^HKCU:', 'HKCU'
        & reg.exe export $regExport $OutFile /y | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "reg export failed for $RegPath ($regExport)" }
        return $true
    }
    return $false
}

function Import-RegFileIfPresent([string]$File) {
    if (Test-Path -LiteralPath $File) {
        & reg.exe import $File | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "reg import failed: $File" }
    }
}

function Copy-IfExists([string]$Src, [string]$Dest) {
    if (Test-Path -LiteralPath $Src) {
        Ensure-Dir (Split-Path -Parent $Dest)
        Copy-Item -LiteralPath $Src -Destination $Dest -Force
        return $true
    }
    return $false
}

$script:Root = Resolve-RepoRoot
$distDir = Join-Path $script:Root 'native\dist'
$setupName = "cmnet-native-setup-$Version.exe"
$defaultSetup = Join-Path $distDir $setupName
$buildScript = Join-Path $script:Root 'native\installer\Build-NativeInstaller.ps1'

if (-not $InstallDir) {
    $InstallDir = Join-Path $env:TEMP 'cmnet-gate35-cleanvm-install'
}
# NSIS /D= is safest without spaces; normalize.
$InstallDir = [System.IO.Path]::GetFullPath($InstallDir)

if (-not $EvidenceDir) {
    $EvidenceDir = Join-Path $script:Root 'docs\_gate0\gate3-friend-trial-wip'
}
Ensure-Dir $EvidenceDir

$appDataCmnet = Join-Path $env:APPDATA 'cmnet'
$markerName = 'gate35-cleanvm-marker.txt'
$markerPath = Join-Path $appDataCmnet $markerName
$markerPayload = "gate35-cleanvm marker`nutc=$((Get-Date).ToUniversalTime().ToString('o'))`npid=$PID`ninstall=$InstallDir`n"

$results = [ordered]@{
    gate                 = '3.5'
    script               = 'native/installer/Test-CleanVmFriendInstall.ps1'
    started_utc          = (Get-Date).ToUniversalTime().ToString('o')
    version              = $Version
    setup_path           = $null
    install_dir          = $InstallDir
    appdata_cmnet        = $appDataCmnet
    marker_path          = $markerPath
    steps                = [ordered]@{}
    released_claim       = $false
    electron_preserved   = $true
    human_friend_trials  = 'BLOCKED — pending real friends (see NOTES.md)'
    pass                 = $false
}

Write-Host "RepoRoot=$($script:Root)"
Write-Host "InstallDir=$InstallDir"
Write-Host "EvidenceDir=$EvidenceDir"

# --- Resolve setup EXE ---
Write-Step 'Resolve cmnet-native-setup'
$resolved = $null
if ($SetupPath) {
    $resolved = (Resolve-Path -LiteralPath $SetupPath).Path
}
elseif ((-not $Build) -and (Test-Path -LiteralPath $defaultSetup)) {
    $resolved = $defaultSetup
    Write-Host "  consuming existing $resolved"
}
elseif ($SkipBuild) {
    throw "Setup missing at $defaultSetup and -SkipBuild was set. Pass -SetupPath or -Build."
}
else {
    Write-Step 'Build setup via Build-NativeInstaller.ps1'
    if (-not (Test-Path -LiteralPath $buildScript)) {
        throw "Build script missing: $buildScript"
    }
    & $buildScript -Version $Version
    if (-not (Test-Path -LiteralPath $defaultSetup)) {
        throw "Build finished but setup missing: $defaultSetup (NSIS may have fallen back to zip-only)"
    }
    $resolved = $defaultSetup
}

$results.setup_path = $resolved
$setupHash = (Get-FileHash -LiteralPath $resolved -Algorithm SHA256).Hash
$results.setup_sha256 = $setupHash
$results.setup_bytes = (Get-Item -LiteralPath $resolved).Length
Write-Host "  SETUP=$resolved"
Write-Host "  SHA256=$setupHash"
$results.steps.resolve_setup = 'PASS'

# --- Snapshot existing per-user install registration (builder safety) ---
Write-Step 'Snapshot existing HKCU uninstall / shortcuts (restore after trial)'
$snapRoot = Join-Path $env:TEMP ("cmnet-gate35-regsnap-" + [guid]::NewGuid().ToString('N'))
Ensure-Dir $snapRoot
$hadUninstallKey = Export-RegKeyIfPresent 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native' (Join-Path $snapRoot 'uninstall.reg')
$hadNativeKey = Export-RegKeyIfPresent 'HKCU:\Software\CMNET\Native' (Join-Path $snapRoot 'native.reg')
$desktopLnk = Join-Path ([Environment]::GetFolderPath('Desktop')) 'CMNET.lnk'
$startLnk = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\CMNET\CMNET.lnk'
$hadDesktop = Copy-IfExists $desktopLnk (Join-Path $snapRoot 'CMNET.desktop.lnk')
$hadStart = Copy-IfExists $startLnk (Join-Path $snapRoot 'CMNET.start.lnk')
$results.steps.snapshot = [ordered]@{
    uninstall_key = $hadUninstallKey
    native_key    = $hadNativeKey
    desktop_lnk   = $hadDesktop
    start_lnk     = $hadStart
}

$restored = $false
try {
    # Fresh install dir
    if (Test-Path -LiteralPath $InstallDir) {
        Write-Step "Remove prior trial install dir $InstallDir"
        Remove-Item -LiteralPath $InstallDir -Recurse -Force -ErrorAction SilentlyContinue
    }
    Ensure-Dir (Split-Path -Parent $InstallDir)

    # --- Silent install ---
    # NSIS: /S silent; /D=path MUST be last and unquoted (path may contain spaces).
    Write-Step 'Silent install (/S /D=TEMP)'
    $installArgs = @('/S', "/D=$InstallDir")
    $code = Invoke-QuietProcess -FilePath $resolved -ArgumentList $installArgs -TimeoutMs 600000 -Label 'nsis-install'
    if ($code -ne 0) { throw "Silent install exit $code" }
    $nativeExe = Join-Path $InstallDir 'Cmnet.Native.exe'
    $advisorExe = Join-Path $InstallDir 'advisor.exe'
    $uninstaller = Join-Path $InstallDir 'Uninstall.exe'
    if (-not (Test-Path -LiteralPath $nativeExe)) { throw "Missing installed payload: $nativeExe" }
    if (-not (Test-Path -LiteralPath $advisorExe)) { throw "Missing installed sidecar: $advisorExe" }
    if (-not (Test-Path -LiteralPath $uninstaller)) { throw "Missing uninstaller: $uninstaller" }
    $results.steps.silent_install = 'PASS'
    Write-Host "  installed payload OK"

    # --- smoke-ui ---
    Write-Step 'Installed --smoke-ui'
    $code = Invoke-QuietProcess -FilePath $nativeExe -ArgumentList @('--smoke-ui') -TimeoutMs 120000 -Label 'smoke-ui'
    if ($code -ne 0) { throw "smoke-ui exit $code" }
    $results.steps.smoke_ui = 'PASS'
    Write-Host "  smoke-ui exit 0"

    # --- Optional upgrade overwrite ---
    if (-not $SkipUpgrade) {
        Write-Step 'Silent upgrade (re-install same setup over INSTDIR)'
        $code = Invoke-QuietProcess -FilePath $resolved -ArgumentList $installArgs -TimeoutMs 600000 -Label 'nsis-upgrade'
        if ($code -ne 0) { throw "Silent upgrade exit $code" }
        if (-not (Test-Path -LiteralPath $nativeExe)) { throw 'Upgrade lost Cmnet.Native.exe' }
        $code = Invoke-QuietProcess -FilePath $nativeExe -ArgumentList @('--smoke-ui') -TimeoutMs 120000 -Label 'smoke-ui-after-upgrade'
        if ($code -ne 0) { throw "smoke-ui after upgrade exit $code" }
        $results.steps.silent_upgrade = 'PASS'
        Write-Host "  upgrade + smoke-ui OK"
    }
    else {
        $results.steps.silent_upgrade = 'SKIPPED'
    }

    # --- Marker in APPDATA before uninstall ---
    Write-Step 'Create %APPDATA%\cmnet marker (must survive uninstall)'
    Ensure-Dir $appDataCmnet
    Set-Content -LiteralPath $markerPath -Value $markerPayload -Encoding utf8
    if (-not (Test-Path -LiteralPath $markerPath)) { throw "Failed to write marker $markerPath" }
    $markerBeforeHash = (Get-FileHash -LiteralPath $markerPath -Algorithm SHA256).Hash
    $results.marker_sha256_before = $markerBeforeHash
    $results.steps.marker_create = 'PASS'
    Write-Host "  marker written: $markerPath"

    # --- Silent uninstall ---
    Write-Step 'Silent uninstall (/S)'
    # Re-resolve Uninstall.exe (upgrade may have rewritten it)
    $uninstaller = Join-Path $InstallDir 'Uninstall.exe'
    $code = Invoke-QuietProcess -FilePath $uninstaller -ArgumentList @('/S') -TimeoutMs 300000 -Label 'nsis-uninstall'
    if ($code -ne 0) { throw "Silent uninstall exit $code" }

    # Give FS a moment; NSIS may return before last handle closes.
    Start-Sleep -Seconds 2
    if (Test-Path -LiteralPath $nativeExe) {
        throw "Uninstall left Cmnet.Native.exe behind: $nativeExe"
    }
    if (Test-Path -LiteralPath $InstallDir) {
        $leftover = @(Get-ChildItem -LiteralPath $InstallDir -Force -ErrorAction SilentlyContinue)
        if ($leftover.Count -gt 0) {
            Write-Warning "Install dir still has $($leftover.Count) leftover item(s); expecting empty/absent after uninstall"
            # Soft: some AV locks Uninstall.exe briefly; fail only if payload remains.
            $payloadLeft = $leftover | Where-Object { $_.Name -match '^(Cmnet\.Native\.exe|advisor\.exe)$' }
            if ($payloadLeft) {
                throw "Uninstall left payload: $($payloadLeft.Name -join ', ')"
            }
        }
    }
    $results.steps.silent_uninstall = 'PASS'
    Write-Host "  install payload removed"

    # --- Assert APPDATA preserved ---
    Write-Step 'Assert %APPDATA%\cmnet marker preserved'
    if (-not (Test-Path -LiteralPath $markerPath)) {
        throw "MARKER MISSING after uninstall — data-preserve contract BROKEN: $markerPath"
    }
    $markerAfterHash = (Get-FileHash -LiteralPath $markerPath -Algorithm SHA256).Hash
    $results.marker_sha256_after = $markerAfterHash
    if ($markerAfterHash -ne $markerBeforeHash) {
        throw "Marker content changed after uninstall (before=$markerBeforeHash after=$markerAfterHash)"
    }
    if (-not (Test-Path -LiteralPath $appDataCmnet)) {
        throw "%APPDATA%\cmnet directory missing after uninstall"
    }
    $results.steps.appdata_preserved = 'PASS'
    Write-Host "  APPDATA\cmnet preserved (marker hash match)"

    $results.pass = $true
    $results.finished_utc = (Get-Date).ToUniversalTime().ToString('o')
    Write-Step 'PASS Gate 3.5 scripted clean-VM path (human friend trials still BLOCKED)'
}
catch {
    $results.pass = $false
    $results.error = $_.Exception.Message
    $results.finished_utc = (Get-Date).ToUniversalTime().ToString('o')
    Write-Host "FAIL: $($_.Exception.Message)" -ForegroundColor Red
    throw
}
finally {
    Write-Step 'Restore prior HKCU / shortcuts (if any)'
    try {
        # Clear trial keys first so a restore import is authoritative.
        if (Test-Path -LiteralPath 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native') {
            Remove-Item -LiteralPath 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\CMNET-Native' -Recurse -Force -ErrorAction SilentlyContinue
        }
        if (Test-Path -LiteralPath 'HKCU:\Software\CMNET\Native') {
            Remove-Item -LiteralPath 'HKCU:\Software\CMNET\Native' -Recurse -Force -ErrorAction SilentlyContinue
        }
        if (Test-Path -LiteralPath 'HKCU:\Software\CMNET') {
            $left = Get-ChildItem -LiteralPath 'HKCU:\Software\CMNET' -ErrorAction SilentlyContinue
            if (-not $left) {
                Remove-Item -LiteralPath 'HKCU:\Software\CMNET' -Force -ErrorAction SilentlyContinue
            }
        }
        Import-RegFileIfPresent (Join-Path $snapRoot 'uninstall.reg')
        Import-RegFileIfPresent (Join-Path $snapRoot 'native.reg')
        if ($hadDesktop) {
            Copy-Item -LiteralPath (Join-Path $snapRoot 'CMNET.desktop.lnk') -Destination $desktopLnk -Force
        }
        if (-not $hadDesktop -and (Test-Path -LiteralPath $desktopLnk)) {
            # Heuristic: if shortcut target is our trial InstallDir, delete it.
            try {
                $sh = New-Object -ComObject WScript.Shell
                $sc = $sh.CreateShortcut($desktopLnk)
                if ($sc.TargetPath -and $sc.TargetPath.StartsWith($InstallDir, [System.StringComparison]::OrdinalIgnoreCase)) {
                    Remove-Item -LiteralPath $desktopLnk -Force -ErrorAction SilentlyContinue
                }
            }
            catch { }
        }
        if ($hadStart) {
            Ensure-Dir (Split-Path -Parent $startLnk)
            Copy-Item -LiteralPath (Join-Path $snapRoot 'CMNET.start.lnk') -Destination $startLnk -Force
        }
        elseif (Test-Path -LiteralPath $startLnk) {
            try {
                $sh = New-Object -ComObject WScript.Shell
                $sc = $sh.CreateShortcut($startLnk)
                if ($sc.TargetPath -and $sc.TargetPath.StartsWith($InstallDir, [System.StringComparison]::OrdinalIgnoreCase)) {
                    Remove-Item -LiteralPath $startLnk -Force -ErrorAction SilentlyContinue
                    $smDir = Split-Path -Parent $startLnk
                    if ((Test-Path $smDir) -and -not (Get-ChildItem $smDir -Force -ErrorAction SilentlyContinue)) {
                        Remove-Item -LiteralPath $smDir -Force -ErrorAction SilentlyContinue
                    }
                }
            }
            catch { }
        }
        $restored = $true
        $results.steps.restore = 'PASS'
    }
    catch {
        $results.steps.restore = "FAIL: $($_.Exception.Message)"
        Write-Warning "Restore after trial failed: $($_.Exception.Message)"
    }

    $evidencePath = Join-Path $EvidenceDir 'script-evidence.json'
    ($results | ConvertTo-Json -Depth 6) | Set-Content -LiteralPath $evidencePath -Encoding utf8
    Write-Host "EVIDENCE=$evidencePath"
    Write-Host "RESTORED=$restored"
    Write-Host "PASS=$($results.pass)"
}

if (-not $results.pass) {
    exit 1
}
Write-Host "DONE Gate 3.5 scripted path — human friend trials remain BLOCKED/pending in NOTES.md"
exit 0
