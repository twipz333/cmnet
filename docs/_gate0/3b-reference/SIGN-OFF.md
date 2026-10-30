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

Current `--capture-ui` fixtures already use dark teal accents and sidebar nav, but diverge from 3B in: metric strip composition, threat/ban recommendation modules, scout signal cards, draft timer/first-class why panel, role filters, and denser IA. Gate 1 must close these deltas against `scout-approved.png` / `draft-approved.png` with real WPF screenshots — fixtures alone are not acceptance.

## Authority

User attached the 3A/3B/3C boards and labeled them **3B** in the Continuous Product Improvement kickoff. This file records that as visual contract unlock. Further redesign away from 3B requires a new user decision gate.
