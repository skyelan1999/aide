/* Preserve a standalone file route across in-document anchors and tab restoration. */
(() => {
  'use strict';
  function parse(value) {
    for (const raw of [value, (()=>{try{return decodeURIComponent(value||'');}catch{return '';}})()]) {
      try {const spec=JSON.parse(raw||'');if(spec && typeof spec.path==='string' && spec.path)return spec;}catch{}
    }
    return null;
  }
  function read() {
    const value=new URLSearchParams(location.hash.slice(1)).get('file');
    return parse(value) || (history.state?.aideFileRoute?.path ? history.state.aideFileRoute : null);
  }
  function pin(spec) {
    if(!spec?.path)return;
    const hash='#file='+encodeURIComponent(JSON.stringify(spec));
    history.replaceState({...history.state,aideFileRoute:spec},'',location.pathname+location.search+hash);
  }
  window.AideFileRoute={read,pin};
})();
