'use strict';

// Embedded into the private PDF.js decoder adapter by sync_pdf_openjpeg.cjs.
// Policy limits are Aide preview limits, not claims about JPEG 2000 validity.
function preflightJPX(input, metadata = {}) {
 const bytes = input;
 const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
 const fail = message => { throw new Error('JPX preflight: ' + message); };
 const u16 = p => view.getUint16(p), u32 = p => view.getUint32(p);
 const budget = (value, limit, label) => { if (!Number.isSafeInteger(value) || value > limit) fail(label + ' budget exceeded'); };
 const MiB = 1024 * 1024;
 budget(bytes.length,64*MiB,"input");
 if (bytes.length < 12) throw new Error('OpenJPEG input too short');
 let edited, streams = [], palette = 0, sycc = false, color=0,channelDefinitions=[];
 const patchType = p => { edited ||= Uint8Array.from(bytes); edited.set([102,114,101,101],p+4); };
 function boxes(start, end, header = false) {
  let first = true, seenColor = false, count = 0;
  for (let p = start; p < end;) {
   budget(++count, 4096, 'box count');
   if (p + 8 > end) fail('truncated box');
   let length = u32(p), size = 8;
   const type = u32(p+4);
   if (length === 1) {
    if (p+16>end) fail('truncated extended box');
    length=u32(p+8)*4294967296+u32(p+12);size=16;
   } else if (!length) length=end-p;
   if (!Number.isSafeInteger(length)||length<size||p+length>end) fail('invalid box extent');
   const body=p+size, stop=p+length;
   if (header && first && type!==0x69686472) fail('IHDR must be first JP2H child');
   if (type===0x66747970) budget(length,64*1024,'FTYP');
   if (type===0x6a703268) {budget(length,8*MiB,'JP2H');boxes(body,stop,true);}
   if (type===0x6a703263) streams.push([body,stop]);
   if (header && type===0x636f6c72) {
    if (stop-body<3) fail('short COLR');
    if (seenColor) patchType(p);
    else {
     seenColor=true;
     if (bytes[body]===2) budget(stop-body-3,4*MiB,'ICC');
     if (bytes[body]===1 && stop-body>=7) {color=u32(body+3);sycc=color===18;}
    }
   }
   if (header && type===0x70636c72) {
    if (body+3>stop) fail('short PCLR');
    palette=bytes[body+2];
    if (body+3+palette>stop) fail('short PCLR precision');
    for(let i=0;i<palette;i++) if((bytes[body+3+i]&127)+1>31) fail('unsupported PCLR precision');
   }
   if (header && type===0x63646566) {
    if(body+2>stop)fail('short CDEF');
    const n=u16(body);budget(n,256,'CDEF entries');
    if(body+2+6*n>stop)fail('short CDEF entries');
    for(let i=0;i<n;i++){const q=body+2+6*i;channelDefinitions.push({channel:u16(q),type:u16(q+2),association:u16(q+4)});}
   }
   first=false;p=stop;
  }
 }
 if(u16(0)===0xff4f)streams.push([0,bytes.length]);else boxes(0,bytes.length);
 if(streams.length>1)fail("multiple codestreams unsupported");
 let markers=0, packed=0, mct=0;
 for(const [start,end]of streams){
  if(start+2>end||u16(start)!==0xff4f)fail('missing SOC');
  let p=start+2, width=0,height=0,components=0,tiles=0,tileEnd=0,plmPending=false,maxLayers=0,maxPrecincts=0,laterTilePart=false;
  while(p+2<=end){
   const marker=u16(p);
   if(marker===0xffd9)break;
   if(marker===0xff93){if(!tileEnd)break;p=tileEnd;tileEnd=0;continue;}
   if(p+4>end||bytes[p]!==255)fail('invalid marker');
   const length=u16(p+2),body=p+4,stop=p+2+length;
   if(length<2||stop>end)fail('invalid marker extent');
   budget(++markers,4096,'marker count');
   if(marker===0xff51){
    if(body+36>stop)fail('short SIZ');
    const x=u32(body+2),y=u32(body+6),ox=u32(body+10),oy=u32(body+14),tx=u32(body+18),ty=u32(body+22),tox=u32(body+26),toy=u32(body+30);
    components=u16(body+34);width=x-ox;height=y-oy;
    if(!components||components>256||width<=0||height<=0||!tx||!ty||tox>ox||toy>oy||body+36+3*components>stop)fail('invalid/unsupported SIZ geometry');
    tiles=Math.ceil((x-tox)/tx)*Math.ceil((y-toy)/ty);
    budget(tiles,4096,'tile count');budget(tiles*components,16384,'tile/component metadata');
    // Include full native component planes plus worst-case temporary RGB planes.
    let samples=0;const specs=[];
    for(let i=0;i<components;i++){
     const dx=bytes[body+37+3*i],dy=bytes[body+38+3*i];
     if(!dx||!dy)fail('zero component sampling');
     specs.push({precision:(bytes[body+36+3*i]&127)+1,signed:!!(bytes[body+36+3*i]&128),dx,dy});
     samples+=(Math.ceil(x/dx)-Math.ceil(ox/dx))*(Math.ceil(y/dy)-Math.ceil(oy/dy));
    }
    Object.assign(metadata,{width,height,x0:ox,y0:oy,components,specs,color,palette,channelDefinitions});
    if(color===24 && specs.some(c=>c.dx!==1||c.dy!==1))fail('subsampled e-sYCC is unsupported; refusing incorrect sYCC substitution');
    if(components===3&&bytes[body+37]===bytes[body+38]&&bytes[body+40]!==1)sycc=true;
    budget(4*(samples+(sycc?3*width*height:0)+(palette?palette*width*height:0)),96*MiB,'component planes');
    budget(width*height*4,64*MiB,'RGBA output');
   }
   if(marker===0xff52||marker===0xff53){
    if(laterTilePart)fail("coding overrides after tile-part zero are unsupported");
    const coc=marker===0xff53,style=body+(coc?1:0),coding=body+(coc?2:5),precinct=coding+5;
    if(precinct>stop||!components)fail('short COD/COC or coding before SIZ');
    const layers=coc?maxLayers:u16(body+2),resolutions=bytes[coding]+1;
    if(!layers||resolutions>33)fail('invalid COD/COC');
    budget(layers,4096,'quality layers');maxLayers=Math.max(maxLayers,layers);
    let precincts=0;
    if(bytes[style]&1 && precinct+resolutions>stop)fail('short COD/COC precincts');
    for(let r=0;r<resolutions;r++){
     const scale=2**(resolutions-1-r),v=bytes[style]&1?bytes[precinct+r]:255;
     precincts+=(Math.ceil(width/scale/2**(v&15))+1)*(Math.ceil(height/scale/2**(v>>4))+1);
    }
    maxPrecincts=Math.max(maxPrecincts,precincts);
    budget((maxLayers+1)*components*tiles*maxPrecincts,8*MiB,'packet iterator');
   }
   if(marker===0xff5e){const i=components<257?1:2;if(body+i+2>stop||bytes[body+i]!==0)fail('unsupported ROI style');}
   if(marker===0xff5f){const n=components<257?7:9;if((stop-body)%n)fail('invalid POC');for(let q=body;q<stop;q+=n)if(bytes[q+n-1]>4)fail('unsupported progression order');}
   if(marker===0xff55){if(body+2>stop||((bytes[body+1]>>4)&3)===3)fail('reserved TLM tile-index size');}
   if(marker===0xff57){
    if(body+1>stop)fail('short PLM');
    let q=body+1;
    while(q<stop){const n=bytes[q++];if(q+n>stop)fail('short PLM group');for(let i=0;i<n;i++)plmPending=!!(bytes[q++]&128);}
   }
   if(marker===0xff60||marker===0xff61){packed+=stop-body;budget(packed,4*MiB,'packed packet headers');}
   if(marker===0xff74){mct+=stop-body;budget(mct*Math.max(1,tiles),16*MiB,'MCT tile copies');}
   if(marker===0xff90){
    if(body+8>stop)fail('short SOT');
    laterTilePart=bytes[body+6]!==0;
    const part=u32(body+2);
    if(part){tileEnd=p+part;if(tileEnd> end||tileEnd<stop)fail('invalid tile-part extent');}
   }
   if(marker===0xff90&&plmPending)fail('unterminated PLM packet length');
   p=stop;
  }
  if(plmPending)fail('unterminated PLM packet length');
 }
 return edited||input;
}
module.exports=preflightJPX;
