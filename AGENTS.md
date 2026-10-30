# CMNET — binding instructions for all coding agents

Read this file first, then [docs/PRODUCT-BAR.md](docs/PRODUCT-BAR.md), [docs/DELIVERY-BOARD.md](docs/DELIVERY-BOARD.md), [docs/ORCA-PLAYBOOK.md](docs/ORCA-PLAYBOOK.md), and [docs/ORCA-CONTINUOUS-LOOP.md](docs/ORCA-CONTINUOUS-LOOP.md).

## Product over implementation
CMNET is a polished, local-first Dota 2 Windows desktop product for a captain and friends **before, during, and after a match**, not a prototype or admin dashboard. The release target is a self-contained **native WPF / .NET 10 frontend + existing Go engine**. The Electron build remains the supported migration source until parity is proven. Do not start a third frontend or rewrite the recommendation engine without evidence.

**Source-of-truth priority on conflict**:
1. The user's explicit, latest decision and approved UI reference (Minimal Tactical **3B**).
2. `docs/PRODUCT-BAR.md` (current product and acceptance contract).
3. `docs/DELIVERY-BOARD.md` (release gates and order).
4. `docs/LOCAL-FIRST-V1.md`, `native/DESIGN.md`, and the versioned API contract.
5. Historic `CLAUDE.md`, `README.md`, and other documents are background context, not permission to revert to TUI, web UI, or an unfinished WPF preview.

## Mandatory working agreement
- First identify an observable user journey and its acceptance checks. Never equate "build passed" or "files modified" with done.
- One task owns one user-visible outcome, with concrete files, reproducible setup, screenshots when UI changes, tests, and a verification note.
- Do not silently switch tech stacks, redesign the approved 3B direction, invent cloud dependencies, require paid APIs, or remove compatibility with existing user data.
- Preserve the SQLite cache and data directory. Changes to migrations, sharing, updates, or authorization require dedicated tests and rollback instructions. Avoid concurrent Electron/WPF access to the same database.
- Keep requests to public free APIs bounded, cancellable and transparent. Explicitly differentiate **cached / live / unavailable / partial** evidence; no invented scouting insight.
- For UI work, verify the **actual Windows WPF executable**. Orca's browser Design Mode examines DOM/CSS and is not a visual acceptance tool for WPF. Use screenshots, Windows app automation/accessibility, and hands-on acceptance.
- On every PR include: user problem, before/after evidence, tested commands + output, screenshots (Draft and Scout if affected), rollback/data risk, remaining gaps. If any mandatory evidence is missing, mark BLOCKED; never claim complete.
- Task is DONE only after independent review of functionality, UI where applicable, and actual installation/user journey. Agents may implement and recommend approval, but must not self-approve or auto-merge.
- Limit parallel agents to **two implementation worktrees** by default, with separate coordinator/reviewer sessions where resources allow. No agent should spend the entire subscription budget on unbounded loops. Stop and escalate after two failed correction attempts rather than improvising architecture.
- Never delete branches, PRs, releases, or user data as cleanup without explicit permission; do not merge directly to `master` without acceptance.

## Deliver a product, not documents
These rules are a delivery framework. After the baseline/design gate, spend effort on working, demonstrable, production-quality flows and a friendly installer, not endless planning. The target is the full release bar, not an MVP declared finished.
