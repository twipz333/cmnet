# Gate 3.3 — Native update channel (fail-closed unsigned) (#58)

**Branch:** `twipz333/cmnet-gate33-updates`  
**Issue:** #58  
**Base:** `origin/master` @ `80f7c49` (Gate 3.2 NSIS #57)  
**Status:** implementer recommends acceptance after CI + independent review. **Not Released.**

## User outcome

Settings → О программе can **check** a versioned native update manifest. Download lands in staging with hash + signature gates. Without a configured public key, verification is **fail-closed**: honest «unsigned / unavailable» UI, **no silent apply**. Electron `desktop.yml` / `latest.yml` / `guest.yml` channel stays separate.

## Schema (`cmnet-native-update/v1`)

Canonical signature payload (UTF-8, rsa-sha256 PKCS#1):

```text
cmnet-native-update/v1
native
<version>
<artifact.sha256 lowercase hex>
<artifact.fileName>
<artifact.url>
```

Required JSON fields: `schema`, `channel` (`native`), `version`, `artifact.{kind,fileName,url,sha256,sizeBytes}`.  
Optional: `informational`, `released`, `publishedUtc`, `minClientVersion`, `notes`, `signature.{alg,keyId,value}`.  
`artifact.kind`: `nsis-setup` | `portable-zip`.

Electron-shaped JSON (`sha512` + `path`, no `schema`) is **rejected** at parse time so native never drinks `latest.yml` metadata by accident.

## Fail-closed trust

| Source | How |
|---|---|
| Sibling PEM | `update-trust.pem` next to `Cmnet.Native.exe` |
| Env file | `CMNET_UPDATE_PUBLIC_KEY_FILE` |
| Env PEM | `CMNET_UPDATE_PUBLIC_KEY_PEM` |

**No key configured → `Unsigned`, `canApply=false`.**  
Key present but missing/invalid signature → `Unsigned` / `Rejected`, never apply.  
Do not invent fake trust or ship a production private key in-repo.

### How a real cert / key plugs in later

1. **Manifest key (this gate):** generate RSA-2048+, keep private offline; distribute **public** PEM as `update-trust.pem` in the NSIS payload (or set env for enterprise). Sign each release manifest with the canonical string above; publish manifest URL via `CMNET_UPDATE_MANIFEST_URL` or `desktop.json` → `nativeUpdateManifestUrl`.
2. **Authenticode (ops, separate):** sign `cmnet-native-setup-*.exe` with the purchased code-signing certificate so SmartScreen calms down. That cert is **not** the same as the manifest RSA key; both are required for a trusted friend path.
3. **Do not** publish native artifacts into the Electron GitHub Release that holds `latest.yml`/`guest.yml` in a way that splits channels (lesson from v0.3.3 host+guest double-publish). Prefer a dedicated native manifest host or a clearly named asset set that electron-updater never reads. Keep `.github/workflows/desktop.yml` untouched.

## Failure recovery / rollback (do not brick)

1. Download writes only under `%LOCALAPPDATA%\CMNET\updates\staging\<version>\` (`.partial` then rename).  
2. SHA-256 / size mismatch → delete staging file; **install dir untouched**.  
3. Apply launches staged NSIS via shell; it does **not** patch files in-place from the WPF process.  
4. **Rollback:** reinstall the previous `cmnet-native-setup-*.exe` (silent: `setup.exe /S`). Uninstall via Apps → «CMNET (native)» preserves `%APPDATA%\cmnet` (Gate 3.2 contract). Portable zip users: keep the prior zip and re-extract over the install folder after quitting the app.  
5. Never claim Released for unsigned Gate 3.3 builds.

## Code / CLI

| Piece | Path |
|---|---|
| Schema + parse | `native/UpdateManifest.cs` |
| Trust load / verify | `native/UpdateTrust.cs` |
| Check / stage / launch | `native/UpdateChannel.cs` |
| Fixture harness | `native/UpdateSelfTest.cs` |
| About UI | `native/SettingsView.cs` (О программе) |
| CLI | `--self-test-updates [fixturesDir]`, `--check-updates [--manifest path] [--trust pem]` |

## Fixtures

Under `docs/_gate0/gate3-updates-wip/fixtures/`:

| File | Role |
|---|---|
| `manifest-unsigned.json` | Valid schema, no signature |
| `manifest-signed.json` | Signed with **test** RSA (not production) |
| `manifest-bad-signature.json` | Tampered sig → Rejected |
| `manifest-bad-schema.json` | Wrong schema id |
| `electron-latest-style.json` | Must be rejected |
| `test-public.pem` | Fixture public key |

`test-private.pem` is **gitignored** (regenerate locally if needed; do not commit).

## Verify (this workstation, 2026-10-31)

| Check | Result |
|---|---|
| `dotnet publish` win-x64 self-contained | PASS |
| `--self-test-updates` (11 fixtures) | PASS → `self-test-updates.txt` |
| `--check-updates` signed, no trust | `[Unsigned] canApply=no` |
| `--check-updates` signed + test-public.pem | `[Available] canApply=yes` |
| `--smoke-ui` | exit 0 |
| `--capture-updates-only` | `about-updates-unsigned.png`, `about-updates-unavailable.png` |
| `desktop.yml` | **untouched** |

```powershell
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out
native/out/Cmnet.Native.exe --self-test-updates docs/_gate0/gate3-updates-wip/fixtures
native/out/Cmnet.Native.exe --check-updates --manifest docs/_gate0/gate3-updates-wip/fixtures/manifest-signed.json
native/out/Cmnet.Native.exe --check-updates --manifest docs/_gate0/gate3-updates-wip/fixtures/manifest-signed.json --trust docs/_gate0/gate3-updates-wip/fixtures/test-public.pem
native/out/Cmnet.Native.exe --smoke-ui
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/gate3-updates-wip --capture-updates-only
```

## Honesty / out of scope

- **UNSIGNED** production trust — no Authenticode, no production manifest key in-repo.  
- Do **not** claim **Released**.  
- Do **not** merge Electron and native update channels.  
- Friend VM trials / full release review = later Gate 3 slices.  
- Buying/storing the real code-signing cert = human ops (issue out of scope).

## Ready-for-acceptance

Implementer recommends acceptance after green `native-windows` CI, review of this NOTES + `self-test-updates.txt`, and independent About check UX review. Coordinator merges; worker does **not** merge.
