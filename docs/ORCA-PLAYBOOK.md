# Running CMNET productization in Orca

This runbook targets **stablyai/orca** (desktop Orca, https://www.onorca.dev/docs/) and its experimental Orchestration layer. It is not the unrelated `orca-cli/orca` binary or `araa47/orca`. Verify the installed product first.

## First local setup (on Windows, where WPF can actually be tested)

1. Open the **CMNET repo in Orca**. Set `master` as the baseline; do not run two agents in one working directory.
2. In Orca Settings → Experimental enable **CLI** and **Orchestration** (if your version has them). From an Orca terminal verify:

   ```powershell
   orca status --json
   orca skills get orchestration --full
   orca computer permissions --json
   orca computer capabilities --json
   ```

   The local version-matched skill output takes precedence over the example command forms below.
3. Install agent skills into detected agents if absent:

   ```powershell
   orca skills install --skill orca-cli --skill orchestration --skill computer-use
   ```

   Verify with `orca skills list`. Computer Use depends on OS permissions/capabilities.
4. Use Claude as a coordinator while Codex is out of quota; assign Cursor/Claude to bounded, non-overlapping work. If an independent model is unavailable for review, do **not** fake cross-model approval: maintain the review gate and use tests and explicit human acceptance.
5. Only **one implementation task at a time** until the screenshots and design system are approved. After that, allow a second worktree only for code that does not change the same WPF shell/theme or DB schema.

## Coordinator instructions (paste into the Orca coordinator chat)

> You are CMNET's product delivery lead, not a prompt repeater. Read `AGENTS.md`, `docs/PRODUCT-BAR.md`, `docs/DELIVERY-BOARD.md` and the actual code. Own the path to a complete, $10-quality native Windows product for its small friend group. Use the current 3B design reference; do not invent its exact layout. Start with Gate 0 read-only audit and collect real WPF screenshots / test evidence. Create bounded tasks with dependencies and stop conditions, supervise at most two non-overlapping workers, ensure model budget is not wasted, inspect each diff and actual app behavior, then request independent review. A worker report is not acceptance. Never auto-merge or silently delete branches. Preserve Go core, local data, free-API constraints and Electron migration. Report each outcome as VERIFIED / BLOCKED with evidence and a precise next task; no inflated "finished" claims.

## Durable Run (example command sequence)

Orca's documented supervised workflow (check the installed skill for changed syntax):

```powershell
orca orchestration run-create --objective "CMNET complete native productization with evidence gates" --json
orca orchestration task-create --task-title "Gate 0 - baseline and native UI evidence" --spec "Read-only audit of master. Build/test, collect native screenshots and geometry matrix, compare Draft/Scout to approved 3B reference, enumerate parity and broken journeys. No code edits. Return a reproducible report." --json
# Copy the actual task id from the previous command:
orca orchestration worker-start --task <task-id> --worktree new-child --name cmnet-baseline-audit --agent claude --setup run --json
orca orchestration check --wait --types worker_done,escalation,question --timeout-ms 900000 --json
```

A worker must send a `worker_done` receipt with task and dispatch IDs, test evidence, and any blockers. The coordinator inspects the work, acknowledges the delivery, and releases or retries. The worker is never allowed to mark its own product acceptance.

**Do not** use the retired `orca orchestration run` command; the current documentation says to use `run-create`, `task-create`, and `worker-start`. Do not use guessed model IDs or ask Orca to perform work when quotas are exhausted.

## Visual acceptance: WPF is not an HTML page

Orca **Design Mode** captures DOM/CSS of browser pages; it cannot validate a true WPF screen. Use Orca **Computer Use** on an actual Windows desktop with the native app foregrounded. First discover the actual application identifier, then take a state/screenshot and interact:

```powershell
orca computer list-apps --json
orca computer get-app-state --app "Cmnet.Native" --json
```

Use the returned app selector/element indexes; do not fabricate accessibility IDs. The command writes screenshot locations in JSON. WPF desktop screenshots must be compared to **approved 3B references** at target DPI/resolution, with functional walkthrough evidence and failure states. A CI-generated image file alone does not imply a successful visual review.

The checked-in `.github/workflows/native.yml` already publishes `cmnet-wpf-3b-screenshots` with a 33-entry geometry matrix; retrieve it from the Windows workflow and audit the images rather than building another screenshot pipeline from scratch.

## Safe development loop

1. **Spec**: coordinator writes one task with happy, offline, error and recovery criteria and expected files. Attach reference screenshot for UI.
2. **Implement**: one worker, isolated Orca worktree, no speculative refactor of unrelated code.
3. **Self-test**: worker builds and runs tests; captures the real WPF UI and user-journey recording/screenshot when UI is involved.
4. **Independent review**: another agent/model reviews against acceptance and checks false "done" claims, data integrity and actual evidence.
5. **Human acceptance**: user reviews the **running app or screenshots**, not a text promise. Failed acceptance becomes a targeted correction with unchanged spec.
6. **Merge**: only after checks and visual acceptance; one integrated branch, fresh smoke, no unwanted leftover worktrees/PRs.

**Escalation:** after two failed revisions, stop. Return diagnosis, current state, what failed, and a smaller next decision. Do not burn the rest of a subscription with unbounded attempts or spawn five fresh agents.

## Hand-off report format

```text
TASK: <id> / <short user-visible result>
STATUS: VERIFIED | BLOCKED | READY FOR ACCEPTANCE
BASE: <commit>
CHANGED: <files/PR>
PROOF: <exact test commands + results; screenshot/artifact links; app scenario>
DESIGN: <reference compared; observed differences>
DATA/ROLLBACK: <what was preserved; how to recover>
GAPS: <remaining issues, never omitted>
NEXT: <one bounded task>
```

These statuses describe **verification**, not the act of typing code. A coordinator should reject any report without concrete reproducible proof.
