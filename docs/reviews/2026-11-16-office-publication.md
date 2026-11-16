# Office 1.2.0 publication

The user explicitly authorized pushing and publishing to the marketplace after
the local implementation and acceptance recorded in
[Collabora integration verification](2026-11-16-office-collabora.md).

The publication scope is the tested Aide host integration on `main`, the Office
plugin v1.2.0 release and the matching immutable marketplace package. It does
not create an Aide application version/tag or activate the daily runtime.
The host and marketplace changes are isolated from pre-existing local work.

The market entry requires Aide 0.1.19.0 RC2 or newer **and** the
`office-collabora-v1` capability. Older same-version hosts reject this unknown
capability; they must update to a build containing the WOPI and native AI
executors. Installing only the ZIP does not upgrade the host.

The final release package is built from the plugin repository with its existing
packager. Its expected SHA-256 is
`66ad7f5726f5eea843d9954690764d0afffdb084ce638bf2fa9415aef5978595`.
Release assets and the marketplace must contain the identical ZIP bytes.

## Published and independently checked

- [Host PR #934](https://github.com/skyelan1999/aide/pull/934) merged as
  `d4c11b1836dae55e2bcf7e6adc135d65b865bdb4`; tested source commit
  `006d28479b0d71212ca23b54af1d23d066519b2f` is an ancestor of main.
- [Office v1.2.0](https://github.com/skyelan1999/aide-pkg-office/releases/tag/v1.2.0)
  is public, with tag pointing to `fadcb8bc14b23f89daa53aa0d5987be2849a50d6`.
  ZIP size is 10,586 bytes. Public Release, pinned market Git commit, live Pages,
  and headed-browser downloads all match the SHA-256 above.
- [Market PR #54](https://github.com/skyelan1999/aide-pkg-shop/pull/54) merged as
  `c94c14fc498c5a3824fc72772115a6cb80126a2d`.
  [Pages deployment](https://github.com/skyelan1999/aide-pkg-shop/actions/runs/43825336992)
  succeeded. The [public market](https://skyelan1999.github.io/aide-pkg-shop/)
  shows Office 1.2.0, Collabora dependencies, required host capability and digest.
  Only the Office listing/package and derived catalog/routing changed; historical
  packages and all other entries remain intact.
- Central requirement and acceptance record:
  [Aide issue #935](https://github.com/skyelan1999/aide/issues/935).

## Verification

Clean-source `python3 scripts/agent-route.py verify full` passed all 349 commands
at the tested commit. Source fingerprint:
`3405f902a91153ef4458ca91ec46911dff9d281224a09fdda402536ed88a4e1e`.
The Go server race inventory contained 1,792 registered roots: 1,698 passed and
94 explicitly skipped under their existing conditions. All 24 shards, the
remaining Go race packages, platform contracts and vet passed. Conditional
skips are not functional acceptance. GitHub quick/go/gate also passed.

The exact final release ZIP was installed through the real plugin upload API:
six tools registered, DOCX/XLSX/PPTX read/edit/readback succeeded, and stale hash
and read-only writes were rejected. An actual 1.1.1 to 1.2.0 upgrade rejected an
incorrect digest and preserved settings, disabled state, plugin user data and
source documents. The isolated candidate runtime supplied the previously
verified Office executor; current-source Go tests separately cover the new
market capability gate.

Headed Chromium checked live Collabora search, Office details, XLSX routing and
the download button. The downloaded ZIP matched the released digest. The
desktop dialog and primary action were visible without horizontal overflow,
clipping or obscured labels. Owned browser sessions and test containers were
closed. Raw receipts and screenshots remain under
`.agent-state/office-publication/`; earlier Collabora edit/save/AI interaction
acceptance is recorded in the linked implementation review.

## Runtime and rollback

The daily `localhost:9999` runtime was not rebuilt, restarted or upgraded.
Installing this ZIP alone does not add the host interfaces. Enablement requires
a host build containing PR #934, the plugin update and a reachable, separately
deployed Collabora service. No Aide application tag/Release, paid subscription,
Windows/macOS runtime acceptance or enterprise capacity/SLA validation is
claimed. The earlier candidate image/package identities in the task record are
historical local acceptance artifacts, distinct from this published ZIP.

Rollback remains a normal plugin downgrade or disabling Collabora to use the
existing fallback. Existing packages are retained. No daily-runtime rollback is
needed because it was unchanged.
