// Minimal Node globals for the four standalone CommonJS bridge scripts.
// The repository intentionally does not add @types/node to the browser build.
declare var require: any;
declare var module: any;
declare var process: any;
declare var Buffer: any;
