// Minimal Node globals for standalone CommonJS scripts.
// The repository intentionally does not add @types/node to the browser build.
declare var require: any;
declare var module: any;
declare var process: any;
declare var Buffer: any;
declare var __dirname: string;
declare var global: any;
declare var __AIDE_CODE_INPUT__: string;
declare var acorn: any;
declare var babelParser: any;
