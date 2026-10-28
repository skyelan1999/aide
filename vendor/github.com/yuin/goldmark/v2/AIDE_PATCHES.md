# Aide local Goldmark patches

The module remains pinned to upstream v2.1.6. These local patches must be retained or re-evaluated when refreshing vendor:

- #250: backport the 256-byte delimiter fast-path budget and return to the general delimiter parser on a rejected closing candidate, following upstream v2.1.8. Longer text still uses the normal parser.
- #251: compact paragraph source segments once after extracting link references, retaining definition order and source positions.

Focused semantic and scaling coverage: `go test ./scripts/goldmark-review -v -bench . -benchtime=1x`. Aide knowledge/reference integration tests also use this vendored parser.
