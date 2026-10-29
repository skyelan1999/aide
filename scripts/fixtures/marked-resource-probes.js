/* Pure, bounded test inputs shared by Node and real browser workers. */
function runMarkedResourceProbe(marked,kind){
 let source,expected,issue;
 if(kind==='mask'){issue=412;source='\\*'.repeat(200000);expected='<p>'+'*'.repeat(200000)+'</p>\n';}
 else if(kind==='html'){issue=431;source='<script>'+'a</script>'.repeat(60000);expected=source;}
 else if(kind==='fence'){issue=431;source='owned\n'+'~'.repeat(100000);expected='<p>'+source+'</p>\n';}
 else throw Error('Unknown owned probe');
 const start=performance.now(),html=marked.parse(source);
 if(html!==expected)throw Error('Parser output changed in '+kind);
 return{issue,kind,inputCharacters:source.length,outputCharacters:html.length,ms:performance.now()-start};
}
if(typeof module==='object')module.exports={runMarkedResourceProbe};
