'use strict';
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const {chromium}=require('./browser-runtime/node_modules/playwright');
const web=path.resolve('internal/server/web'),fixtures=JSON.parse(fs.readFileSync('scripts/fixtures/urdf-texture-headers.json','utf8')).images;
const dae=fs.readFileSync('scripts/fixtures/urdf/robot_description/meshes/sensor.dae','utf8');
const tooLarge=Buffer.from(fixtures.find(x=>x.extension==='png'&&!x.animated).base64,'base64');tooLarge.writeUInt32BE(4096,16);tooLarge.writeUInt32BE(4096,20);
function stl(count){const bytes=Buffer.alloc(84+count*50);bytes.writeUInt32LE(count,80);for(let i=0;i<count;i++){const offset=84+i*50;[0,0,1,count>10000?-0.00005:-1,-1,0,count>10000?0.00005:1,-1,0,0,1,0].forEach((v,k)=>bytes.writeFloatLE(v,offset+k*4));}return bytes;}
const models={normal:stl(20),dense:stl(20000)};
function texture(mode){if(mode.includes('large'))return tooLarge;const ext=mode.split('-').pop();return Buffer.from(fixtures.find(x=>x.extension===(ext==='dae'?'gif':ext)&&!!x.animated===mode.includes('animated')).base64,'base64');}
const html=`<!doctype html><html data-theme="dark" data-palette="blue"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/themes/dark/tokens.css"><link rel="stylesheet" href="/themes/light/tokens.css"><link rel="stylesheet" href="/urdf-preview.css"><style>body{margin:0;background:var(--bg);color:var(--text);font:14px system-ui}main{height:100dvh}button{font:inherit}</style><main id="preview"></main><script src="/locales/en.js"></script><script src="/i18n.js"></script><script src="/vendor/three.min.js"></script><script src="/vendor/OrbitControls.js"></script><script src="/vendor/STLLoader.js"></script><script src="/vendor/ColladaLoader.js"></script><script src="/vendor/OBJLoader.js"></script><script src="/vendor/urdf-loader/URDFLoader.js"></script><script src="/urdf-preview.js"></script><script>
let cleanup;const NativeRenderer=THREE.WebGLRenderer;THREE.WebGLRenderer=function(...args){const renderer=new NativeRenderer(...args),render=renderer.render.bind(renderer);renderer.render=(scene,camera)=>{window.testScene=scene;window.testCamera=camera;return render(scene,camera)};return renderer;};
const meshRaycast=THREE.Mesh.prototype.raycast;THREE.Mesh.prototype.raycast=function(...args){window.triangleQueries++;return meshRaycast.apply(this,args);};const allHits=THREE.Raycaster.prototype.intersectObject;THREE.Raycaster.prototype.intersectObject=function(...args){window.allHitQueries++;return allHits.apply(this,args);};
const createURL=URL.createObjectURL.bind(URL);URL.createObjectURL=blob=>{if((window.fixtureMode.includes('large')||window.fixtureMode.includes('animated'))&&blob.size===window.blockedBytes){window.blockedBlobCalls++;throw Error('owned pre-decode Blob trap');}return createURL(blob);};
window.runFixture=async(mode,blockedBytes)=>{cleanup?.();window.testScene=null;window.fixtureMode=mode;window.blockedBytes=blockedBytes;window.triangleQueries=0;window.allHitQueries=0;window.blockedBlobCalls=0;
const textured=mode.startsWith('texture-'),primitive=mode==='texture-large-urdf';const ext=mode.includes('animated')?'gif':mode.includes('large')?'png':mode.split('-').pop();const geometry=primitive?'<box size="1 1 1"/>':'<mesh filename="meshes/'+(textured?'sensor.dae':mode+'.stl')+'"/>';const material=primitive?'<material name="image"><texture filename="meshes/textures/checker.png"/></material>':'';
const xml='<robot name="budget-fixture"><link name="base_link"><visual><geometry>'+geometry+'</geometry>'+material+'</visual></link></robot>';
await AideUrdfPreview.setup({container:document.querySelector('main'),filePath:'robot.urdf',root:'workspace',source:'',xmlText:xml,el:(tag,cls='',text)=>{const e=document.createElement(tag);e.className=cls;if(text!=null)e.textContent=text;return e;},t:aideI18n.t,fileRawUrl:file=>'/raw?file='+encodeURIComponent(file)+'&mode='+mode,teardownViewer:()=>{},bindViewerTeardown:(_host,fn)=>cleanup=fn,ensureThreeStack:async()=>{},ensureVendorScript:async()=>{},detectWebGLContext:()=>true,isCurrent:()=>true});};
window.cleanFixture=()=>cleanup?.();window.textureImages=()=>{const images=[];window.testScene?.traverse(node=>{for(const material of [].concat(node.material||[])){const image=material.map?.image;if(image)images.push({width:image.naturalWidth||image.width,height:image.naturalHeight||image.height});}});return images;};
</script>`;
async function main(){
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
 const server=http.createServer((request,response)=>{const url=new URL(request.url,'http://fixture');if(url.pathname==='/'){response.setHeader('Content-Type','text/html; charset=utf-8');return response.end(html);}if(url.pathname==='/raw'){
  const mode=url.searchParams.get('mode'),file=url.searchParams.get('file');let data;
  if(file.endsWith('.stl'))data=models[mode];else if(file.endsWith('.dae')){const ext=mode.includes('animated')?'gif':mode.includes('large')?'png':mode.split('-').pop();data=Buffer.from(dae.replaceAll('checker.png','checker.'+ext));}else data=texture(mode);
  if(!data){response.writeHead(404);return response.end();}response.setHeader('Content-Length',data.length);return response.end(data);
 }const file=path.resolve(web,'.'+url.pathname);if(!file.startsWith(web+path.sep)||!fs.existsSync(file)){response.writeHead(404);return response.end();}response.setHeader('Content-Type',file.endsWith('.css')?'text/css':'application/javascript');response.end(fs.readFileSync(file));});
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));let browser;const rows=[];
 try{
  browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage','--enable-unsafe-swiftshader']});
  for(const profile of[{width:1280,theme:'light',locale:'en-US'},{width:390,theme:'dark',locale:'zh-CN'}]){
   const context=await browser.newContext({viewport:{width:profile.width,height:1000},locale:profile.locale}),page=await context.newPage(),errors=[];page.setDefaultTimeout(10000);page.on('pageerror',error=>errors.push(error.message));
   try{
    await page.goto('http://127.0.0.1:'+server.address().port);await page.evaluate(theme=>document.documentElement.dataset.theme=theme,profile.theme);
    for(const mode of['normal','dense','texture-normal-png','texture-normal-jpeg','texture-normal-gif','texture-normal-webp','texture-large-dae','texture-large-urdf','texture-animated-dae']){
     console.log(JSON.stringify({stage:'start',mode,...profile}));
     const blocked=mode.includes('large')?tooLarge.length:mode.includes('animated')?texture(mode).length:0;
     await page.evaluate(({mode,blocked})=>runFixture(mode,blocked),{mode,blocked});assert.equal(await page.locator('.urdf-error').count(),0);
     await page.waitForFunction(()=>window.testScene&&document.querySelector('.urdf-canvas canvas'));
     if(mode==='normal'||mode==='dense'){
      const canvas=page.locator('.urdf-canvas canvas'),rect=await canvas.boundingBox();await page.mouse.click(rect.x+rect.width/2,rect.y+rect.height/2);
      await page.waitForFunction(()=>!document.querySelector('.urdf-side').hidden);const stats=await page.evaluate(()=>({triangleQueries,allHitQueries}));assert.equal(stats.allHitQueries,0);
      if(mode==='dense'){
       assert.equal(stats.triangleQueries,0);assert.equal(await page.locator('.urdf-links').evaluate(e=>e.open),true);
       assert.match(await page.locator('.urdf-warnings').innerText(),profile.locale==='en-US'?/selection exceeds preview limits/:/点选复杂度超出预览预算/);
       await page.locator('.urdf-links button').first().click();
      }else assert(stats.triangleQueries>0);
      await page.screenshot({path:path.join(out,mode+'-'+profile.width+'.png')});rows.push({mode,...profile,status:'PASS',...stats});
     }else{
      const rejected=mode.includes('large')||mode.includes('animated');
      if(rejected){await page.locator('.urdf-warnings > summary').click();assert.equal(await page.evaluate(()=>blockedBlobCalls),0);assert.match(await page.locator('.urdf-warnings').innerText(),mode.includes('animated')?/Animated textures|动画纹理/:/Texture pixels|纹理像素/);}
      else await page.waitForFunction(()=>textureImages().some(image=>image.width===8&&image.height===8));
      if(rejected&&mode!=='texture-large-urdf')await page.waitForFunction(()=>textureImages().some(image=>image.width===1&&image.height===1));
      const images=await page.evaluate(()=>textureImages());if(rejected)assert(images.every(image=>image.width<=1&&image.height<=1));
      rows.push({mode,...profile,status:'PASS',rejectedBeforeBlob:rejected,decodedImages:images});
     }
     await page.evaluate(()=>cleanFixture());assert.equal(await page.locator('.urdf-canvas canvas').count(),0);
    }
    assert.deepEqual(errors,[]);
   }catch(error){try{console.log(JSON.stringify({diagnostic:await page.evaluate(()=>({mode:fixtureMode,triangles:triangleQueries,allHits:allHitQueries,warnings:document.querySelector('.urdf-warnings')?.innerText,sideHidden:document.querySelector('.urdf-side')?.hidden,body:document.querySelector('.urdf-meta')?.innerText}))}));await page.screenshot({path:path.join(out,'failure-'+profile.width+'.png'),timeout:3000});}catch{}throw error;}finally{await context.close();}
  }
 }finally{if(browser)await browser.close();await new Promise(resolve=>server.close(resolve));fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(rows,null,2)+'\n');}
 console.log(JSON.stringify({checks:rows.length,rows}));
}
main().catch(error=>{console.error(error);process.exitCode=1;});
