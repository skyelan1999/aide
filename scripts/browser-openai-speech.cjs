'use strict';
// Compatibility entrypoint: the speech settings now use centralized model
// bindings and auto-save. The shared browser suite covers both voice providers,
// permission rollback, request ordering and preview-after-save.
require('./browser-settings-autosave.cjs');
