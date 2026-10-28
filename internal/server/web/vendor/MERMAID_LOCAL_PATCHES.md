# Local Mermaid 10.9.8 corrections

Keep the upstream version, bundled license metadata, and strict security settings. Original bundle SHA-256: `8d607d7ef1d077a8aa202e18e62212bfa992c68bfeabc5cf45d51a128fe6675d`.

The native Draw.io converter is a separate library. This file describes `mermaid.min.js`, used by Markdown SVG previews.

- #469/#515/#523/#524/#525/#527/#528: prototype-free block/style, pie, requirement, sequence actor, class, ER, and Gantt task maps, including clear/reset paths. Class IDs also pass through Graphlib/Dagre indexes; their node, adjacency, DFS and layout dictionaries use prototype-free objects. Gantt clears its index along with tasks between diagrams.
- #517: reject Timeline event rows without a preceding period with a clear error.
- #521: decode sanitized title entities at the shared SVG `.text()` sink; decoded text is never passed to an HTML sink.
- #522: reject empty or nonfinite Sankey numeric cells before storing a link.
- #526: reject nonfinite XY plot values before scale/layout calculations.

`browser-mermaid-svg-issues.cjs` checks ordinary/special inputs and repeated database resets. `browser-mermaid-proof-auth.cjs` covers Aide's strict-CSP Markdown integration and theme/stylesheet lifecycle.
