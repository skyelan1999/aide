Reviewed the actual `newLiveBoundaryState` / `advanceLiveBoundary` functions in `internal/server/web/app.js` at review branch baseline `7936ed69`.

The fence regex is `/^ {0,3}(```|~~~)/`: it matches a prefix and has no end-of-line anchor. A trailing carriage return therefore does not prevent an opening fence from matching. The whitespace scan also explicitly tolerates `\r`.

I extracted and executed those actual functions in a Node VM with both LF and CRLF input containing an opening JavaScript fence, a code line, an interior blank line, and another code line. Both returned `fenceOpen: true` and `boundary: 0`; the blank line was not committed as a top-level boundary.

Closing the stated CRLF trigger as not reproduced and contradicted by the prefix-matching condition plus the minimal execution result. This does not establish correctness for all Markdown constructs; a different failing input should be tracked with its exact streamed chunks.
