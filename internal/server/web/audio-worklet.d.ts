// Minimal AudioWorkletGlobalScope surface. These two worklets compile separately
// from DOM scripts because AudioWorkletProcessor is not part of the DOM library.
declare const sampleRate: number;
declare function registerProcessor(name: string, processor: any): void;
declare class AudioWorkletProcessor {
  readonly port: any;
  constructor(...args: any[]);
}
