'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {exporter,files,countGifFrames}=require('./test_drawio_gif_frames.cjs');
function code(file){
 const base='internal/server/web/vendor/drawio/js/';
 if(file===files[0])return fs.readFileSync(base+'diagramly/gif/GifEncoder.js','utf8')+'\n'+exporter(file);
 const text=fs.readFileSync(base+file,'utf8'),a=text.indexOf('function GifEncoder(');let b=text.indexOf('/* AIDE GIF frame budget #181 */',a);if(b<0)b=text.indexOf('function AnimatedGifExport(',a);assert.ok(a>=0&&b>a);return text.slice(a,b)+exporter(file);
}
module.exports=async function(frame,out,theme){
 await frame.evaluate(()=>{window.ownedGifOriginal={Export:AnimatedGifExport,Encoder:GifEncoder,save:ownedUi.saveData,error:ownedUi.handleError,stop:ownedUi.spinner.stop,flow:ownedEditor.graph.enableFlowAnimation};});
 const checks=[];
 try{
  for(const file of files){
   await frame.addScriptTag({content:code(file)});
   const failed=await frame.evaluate(()=>{
    ownedUi.setFileData('<mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/></root></mxGraphModel>');
    const graph=ownedEditor.graph;graph.enableFlowAnimation=true;
    const cell=graph.insertEdge(graph.getDefaultParent(),null,'',null,null,'edgeStyle=none;endArrow=none;dashed=1;dashPattern=8 8;flowAnimation=1;flowAnimationDuration=1000000000;strokeColor=#1989d1;strokeWidth=1;');
    const geometry=cell.geometry.clone();geometry.sourcePoint=new mxPoint(20,20);geometry.targetPoint=new mxPoint(140,60);graph.model.setGeometry(cell,geometry);
    const state={cell,encoders:0,errors:[],stops:0,rejectRaster:true};window.ownedGifCase=state;
    const Encoder=GifEncoder;window.GifEncoder=new Proxy(Encoder,{construct(target,args){state.encoders++;if(state.rejectRaster)throw Error('Owned unexpected GIF rasterization');return Reflect.construct(target,args);}});
    ownedUi.handleError=function(error){state.errors.push({message:error?.message,code:error?.code});if(state.onError)state.onError(error);return ownedGifOriginal.error.apply(this,arguments);};
    ownedUi.spinner.stop=function(){state.stops++;return ownedGifOriginal.stop.apply(this,arguments);};
    ownedUi.saveData=function(filename,ext,data,mime){if(state.onSave)state.onSave({filename,ext,data,mime});};
    const svg=graph.getSvg('#ffffff',1,0,true,null,true,null,null,null,graph.shadowVisible,null,null);
    const infos=AnimatedGifExport.findAnimatedPaths(svg),duration=AnimatedGifExport.detectDuration(infos);
    if(!infos.length||duration<100000000)throw Error('actual long flow SVG not reproduced: '+duration);
    ownedUi.exportAnimatedGif({fps:15,scale:1,border:0,background:'#ffffff'});
    return {duration,errors:state.errors,encoders:state.encoders,stops:state.stops};
   });
   assert.equal(failed.errors.length,1);assert.equal(failed.errors[0].code,'AIDE_GIF_EXPORT_LIMIT');assert.equal(failed.encoders,0);assert.ok(failed.stops>=1);
   const error=frame.getByText(failed.errors[0].message,{exact:true});await error.waitFor();await frame.page().screenshot({path:path.join(out,theme+'-gif-limit.png')});await frame.getByRole('button',{name:'OK',exact:true}).click();
   const saved=await frame.evaluate(async()=>{
    const state=ownedGifCase;state.rejectRaster=false;
    ownedEditor.graph.model.setStyle(state.cell,state.cell.style.replace('flowAnimationDuration=1000000000','flowAnimationDuration=500'));
    const result=await new Promise((resolve,reject)=>{
     const timeout=setTimeout(()=>reject(Error('small GIF export timeout')),10000);
     state.onSave=value=>{clearTimeout(timeout);resolve(value);};state.onError=error=>{clearTimeout(timeout);reject(error);};
     ownedUi.exportAnimatedGif({fps:8,duration:250,scale:1,border:1,background:'#ffffff'});
    });
    const blob=new Blob([Uint8Array.from(atob(result.data),c=>c.charCodeAt(0))],{type:'image/gif'}),url=URL.createObjectURL(blob);
    try{
     const image=new Image();await new Promise((resolve,reject)=>{image.onload=resolve;image.onerror=()=>reject(Error('native GIF decode failed'));image.src=url;});
     return {...result,width:image.naturalWidth,height:image.naturalHeight,encoders:state.encoders,stops:state.stops};
    }finally{URL.revokeObjectURL(url);}
   });
   assert.equal(saved.mime,'image/gif');assert.equal(saved.ext,'gif');assert.match(saved.filename,/\.gif$/);assert.equal(saved.encoders,1);
   const gif=countGifFrames(Buffer.from(saved.data,'base64'));assert.equal(gif.frames,2);assert.equal(gif.width,saved.width);assert.equal(gif.height,saved.height);assert.ok(gif.width>0&&gif.height>0);
   checks.push({case:'actual long-flow SVG GIF UI rejection before encoder and spinner recovery; real matching source/bundle 2-frame GIF raster/encoding/native decode/saveData',file,theme,result:'PASS',durationRejected:failed.duration,gif});
  }
 }finally{await frame.evaluate(()=>{window.AnimatedGifExport=ownedGifOriginal.Export;window.GifEncoder=ownedGifOriginal.Encoder;ownedUi.saveData=ownedGifOriginal.save;ownedUi.handleError=ownedGifOriginal.error;ownedUi.spinner.stop=ownedGifOriginal.stop;ownedEditor.graph.enableFlowAnimation=ownedGifOriginal.flow;delete window.ownedGifCase;delete window.ownedGifOriginal;});}
 return checks;
};
module.exports.code=code;
