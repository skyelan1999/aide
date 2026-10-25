# Interface localization

Settings → Language offers two immediate-action buttons: 中文 and English. Preferences are stored in the browser's existing `aide.ui` settings, synchronized between tabs on the same origin. They do not change model response language.

`internal/server/web/locales/en.js` owns English product strings; `i18n.js` resolves explicit translation keys and numbered placeholders. `settings-init.js` selects the initial document language before rendering. Static elements opt in with `data-i18n` attributes. Dynamic views use `t(key, ...values)` and re-render on `aide:language`. Do not translate arbitrary DOM text, user content, file names, or provider output. Unknown translation keys fall back to their source text.

When adding UI, add matching catalog entries and keep placeholders intact. Test switching both ways with an unsaved task draft and a file editor. Check narrow layouts, accessible labels, and keyboard focus. Run `node scripts/test_i18n.cjs` and the shared verification route before merging.


## Coverage and regression checks

The main workbench, standalone file UI, knowledge map (including history/topics/evidence), mobile capture page and product-owned validation/error text use the same catalog. Static HTML uses explicit attributes on text nodes or labels; it is never translated through a DOM-wide replacement. Dynamic source IDs, filenames, tool names, model responses and protocol fields remain unchanged. Default provider or server messages without an exact owned key retain their original content.

Use whole-sentence numbered placeholders for dynamic output. Verify both numbered and named placeholder sets when maintaining the catalog. Named placeholders are expanded only by the component that owns that format; the base translator expands numbered placeholders. Do not translate confirmation tokens that the server expects literally.

Run `node scripts/test_i18n.cjs`, the coverage test and `python3 scripts/agent-route.py verify quick`. Then inspect a real English browser session, switch back to Chinese, and verify unsaved input/editor content remains intact. Test lock/login, settings, files, plugins, assistant, knowledge navigation and mobile pairing/error text. Avoid real microphone capture, provider calls or business-data changes merely to inspect labels.

A nonzero fallback audit is an actionable gap, not proof that all UI is translated. Historical review/task records are preserved rather than rewritten as new English acceptance evidence.
