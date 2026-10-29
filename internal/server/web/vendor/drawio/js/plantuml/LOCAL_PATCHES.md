# Local PlantUML converter patches

This remains the existing native draw.io converter bundle, with its original licenses and theme metadata. Do not treat replacing the vendor file as an unrelated asset refresh: review and carry these local changes, or confirm an upstream replacement contains them.

- Direct own-property checks call the native `Object.prototype.hasOwnProperty` explicitly. Diagram/configuration keys can no longer replace the method being called. ID, adjacency, measurement and layout caches in the affected parser/renderer scopes use objects with no prototype. The persistent state-parser index and the timing-row/subpart dictionaries follow the same rule.
- Stylesheet name/stereotype matching requires an own entry; ordinary classes do not match `.constructor` through inheritance. Explicit matching stereotypes still work.
- Component edge pairs use serialized two-element tuples, so names containing the old `|` separator cannot make unrelated pairs share parallel-edge offsets.
- `!ifdef` / `!ifndef` accept `$` variable names and check the variable scope as well as existing macro definitions. Mono preserves a caller-specified font.
- Hacker defines a missing foreground using the existing dark-background predicate, while preserving an explicitly supplied foreground. Dark backgrounds use the theme's light text; transparent/light backgrounds use its dark text. Mars color literals include their required `#` prefix.
- NWDiag consumes complete skinparam blocks, with nested-brace and quoted-string handling, so expanded theme internals are not reported as dropped user statements. Unterminated directive blocks still produce a warning.

Regression fixtures are in `scripts/fixtures/plantuml-issue-cases.json`. `node scripts/test_plantuml_converter_issues.cjs` checks each report, ordinary-name controls, finite geometry, valid relationship endpoints, themes, native-object integrity and additional parser/subpart smoke cases. Browser acceptance uses the actual `EditorUi.parsePlantUmlDiagram` loader, imports the XML into real graphs and validates SVG at desktop/narrow sizes in both themes. It is component acceptance, not a full Aide workbench or external PlantUML service test.

## Conversion work bounds (#474, #480, #486, #487, #489, #493)

The existing native converter now shares a budget across one conversion and clears it in `finally`, including failed conversions. State sizes and child layouts use per-conversion WeakMap caches; map-link incidence and first map-row positions are indexed once. Shallow normal output, including orthogonal states, duplicate map keys and shared targets, is checked byte for byte against pre-patch XML.

Package/activity traversal is limited to 128 simultaneous calls per traversal kind. Object note placement accepts at most 2,000 notes and `notes × (notes + node boxes) <= 4,000,000` collision candidates before the existing placement loop. Over-limit conversion fails through the existing native loader error callback; it does not silently drop remaining nodes. This is a conversion policy, not a universal browser paint deadline or a bound on every converter path.

`splitstr_regex` uses the private, offline **RE2JS 2.9.1** MIT payload. Its compiler is locally limited to 512 instructions and 32,768 rune entries; origin, SHA-512 archive integrity, payload/license hashes, exact local changes and build command are recorded in [provenance](re2js-provenance.json). The [MIT license](re2js-LICENSE.txt) is retained. The engine is embedded in this converter by `scripts/sync_plantuml_resource_helpers.cjs`, with readable policy in `internal/server/web/plantuml-resource-helpers.js`; it adds no external fetch or plugin.

Per regex call: at most 16,384 UTF-16 input units, 256 pattern units, 32 capture groups and 4,096 intermediate result parts. One conversion retains at most 64 compiled patterns, 16,384 result parts and 16,777,216 weighted matching-work units. Invalid native syntax retains the prior single-input fallback; valid expressions outside the supported RE2 subset fail explicitly. Native `RegExp` is only constructed for syntax validation and never executes the user pattern. Common delimiters, capture groups, empty matches, ECMAScript BMP whitespace and dot line-break exclusions are retained. RE2 matches Unicode code points; it does not reproduce every legacy non-Unicode JavaScript regex behavior. Backreferences/unsupported lookarounds fail, and an end anchor on input ending with a line break is rejected to avoid silently changing JavaScript's anchor result. Use a simpler regex, trimmed input or a literal separator in these cases.

Validation commands from the repository root:

```sh
node scripts/sync_plantuml_resource_helpers.cjs --check
node scripts/plantuml-resource-budget-review.cjs
node scripts/test_plantuml_converter_issues.cjs
AIDE_DRAWIO_PLANTUML_RESOURCE_REVIEW=1 AIDE_DRAWIO_PLANTUML_ISSUE_REVIEW=1 AIDE_COMPONENT_EVIDENCE_DIR=<directory> node scripts/browser-drawio-preview-bounds.cjs
```

Work tests use operation/allocation boundaries rather than allowing expensive native regex or unbounded recursion to run. The browser fixture calls actual `EditorUi.parsePlantUmlDiagram`, imports normal XML into real graphs, validates SVG/finite geometry and verifies a successful conversion after each rejected input. It remains component acceptance, not full Aide or real-device acceptance.
