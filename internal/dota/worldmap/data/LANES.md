# Lane geometry 7.39-lanes-v1

Paired with map geometry `7.39-macro-v1`. Polylines are **Radiant rax/T3 → Dire rax/T3**.

## Provenance

Vertices are GSI minimap coordinates of rax and towers from capture
`data/gsi-probe/2026-08-18T11-59-35Z` (Dire seat, 2026-08-18). They are not a
navmesh. Creeps walk beside towers, so assignment uses a radius, not exact
centerline coincidence.

## Thresholds (tuned on that capture)

- `assignment_radius` 1400: ~99.8% of standard lane-creep observations assign;
  leftover is `UNKNOWN` (too far or ambiguous). No snap-to-nearest.
- `ambiguity_epsilon` 250: near the Radiant fountain, TOP vs MID can be equally
  close; those ticks stay `UNKNOWN` rather than polluting a lane.
- `wave_cluster_gap` 1200: intra-wave consecutive gaps sit around 70–400 units
  (p80); inter-wave gaps are typically >3000. 1200 keeps melee/ranged/siege
  together and does not merge successive waves.
- `wave_contact_gap` 900: observational only — opposing group centers along the
  path, not a combat prediction.

## Direction

`absolute_progress` 0 = Radiant end, 1 = Dire end, regardless of the local player.
