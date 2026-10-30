# CMNET — product bar / definition of finished

**Status:** target contract, not a claim that the current application meets it.
**Audience:** one captain and a small friend group. **Platform:** Windows desktop. **Target experience:** a product that feels worth paying ~$10 for, with zero "rocket science" to launch. **Visual direction:** user-approved **Minimal Tactical 3B** (the actual reference images must be attached to the design review; the label alone is not a visual specification).

## One product, one happy path

1. **Install & open.** Download a single reputable Windows installer, install, click a shortcut, and land in an interactive, understandable app. No terminal, Go, .NET SDK, npm, manual service, JSON editing, API token, or model-training requirement. Give useful offline/cached/demo experience before external data is available; label sample data clearly.
2. **Before match / Scout.** Identify 5 allies and up to 5 opponents, paste Steam links/IDs or select saved players, inspect each person's heroes/roles and the team's meaningful strengths/risks. Results can be partial, private, cached, or temporarily unavailable; all states are explicit. No global UI freeze, stale overwrite, avatar flicker, or timeouts presented as success.
3. **During match / Draft.** Hero portraits, picks/bans, sides, turn context, top recommendations and reasons remain readable. Live GSI is optional; manual mode works independently. Missing data is explained, not masked by plausible-looking fabricated estimates. Keyboard input, undo/redo and switching tabs preserve state.
4. **After match.** Review saved draft/recommendations and decision log without digging through files. If post-match statistics depend on absent source data, say so and provide what is genuinely available.
5. **Friends.** A friend can get the installer and use the product with their own local data without developer assistance. Any data exchange is intentional, secure, and aligned with the local-only boundary. Do not silently reintroduce a public relay.
6. **Maintain.** Settings are understandable, saved with confirmation; sync and train jobs are background/cancellable with progress. Updates cannot silently destroy local state. Recovery paths exist.

## Hard release gates (all required, none replaced by a green build)

| Gate | Evidence needed for sign-off |
|---|---|
| Product identity | Full-size, legible, consistent 3B styling across Scout, Draft, Data, Settings; no broken layouts, raw JSON, developer-only affordances, dead controls, or placeholder hero tiles |
| First run | Clean Windows installation and start from shortcut **without installing SDKs**; show real empty/demo state; setup completion without console |
| Functional parity | Electron-to-native behavior matrix completed for all user-facing features, including scout, draft, data, settings, GSI, decision history, import/export/sharing or explicitly approved local alternative |
| Data integrity | Existing Electron data loads in native with a tested backup, migration, restart, and rollback. No simultaneous clients modifying one DB |
| UI resilience | Async scout/sync/GSI updates do not steal focus, reset selection, move scroll or freeze navigation; errors, offline and private profiles handled honestly |
| Visual inspection | Real WPF captures and review at 1366×768 100%, 1920×1080 125%, 2560×1440 150% (plus accessibility/keyboard checks); compare with approved reference |
| Performance | Benchmarked startup, cold/warm scouting, cancellation, redraw/scroll responsiveness, resource use. Record measured baseline and regressions; no unmeasured "fast" claims |
| Packaging | Versioned self-contained Windows installer, safe update/rollback path, clean uninstall preserving user data by default; explicit signing and release authenticity verification |
| Release QA | End-to-end Windows smoke with actual UI; CLI tests, integration tests, reproducible artifacts; independently reviewed PRs; a real friend can finish a test session unassisted |

Release must explicitly distinguish **not started / in progress / verified / blocked** for each gate. Documentation, code coverage, and green CI are necessary but not sufficient. Do not lower gates to create a false "v1" label.

## UI quality principles

- 3B is a visual contract: dark tactical surfaces, restrained accent, carefully controlled contrast, hero/player imagery as first-class content, premium typography and hierarchy, meaningful whitespace, semantic Radiant/Dire colors. **Do not infer the exact 3B geometry without the approved screenshot.**
- Design each screen as an intentional composition, not rows of equally weighted boxes. The critical next action must be obvious within five seconds.
- Consistent components and tokens: navigation, status, empty state, loading, errors, hero tile, player card, recommendation card, inline detail, confirmation. Avoid emoji as controls.
- Real assets with licensing/source considered; asynchronously cache images and provide graceful fallbacks. Asset requests must not block layout.
- No "success" until the action actually completed. No fake loading bars, fake stats, or mock values presented as real.
- Accessible contrast, tab order, focus indicators and readable localization. Screen and text must not overflow at intended DPI and resolutions.

## Constraints that remain binding

- Go engine is the single owner of calculations; UI renders versioned state and never reimplements ranking.
- WPF is the intended Windows native client (.NET `net10.0-windows` according to the checked-in project). Electron remains production during measured migration.
- Local-first/on-device data and free public APIs only; no mandatory hosted backend or paid model/API dependency.
- Preserve deterministic recommendation ordering and transparent limitations of the data.
- If a feature cannot be implemented credibly (e.g. external data hidden), provide an honest failure/partial state, not AI fiction.

## Completion language

"Implemented" = code exists. "Verified" = demonstrated in the real app, tests and artifacts collected. "Released" = all gates and the friend test passed. Agents must not use these words interchangeably.
