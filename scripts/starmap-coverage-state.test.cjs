const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/starmap.js','utf8');
const context=vm.createContext({sourceStateLabel:v=>({ready:'已索引',partial:'部分索引',unavailable:'暂不可用'})[v]});
vm.runInContext(source.slice(source.indexOf('function sourceCoverageState('),source.indexOf('function sourceCoverageLabel(')),context);
for(const [input,expected] of [
 [{state:'partial',coverage:{progressive:true,pending:3}},'分批扫描中'],
 [{state:'partial',coverage:{progressive:true,pending:0,reasons:['catalog_limit']}},'范围受限'],
 [{state:'ready',coverage:{progressive:true,pending:0}},'限定范围已扫描'],
 [{state:'partial'},'部分索引'],
 [{state:'unavailable',coverage:{pending:2}},'暂不可用']
])assert.equal(context.sourceCoverageState(input),expected);
assert.match(fs.readFileSync('internal/server/web/starmap.css','utf8'),/\.chips \.source-chip-meta \{ display:block/);
console.log('PASS: source coverage labels and visible source metadata (source VM/CSS)');
