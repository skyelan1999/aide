'use strict';
// Read-only verification of published bytes. Hash equality is not artwork review
// or proof that the historic source/extraction claims are authentic.
const fs=require('node:fs/promises'),path=require('node:path'),crypto=require('node:crypto');
const hash=bytes=>crypto.createHash('sha256').update(bytes).digest('hex');
async function readRegular(dir,name){
 if(typeof name!=='string'||!name||path.basename(name)!==name||name.includes('\\'))throw Error('Invalid avatar audit filename');
 const file=path.join(dir,name),stat=await fs.lstat(file);
 if(!stat.isFile()||stat.isSymbolicLink())throw Error('Avatar audit requires a regular file: '+name);
 return fs.readFile(file);
}
function sameIDs(ids,records,label){
 if(!Array.isArray(records)||records.length!==ids.length||new Set(records.map(x=>x?.id)).size!==ids.length||
    records.some(x=>!ids.includes(x?.id)))throw Error(label+' IDs do not match manifest');
}
function assetNames(manifest,id){
 if(!/^[A-Za-z0-9_-]{1,80}$/.test(id))throw Error('Invalid avatar audit ID');
 const sheet=manifest.sheets?.[id],variant=manifest.variants?.[id],clip=manifest.clips?.[id];
 if(!sheet||!variant||!clip||clip.sheet!==id||sheet.src!==id+'-atlas.webp'||variant.animation!==id+'.webp'||variant.poster!==id+'-poster.webp')throw Error('Invalid avatar asset references: '+id);
 return [sheet.src,variant.animation,variant.poster];
}
async function fileHashes(dir,manifest,id){
 const files={};for(const name of assetNames(manifest,id))files[name]=hash(await readRegular(dir,name));return files;
}
function sameFiles(recorded,actual,label){
 if(!recorded||Object.keys(recorded).length!==Object.keys(actual).length||Object.entries(actual).some(([name,digest])=>recorded[name]!==digest))throw Error(label+' file hashes do not match published assets');
}
async function verifyAssetAudit(dir){
 const manifest=JSON.parse(await readRegular(dir,'manifest.json'));
 if(Object.hasOwn(manifest.provenance||{},'interpolationInProgress'))throw Error('Avatar interpolation is incomplete');
 const ids=Object.keys(manifest.variants||{});if(!ids.length)throw Error('Empty avatar manifest');
 for(const key of ['sheets','clips'])if(Object.keys(manifest[key]||{}).length!==ids.length||ids.some(id=>!Object.hasOwn(manifest[key],id)))throw Error('Avatar '+key+' IDs do not match variants');
 const records=JSON.parse(await readRegular(dir,'art-audit.json'));sameIDs(ids,records,'Art audit');
 for(const record of records)sameFiles(record.files,await fileHashes(dir,manifest,record.id),record.id);
 const interpolated=Object.hasOwn(manifest.provenance||{},'interpolation');let interpolation;
 try{interpolation=JSON.parse(await readRegular(dir,'frame-interpolation-audit.json'));}
 catch(error){if(error.code!=='ENOENT'||interpolated)throw error;}
 if(!interpolated&&interpolation!==undefined)throw Error('Stale interpolation audit without manifest interpolation provenance');
 if(interpolated){
  sameIDs(ids,interpolation,'Interpolation audit');
  for(const item of interpolation){
   const clip=manifest.clips[item.id],art=records.find(x=>x.id===item.id);
   if(!Number.isSafeInteger(item.originalFrames)||item.originalFrames<1||item.newFrames!==clip.frames.length||
      item.newFrames!==art.frameCount||!Array.isArray(art.frameHashes)||art.frameHashes.length!==item.newFrames||
      JSON.stringify(item.durations)!==JSON.stringify(clip.durations)||item.totalDuration!==clip.durations.reduce((n,x)=>n+x,0))throw Error('Interpolation frame metadata does not match manifest: '+item.id);
   sameFiles(item.files,art.files,'Interpolation '+item.id);
   const source=item.sourceFiles,names=assetNames(manifest,item.id);
   if(!source||Object.keys(source).length!==names.length||names.some(name=>typeof source[name]!=='string'||!/^[a-f0-9]{64}$/.test(source[name])))throw Error('Missing interpolation input hashes: '+item.id);
  }
 }
 return {manifest,records,variants:ids.length,files:ids.length*3};
}
async function atomicJSON(dir,name,value){
 const file=path.join(dir,name),temporary=path.join(dir,'.avatar-audit-'+crypto.randomUUID()+'.json');
 try{await fs.writeFile(temporary,JSON.stringify(value,null,2)+'\n',{flag:'wx'});await fs.rename(temporary,file);}
 finally{await fs.rm(temporary,{force:true});}
}
module.exports={verifyAssetAudit,fileHashes,atomicJSON};
if(require.main===module){
 const dir=process.argv[2]||path.resolve(__dirname,'../internal/server/web/avatars/xiaomi-enhanced');
 verifyAssetAudit(dir).then(r=>console.log(`PASS avatar published audit: ${r.variants} variants / ${r.files} asset hashes`)).catch(e=>{console.error(e.message);process.exitCode=1;});
}
