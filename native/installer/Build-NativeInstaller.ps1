<#
.SYNOPSIS
  Gate 3.2 — build a friend-installable CMNET native Windows package.

.DESCRIPTION
  From a clean Windows checkout with Go + .NET SDK (builder machine only):
    1) Builds advisor.exe (Go sidecar) into native/out
    2) Publishes Cmnet.Native.exe self-contained win-x64 into native/out
    3) Always writes portable zip: native/dist/cmnet-native-<ver>-win-x64.zip
    4) Builds NSIS installer when makensis is available (or after download):
         native/dist/cmnet-native-setup-<ver>.exe
    5) Optional smoke of the staged payload (--smoke-ui)

  End users need neither SDK nor CLI — only the setup EXE (or unzipped portable).
  Artifact names are DISTINCT from Electron desktop.yml (`cmnet-setup-*.exe`).
  Package is UNSIGNED (Gate 3.3). Do not claim Released.

.PARAMETER Version
  Numeric version for file naming and PE (default 0.4.0).

.PARAMETER InformationalVersion
  Friend-facing stamp (default 0.4.0-native).

.PARAMETER SkipPublish
  Reuse existing native/out payload (must contain Cmnet.Native.exe + advisor.exe).

.PARAMETER SkipNsis
  Produce portable zip only.

.PARAMETER SkipSmoke
  Skip --smoke-ui after publish.

.PARAMETER RepoRoot
  Repository root (default: parent of native/).
#>
[CmdletBinding()]
param(
    [string]$Version = '0.4.0',
    [string]$InformationalVersion = '0.4.0-native',
    [switch]$SkipPublish,
    [switch]$SkipNsis,
    [switch]$SkipSmoke,
    [string]$RepoRoot = ''
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
    if (-not (Test-Path $Path)) {
        New-Item -ItemType Directory -Force -Path $Path | Out-Null
    }
}

function Find-MakeNsis {
    $pf86 = ${env:ProgramFiles(x86)}
    $probe = @(
        (Get-Command makensis -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source -ErrorAction SilentlyContinue)
        $(if ($pf86) { Join-Path $pf86 'NSIS\makensis.exe' })
        $(if ($env:ProgramFiles) { Join-Path $env:ProgramFiles 'NSIS\makensis.exe' })
        $(if ($env:LOCALAPPDATA) { Join-Path $env:LOCALAPPDATA 'Programs\NSIS\makensis.exe' })
    )
    # Force array: a single match must not become a scalar (else [0] is first char).
    $candidates = @($probe | Where-Object { $_ -and (Test-Path -LiteralPath $_) })
    if ($candidates.Count -gt 0) { return [string]$candidates[0] }

    $toolsMakensis = Join-Path $script:Root 'tools\nsis\makensis.exe'
    if (Test-Path -LiteralPath $toolsMakensis) { return $toolsMakensis }
    return $null
}

function Install-NsisViaWinget {
    $winget = Get-Command winget -ErrorAction SilentlyContinue
    if (-not $winget) { return $null }
    Write-Step 'Installing NSIS via winget (NSIS.NSIS)'
    & winget install --id NSIS.NSIS -e --accept-package-agreements --accept-source-agreements --disable-interactivity
    # winget may return non-zero when already installed; still probe paths
    return (Find-MakeNsis)
}

