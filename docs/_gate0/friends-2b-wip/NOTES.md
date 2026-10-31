# Gate 2.5 — Friends local-only import/export (#49)

**Branch:** `twipz333/cmnet-gate25-friends`  
**Issue:** #49  
**Base:** `origin/master` @ `6ebad4e` (Gate 2.4 History #51)  
**Evidence:** `dotnet publish` win-x64 self-contained + `--smoke-ui` + `--capture-ui` (+ `--capture-friends`) → this folder  
**geometry.json:** **33** entries (unchanged CI matrix). Friends honesty PNGs are **outside** geometry (same pattern as Data/Settings/GSI/History). Default CI `--capture-ui` must stay under 180s: Friends kit is opt-in (`--capture-friends` / `--capture-friends-only`).

## Outcome

1. **WPF «Друзья»** — primary path is intentional local file exchange: Export package / Import with confirmation. Public relay / room-code actions are not the main UX under `CMNET_LOCAL_ONLY=1`.
2. **API** — `POST /api/share/export` `{path}` writes portable `CMNETPACK1` (gzip+tar of bundle and/or snapshot + optional team/heroes/assets). `POST /api/share/import` `{path, confirm}` previews (`confirm=false`) or applies (`confirm=true`) after `import-backup/<stamp>/` copy of existing targets.
3. **StateKit** — empty / loading / partial / error / offline / ready / imported on the Friends banner.
4. **Тема** — Theme.xaml + Surface/Evidence; Scout/Draft/Data/Settings/History/GSI not redesigned.
5. **Relay stay fail-closed** — `/api/share/start|receive|follow|mode` still blocked when `CMNET_LOCAL_ONLY=1`.

## Captures (SAMPLE DATA labeled)

| Surface | Files |
|---|---|
| Happy | `friends.png`, `friends-ready.png` |
| States | `friends-empty.png`, `friends-partial.png`, `friends-loading.png`, `friends-error.png`, `friends-offline.png`, `friends-imported.png` |
| CI matrix (unchanged) | Draft×5×3 + Scout×5×3 + scout empty/loading/error → `geometry.json` Count==33 |

## Verify

```text
go build -o native/out/advisor.exe ./cmd/advisor
go test ./internal/share/ ./internal/web/ -count=1 -run "Pack|LocalFile|GuestState|HostWithout"
dotnet publish native/Cmnet.Native.csproj -c Release -r win-x64 --self-contained true -o native/out
native/out/Cmnet.Native.exe --smoke-ui
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/friends-2b-wip --capture-friends
# optional fast Friends re-shot (does not rewrite geometry.json):
native/out/Cmnet.Native.exe --capture-ui docs/_gate0/friends-2b-wip --capture-friends-only
```

Measured locally: capture wall clock ~37s; geometry Count==33.

## Honesty

- All Friends captures labeled `SAMPLE DATA · friends-<scenario>`.
- Preview does not read user packages and does not start Go.
- Import never overwrites silently: UI confirmation + server-side `import-backup/`.
- No Discord / magic-wormhole / ppng.io reintroduced on the native path.

## Product cut (coordinator-approved)

| Choice | Decision |
|---|---|
| Exchange mechanism | Local file import/export only |
| Relay | Remain disabled under `CMNET_LOCAL_ONLY` |
| Package contents | Existing share payload: `bundle.json` and/or `snapshot.json` (+ team/heroes/assets when present) |
| LAN live share | Out of scope for Gate 2.5 |

## Deferred / out of Gate 2.5

| Gap | Rationale |
|---|---|
| Live LAN collaboration | Separate product cut |
| Electron Friends rewrite to file-first | Electron keeps legacy relay during migration window |
| Signed installer | Gate 3 |
| Independent product-owner visual OK | Required before merge; this PR does not self-approve |

## Ready-for-acceptance

Implementer recommends acceptance after independent review of Friends journey + screenshots. Do **not** auto-merge.
