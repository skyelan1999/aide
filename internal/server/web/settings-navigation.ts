interface Window {
  AideSettingsNavigation?: { build: (schema: any) => any; resolve: (model: any, sectionId: any, childId: any) => any; search: (model: any, query: any) => any[] };
}
'use strict';
// Presentation routes keep the existing section/child IDs used by shortcuts.
(function () {
  function build(schema: any) {
    const pages = (schema.sections || []).flatMap((section: any) => section.children?.length
      ? section.children.map((child: any) => ({...child, sectionId:section.id, childId:child.id}))
      : [{...section, sectionId:section.id, childId:''}]);
    const seen = new Set<any>();
    const groups = (schema.navigation || []).map((group: any) => ({...group, pages:(group.pages || []).map((id: any) => {
      const page = pages.find((page: any) => page.id === id);
      if (!page || seen.has(id)) return null;
      seen.add(id); return page;
    }).filter(Boolean)})).filter((group: any) => group.pages.length);
    // A new schema section must remain reachable even before it is categorized.
    for (const page of pages) if (!seen.has(page.id)) groups.push({id:page.id,title:page.title,pages:[page]});
    return {groups,pages};
  }
  function resolve(model: any, sectionId: any, childId: any) {
    return model.pages.find((page: any) => page.sectionId === sectionId && page.childId === childId)
      || model.pages.find((page: any) => page.id === sectionId)
      || model.pages.find((page: any) => page.sectionId === sectionId)
      || model.pages[0];
  }
  function search(model: any, query: any): any[] {
    const needle = String(query || '').trim().toLocaleLowerCase();
    if (!needle) return [];
    return model.groups.flatMap((group: any) => group.pages.filter((page: any) => {
      const terms = [group.title, page.title, page.description,
        ...(page.controls || []).flatMap((control: any) => [control.label, control.description])];
      return terms.some((term: any) => String(term || '').toLocaleLowerCase().includes(needle));
    }).map((page: any) => ({group, page})));
  }
  window.AideSettingsNavigation = {build,resolve,search};
})();