function Install-NsisPortable {
    # Prefer winget (reliable on Windows CI/dev). Fallback: SourceForge zip → tools/nsis.
    $viaWinget = Install-NsisViaWinget
    if ($viaWinget) { return $viaWinget }

    $nsisVer = '3.11'
    $dest = Join-Path $script:Root 'tools\nsis'
    $marker = Join-Path $dest "nsis-$nsisVer.ok"
    $makensis = Join-Path $dest 'makensis.exe'
    if ((Test-Path $makensis) -and (Test-Path $marker)) {
        return $makensis
    }

    Write-Step "Downloading NSIS $nsisVer (portable) into tools/nsis"
    Ensure-Dir (Join-Path $script:Root 'tools')
    $zip = Join-Path $env:TEMP "nsis-$nsisVer.zip"
    $urls = @(
        "https://sourceforge.net/projects/nsis/files/NSIS%203/$nsisVer/nsis-$nsisVer.zip/download",
        "https://downloads.sourceforge.net/project/nsis/NSIS%203/$nsisVer/nsis-$nsisVer.zip"
    )
    $downloaded = $false
    foreach ($url in $urls) {
        try {
            Write-Host "  trying $url"
            Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing -MaximumRedirection 8
            if ((Test-Path $zip) -and ((Get-Item $zip).Length -gt 1MB)) {
                $downloaded = $true
                break
            }
        }
        catch {
            Write-Warning "  download failed: $($_.Exception.Message)"
        }
    }
    if (-not $downloaded) {
        throw 'Could not obtain NSIS. Install NSIS 3.x (makensis on PATH), enable winget, or pass -SkipNsis for zip-only.'
    }

    if (Test-Path $dest) { Remove-Item -Recurse -Force $dest }
    Ensure-Dir $dest
    Expand-Archive -Path $zip -DestinationPath (Join-Path $script:Root 'tools') -Force
    $extracted = Join-Path $script:Root "tools\nsis-$nsisVer"
    if (-not (Test-Path $extracted)) {
        $guess = Get-ChildItem (Join-Path $script:Root 'tools') -Directory -Filter 'nsis-*' |
            Where-Object { Test-Path (Join-Path $_.FullName 'makensis.exe') } |
            Select-Object -First 1
        if (-not $guess) { throw "NSIS extract missing makensis.exe under tools/" }
        $extracted = $guess.FullName
    }
    if ($extracted -ne $dest) {
        if (Test-Path $dest) { Remove-Item -Recurse -Force $dest }
        Move-Item -Path $extracted -Destination $dest
    }
    if (-not (Test-Path $makensis)) { throw "makensis.exe missing after extract: $makensis" }
    Set-Content -Path $marker -Value $nsisVer -Encoding ascii
    return $makensis
}

$script:Root = Resolve-RepoRoot
$outDir = Join-Path $script:Root 'native\out'
$distDir = Join-Path $script:Root 'native\dist'
$installerDir = Join-Path $script:Root 'native\installer'
$nsi = Join-Path $installerDir 'cmnet-native.nsi'
$setupName = "cmnet-native-setup-$Version.exe"
$zipName = "cmnet-native-$Version-win-x64.zip"
$setupPath = Join-Path $distDir $setupName
$zipPath = Join-Path $distDir $zipName

Write-Host "RepoRoot=$($script:Root)"
Write-Host "Version=$Version InformationalVersion=$InformationalVersion"

Ensure-Dir $outDir
Ensure-Dir $distDir

