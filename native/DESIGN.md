# cmnet native — interface direction

## Objective

A production-quality **native Windows match-intelligence cockpit**, not a browser-like admin dashboard or an imitation of the Dota 2 client. Backend remains Go; no WebView, Electron renderer, or iframe. Designers/engineers should use the same vocabulary and layout tokens on Scout, Draft, Data, and Settings.

## Source guidelines (current)

- [Microsoft Fluent 2 — layout](https://fluent2.microsoft.design/layout): 4 px spacing scale, meaningful visual hierarchy, adaptive grid and content density.
- [Fluent 2 — typography](https://fluent2.microsoft.design/typography): Segoe UI Variable, readable 12/14/16/20/24 ramps, non-ALL-CAPS labels for longer text.
- [Microsoft Windows app design guidelines](https://learn.microsoft.com/en-us/windows/apps/design/guidelines-overview): navigation, material, motion, iconography, accessibility.
- [WPF UI, MIT](https://github.com/lepoco/wpfui): `FluentWindow`, `NavigationView`, native control templates, Snackbar, ContentDialog; integrate incrementally instead of hardcoding more WPF visual styles. Validate licensing/version/target framework before adoption.
- [Dota Draft Wars](https://www.dotadraftwars.com/): portrait-dense hero picker and visible Radiant/Dire draft state.
- [Dota 2 UI redesign (Behance)](https://www.behance.net/gallery/23451901/DOTA-2-Interface-Redesign): visual *reference*, not a template to copy.
- Valve hero artwork is downloaded from its current Dota React CDN using validated internal hero names, cached per hero ID under `assets/heroes`; never hotlink random external URLs or hold up the UI for a picture.

## Information architecture

### Draft: the hero artwork is the affordance

- Primary: draft state visible at all times, ideally Radiant and Dire picks/bans as portrait strips with distinct states.
- Hero picker: compact image **gallery** with search and clear selected state. Text-only multi-hundred-row ListBox is not an acceptable default.
- Next-action recommendation: top heroes with portrait, win probability, clear explanation/details action; avoid percentage presented without context or confidence.
- Matchups/context: accessible as a detail pane, not as a modal navigation detour.
- Search, apply, undo, new draft and recommendations must remain keyboard operable.
- Only one primary vertical scrollbar per panel; avoid nested page + list scrollbars.

### Scout: the intelligence, not just five editable boxes

- Side-by-side five-player rosters are for positioning, not a substitute for scouting results.
- Provide **a list of all scouted profiles**, even those not currently seated in either five-player roster.
- Selecting a player shows Steam name/avatar, status (available/private/error/stale), top hero portraits, games, win-rate and affinity.
- Refresh selected player, refresh team, queue state and source wait should be distinct.
- Progress must not rebuild the full roster or lose selected profile/scroll.
- Explicit empty, loading, timeout and partial-data states.

### Data/settings

- Data: true source metrics, offline/cached state, job progress, log link and failures, not raw JSON snippets.
- Settings: native form layout with validation and explicit save feedback, not a featureless dump.
- Shared design elements: one navigation, one accent/action scale, status indicators and confirmations.

## Design tokens / mechanics

- Spacing: `4, 8, 12, 16, 24, 32` DIP (Fluent 2 aligned).
- Text: Segoe UI Variable; 12 caption, 14 body, 16 emphasis, 20 section, 24–28 page heading. Use bold sparingly.
- Surfaces: one window background + one recessed panel + one elevated detail surface; no five layers of bordered cards.
- Colors: mostly neutral charcoal/slate; Radiant green and Dire red are **semantic team colors**; one restrained active-selection accent. Gold only when it marks a draft recommendation or attention. Color must not be the sole meaning.
- Icons: standardized Fluent symbols; no arbitrary Unicode glyph as the only meaning of an action.
- Motion: 120–180 ms hover/selection, respect reduced motion; never animate the whole UI on SSE updates.
- Layout targets: 1366×768 at 100%, 125%, and 150% scaling; minimum usable width 900 DIP. Collapse detail side-panel before causing horizontal scrolling.
- Scroll: lists are virtualized when large, thumbnails loaded asynchronously and cached. No scroll-jumping on SSE/progress.
- Accessibility: tab navigation, enter/escape, screen reader descriptions, focus styling, contrast 4.5:1 minimum for normal text.

## Implementation milestones (issue #17)

1. **This iteration**: functioning hero portrait gallery and real scouting detail, not just roster controls. Local/cache-first hero art.
2. Migrate the general shell to WPF UI / Fluent primitives (FluentWindow, NavigationView, dialogs, Snackbar); remove bespoke control templates when parity is verified.
3. Finish feature parity with Electron (all draft editing, scout jobs/history, GSI and external flows).
4. Native installer and auto-update channel with rollback; no accidental duplicate Releases.
5. Real Windows UI tests: no logical parent collisions, no nested scroll, no missing images after warm cache, no input loss during SSE, accessibility + DPI smoke.

**Issue #17 remains open until all five are accepted.**