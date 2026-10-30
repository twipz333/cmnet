# CMNET — Orca continuous improvement loop

**Purpose:** run a supervised, *repeatable* product development loop until the complete release gates in `docs/PRODUCT-BAR.md` are verified. This is not a one-shot audit, an autonomous software publisher, or a promise of 24/7 background execution.

Read `AGENTS.md`, `docs/DELIVERY-BOARD.md`, `docs/PRODUCT-BAR.md`, and `docs/ORCA-PLAYBOOK.md` first. Run the documented Gate 0 audit if it has not been completed with evidence.

## Actual Orca capability and limitations

Orca Orchestration provides a durable Run namespace, Tasks, supervised `worker-start` Dispatches, `check`/acknowledgement, `ask`/`reply`, `escalation`, and `gate-create`/`gate-resolve`. **The coordinating agent itself drives the loop**; `run-create` merely creates a namespace. Do not call the retired `orca orchestration run`/ `run-stop` as a background coordinator. A closed/stopped Orca/agent session does not guarantee continued coding. An optional external scheduler requires separate implementation and explicit authorization; this document does not create one.

Use the *installed version's* `orca skills get orchestration --full` and `--help` output for exact flags. Avoid outdated third-party fork documentation. In a WSL-managed session, use `ORCA_CLI_COMMAND` if exported; do not accidentally invoke the GNOME screen reader `orca`.

## Autonomy policy

**Proceed automatically when all are true:**
- Scope fits an already approved product spec, 3B reference and documented release gate.
- The worker can verify functionality and WPF UX locally with existing permissions.
- No new secrets, paid services, cloud dependency or unbounded usage is required.
- There is no user data loss, schema-risk, irreversible repo action, license ambiguity or security exception.
- Scope does not overlap a running worker or consume the only remaining verification budget.

**Ask the user — and block only the affected task — when:**
1. A **product/design choice** changes the approved 3B reference, navigation, behavior or feature priority and cannot be inferred from existing decisions.
2. The correct behavior is ambiguous and choosing wrong would create noticeable product debt.
3. A change might **lose or irreversibly migrate data**, alter security/privacy, enable external sharing, remove a compatibility path, or require destructive repository actions.
4. A release, installer replacement, `master` merge, destructive cleanup, paid API/credentials, or new recurring spend needs approval.
5. Reference screenshots, test access, local credentials, external API consent, or other indispensable evidence is missing.
6. A task has failed two bounded correction attempts, tests are inconclusive, or agent quotas/budget are exhausted.

**Do not interrupt the user for:** naming private variables, routine refactors with behavior preserved, low-risk reversible implementation detail, already-approved tokens/layout details, selecting tests, fixing lints, or another task's local debugging. A coordinator must resolve these independently using evidence.

**Question format** (one concise message, not 12 fragmented questions):
```text
NEEDS YOUR DECISION — <single concrete topic>
Why it matters: <one sentence, user-visible impact>
Recommended: A — <concrete default and rationale>
Alternative: B — <trade-off>
What will pause: <task / dependent tasks>
What continues: <independent work>
Answer with A / B / adjustment.
```
Never manufacture user approval after a timeout, assume the answer, or use silence as consent. Questions from workers use the injected `orca orchestration ask` (to the coordinator), **not** an unmanaged local question UI. The coordinator answers routine questions using `reply`. For human decisions the coordinator surfaces the question in its user-facing chat; a `gate-create` protects dependent tasks until the user answers and the coordinator records `gate-resolve`. A gate does not itself guarantee a native desktop notification.

## Product loop — repeat while coordinator session is active

