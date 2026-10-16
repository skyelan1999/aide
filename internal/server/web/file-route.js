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
  function returnToMap(){
    const value=new URLSearchParams(location.search).get('starmapReturn');
    if(!value)return null;
    try{
      const url=new URL(value,location.origin);
      if(url.origin!==location.origin||url.pathname!=='/starmap.html'||url.username||url.password)return null;
      const safe=new URL('/starmap.html',location.origin),channel=url.searchParams.get('channel');
      if(channel&&/^[A-Za-z0-9_-]{1,128}$/.test(channel))safe.searchParams.set('channel',channel);
      if(url.searchParams.get('code')==='1')safe.searchParams.set('code','1');
      return safe.pathname+safe.search;
    }catch{return null;}
  }
  async function matchesSourceLocation(spec, data) {
    if(spec.codeHash)return data.hash===spec.codeHash;
    if(spec.indexedTextHash!==undefined||spec.indexedTextBytes!==undefined){
      const count=spec.indexedTextBytes;
      if(!/^[a-f0-9]{64}$/.test(spec.indexedTextHash||'')||!Number.isSafeInteger(count)||count<1||count>8192||typeof data.content!=='string')return false;
      try{
        // At most8192 UTF-16 units need encoding to obtain8192 UTF-8 bytes.
        const bytes=new TextEncoder().encode(data.content.slice(0,count));
        if(bytes.length<count)return false;
        const digest=await crypto.subtle.digest('SHA-256',bytes.subarray(0,count));
        return Array.from(new Uint8Array(digest),n=>n.toString(16).padStart(2,'0')).join('')===spec.indexedTextHash;
      }catch{return false;}
    }
    // Old map routes without a proof must refresh instead of silently using
    // stale line numbers. Ordinary non-map file routes remain compatible.
    return !spec.knowledgeWorkspace;
  }
  window.AideFileRoute={read,pin,returnToMap,matchesSourceLocation};
})();
