# Map geometry 7.39-macro-v1

Hand-authored **macro** polygons for the 7.38+ river-Roshan map (7.39 pit adjustments).

This is not a pixel-perfect navmesh. The goal is that most live GSI hero
positions classify into a useful region (base, lane segment, river, jungle,
triangle). Leftover coordinates are `OTHER`, never snapped to a nearest region.

## Provenance

- Building anchors (ancients, T1–T3) taken from player-mode GSI capture
  `data/gsi-probe/2026-08-18T11-59-35Z` (Dire seat, 2026-08-18).
- Bottom Roshan pit landmark is the capture's long-stay position `(2859, -2764)`.
- Top Roshan pit is the approximate river mirror (`ROSHAN_PIT_A`); that pit was
  not occupied in the capture.
- Tormentor landmarks are approximate map-corner positions from the 7.38 notes
  (NW Dire / SE Radiant). They were **not** verified in the capture.

## Coordinate system

Same as Dota GSI `xpos`/`ypos`: +X east, +Y north, Radiant SW, Dire NE.
`(0,0)` is a real point near mid and is classified, not treated as invalid.

Lane polylines live in `7.39-lanes-v1.json` (see `LANES.md`). They must pair with this map version.
