# CMNET — productization delivery board

This is the prioritized **complete-product** program, not a list of optional MVP shortcuts. Every checkbox starts unchecked until the corresponding evidence is collected; do not infer completion from historical PR titles.

## Gate 0 — establish the truth (first Orca Run)

- [ ] Record exact `master` SHA, open issues, last release and WPF build status.
- [ ] Run the existing Windows CI snapshot capture. Retrieve and inspect the **33-case geometry matrix**, Draft/Scout screenshots and real Windows `--smoke-ui`; log reproducible UI defects. Existing capture is evidence of rendering, **not** of premium visual fidelity.
- [ ] Run `go test ./...`, `go vet ./...`, and `dotnet publish`; capture exact failures and their environments. Don't modify code during this inventory.
- [ ] Build a parity matrix: Electron feature -> native working behavior -> missing edge cases -> acceptance test. Do not rely on the native preview README as ground truth.
- [ ] Place the actual approved 3B reference screenshots in a reviewable location; obtain visual sign-off on one full Draft and one full Scout state **before** broad screen-level implementation.
- [ ] Establish measurable baseline: startup to interactive, cached/warm scout, cold scout, UI responsiveness and memory usage on Windows.

**Gate 0 deliverable:** a single evidence-based issue or PR with screenshots, parity matrix, known problems, design reference and test output. No unverified "all good" summary.

## Gate 1 — visual craft + real tactical experience

- [ ] One cohesive 3B system applied to navigation, Draft and Scout: approved layout, typography, artwork, hierarchy and interaction states.
- [ ] Draft: legible 5v5 portrait states, bans, search, recommendations with explainability, manual actions, GSI status, undo/redo, and state continuity.
- [ ] Scout: usable 5v5 roster, full player intelligence/detail, explicit freshness/private/partial states, fast cached results, bounded live requests, cancellation and stable UI.
- [ ] Whole-app states: empty, loading, offline, partial, success, error, stale and reconnecting; no fake intelligence.
- [ ] WPF screenshots and a witnessed interactive walkthrough for both screens. Must look like the reference **and** behave correctly.

## Gate 2 — complete experience & legacy parity

- [ ] Data: understandable corpus/model/status, progress logs, cancel/retry, no raw JSON admin wall.
- [ ] Settings: complete editable, validated configuration; alerts/Discord/rules only if still desired and safe in local-first design.
- [ ] GSI install/configuration UX, active-match resilience, correct reconciliation of background updates.
- [ ] After-match history and decision review, including confidence/provenance and honest limitations.
- [ ] Friends mode: define and test a secure local-only sharing path or agreed import/export without public relay.
- [ ] Existing user data: real migration/backup/restore test against Electron, no double-opening same database.
- [ ] Keyboard/accessibility, high-DPI, multi-monitor, suspend/resume, cancellation and degraded-network handling.

## Gate 3 — ship a trusted desktop product

- [ ] Stable app naming, icon, versioning, installer and polished first-run/welcome flow.
- [ ] Fully self-contained Windows package: no SDK/CLI prerequisite for friends.
- [ ] Update channel with manifest, signature verification, failure recovery and rollback; no duplicated/accidentally published releases.
- [ ] Crash feedback, structured local diagnostics without secrets, clean data-preserving uninstall.
- [ ] One scripted clean-VM install/upgrade/uninstall test and **two human friend tests** with no developer assistance.
- [ ] Release review: actual UI walkthrough, parity matrix and all `docs/PRODUCT-BAR.md` gates verified.

## Orca task sizing and order

Do not send this document as one giant implementation prompt. Create Orca Run tasks in dependency order:

1. **Evidence-only audit** (read-only): identify actual breakage and screenshot/design gaps. Stop at a bounded report.
2. **Reference-driven vertical slice**: polish Scout end-to-end, including real result data and in-app verification; lock shared components. Do not work on Draft in another worktree if it is changing the same shared WPF shell/theme.
3. **Draft vertical slice** after the shared UI contract is stable.
4. **Parity tickets** individually, with nonoverlapping ownership and automated acceptance checks.
5. **Installer/updates/friend trial** once real user journeys pass.

The work is not done after the first screen. Keep the rest of the product board open until all release gates pass.

## Issue/PR template per user outcome

- **User story:** as a user I can …
- **Before:** exact reproduction and screenshots / failure logs.
- **After:** observable action and output, reference comparison.
- **Acceptance:** happy path + empty/offline/error + repeated interaction + no state corruption.
- **Scope/owner/dependencies:** changed modules and conflicting tasks.
- **Proof:** tests, app run, screenshots, performance if relevant, independent review.
- **Risks:** schema, permissions, migration, installer, rollback.
- **Status:** not started / in progress / blocked / verified (with proof).
