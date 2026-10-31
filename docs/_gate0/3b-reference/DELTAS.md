# WPF fixtures vs approved Minimal Tactical 3B — deltas only

**Compared:** `2026-10-30`  
**References:** `draft-approved.png`, `scout-approved.png`  
**Fixtures (sampleData, not live):**  
- Draft: `docs/_gate0/ci-screenshots/draft-1920x1080-layout100.png`  
- Scout: `docs/_gate0/ci-screenshots/scout-1920x1080-layout100.png`  

No redesign in this note. Gate 1 closes these deltas against the approved PNGs with real WPF captures.

---

## Draft — fixture vs `draft-approved.png`

| # | Delta (fixture lacks / differs) |
|---|---|
| D1 | Full Captains Mode board: dual Radiant/Dire pick+ban rows, turn ownership, and live timer are missing; fixture shows a single-team slot strip (Radiant 3/5 + bans count) without Dire / whose-turn chrome. |
| D2 | Recommendation rows lack Comfort / Synergy (and similar) tags; fixture is portrait + name + decimal score only (`0.922`…), not integer score + tag chips. |
| D3 | Hero gallery has search field but no role/attribute filter strip (Str/Agi/Int/Uni icons) present in 3B. |
| D4 | No first-class selected-hero panel: roles, WR, popularity, **why**, and primary Select CTA (`Выбрать героя`); fixture only highlights a grid cell (e.g. Clockwerk). |
| D5 | No enemy-intelligence / scout-signals strip on the Draft surface. |
| D6 | IA density: 3B is three-column (recs \| gallery \| detail); fixture is two-block (recs \| dense named grid) under a simpler header. |
| D7 | Nav chrome differs (fixture groups MATCH/SYSTEM + Local Client footer); acceptable to converge toward 3B sidebar in Gate 1 without inventing new destinations. |

## Scout — fixture vs `scout-approved.png`

| # | Delta (fixture lacks / differs) |
|---|---|
| S1 | Metric strip: 3B uses four status cards (found / data received / hidden / errors); fixture uses inline counts (`5/5 игроков`, `3/5 готово`) without that four-card composition. |
| S2 | Threat analytics block missing as structured modules: signature heroes, dangerous combos (with strength bars), key risks list, and prominent recommended-ban card (e.g. Pudge / high priority). |
| S3 | Player cards: fixture uses letter avatars, fewer hero chips, `#n` + ГОТОВО, and «Подробнее»; 3B uses portraits, Ready/Partial status, WR + games emphasis, and stronger status border language. |
| S4 | No selected-player insights list + quick actions (open profile / analytics / notes) as in 3B component breakdown. |
| S5 | «Аналитика состава» is a flat four-bullet summary, not the 3B threat/ban layout. |
| S6 | Fixture sample shows fewer populated opponent cards than the five-card 3B row (partial roster in capture). |

## Shared / out of scope for this list

- Palette direction already roughly dark + teal; lock exact tokens from SIGN-OFF when crafting — do not invent alternate accents.
- Fixtures prove geometry/rendering only; they are not 3B acceptance.
- Data / Friends / Settings panels on multi-board reference sheets are context for product IA, not deltas to implement in this Gate 0.5 compare step.
