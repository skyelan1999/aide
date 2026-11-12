'use strict';
// Backport the numeric SMaskInData predicate and preblend handling from
// https://github.com/mozilla/pdf.js/pull/21587 to the pinned worker.
const fs=require('node:fs'),assert=require('node:assert/strict');
const path='internal/server/web/vendor/pdfjs/pdf.worker.min.mjs';
const before=fs.readFileSync(path,'utf8');let source=before;
function replace(original,updated){
 if(source.includes(updated)){assert.equal(source.split(updated).length,2);return;}
 assert.equal(source.split(original).length,2,'unexpected PDFImage shape');source=source.replace(original,updated);
}
replace('smaskInData:c.has("SMaskInData")','smaskInData:c.get("SMaskInData")===2?2:c.get("SMaskInData")===1?1:0');
replace('const r=!!s;if(r)this.jpxDecoderOptions', 'const r=!!s;if(this.jpxDecoderOptions?.smaskInData&&c.get("SMaskInData")===2){this.jpxPremultiplied=true;if(this.matte)this.preblendMatte=ColorSpace.parse({cs:r?s:Name.get("DeviceRGB"),xref:e,resources:a?t:null,pdfFunctionFactory:g,localColorSpaceCache:o}).getRgb(this.matte,0)}if(r)this.jpxDecoderOptions');
const helper='static undoJPXPreblend(buffer,matteR=0,matteG=0,matteB=0){for(let i=0;i<buffer.length;i+=4){const alpha=buffer[i+3];if(alpha===0){buffer[i]=buffer[i+1]=buffer[i+2]=255;continue}const inverse=255/alpha;buffer[i]=(buffer[i]-matteR)*inverse+matteR;buffer[i+1]=(buffer[i+1]-matteG)*inverse+matteG;buffer[i+2]=(buffer[i+2]-matteB)*inverse+matteB}}';
replace('undoPreblend(e,t,i){const a=this.smask?.matte;',helper+'undoPreblend(e,t,i){const a=this.smask?.matte;');
replace('const e=s.data=await this.getImageBytes(g*n*4,{});return', 'const e=s.data=await this.getImageBytes(g*n*4,{});if(this.jpxPremultiplied)PDFImage.undoJPXPreblend(e,...(this.preblendMatte||[0,0,0]));return');
if(process.argv.includes('--check'))assert.equal(source,before,'JPX alpha backport not synchronized');else fs.writeFileSync(path,source);
