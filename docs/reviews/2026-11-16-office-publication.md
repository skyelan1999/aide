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

Publication is in progress. Final remote commit, PR, Release, Pages deployment
and independent public-download checks will be recorded after observation.
