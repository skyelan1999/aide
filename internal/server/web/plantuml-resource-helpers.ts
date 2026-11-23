declare var AidePlantUmlRE2: any;
/* Private helpers embedded in the existing offline PlantUML converter. */
var aidePlantUmlWork: any = null;
function aideWithBudget(run: any): any {
  var previous = aidePlantUmlWork;
  if (!previous) aidePlantUmlWork = {regexWork:0, regexParts:0, regexCache:new Map<any, any>(), depths:Object.create(null), stateSizes:new WeakMap<any, any>(), stateLayouts:new WeakMap<any, any>()};
  try { return run(aidePlantUmlWork); }
  finally { aidePlantUmlWork = previous; }
}
function aidePlantUmlLimit(detail: any): void {
  var error: any = new Error('PlantUML conversion limit: '+detail+'; split or simplify the diagram.');
  error.code = 'AIDE_PLANTUML_LIMIT'; throw error;
}
function aideNested(kind: any, run: any): any {
  return aideWithBudget(function(work: any) {
    var depth = work.depths[kind] || 0;
    if (depth >= 128) aidePlantUmlLimit('diagram nesting');
    work.depths[kind] = depth+1;
    try { return run(); } finally { work.depths[kind] = depth; }
  });
}
function aideMemoState(kind: any, state: any, measure: any): any {
  return aideWithBudget(function(work: any) {
    var cache = kind === 'size' ? work.stateSizes : work.stateLayouts;
    if (cache.has(state)) return cache.get(state);
    return aideNested('state-'+kind, function() {
      var value = measure(); cache.set(state, value); return value;
    });
  });
}
function aideCheckNoteWork(notes: any, boxes: any): void {
  if (notes > 2000 || notes*(notes+boxes) > 4000000) aidePlantUmlLimit('object note placement');
}
function aideRegexCharge(work: any, amount: any): void {
  if (amount > 16777216-work.regexWork) aidePlantUmlLimit('regular expression work');
  work.regexWork += amount;
}
// Keep ECMAScript's BMP whitespace and identity escapes for common existing
// delimiters. RE2 matching otherwise follows its documented Unicode syntax.
var aideRegexWhitespace = [[9,13],[32,32],[160,160],[5760,5760],[8192,8202],[8232,8233],[8239,8239],[8287,8287],[12288,12288],[65279,65279]];
function aideRegexRanges(ranges: any): string {
  return ranges.map(function(range: any) {
    var first = '\\x{'+range[0].toString(16)+'}';
    return range[0] === range[1] ? first : first+'-\\x{'+range[1].toString(16)+'}';
  }).join('');
}
var aideRegexSpace = aideRegexRanges(aideRegexWhitespace);
var aideRegexNonSpace = (function() {
  var ranges: any[] = [], start = 0;
  for (var range of aideRegexWhitespace) { if (start < range[0]) ranges.push([start,range[0]-1]); start = range[1]+1; }
  ranges.push([start,1114111]); return aideRegexRanges(ranges);
})();
function aideNormalizeRegex(pattern: any): string {
  var result = '', inClass = false;
  for (var i=0;i<pattern.length;i++) {
    var ch = pattern[i];
    if (ch === '\\' && i+1 < pattern.length) {
      var escaped = pattern[++i];
      if (escaped === 's' || escaped === 'S') {
        var ranges = escaped === 's' ? aideRegexSpace : aideRegexNonSpace;
        result += inClass ? ranges : '['+ranges+']';
      } else if (inClass && escaped === 'b') result += '\\x{8}';
      else if (inClass && escaped === 'B') result += 'B';
      else if (escaped === 'u' && pattern[i+1] === '{') result += 'u';
      else if (/[A-Za-z]/.test(escaped) && !'bBfnrtvcdDsSwWxuk'.includes(escaped)) result += escaped;
      else result += '\\'+escaped;
      continue;
    }
    if (ch === '[') inClass = true;
    if (ch === ']') inClass = false;
    result += ch === '.' && !inClass ? '[^\\n\\r\\x{2028}\\x{2029}]' : ch;
  }
  if (result.length > 16384) aidePlantUmlLimit('regular expression complexity');
  return result;
}
function aideRegexEndAnchor(pattern: any): boolean {
  var inClass = false;
  for (var i=0;i<pattern.length;i++) {
    if (pattern[i] === '\\') { i++; continue; }
    if (pattern[i] === '[') inClass = true;
    if (pattern[i] === ']') inClass = false;
    if (pattern[i] === '$' && !inClass) return true;
  }
  return false;
}
function aideSafeRegexSplit(input: any, pattern: any): any {
  return aideWithBudget(function(work: any) {
    input = String(input); pattern = String(pattern);
    if (input.length > 16384 || pattern.length > 256) aidePlantUmlLimit('regular expression input');
    aideRegexCharge(work, input.length+pattern.length+1);
    var compiled: any;
    if (work.regexCache.has(pattern)) compiled = work.regexCache.get(pattern);
    else {
      if (work.regexCache.size >= 64) aidePlantUmlLimit('regular expression cache');
      // Syntax validation only. Never execute the user's native RegExp.
      try { new RegExp(pattern); } catch (_) { compiled = null; }
      if (compiled !== null) {
        try { compiled = AidePlantUmlRE2.RE2JS.compile(AidePlantUmlRE2.RE2JS.translateRegExp(aideNormalizeRegex(pattern))); }
        catch (error: any) {
          if (error.code === 'AIDE_PLANTUML_LIMIT' || /expression too large|repeat.*size/i.test(error.message)) aidePlantUmlLimit('regular expression complexity');
          var unsupported: any = new Error('Unsupported PlantUML regular expression; use a simpler pattern or a literal separator.');
          unsupported.code = 'AIDE_PLANTUML_REGEX_UNSUPPORTED'; throw unsupported;
        }
        if (compiled.programSize() > 512 || compiled.groupCount() > 32) aidePlantUmlLimit('regular expression complexity');
        compiled.aideEndAnchor = aideRegexEndAnchor(pattern);
      }
      work.regexCache.set(pattern,compiled);
    }
    if (compiled === null) {
      if (work.regexParts >= 16384) aidePlantUmlLimit('regular expression output');
      work.regexParts++; return [input];
    }
    // RE2's end-text anchor differs from JavaScript before a final line break.
    // Reject that combination rather than silently produce different fields.
    if (compiled.aideEndAnchor && /[\n\r\u2028\u2029]$/.test(input)) {
      var anchorError: any = new Error('Unsupported PlantUML end anchor on a trailing line break; trim the input or use a literal separator.');
      anchorError.code = 'AIDE_PLANTUML_REGEX_UNSUPPORTED'; throw anchorError;
    }
    aideRegexCharge(work, (input.length+pattern.length+1)*Math.max(1,compiled.programSize()));
    if (!input.length) return [];
    var parts: any[] = [], last = 0;
    function add(value: any): void {
      if (parts.length >= 4096 || work.regexParts >= 16384) aidePlantUmlLimit('regular expression output');
      parts.push(value); work.regexParts++;
    }
    for (var match of compiled.matchAll(input)) {
      var end = match.index+match[0].length;
      if (end === last || !match[0].length && match.index === input.length) continue;
      add(input.slice(last,match.index));
      for (var group=1;group<match.length;group++) add(match[group]);
      last = end;
    }
    add(input.slice(last));
    while (parts.length && parts[parts.length-1] === '') parts.pop();
    return parts;
  });
}
