# Issue 667: Safari prepaid balance acceptance

OpenAISupporter remains a data-only provider catalog. It declares the HTTPS page,
balance/account labels, visible project identity and five-minute interval. Aide
owns account confirmation, private snapshots and background scheduling. Native
reads are limited to the exact owner-opened source URL and nonce; no model key,
Cookie, screenshot or arbitrary JavaScript result is returned.

## Verified

- Production Swift parser: eleven synthetic cases passed; native typecheck and
  final app build/signature verification passed.
- Focused host Go race suite passed (20.793s), including binding isolation,
  persistence, identity changes, key rotation, inflight disable and scheduling.
- Account-pool Node suite: fourteen tests passed. Actual final plugin ZIP host
  install/enable/disable/pricing/accounting race acceptance passed (2.713s).
- Final quick checks passed. Startup verification reported buildRequired=false.
  Served UI assets, protected launcher/environment hashes and data mounts match.
- Real Aide/native Safari reading returned the visible website amount, currency,
  account and project identity; the owner confirmed the Key association. Actual
  account values and identifiers remain private.
- A complete natural five-minute interval passed with settings closed and no
  manual read. The final bounded reader completed the scheduled read in about
  five seconds without changing Safari's focused Aide window.
- A closed source hides the current amount and retains its last successful time.
  On the subsequent restoration check, source_closed was confirmed, the dedicated
  source was restored, and another natural interval successfully updated it.
- Real Safari UI shows the balance and a default-collapsed “官网同步说明” section.
  Long guidance/account/project text is inside it; controls remain accessible.
  Initial account-confirmation identity stays visible outside the collapsed help.
  Expansion is retained across account-pool refreshes.

## Runtime constraints

Use a normal dedicated Safari window with its reload button. If Safari opens a
new tab, separate it once with Window → Move Tab to New Window. Minimal popup
chrome omits the reload button. The source window must stay open and logged in.

The reader requests Safari background accessibility and bounds individual AX
messages at 0.5 seconds and the overall read at twenty seconds; the host transport
is bounded at twenty-five seconds. This addresses slow background page/AX loading
observed during the actual interval test. Accessibility is granted; Screen
Recording remains off. Temporary ad-hoc native builds can require reauthorizing
the same app after its code signature changes.

The final local image and OpenAISupporter0.2.0 candidate are installed on9999.
Original user pages/data and native/image rollback points are retained; no test
source is closed at handoff. Windows/Linux native webpage reading is unsupported;
normal pricing remains available. No paid inference was used. This feature has
not been merged, tagged, released or published to the marketplace.
