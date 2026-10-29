# Local native Mermaid corrections

The upstream @drawio/mermaid 0.1.0 license/header is retained. This module is also embedded verbatim in `../extensions.min.js`; both copies must change together.

Issues #434/#436/#445/#449: canonical own-property calls and prototype-free parser, IR, and native layout indexes preserve identifiers such as `__proto__` and `hasOwnProperty`. #446: generated state note/group/region IDs reserve all user IDs before allocating names. #447: Kanban skips style directives instead of creating phantom columns. #448: radar parser and public layout reject more than 1,000 ticks before allocation. #450: explicit event-model source links remain on reset frames. #451: flowchart parser limits nested groups to 64; public layout checks IR depth (65 including a leaf) and 20,000 nodes iteratively before recursive measuring.

Regression cases: `scripts/test_mermaid_native_issues.cjs`; actual EditorUi conversion/import: `scripts/drawio-mermaid-native-browser-cases.cjs`. These limits do not raise the enclosing preview resource limits.
