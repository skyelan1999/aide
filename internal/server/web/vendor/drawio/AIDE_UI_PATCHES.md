# Aide local UI corrections

Cycle 84 keeps vendored source and the four app/integrate/viewer bundles in sync. `mxgraph/mxClient.js` is unchanged. The synchronization tool requires complete identifier boundaries and the last PrintDialog.create override; base mxGraph methods must never be matched as Graph methods.

- #452: malformed URI-list UTF-8 keeps the original URI; drop processing completes and consumes the event.
- #453/#454/#466: stencil custom color names, shape-picker object fields, and page IDs retain prototype-named keys.
- #455/#456: invalid optional toolbar JSON uses defaults; viewer layer popups anchor below the top toolbar.
- #457: fix the base serializer typo to mxUtils. The complete app's Pages override already handled standalone model XML, so that reported app symptom was not reproduced. The base method and its bundled fallback definitions are tested separately.
- #459: mixed asynchronous layout sequences retain the original selected-cell snapshot for synchronous steps.
- #460: Explore rejects non-positive/non-integer page sizes before constructing a viewer.
- #461: element replacement keeps its attribute iterator local.
- #462: repeated click-handler installation cleans up the prior handler; graph destruction removes document, model and mouse listeners and destroys the highlight.
- #465: Fit-to-sheet counts must be positive safe integers before print/export state and geometry are computed.

#458 is not a defect in these non-strict scripts: ordinary formals and arguments are mapped. Actual browser viewport-clamp checks pass without changing this code. It is excluded from the fix count.

Validation: actual Chromium editor component, light/dark themes; base serializer regression and exact method/bundle audits. This is not a full Aide workbench or native-platform acceptance run.
