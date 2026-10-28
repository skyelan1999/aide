# highlight.js vendor

- **highlight.js**: v11.9.0, BSD-3-Clause
  - Core: `highlight.min.js` (133231 bytes, Aide registration repairs appended)
  - SHA256: `d548b4aae2886fa7062b1912b076a5654cd443dbd8d25717c3528b9d9d923742`
  - Languages loaded individually:
    - python (3529B), go (1206B), javascript (6542B), typescript (7635B)
    - json (496B), bash (3103B), yaml (1810B), sql (6544B), markdown (2119B)

Loaded via `<script>` tags in index.html (core first, then languages).

Local repairs (#268–281, #283, #285–291) are maintained in `internal/server/web/highlight-grammar-fixes.js` and appended to the core with `node scripts/sync_highlight_grammar_fixes.cjs`. The original v11.9.0 core SHA256 is `837a6fa5b0c736b52bbde2b2b6190f305da3fc9ed41681db5321507057b5c846`. Individual grammar assets remain upstream sources; their registrations pass through the repaired core, including languages reloaded later.

The layer indexes JSX closing tags once, balances nested interpolation/parameters, matches bracket and Swift hash delimiters, and repairs the reported token scopes. It preserves HTML escaping and raw editor content. Scope controls and source/core/standalone loading paths are verified by `node scripts/test_highlight_grammar_review.cjs`; browser coverage exercises the actual Markdown and text-editor modules. This is syntax coloring, not language execution or compiler validation.
