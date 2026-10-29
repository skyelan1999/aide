# highlight.js vendor

- **highlight.js**: v11.9.0, BSD-3-Clause
  - Core: `highlight.min.js` (135796 bytes, Aide registration repairs appended)
  - SHA256: `2ed4596bc257337812ed816744dbd07e565687cea50fac8c71f6bb2ec91c6bee`
  - Languages loaded individually:
    - python (3529B), go (1206B), javascript (6542B), typescript (7635B)
    - json (496B), bash (3103B), yaml (1810B), sql (6544B), markdown (2119B)

Loaded via `<script>` tags in index.html (core first, then languages).

Local repairs (#268–281, #283, #285–291) are maintained in `internal/server/web/highlight-grammar-fixes.js` and appended to the core with `node scripts/sync_highlight_grammar_fixes.cjs`. The original v11.9.0 core SHA256 is `837a6fa5b0c736b52bbde2b2b6190f305da3fc9ed41681db5321507057b5c846`. Individual grammar assets remain upstream sources; their registrations pass through the repaired core, including languages reloaded later.

The layer indexes JSX closing tags once, balances nested interpolation/parameters, matches bracket and Swift hash delimiters, and repairs the reported token scopes. It preserves HTML escaping and raw editor content. Scope controls and source/core/standalone loading paths are verified by `node scripts/test_highlight_grammar_review.cjs`; browser coverage exercises the actual Markdown and text-editor modules. This is syntax coloring, not language execution or compiler validation.

Additional work bounds (#437–440): C/C++ declarations use the upstream #4413 whitespace/pointer disambiguation plus at most 16 type/pointer prefix groups; C# declarations retain up to 16 modifier/type prefix words. These are coloring heuristics; unusually long prefixes may begin their function scope later without changing source text. INI/TOML assignment keys begin at a line boundary and do not consume another line’s whitespace. Ruby heredoc lookahead is replaced with a per-input index of candidate first-word terminators, preserving the pinned grammar’s label boundary semantics; the index is cleared between calls. Individual language re-registration is still repaired.

`node scripts/highlight-work-budget-review.cjs` checks normal scopes and editor-size work in bounded child processes. `AIDE_COMPONENT_EVIDENCE_DIR=<directory> node scripts/browser-highlight-work-budgets.cjs` exercises actual Markdown/editor paths. Grammar scanning bounds do not guarantee a browser paint deadline: large token-heavy editor DOM can still take seconds.
