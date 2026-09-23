# Interface localization

Settings → Language offers two immediate-action buttons: 中文 and English. Preferences are stored in the browser's existing `aide.ui` settings, synchronized between tabs on the same origin. They do not change model response language.

`internal/server/web/locales/en.js` owns English product strings; `i18n.js` resolves explicit translation keys and numbered placeholders. `settings-init.js` selects the initial document language before rendering. Static elements opt in with `data-i18n` attributes. Dynamic views use `t(key, ...values)` and re-render on `aide:language`. Do not translate arbitrary DOM text, user content, file names, or provider output. Unknown translation keys fall back to their source text.

When adding UI, add matching catalog entries and keep placeholders intact. Test switching both ways with an unsaved task draft and a file editor. Check narrow layouts, accessible labels, and keyboard focus. Run `node scripts/test_i18n.cjs` and the shared verification route before merging.