if (-not $SkipPublish) {
    Write-Step 'Build Go advisor.exe (windows/amd64, CGO=0)'
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $advisor = Join-Path $outDir 'advisor.exe'
    & go build -trimpath -ldflags "-s -w -X main.version=$InformationalVersion" -o $advisor ./cmd/advisor
    if ($LASTEXITCODE -ne 0) { throw "go build failed: $LASTEXITCODE" }
    Set-Content -Path (Join-Path $outDir 'advisor.version') -Value $InformationalVersion -NoNewline -Encoding ascii

    Write-Step 'dotnet publish Cmnet.Native (self-contained win-x64)'
    & dotnet publish (Join-Path $script:Root 'native\Cmnet.Native.csproj') `
        -c Release -r win-x64 --self-contained true -o $outDir `
        -p:PublishSingleFile=true -p:PublishTrimmed=false `
        -p:Version=$Version -p:InformationalVersion=$InformationalVersion
    if ($LASTEXITCODE -ne 0) { throw "dotnet publish failed: $LASTEXITCODE" }
}

$nativeExe = Join-Path $outDir 'Cmnet.Native.exe'
$advisorExe = Join-Path $outDir 'advisor.exe'
if (-not (Test-Path $nativeExe)) { throw "Missing $nativeExe" }
if (-not (Test-Path $advisorExe)) { throw "Missing $advisorExe" }
if (-not (Test-Path (Join-Path $outDir 'advisor.version'))) {
    Set-Content -Path (Join-Path $outDir 'advisor.version') -Value $InformationalVersion -NoNewline -Encoding ascii
}

Write-Step "Portable zip → $zipPath"
if (Test-Path $zipPath) { Remove-Item -Force $zipPath }
Compress-Archive -Path (Join-Path $outDir '*') -DestinationPath $zipPath -Force

$nsisBuilt = $false
if (-not $SkipNsis) {
    Write-Step 'NSIS installer'
    $makensis = Find-MakeNsis
    if (-not $makensis) {
        try {
            $makensis = Install-NsisPortable
        }
        catch {
            Write-Warning $_.Exception.Message
            Write-Warning 'Falling back to portable zip only (-SkipNsis equivalent).'
            $makensis = $null
        }
    }
    if ($makensis) {
        Write-Host "makensis=$makensis"
        # Define paths relative to the .nsi file location for File / OutFile.
        $payloadRel = '..\out'
        $outFileRel = "..\dist\$setupName"
        & $makensis `
            "/DCMNET_VERSION=$Version" `
            "/DCMNET_INFO_VERSION=$InformationalVersion" `
            "/DCMNET_PAYLOAD_DIR=$payloadRel" `
            "/DCMNET_OUT_FILE=$outFileRel" `
            $nsi
        if ($LASTEXITCODE -ne 0) { throw "makensis failed: $LASTEXITCODE" }
        if (-not (Test-Path $setupPath)) { throw "Installer missing: $setupPath" }
        $nsisBuilt = $true
    }
}

if (-not $SkipSmoke) {
    Write-Step 'Smoke staged payload (--smoke-ui)'
    $proc = Start-Process -FilePath $nativeExe -ArgumentList '--smoke-ui' -WorkingDirectory $outDir -WindowStyle Hidden -PassThru
    if (-not $proc.WaitForExit(60000)) {
        Stop-Process -Id $proc.Id -Force
        throw 'Smoke timed out'
    }
    if ($proc.ExitCode -ne 0) {
        throw "Smoke failed: exit $($proc.ExitCode)"
    }
    Write-Host "smoke-ui exit 0"
}

# Manifest for evidence / CI
$manifest = [ordered]@{
    gate            = '3.2'
    version         = $Version
    informational   = $InformationalVersion
    unsigned        = $true
    released_claim  = $false
    electron_channel_preserved = $true
    electron_artifact_pattern  = 'cmnet-setup-*.exe (desktop.yml v0.3.x)'
    native_setup    = if ($nsisBuilt) { $setupName } else { $null }
    native_zip      = $zipName
    payload         = @('Cmnet.Native.exe', 'advisor.exe', 'advisor.version')
    install_dir_default = '%LOCALAPPDATA%\Programs\CMNET'
    data_dir        = '%APPDATA%\cmnet (preserved on uninstall)'
    built_utc       = (Get-Date).ToUniversalTime().ToString('o')
}
$manifestPath = Join-Path $distDir 'cmnet-native-package.manifest.json'
$manifest | ConvertTo-Json -Depth 5 | Set-Content -Path $manifestPath -Encoding utf8

Write-Step 'Artifacts'
Get-ChildItem $distDir | Format-Table Name, Length, LastWriteTime
Write-Host "MANIFEST=$manifestPath"
if ($nsisBuilt) {
    Write-Host "SETUP=$setupPath"
}
else {
    Write-Host "SETUP=(zip-only; NSIS not built)"
}
Write-Host "ZIP=$zipPath"
Write-Host "DONE Gate 3.2 package build (unsigned, not Released)"
