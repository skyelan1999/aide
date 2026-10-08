// Source-only AST extraction. Never require, import or evaluate workspace code.
const fs=require('fs');
const files=JSON.parse(fs.readFileSync(0,'utf8')), out=[];
for(const file of files){
 const u={file:file.id,language:'JavaScript',package:'',symbols:[],calls:[],imports:[],diagnostics:[]};
 try {
  let tree;try{tree=acorn.parse(file.text,{ecmaVersion:'latest',sourceType:'module',locations:true,allowHashBang:true})}catch(e){tree=acorn.parse(file.text,{ecmaVersion:'latest',sourceType:'script',locations:true,allowHashBang:true})}
  let ordinal=0;
  function walk(n,scope='',parent=null){
   if(!n||typeof n.type!=='string')return;
   if(n.type==='ImportDeclaration')for(const s of n.specifiers)u.imports.push({alias:s.local.name,path:n.source.value,name:s.type==='ImportSpecifier'?s.imported.name:s.type==='ImportDefaultSpecifier'?'default':'*'});
   let next=scope;
   const isFunc=/^(FunctionDeclaration|FunctionExpression|ArrowFunctionExpression)$/.test(n.type),isClass=/^(ClassDeclaration|ClassExpression)$/.test(n.type);
   if(isFunc||isClass){
    const named=n.id?.name || (parent?.type==='VariableDeclarator'?parent.id?.name:null) || (parent?.type==='MethodDefinition'||parent?.type==='Property'?(!parent.computed?parent.key?.name||parent.key?.value:null):null);
    const name=String(named||`anonymous@${n.loc.start.line}:${++ordinal}`),key=(scope?scope+'.':'')+name;
    const head=file.text.slice(n.start,isFunc?n.body.start:n.start+Math.min(n.end-n.start,120));
    u.symbols.push({key,parent:scope,name,kind:isClass?'class':parent?.type==='MethodDefinition'?'method':isFunc&&n.async?'async function':'function',line:n.loc.start.line,endLine:n.loc.end.line,signature:head.slice(0,240),anonymous:!named});next=key;
   }
   if(n.type==='CallExpression'||n.type==='NewExpression'){
    const c=n.callee;let name='',qualifier='',dynamic=false;
    if(c.type==='Identifier')name=c.name;
    else if(c.type==='MemberExpression'&&!c.computed&&c.property.type==='Identifier'){name=c.property.name;qualifier=c.object.type==='Identifier'?c.object.name:c.object.type==='ThisExpression'?'this':'<expression>';dynamic=true;}
    else {name='<dynamic>';dynamic=true;}
    u.calls.push({caller:scope,name,qualifier,line:n.loc.start.line,dynamic});
   }
   for(const [k,v]of Object.entries(n)){if(['loc','start','end','type'].includes(k))continue;if(Array.isArray(v))for(const c of v)walk(c,next,n);else if(v&&typeof v.type==='string')walk(v,next,n);}
  }
  walk(tree);
 }catch(e){u.diagnostics.push('语法解析失败：'+String(e.message).slice(0,180));}
 out.push(u);
}
process.stdout.write(JSON.stringify(out));
