# Source parsers

`javascript.cjs` and `python.py` parse source strings only; they do not load or execute workspace modules.

`acorn.cjs` is the unmodified `dist/acorn.js` from Acorn 8.15.0, MIT licensed. License is preserved in `ACORN-LICENSE`.

- Registry archive: https://registry.npmjs.org/acorn/-/acorn-8.15.0.tgz
- Upstream: https://github.com/acornjs/acorn
- acorn.cjs SHA-256: fdb08546776ec6228b03e8d02b40d4ab3255bae5f401adba7ff5dad927ac5c9c
- ACORN-LICENSE SHA-256: 76a876cf886ff9be2a8b5e2e86514fed06223c8c9f0c1e9ee9606e93841e00b7

Embedded by knowledge_code.go. Node.js and Python are optional host runtime dependencies; missing parsers produce diagnostics. TypeScript/TSX and JSX declarations use the pinned Babel ESTree parser below. Runtime call resolution is not supported.

## TypeScript / TSX / JSX

`babel-parser.cjs` is unmodified `lib/index.js` from `@babel/parser` 7.28.5, MIT licensed. It is embedded from this repository and never loaded from a workspace. Plain JavaScript keeps Acorn; TypeScript and JSX use Babel's `typescript` / `jsx` and `estree` syntax plugins.

- Registry archive: https://registry.npmjs.org/@babel/parser/-/parser-7.28.5.tgz
- Archive SHA-512 (base64): `KKBU1VGYR7ORr3At5HAtUQ+TV3SzRCXmA/8OdDZiLDBIZxVyzXuztPjfLd3BV1PRAQGCMWWSHYhL0F8d5uHBDQ==`
- Parser SHA-256: `4fad5d076afb8c346159a06bb942d6e14c6167d590082b7c3bdebd4c108af201`
- `BABEL-PARSER-LICENSE` SHA-256: `2e97627cb278aa7556fb9e8817368302301a595b6c7582512b8d74c57b773652`

Locations and excerpts refer to original source; no transpilation or project execution occurs. Type-only imports and ambient declarations do not become runtime bindings. Namespaces and syntax failures produce diagnostics; JSX rendering is not inferred as a call. Relative imports consider TS/TSX/JS/JSX source and index files, with ambiguous matches left unresolved. Package aliases, compiler type checking and dynamic dispatch remain unsupported. Existing file, total text, symbol, process-memory and timeout budgets still apply.
