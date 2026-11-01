'use strict';

// WebKit can report the destruction of an old document's pending fetch as a
// pageerror. This is cancellation evidence, never evidence of a successful API
// operation (particularly for POST/PATCH). New-document errors still fail.
function navigationCancellation(error,{engine,base,documentGeneration,requests}){
 if(engine!=='webkit'||error.phase!=='reload'||error.documentGeneration!==documentGeneration)return null;
 const prefix='/'+new URL(base).host,suffix=' due to access control checks.';
 if(!error.message.startsWith(prefix+'/api/')||!error.message.endsWith(suffix))return null;
 const target=error.message.slice(prefix.length,-suffix.length),methods=requests.get(target);
 if(!methods?.size)return null;
 const url=new URL(target,base);
 if(url.origin!==new URL(base).origin)return null;
 return {phase:error.phase,documentGeneration,path:url.pathname,methods:[...methods].sort()};
}
module.exports={navigationCancellation};