0. **Reconcile:** inspect current `master`, open PRs/issues, current Run tasks/dispatches, confirmed release gates and actual test evidence. Do not duplicate already running work or assume a previous agent report was accepted.
1. **Find:** maintain a single prioritized GitHub Issues backlog. Gather defects from real UI captures, user journeys, error states, baseline measurements, tests and completed-feature parity. Prefer **observable user pain** over speculative features.
2. **Select:** choose the highest-impact unblocked *user outcome* using impact on polished product + correctness + frequency + blocking dependencies, with effort and risk considered. Avoid cosmetics-only loops while basic journeys fail, but preserve approved 3B as a hard gate.
3. **Specify:** create a bounded Task including target files, result, constraints, acceptance tests, UI reference (when applicable), negative states, and what the worker must not touch. One implementation worktree until the UI foundation is approved; at most two non-overlapping implementation worktrees afterward.
4. **Dispatch:** start a supervised worker, wait for `worker_done` / `escalation` / `question`, handle each delivery **before** acknowledging it. On timeout, inspect liveness; don't spawn duplicate work from absence alone. Use the current Orca skill's exact recovery procedure.
5. **Verify:** tests + running Windows WPF app + realistic user journey. Compare actual screenshots with approved 3B. Require separate reviewer when available and verify known failure states. The worker's successful `worker_done` only concludes an attempt, **not** product acceptance.
6. **Decide:** if rejected, send a concrete failing reproduction back for a bounded retry, no more than two; if blocked, record and ask one necessary question; if verified, commit/checkpoint and prepare a narrowly scoped PR with evidence. Never silently merge into `master` or claim a release.
7. **Integrate:** maintain a clean integration candidate on an isolated branch, resolve conflicts, rerun cross-feature smoke. **Only merge PRs or publish installers when the user explicitly approves.** Continue independent unblocked tasks while waiting for a decision. Do not pile unreviewed PRs onto stale bases.
8. **Persist:** record task status, commit SHA, reproduced issue, tests, screenshots/artifact references, decisions, constraints, unanswered questions and exact next task in GitHub Issue/PR. No ephemeral chat-only state.
9. **Repeat:** pick the next highest-value unblocked item automatically. Every few completed tasks produce a compact digest of tangible results; don't stop solely to ask the user whether to continue. Revisit backlog priorities when user feedback or failures justify it.

**Hard stops:** required user decision; quotas or inability to spawn/verify; unsupported environmental permission; data/security/release risk; repeated failure; no unblocked meaningful work; Orca runtime shutdown. Stop without claiming success and preserve a resume checkpoint.

**No false perpetual work:** don't keep polling indefinitely after a valid block or empty budget. Suspend, tell the user the exact blocker and leave the Run/Issues resumable. There is no cost-free unattended background executor in `run-create`.

## State, review and release rules

- **Issue/PR is the durable record**, `docs/DELIVERY-BOARD.md` is the product scope and release gates, Orca Run is the in-session coordinator mailbox.
- Keep a living issue list for: `ready`, `in-progress`, `blocked-needs-user`, `ready-for-acceptance`, `verified`. Use existing labels when present; only create missing labels through authorized project tools.
- Do not close a feature as done from CI alone. Verified means actual interaction and evidence; released means all release gates and user approval.
- On interruptions/restart, fetch current `master` SHA, Orca Run and GitHub issue/PR state first; identify unacknowledged deliveries, old worktrees and dirty branches without deleting them. Resume the **next unblocked** task rather than repeating the previous spec.
- Optimize resource use: 1 implementer + 1 reviewer, use another model only where it adds independent scrutiny. Unavailable Codex quota is not a reason to block all work. Don't launch multiple agents for the same screen/theme.

## One-time kickoff prompt to coordinator

```text
You are the persistent CMNET Product Delivery Lead in Orca, not a one-shot coding worker.

Read AGENTS.md, docs/PRODUCT-BAR.md, docs/DELIVERY-BOARD.md,
docs/ORCA-PLAYBOOK.md, and docs/ORCA-CONTINUOUS-LOOP.md.
Use the Orca orchestration skill from the installed CLI. Resume an existing
valid CMNET Run when available; otherwise create a Run. Establish current
facts first and complete Gate 0 if not evidenced.

Start and maintain the continuous product loop in
docs/ORCA-CONTINUOUS-LOOP.md: reconcile -> select -> specify -> dispatch ->
verify -> integrate candidate -> persist -> select next task.
Do not stop after the audit, first implemented feature, first worker_done,
or first PR. Continue with unblocked meaningful tasks until a genuine
stop condition. No fake task completion, unbounded retries or unlimited spend.

Ask me only when a genuine product decision or authorization is required.
Workers ask YOU via Orca ask/reply. You answer routine technical decisions.
For a user-level decision present one concise A/B question and place an
explicit Orca decision gate on dependent work; continue unrelated work.
Never equate my silence with approval.

Preserve the approved Minimal Tactical 3B direction, local-only/free-API
constraints, Go backend and existing data. Test actual Windows WPF,
not a browser mock. Use at most one overlapping WPF implementer at a time.
Do not auto-merge, delete repo state, publish installers, or relax release
quality gates. Leave reviewable PRs and screenshots for acceptance.

Persist progress in GitHub Issues/PRs and report a short digest periodically.
On interruption, report exactly how to resume. The goal is a finished
premium Windows product, not an endless stream of prototype changes.
```

## Resume prompt

```text
Resume CMNET using docs/ORCA-CONTINUOUS-LOOP.md. Reconcile GitHub and Orca
state; do not rerun finished tasks or invent missing worker outcomes.
Show current blocker if any, continue the highest-value unblocked task,
and follow the same quality, question and merge rules.
```
