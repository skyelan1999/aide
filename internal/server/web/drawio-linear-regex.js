/* CORS rules come from the page URL. Never send them to a backtracking engine. */
function createDrawioLinearRegex(engine, buildId) {
  const maxPattern = 512, maxInput = 8192, maxProgram = 4096;
  return Object.freeze({
    version: 're2js-2.9.1',
    buildId: buildId || 'source',
    maxPattern, maxInput, maxProgram,
    compile(pattern) {
      if (typeof pattern !== 'string' || pattern.length > maxPattern) return null;
      try {
        const compiled = engine.compile(pattern);
        if (compiled.programSize() > maxProgram) return null;
        return Object.freeze({test(input) {
          return typeof input === 'string' && input.length <= maxInput && compiled.test(input);
        }});
      } catch (_) {
        // Unsupported RE2 syntax, including lookaround/backreferences, fails closed.
        return null;
      }
    }
  });
}
if (typeof module !== 'undefined' && module.exports) module.exports = createDrawioLinearRegex;
