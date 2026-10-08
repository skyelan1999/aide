# Source parsers

`javascript.cjs` and `python.py` parse source strings only; they do not load or execute workspace modules.

`acorn.cjs` is the unmodified `dist/acorn.js` from Acorn 8.15.0, MIT licensed. License is preserved in `ACORN-LICENSE`.

- Registry archive: https://registry.npmjs.org/acorn/-/acorn-8.15.0.tgz
- Upstream: https://github.com/acornjs/acorn
- acorn.cjs SHA-256: fdb08546776ec6228b03e8d02b40d4ab3255bae5f401adba7ff5dad927ac5c9c
- ACORN-LICENSE SHA-256: 76a876cf886ff9be2a8b5e2e86514fed06223c8c9f0c1e9ee9606e93841e00b7

Embedded by knowledge_code.go. Node.js and Python are optional host runtime dependencies; missing parsers produce diagnostics. TypeScript/JSX and runtime call resolution are not supported by this extractor.
