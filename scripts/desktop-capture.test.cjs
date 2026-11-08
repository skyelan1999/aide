'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const src=fs.readFileSync('internal/server/web/desktop-capture.js','utf8');
(async()=>{
 let scope='a',unlocked=true,attached=[],notices=[],focused=0,files;
 const ctx={window:{},Date,crypto:require('node:crypto').webcrypto,Uint8Array,File,atob};vm.createContext(ctx);vm.runInContext(src,ctx);
 const api=ctx.window.AideDesktopCapture;
 api.install({available:()=>unlocked,scope:()=>scope,t:s=>s,notify:s=>notices.push(s),focus:()=>focused++,attach:async(f,options)=>{files=f;attached.push(options);return {added:2};}});
 const png=Buffer.from([137,80,78,71,13,10,26,10,0]).toString('base64');
 const payload=ticket=>({ticket,imageBase64:png,app:'Test App',bundleId:'local.test',windowTitle:'A <window> title',capturedAt:'2026-11-08T00:00:00Z',pixelWidth:1,pixelHeight:1});
 let ticket=api.prepare();assert(ticket);assert(await api.receive(payload(ticket)));assert.equal(files[0].type,'image/png');
 const context=JSON.parse(await files[1].text());assert.equal(context.app,'Test App');assert.equal(context.windowTitle,'A <window> title');assert.equal(focused,1);
 assert.equal(await api.receive(payload(ticket)),false,'gesture ticket is one-use');
 ticket=api.prepare();scope='b';assert.equal(await api.receive(payload(ticket)),false,'session changed before delivery');assert.equal(attached.length,1);
 ticket=api.prepare();unlocked=false;assert.equal(await api.receive(payload(ticket)),false,'locked before delivery');assert.equal(api.prepare(),null);unlocked=true;
 ticket=api.prepare();assert.equal(await api.receive({...payload(ticket),imageBase64:'bad-data'}),false);assert(notices.length>0);assert.equal(attached.length,1);
 ticket=api.prepare();await api.receive(payload(ticket));scope='c';assert.equal(attached.at(-1).active(),false,'upload callback is revoked on navigation');
 console.log('PASS desktop capture: draft PNG/context, one-use ticket, IME-independent delivery, lock/navigation rejection, invalid data and upload scope');
})().catch(e=>{console.error(e);process.exitCode=1;});
