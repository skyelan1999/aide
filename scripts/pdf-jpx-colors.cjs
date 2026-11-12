'use strict';

// The native explicit-component path scales each component independently and
// upsamples on the reference grid. Convert that bounded 8-bit display buffer in
// JS to avoid native precision-31 integer overflow and retain the opacity plane.
function convertJPXColors(data, metadata, smaskInData) {
 const {width,height,components,specs,color,x0,y0}=metadata;
 if(data.length!==width*height*components)throw new Error('JPX component layout is unsupported');
 const output=new Uint8ClampedArray(width*height*4);
 const clamp=value=>Math.max(0,Math.min(255,Math.trunc(value)));
 for(let p=0,n=0;p<data.length;p+=components,n+=4){
  if(color===18){
   const x=(n/4)%width,y=Math.floor(n/4/width),luma=data[p];
   const chroma=channel=>{
    const {dx,dy}=specs[channel];
    const missing=x+x0<Math.ceil(x0/dx)*dx||y+y0<Math.ceil(y0/dy)*dy;
    return missing?0:data[p+channel]-128;
   };
   const cb=chroma(1),cr=chroma(2);
   output[n]=clamp(luma+1.402*cr);
   output[n+1]=clamp(luma-0.344136*cb-0.714136*cr);
   output[n+2]=clamp(luma+1.772*cb);
   output[n+3]=smaskInData&&components===4?data[p+3]:255;
  } else {
   const ink=channel=>Math.max(0,Math.min(1,data[p+channel]/(specs[channel].signed?127:255)));
   const black=1-ink(3);
   for(let channel=0;channel<3;channel++)output[n+channel]=clamp(255*(1-ink(channel))*black);
   output[n+3]=smaskInData&&components===5?data[p+4]:255;
  }
 }
 return output;
}

function useJPXColorConversion(metadata,numComponents,isIndexedColormap){
 if(numComponents!==0||isIndexedColormap||metadata.palette)return false;
 const {color,components,specs}=metadata;
 if(metadata.channelDefinitions?.some(d=>d.type===0?d.association!==d.channel+1:d.channel!==components-1||d.association!==0||![1,2].includes(d.type)))return false;
 if(color===18 && (components===3||components===4))return specs.slice(0,3).every(c=>!c.signed);
 return color===12 && (components===4||components===5);
}
module.exports={convertJPXColors,useJPXColorConversion};
