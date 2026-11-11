# Reviewed workflow upgrades

Consolidates the useful dependency-only changes from #826–#830 onto current main.
All external Actions remain pinned to 40-character commits with version comments.
The inputs used by Aide remain supported. Upload-artifact retains its default ZIP
archive behavior; no `archive: false` input is introduced. Pages upload/deploy are
updated together. Existing permissions, artifact names and release gates remain.

| Action | Reviewed version | Commit |
| --- | --- | --- |
| actions/setup-go | v7.0.0 | b7ad1dad31e06c5925ef5d2fc7ad053ef454303e |
| actions/setup-python | v7.1.0 | 6d4fd22b20465ccc0f7354476588e8df7c9ca0e0 |
| actions/upload-artifact | v7.0.2 | cf430e030ddbb5b0abf93d22962f4752f3646cd9 |
| actions/upload-pages-artifact | v5.0.0 | fc324d3547104276b827a68afc52ff2a11cc49c9 |
| actions/deploy-pages | v5.0.1 | 368f82528645a54fb793d4d04e342629a3f51346 |

Primary sources reviewed on 2026-11-12:
[setup-go release](https://github.com/actions/setup-go/releases/tag/v7.0.0),
[setup-python release](https://github.com/actions/setup-python/releases/tag/v7.1.0),
[upload-artifact README at the pinned commit](https://github.com/actions/upload-artifact/blob/cf430e030ddbb5b0abf93d22962f4752f3646cd9/README.md),
[Pages uploader action](https://github.com/actions/upload-pages-artifact/blob/fc324d3547104276b827a68afc52ff2a11cc49c9/action.yml),
[Pages deploy README](https://github.com/actions/deploy-pages/blob/368f82528645a54fb793d4d04e342629a3f51346/README.md).

Local `python3 scripts/test_workflow_action_pins.py` — 2/2 PASS; diff check PASS.
Remote CI is required before merge. Manual full binary Delivery remains NOT_RUN.
