# Gate 3.6 — release-review NOTES (blockers)

**Issue:** [#68](https://github.com/twipz333/cmnet/issues/68)  
**Matrix:** [`MATRIX.md`](./MATRIX.md)  
**Master tip reviewed:** `86544a9`  
**Released claim:** **forbidden** until every PRODUCT-BAR hard gate is **verified** and friend trials pass.

## Remaining blockers (ordered)

| # | Blocker | Status | Owner | Pointer |
|---|---|---|---|---|
| 1 | **Two unassisted friend install trials** | **BLOCKED** | Human / product owner | Issue [#62](https://github.com/twipz333/cmnet/issues/62); empty Friend 1/2 tables in [`../gate3-friend-trial-wip/NOTES.md`](../gate3-friend-trial-wip/NOTES.md). Script PASS ≠ friend PASS. |
| 2 | **Authenticode / SmartScreen** | **BLOCKED** (ops) | Ops / cert purchase | Desktop `cmnet-native-setup-0.4.0.exe` → `NotSigned`; Gate 3.3 NOTES: manifest RSA ≠ code-signing cert. Fail-closed updates **verified** without shipping a production private key. |
| 3 | **Release QA sign-off** | **blocked** | Independent reviewer | MATRIX hard gate «Release QA»; needs real UI E2E + friend session, not only CI/`--smoke-ui`. |
| 4 | **Visual inspection on real DPI monitors** | **in progress** | Human QA | Fixture captures exist; physical 100/125/150% + a11y/keyboard matrix not closed. Computer Use permissions still unsupported in prior sessions. |
| 5 | **Parity residuals vs Electron** | **in progress** | Product cut-list | Alerts/TTS, item rules, Discord (intentional local-only?), residual Draft craft vs `draft-approved.png`, live scout cancel under load. See Gate 0B + Gate 1 deferred lists. |
| 6 | **Performance re-baseline @ ship SHA** | **in progress** | Agent/human | [`../perf-baseline.md`](../perf-baseline.md) measured at `fba7271`; live-network scout / scroll FPS still gaps; no claim of «fast» at `86544a9`. |

## Explicit non-blockers for this docs PR

- Gate 3.1–3.4 merged evidence exists (`#55`, `#57`, `#59`, `#61`).
- Gate 3.5 **script** verified (`script-evidence.json` `pass: true` on builder).
- Scout seat Steam vanity URL fix merged (`#67` @ `86544a9`); Desktop drop README cites it.
- Local-only Friends path (`#52`) and migration path (`#53`) have NOTES + tests — still need friend-session confirmation for journey 5 / data integrity on a second machine.

## Do-not

- Do **not** merge this review as proof of Released.
- Do **not** invent Friend 1/2 PASS rows.
- Do **not** publish native setup to the Electron Release channel as a Released native product.
- Do **not** lower PRODUCT-BAR gates to mint a false v1.

## Next human actions

1. Run two friend trials from Desktop drop / USB using the checklist in `gate3-friend-trial-wip/NOTES.md`; file verdicts.
2. Decide Authenticode purchase / signing pipeline (or accept continued SmartScreen friction with explicit UI copy).
3. Independent reviewer walks MATRIX → either promote gates to **verified** with new evidence or keep **blocked** / **in progress**.
