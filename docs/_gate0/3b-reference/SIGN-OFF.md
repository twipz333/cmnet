# Minimal Tactical 3B — visual reference sign-off

**Status:** APPROVED by product owner (user) on **2026-10-30**  
**Direction:** Minimal Tactical **3B** (not 3A Soft, not 3C Premium)  
**Orca Run:** `run_d7ae77d33f9b`  
**Gate:** Gate 0.5 unlock for Gate 1 craft

## Canonical files

| Role | Path |
|---|---|
| Scout approved (cropped 3B column + design tokens) | `scout-approved.png` |
| Draft approved (cropped 3B column + design tokens) | `draft-approved.png` |
| Full Scout board (3A \| **3B** \| 3C) | `board-scout-3A-3B-3C.png` |
| Full Draft board (3A \| **3B** \| 3C) | `board-draft-3A-3B-3C.png` |
| Earlier concept family (context only) | `board-concepts-warroom-esports-minimal.png` |

Source pastes provided by user (Temp) and mirrored Cursor assets; middle column cropped as 3B.

## Locked tokens (from 3B Scout board)

| Token | Hex |
|---|---|
| Background | `#0B1117` |
| Surface | `#121A23` |
| Surface 2 | `#172128` |
| Accent | `#3DD9C4` |
| Success | `#67D9B8` |
| Warning | `#F3B562` |
| Danger | `#F06A6A` |
| Text | `#F3F7FB` |

Draft board additionally shows warm/amber action accents for primary CTA emphasis — use sparingly for high-priority actions, keep teal as structural accent.

## Product IA implied by 3B (do not invent beyond this)

**Scout:** metric strip (found / data / hidden / errors) → opponent player cards with WR/games/status → threat analytics (signature heroes, dangerous combos, key risks, recommended ban) → selected-player insights + quick actions.

**Draft:** Captains Mode board with timer/turn → recommendations (score + tags) → hero gallery with search/role filters → selected hero with roles/WR/popularity/**why** + primary Select → enemy intelligence strip + scout signals.

## Comparison note (current WPF fixtures vs 3B)

**Compared fixtures:** `docs/_gate0/ci-screenshots/draft-1920x1080-layout100.png` and `scout-1920x1080-layout100.png` against `draft-approved.png` / `scout-approved.png` on **2026-10-30** (this Gate 0.5 task).

Full delta list (no redesign): [`DELTAS.md`](./DELTAS.md).

Headline gaps only: Draft lacks dual-team CM board + timer/turn, rec tags, role filters, selected-hero why/Select panel, enemy-intel strip; Scout lacks four-card metric strip, threat analytics + ban card, selected-player insights/quick actions, and 3B player-card status language. Gate 1 must close these against the approved PNGs with real WPF screenshots — fixtures alone are not acceptance.

## Authority

User attached the 3A/3B/3C boards and labeled them **3B** in the Continuous Product Improvement kickoff. This file records that as visual contract unlock. Further redesign away from 3B requires a new user decision gate.
