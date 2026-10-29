'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const base='internal/server/web/vendor/drawio/js/',files=['diagramly/gif/AnimatedExport.js','app.min.js','integrate.min.js'];
function exporter(file){
 const text=fs.readFileSync(base+file,'utf8');if(file===files[0])return text;
 const marker='/* AIDE GIF frame budget #181 */';let a=text.indexOf(marker);if(a<0)a=text.indexOf('function AnimatedGifExport(');
 const last=text.indexOf('AnimatedGifExport.removeAnimationCss=function',a),end=text.indexOf(file==='app.min.js'?'DrawioClient=function':'\nvar $jscomp=',last);assert.ok(a>=0&&last>a&&end>last);return text.slice(a,end);
}
function runtime(file,duration=1000000000){
 const calls={converted:0,encoders:0,frames:null,errors:[]};
 const path={style:{animation:'ge-flow-animation-owned '+duration+'ms linear infinite',strokeDashoffset:'16'},getAttribute:()=>null};
 const svg={getElementsByTagName:name=>name==='path'?[path]:[],getAttribute:()=> '16'};
 const context={Editor:{getDefaultPageBackgroundColor:()=> '#fff'},mxConstants:{NONE:'none'},mxResources:{get:key=>key},mxUtils:{bind:(self,fn)=>fn.bind(self)},GifEncoder:function(){calls.encoders++;throw Error('owned encoder touched');}};
 vm.runInNewContext(exporter(file),context);
 const ui={editor:{graph:{getSvg:()=>svg,shadowVisible:false},convertImages:(svg,callback)=>{calls.converted++;callback(svg);},createImageUrlConverter:()=>null,getMaxCanvasScale:()=>1}};
 const exp=new context.AnimatedGifExport(ui);exp.renderFrames=function(svg,infos,count){calls.frames=count;};
 return {context,ui,exp,calls,svg,path};
}
function main(){
 for(const file of files){
  const r=runtime(file);r.exp.doExport({fps:15},()=>{},error=>r.calls.errors.push(error));
  console.log(JSON.stringify({file,plannedFrames:r.calls.frames,imageConversions:r.calls.converted,error:r.calls.errors[0]?.message,code:r.calls.errors[0]?.code}));
  assert.equal(r.calls.errors[0]?.code,'AIDE_GIF_EXPORT_LIMIT',file+' rejects excessive frames before rasterization');assert.equal(r.calls.converted,0);assert.equal(r.calls.frames,null);assert.ok(r.path.style.animation,'rejection precedes style mutation');
  for(const [fps,duration,count]of [[15,1000,15],[8,250,2],[15,20000,300]]){
   const valid=runtime(file,duration);valid.exp.doExport({fps},()=>{},error=>{throw error;});assert.equal(valid.calls.frames,count);assert.equal(valid.calls.converted,1);
  }
  for(const [fps,duration]of [[-1,1000],[Infinity,1000],[15,-1],[15,Infinity],[15,20034]]){
   const invalid=runtime(file);invalid.exp.doExport({fps,duration},()=>{},error=>invalid.calls.errors.push(error));assert.equal(invalid.calls.errors[0]?.code,'AIDE_GIF_EXPORT_LIMIT');assert.equal(invalid.calls.converted,0);
  }
  for(const options of [{},{fps:0},{fps:NaN},{duration:0},{duration:NaN}]){
   const defaults=runtime(file,1000);defaults.exp.doExport(options,()=>{},error=>{throw error;});assert.equal(defaults.calls.frames,15,'legacy falsy options retain automatic/default values');
  }
  for(const count of [301,Infinity,NaN,2.5,-1]){
   const direct=runtime(file);direct.context.AnimatedGifExport.prototype.renderFrames.call(direct.exp,direct.svg,[],count,15,1,false,0,'#fff',null,()=>{},error=>direct.calls.errors.push(error));assert.equal(direct.calls.errors[0]?.code,'AIDE_GIF_EXPORT_LIMIT');assert.equal(direct.calls.encoders,0);
  }
  for(const [value,expected]of [['ge-flow-animation-owned 0.5s linear infinite',500],['ge-flow-animation-owned 2e3ms linear infinite',2000]]){
   const css=runtime(file);css.path.style.animation=value;assert.equal(css.context.AnimatedGifExport.findAnimatedPaths(css.svg)[0].duration,expected);
  }
  console.log('PASS',file,'pre-raster frame/fps/duration budgets, exact boundary, direct render guard and CSS seconds/exponents');
 }
}
function countGifFrames(data){
 assert.ok(['GIF87a','GIF89a'].includes(Buffer.from(data.subarray(0,6)).toString('ascii')));
 const width=data[6]|data[7]<<8,height=data[8]|data[9]<<8;let pos=13,frames=0;
 if(data[10]&128)pos+=3*(1<<((data[10]&7)+1));
 function blocks(){for(;;){assert.ok(pos<data.length,'complete GIF subblock');const n=data[pos++];if(n===0)return;pos+=n;assert.ok(pos<=data.length);}}
 while(pos<data.length){
  const code=data[pos++];
  if(code===0x3b){assert.equal(pos,data.length);return {frames,width,height};}
  if(code===0x21){pos++;blocks();}
  else if(code===0x2c){frames++;assert.ok(pos+9<=data.length);const packed=data[pos+8];pos+=9;if(packed&128)pos+=3*(1<<((packed&7)+1));pos++;blocks();}
  else throw Error('invalid GIF block '+code);
 }
 throw Error('missing GIF trailer');
}
module.exports={exporter,runtime,files,countGifFrames};if(require.main===module)main();
