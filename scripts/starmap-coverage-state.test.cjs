const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/starmap.js','utf8');
const context=vm.createContext({sourceStateLabel:v=>({ready:'已索引',partial:'部分索引',unavailable:'暂不可用'})[v]});
vm.runInContext(source.slice(source.indexOf('function sourceCoverageState('),source.indexOf('function sourceCoverageLabel(')),context);
for(const [input,expected] of [
 [{state:'partial',coverage:{progressive:true,pending:3}},'分批扫描中'],
 [{state:'partial',coverage:{progressive:true,pending:0,reasons:['catalog_limit']}},'范围受限'],
 [{state:'partial',coverage:{progressive:true,pending:881,discoveryBlocked:true,readErrors:5,reasons:['catalog_limit']}},'范围受限'],
 [{state:'partial',coverage:{progressive:true,pending:881,discoveryBlocked:true,materialPending:3}},'正文与目录分批复核中'],
 [{state:'ready',coverage:{progressive:true,pending:0}},'限定范围已扫描'],
 [{state:'partial'},'部分索引'],
 [{state:'partial',coverage:{progressive:true,pending:3,readErrors:1,reasons:['file-read-error']}},'分批扫描中 · 正文读取异常'],
 [{state:'partial',coverage:{progressive:true,pending:0,readErrors:1,reasons:['file-read-error']}},'部分正文不可读'],
 [{state:'partial',coverage:{progressive:true,materialPending:7}},'正文与目录分批复核中'],
 [{state:'unavailable',coverage:{pending:2}},'暂不可用']
])assert.equal(context.sourceCoverageState(input),expected);
assert.match(fs.readFileSync('internal/server/web/starmap.css','utf8'),/\.chips \.source-chip-meta \{ display:block/);
vm.runInContext(source.slice(source.indexOf('function nodeDetailText('),source.indexOf('function select(n,focus=true)')),context);
assert.match(context.nodeDetailText({readState:'pending',text:'obsolete body'}),/等待分批复核/);
assert.doesNotMatch(context.nodeDetailText({readState:'unavailable',text:'obsolete body'}),/obsolete/);
assert.equal(context.nodeDetailText({text:'readable body'}),'readable body');
console.log('PASS: source coverage labels and visible source metadata (source VM/CSS)');
