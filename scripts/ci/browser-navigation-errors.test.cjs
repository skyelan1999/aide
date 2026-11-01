'use strict';
const assert=require('node:assert/strict');
const {navigationCancellation}=require('./browser-navigation-errors.cjs');
const scope={engine:'webkit',base:'http://127.0.0.1:41231',documentGeneration:4,requests:new Map([
 ['/api/sessions',new Set(['GET'])],['/api/context-preview',new Set(['POST'])],['/api/sessions/synthetic',new Set(['PATCH'])]
])};
const error=path=>({message:'/127.0.0.1:41231'+path+' due to access control checks.',phase:'reload',documentGeneration:4});
for(const [path,method]of [['/api/sessions','GET'],['/api/context-preview','POST'],['/api/sessions/synthetic','PATCH']]){
 assert.deepEqual(navigationCancellation(error(path),scope),{phase:'reload',documentGeneration:4,path,methods:[method]});
}
// Old-page unload may issue a request after reload starts but before navigation
// commits; the retained document map must see that observed request too.
scope.requests.set('/api/sessions/late',new Set(['PATCH']));
assert.equal(navigationCancellation(error('/api/sessions/late'),scope).methods[0],'PATCH');
for(const candidate of [
 error('/api/not-observed'),
 {...error('/api/sessions'),phase:'active'},
 {...error('/api/sessions'),documentGeneration:5},
 {...error('/api/sessions'),message:'/elsewhere.invalid/api/sessions due to access control checks.'},
 {...error('/api/sessions'),message:'WebKit encountered an internal error'},
 {...error('/api/sessions'),message:'TypeError: actual application failure'},
 error('/style.css')
])assert.equal(navigationCancellation(candidate,scope),null);
assert.equal(navigationCancellation(error('/api/sessions'),{...scope,engine:'chromium'}),null);
assert.equal(navigationCancellation(error('/api/sessions'),{...scope,engine:'firefox'}),null);
console.log('PASS: exact observed old-document navigation cancellations; active/new/unknown/cross-origin/script/engine errors remain failures');
