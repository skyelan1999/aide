# Language and local-path integration verification

> 历史快照，当前以 version.md 为准。

Date: 2026-09-23. Baseline: 7ff083a. Branch: feat/english-support.

## Checks executed

- `python3 scripts/agent-route.py verify full`: PASS. Fingerprint `3f7859914ddc0f1d29c9d884928b06fc54a9facfcc9d371f29632ef7f3d605db`. Includes JavaScript syntax, version consistency, routing and installer regression, Markdown links, Docker-isolated Go race tests and vet.
- `node scripts/test_i18n.cjs`: PASS; locale resolution, source fallback, schema identifiers, user placeholder values, all catalog placeholder sets and six path mapping cases.
- `python3 scripts/test_local_root.py`: 2/2 PASS; paths with spaces and dollar signs, preservation of unrelated configuration, invalid-path refusal.
- Existing independent frontend VM acceptance: FRONT-001/002/003, 3/3 PASS; response ordering, draft/attachment ownership and original-workspace file-save binding. This is an adapter test, not a browser substitute.

## Actual browser observations

Isolated localhost:18121, current frontend mounted over the released RC4 backend; local mock provider only. No paid model calls.

- Chinese/English setting buttons switch UI text; selected preference survives reload.
- Draft `Smoke test: preserve 中文 draft` survived language switching, submitted successfully, and completed with a rendered Markdown mock response. Chinese user/model content remained unchanged in English UI. No browser console errors in the final smoke check.
- English New conversation label fits the narrow sidebar. Logo tilt/scale end state visually inspected with keyboard focus using the same transform as hover; actual pointer-hover timing was not measured. Reduced-motion rule disables transform.
- Directory browser lists the real aide repository children under the configured host root and fills its absolute host path into the input.

## Delivery limits

No backend source changes. Browser checks use the RC4 backend plus current source frontend, rather than a newly released image. Existing production service was not restarted. The local ignored `.env` sets aide as `AIDE_LOCAL_ROOT`; it takes effect for a Compose deployment on container recreation. The isolated preview was recreated with that mount.

English README, installation, user and localization guides are provided. Advanced architecture, plugin, operations and workflow references link to their existing Chinese originals. Historical evidence is not retroactively translated. This integration does not create a tag, publish a Release or export a new image.
