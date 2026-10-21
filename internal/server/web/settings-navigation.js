'use strict';
// Presentation routes keep the existing section/child IDs used by shortcuts.
(function () {
  function build(schema) {
    const pages = (schema.sections || []).flatMap(section => section.children?.length
      ? section.children.map(child => ({...child, sectionId:section.id, childId:child.id}))
      : [{...section, sectionId:section.id, childId:''}]);
    const seen = new Set();
    const groups = (schema.navigation || []).map(group => ({...group, pages:(group.pages || []).map(id => {
      const page = pages.find(page => page.id === id);
      if (!page || seen.has(id)) return null;
      seen.add(id); return page;
    }).filter(Boolean)})).filter(group => group.pages.length);
    // A new schema section must remain reachable even before it is categorized.
    for (const page of pages) if (!seen.has(page.id)) groups.push({id:page.id,title:page.title,pages:[page]});
    return {groups,pages};
  }
  function resolve(model, sectionId, childId) {
    return model.pages.find(page => page.sectionId === sectionId && page.childId === childId)
      || model.pages.find(page => page.id === sectionId)
      || model.pages.find(page => page.sectionId === sectionId)
      || model.pages[0];
  }
  window.AideSettingsNavigation = {build,resolve};
})();
