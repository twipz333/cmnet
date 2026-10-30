# Gate 0B supporting notes

Companion to [`parity-and-gaps.md`](./parity-and-gaps.md). Evidence breadcrumbs only.

## SHA / release

- Worktree HEAD: `c75ac6f3ab89c3b3fc3b37ece7b066aa5d2254a7` (matches task TARGET).
- `gh release list`: latest `v0.3.3` (2026-10-29).

## Key file map

| Area | Electron / Go | Native WPF |
|---|---|---|
| Shell / nav | `internal/web/static/ui/app.js`, `index.html` | `native/ShellView.cs`, `NativeWindow.cs` |
| Draft | `ui/draft.js` | `TacticalViews.cs`, `MatchVisuals.cs` |
| Scout | `ui/scout.js`, `ui/team.js` | `ScoutingView.cs`, `MatchVisuals.cs` |
| Data | `ui/data.js` | `NativeWindow.BuildDataView` |
| Settings | `ui/settings.js` | `NativeWindow.BuildSettingsView` |
| Friends | `ui/share.js` | `NativeWindow.BuildShareView` + `CMNET_LOCAL_ONLY` |
| Desktop install/update | `desktop/package.json`, `main.js`, `.github/workflows/desktop.yml` | Preview zip: `.github/workflows/native.yml` |
| Contracts | `docs/PRODUCT-BAR.md`, `DELIVERY-BOARD.md`, `LOCAL-FIRST-V1.md`, `native/DESIGN.md`, `native/README.md` |

## 3B hunt log (negative)

- Repo image search: empty.
- Comments claiming «Minimal Tactical 3B» in `TacticalViews.cs` / `ScoutingView.cs` are implementation labels, not approved assets.
- CI artifact `cmnet-wpf-3b-screenshots` = fixture captures (`sampleData: true` in `UiPreview.cs`).
- Sibling `cmnet-productization/docs/_gate0/ci-screenshots/` = same class.
- Prior AgentMemory decision: approved originals missing → BLOCKING.

## Intentional native constraints (not bugs)

- `AdvisorCore` sets `CMNET_LOCAL_ONLY=1` and `--discord=false`.
- Friends primary actions disabled with tooltips (fail-closed), per `LOCAL-FIRST-V1.md`.
- Native README states Electron remains production until parity + installer.

## Out of scope for this task

- Running `go test` / `dotnet publish` / downloading CI artifacts (owned by other Gate 0 tasks).
- Redesign or commits.
