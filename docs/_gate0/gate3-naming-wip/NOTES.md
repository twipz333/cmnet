# Gate 3.1 — Native naming / icon / version / first-run (#54)

**Branch:** `twipz333/cmnet-gate31-naming`  
**Issue:** #54  
**Base:** `origin/master` @ `ba6b353` (Gate 2.6 migration #53)  
**Evidence:** `dotnet publish` win-x64 self-contained + `--smoke-ui` + `--capture-ui` (+ `--capture-naming`) → this folder  
**geometry.json:** **33** entries (unchanged CI matrix). Naming Welcome/About PNGs are **outside** geometry (same pattern as Settings/Data kits). Default CI `--capture-ui` must stay under 180s: Naming kit is opt-in (`--capture-naming` / `--capture-naming-only`).

## Before → after

| Surface | Before | After |
|---|---|---|
| Product name | Mixed strings (`CMNET · Match Intelligence` ad-hoc; Assembly untitled) | Single `ProductIdentity`: ShortName `CMNET`, WindowTitle / About / shell wordmark + tagline |
| EXE / PE metadata | Empty Product / FileDescription; no InformationalVersion | `Product=CMNET`, `FileDescription=CMNET Match Intelligence`, `InformationalVersion=0.4.0-native` (CI override via `-p:InformationalVersion`) |
| Application icon | `<ApplicationIcon></ApplicationIcon>` → default broken/missing look | `native/Assets/cmnet.ico` (teal diamond on dark tile) wired as ApplicationIcon + window Icon |
| About versions | App only → often `"native preview"` | App from assembly metadata + core from `advisor.version` / `--version` / PE; explicit «не Released» |
| Welcome / first-run | Data-dir chooser only | Same chooser + installer-journey honesty (Gate 3.2 shortcut path, no signing yet, %APPDATA%\cmnet, version footer) |
| Go sidecar stamp | CI built without `-X main.version` | `native.yml` ldflags + `advisor.version` sibling file for About |

## Captures (SAMPLE DATA labeled)

| Surface | Files |
|---|---|
| First-run | `naming-welcome.png` (+ alias `settings-welcome.png`) |
| About / version | `naming-about.png` (+ alias `settings-about.png`) |
| CI matrix (unchanged) | Draft×5×3 + Scout×5×3 + scout empty/loading/error → `geometry.json` Count==33 |

## Verify

```text
# Go core + version stamp
go build -trimpath -ldflags "-s -w -X main.version=0.4.0-native" -o native/out/advisor.exe ./cmd/advisor
printf 0.4.0-native> native/out/advisor.version

dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out `
  -p:PublishSingleFile=true -p:PublishTrimmed=false `
  -p:Version=0.4.0 -p:InformationalVersion=0.4.0-native

native/out/Cmnet.Native.exe --smoke-ui
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/gate3-naming-wip
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/gate3-naming-wip --capture-naming-only
# If publish fails with file lock: Stop-Process -Name Cmnet.Native -Force
```

Measured locally: `--smoke-ui` exit 0; default `--capture-ui` ≈37s, geometry Count==33; `--capture-naming-only` exit 0.

## Honesty

- Does **not** claim Released. Signing / NSIS-MSIX installer / auto-update remain Gate 3.2–3.3.  
- Minimal branded icon is a clean placeholder until a final art pass; must not look like a missing resource.  
- Scout / Draft / Data / History / Friends / GSI journeys not redesigned — Theme.xaml + StateKit reused.  
- Welcome capture sets `page = "Настройки"` so Draft/Scout viewport asserts are skipped (Welcome detaches those visuals).

## Deferred / out of Gate 3.1

| Gap | Rationale |
|---|---|
| Full NSIS/MSIX installer + Start Menu shortcut | Gate 3.2 |
| Code signing + update channel | Gate 3.3 |
| Friend VM / unassisted install trial | Gate 3.5 |
| Final marketing icon art | Optional polish; current ico is installer-ready shape |

## Ready-for-acceptance

Implementer recommends acceptance after independent review of Welcome + About captures and PE Product/Icon. Do **not** auto-merge; coordinator merges after CI.
