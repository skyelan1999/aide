'use strict';

// Graph-only stellar accents. None of these scores represent physical mass.
window.AideStarGravity = Object.freeze({ analyze(nodes = [], edges = [], groups = []) {
 const byID = new Map(nodes.filter(node => node && typeof node.id === 'string').map(node => [node.id, node]));
 const neighbors = new Map([...byID.keys()].map(id => [id, new Set()]));
 const membership = new Map();
 for (const group of groups) {
  for (const id of group.members || []) if (!membership.has(id)) membership.set(id, group.key);
 }
 const pairsByGroup = new Map(groups.map(group => [group.key, new Set()]));
 const pairs = new Set();
 const pairKey = (a, b) => a < b ? JSON.stringify([a, b]) : JSON.stringify([b, a]);
 function link(a, b) {
  if (!byID.has(a) || !byID.has(b) || a === b) return;
  neighbors.get(a).add(b);
  neighbors.get(b).add(a);
  const left = membership.get(a), right = membership.get(b);
  if (left && left === right) pairsByGroup.get(left)?.add(pairKey(a, b));
 }
 for (const edge of edges) {
  if (!edge || ['contains', 'defines'].includes(edge.kind)) continue;
  link(edge.from, edge.to);
  const from = byID.get(edge.from), to = byID.get(edge.to);
  link(from?.parentFile || from?.id, to?.parentFile || to?.id);
 }

 const degrees = new Map([...neighbors].map(([id, set]) => [id, set.size]));
 const nonzero = [...degrees.values()].filter(degree => degree > 0).sort((a, b) => a - b);
 const percentile = (values, fraction) => values.length ? values[Math.floor((values.length - 1) * fraction)] : Infinity;
 const highlightThreshold = Math.max(8, percentile(nonzero, .97));
 const highlightedIds = new Set([...degrees].filter(([, degree]) => degree >= highlightThreshold).map(([id]) => id));

 const candidates = groups.map(group => {
  const memberCount = (group.members || []).length;
  const linkCount = pairsByGroup.get(group.key)?.size || 0;
  const linksPerNode = memberCount ? 2 * linkCount / memberCount : 0;
  return { groupKey: group.key, memberCount, linkCount, linksPerNode,
   massDensity: (memberCount + 2 * linkCount) * linksPerNode };
 }).filter(item => item.memberCount >= 12 && item.linkCount >= 12 && item.linksPerNode >= 2.4);
 const densityThreshold = Math.max(2.4, percentile(candidates.map(item => item.linksPerNode).sort((a, b) => a - b), .8));
 const blackHoles = candidates.filter(item => item.linksPerNode >= densityThreshold)
  .sort((a, b) => b.massDensity - a.massDensity || a.groupKey.localeCompare(b.groupKey))
  .slice(0, 3)
  .map(item => ({ ...item, radius: Math.max(14, Math.min(34, 11 + Math.log2(item.massDensity) * 2.5)) }));

 return Object.freeze({ degrees, highlightedIds, highlightThreshold, blackHoles, densityThreshold });
}});
