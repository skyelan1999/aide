interface Window {
  AideStagedOutput?: { segments: (step: any) => any[]; pending: (step: any, text: any) => any; text: (step: any, tail?: any) => string; append: (step: any, segment: any) => boolean; merge: (previous: any, next: any) => any; render: (step: any, options: any) => any };
}
(function () {
  'use strict';
  const segments = (step: any): any[] => Array.isArray(step?.outputSegments) ? step.outputSegments.filter((s: any) => s?.id && typeof s.content === 'string' && s.content.trim()) : [];
  function pending(step: any, text: any): any {
    const parts = segments(step);
    return parts.length && parts[parts.length - 1].content === text ? '' : (text || '');
  }
  function text(step: any, tail = step?.content || ''): string {
    return [...segments(step).map(s => s.content), pending(step, tail)].filter(Boolean).join('\n\n');
  }
  function append(step: any, segment: any): boolean {
    if (!step || !segment?.id || typeof segment.content !== 'string' || !segment.content.trim()) return false;
    if (segments(step).some(s => s.id === segment.id)) return false;
    step.outputSegments = [...segments(step), segment];
    return true;
  }
  // A GET started before the SSE acknowledgement must not remove a saved part.
  // Scope the merge to the same session/run/step, and retain server ordering.
  function merge(previous: any, next: any): any {
    if (!previous?.id || previous.id !== next?.id) return next;
    for (const run of next.runs || []) {
      const old = previous.runs?.find((r: any) => r.id === run.id);
      for (const step of run.steps || []) {
        for (const segment of segments(old?.steps?.find((s: any) => s.name === step.name))) append(step, segment);
      }
    }
    return next;
  }
  function render(step: any, {el, t, renderMarkdown}: any): any {
    const parts = segments(step);
    if (!parts.length) return null;
    const group = el('div', 'stage-outputs');
    for (const [index, segment] of parts.entries()) {
      const section = el('section', 'stage-output'); section.dataset.segment = segment.id;
      section.append(el('div', 'stage-label', t('阶段输出 {0}', index + 1)));
      const body = el('div', 'stage-answer md-body'); body.innerHTML = renderMarkdown(segment.content);
      section.append(body); group.append(section);
    }
    return group;
  }
  window.AideStagedOutput = {segments, pending, text, append, merge, render};
})();
