'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict'),{spawnSync}=require('node:child_process');
const review=require('./highlight-grammar-review-cases.cjs');
const base='internal/server/web/vendor/highlight.js/';
const controls=[
  ['c','const unsigned char *make(int n) { return 0; }','make','title.function'],
  ['cpp','const std::vector<int> &make(int n) { return value; }','make','title'],
  ['csharp','public static async Task<int> Sum(int x) { return x; }','Sum','title'],
  ['ini','[table]\nfoo.bar = 3\n"quoted.key" = "text"\n dotted . spaced = true\n','foo.bar','attr'],
  ['ruby','puts <<END\nhello #{name}\nEND\nputs 1\n','hello','string'],
  ['ruby',"puts <<'END'\nhello\nEND\nputs <<~NEXT\nworld\nNEXT\n",'world','string']
 ];
function runtime(mode='bundle'){
 const context={console};vm.createContext(context);let code=fs.readFileSync(base+'highlight.min.js','utf8');if(mode==='source')code=code.split('/* AIDE grammar repairs:')[0];vm.runInContext(code,context);
 if(mode==='source')vm.runInContext(fs.readFileSync('internal/server/web/highlight-grammar-fixes.js','utf8'),context);
 for(const lang of ['cpp','csharp','ini','ruby'])vm.runInContext(fs.readFileSync(base+'languages/'+lang+'.min.js','utf8'),context);
 return context.hljs;
}
function pressure(language,kind){
 const h=runtime();const source=language==='ruby'?Array.from({length:32000},(_,i)=>'puts <<H'+String(i).padStart(5,'0')).join('\n')+'\n':language==='ini'?'a.'.repeat(249999)+'!':kind==='pointer'?'a* '.repeat(166666):'a '.repeat(249999);
 const start=performance.now(),result=h.highlight(source,{language,ignoreIllegals:true});assert.equal(review.spans(result).map(s=>s.text).join(''),source);console.log(JSON.stringify({language,kind,bytes:source.length,ms:performance.now()-start}));
}
function main(){
 const rows=[];function test(name,fn){try{rows.push({name,status:'PASS',detail:fn()})}catch(e){rows.push({name,status:'FAIL',error:e.message})}}

 for(const mode of ['source','bundle'])test(mode+' normal scopes and unchanged text',()=>{const h=runtime(mode);for(const [language,code,marker,scope]of controls){const result=h.highlight(code,{language,ignoreIllegals:true});assert.equal(review.spans(result).map(s=>s.text).join(''),code);assert(review.covered(result,code,marker,scope),language+' missing '+scope+' '+marker);}return{controls:controls.length};});
 for(const [language,kind]of [['c','identifiers'],['cpp','identifiers'],['csharp','identifiers'],['ini','dotted-key'],['ruby','heredocs'],['c','pointer'],['cpp','pointer']])test(language+' '+kind+' bounded editor-size work',()=>{
  const result=spawnSync(process.execPath,['--max-old-space-size=192',__filename,'--pressure',language,kind],{encoding:'utf8',timeout:6000,maxBuffer:4000});assert.equal(result.status,0,result.error?.code||result.stderr||'child failed');return JSON.parse(result.stdout);
 });
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_HIGHLIGHT_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
if(require.main===module){if(process.argv.includes('--pressure'))pressure(process.argv[3],process.argv[4]);else try{main()}catch(e){console.error(e.message);process.exitCode=1;}}
module.exports={runtime,controls};
