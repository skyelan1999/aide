Used roughjs 4.6.6 from https://unpkg.com/roughjs@4.6.6/bundled/rough.js
Local patch (#246): zero or non-finite dash periods produce no fill dashes; a scan line exceeding 100,000 dashes is rejected before iteration. Normal positive dash patterns are preserved. The patch is present in rough.min.js, app.min.js and integrate.min.js.
