# Local PlantUML converter patches

This remains the existing native draw.io converter bundle, with its original licenses and theme metadata. Do not treat replacing the vendor file as an unrelated asset refresh: review and carry these local changes, or confirm an upstream replacement contains them.

- Direct own-property checks call the native `Object.prototype.hasOwnProperty` explicitly. Diagram/configuration keys can no longer replace the method being called. ID, adjacency, measurement and layout caches in the affected parser/renderer scopes use objects with no prototype. The persistent state-parser index and the timing-row/subpart dictionaries follow the same rule.
- Stylesheet name/stereotype matching requires an own entry; ordinary classes do not match `.constructor` through inheritance. Explicit matching stereotypes still work.
- Component edge pairs use serialized two-element tuples, so names containing the old `|` separator cannot make unrelated pairs share parallel-edge offsets.
- `!ifdef` / `!ifndef` accept `$` variable names and check the variable scope as well as existing macro definitions. Mono preserves a caller-specified font.
- Hacker defines a missing foreground using the existing dark-background predicate, while preserving an explicitly supplied foreground. Dark backgrounds use the theme's light text; transparent/light backgrounds use its dark text. Mars color literals include their required `#` prefix.
- NWDiag consumes complete skinparam blocks, with nested-brace and quoted-string handling, so expanded theme internals are not reported as dropped user statements. Unterminated directive blocks still produce a warning.

Regression fixtures are in `scripts/fixtures/plantuml-issue-cases.json`. `node scripts/test_plantuml_converter_issues.cjs` checks each report, ordinary-name controls, finite geometry, valid relationship endpoints, themes, native-object integrity and additional parser/subpart smoke cases. Browser acceptance uses the actual `EditorUi.parsePlantUmlDiagram` loader, imports the XML into real graphs and validates SVG at desktop/narrow sizes in both themes. It is component acceptance, not a full Aide workbench or external PlantUML service test.
