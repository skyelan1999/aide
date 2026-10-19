/* Offline URDF preview. Resource paths stay in the current file API scope. */
(() => {
  'use strict';
  const LIMITS=Object.freeze({xml:4*1024*1024,asset:32*1024*1024,texture:8*1024*1024,total:128*1024*1024,links:2048,geometries:4096,assets:512,triangles:3000000});
  function normalizePath(value) {
    const path=String(value||'').replace(/\\/g,'/');
    if(path.startsWith('/')||/^[a-z][a-z0-9+.-]*:/i.test(path)||/[\x00-\x1f?#]/.test(path))throw Error('资源路径必须位于当前工作区内');
    const parts=[];for(const part of path.split('/')){if(!part||part==='.')continue;if(part==='..'){if(!parts.length)throw Error('资源路径超出当前工作区');parts.pop();}else parts.push(part);}return parts.join('/');
  }
  const directory=path=>String(path).split('/').slice(0,-1).join('/');
  const ancestors=path=>{const parts=directory(path).split('/').filter(Boolean),out=[];for(let i=parts.length;i>=0&&out.length<12;i--)out.push(parts.slice(0,i).join('/'));return out;};
  function reference(value) {
    const name=String(value||'').trim();if(!name||/[\x00-\x1f?#\\]/.test(name))throw Error('无效的模型资源引用');
    const match=/^package:\/\/([a-zA-Z0-9_-]+)\/(.+)$/.exec(name);
    const path=decodeURIComponent(match?match[2]:name);
    if(path.startsWith('/')||/^[a-z][a-z0-9+.-]*:/i.test(path)||/[\x00-\x1f?#\\]/.test(path))throw Error('不加载外部网址或绝对文件路径');
    if(match)normalizePath(path);
    return {package:match?.[1]||'',path};
  }
  function candidates(value,base,packages={}) {
    const ref=reference(value);if(!ref.package)return [normalizePath([directory(base),ref.path].filter(Boolean).join('/'))];
    if(Object.prototype.hasOwnProperty.call(packages,ref.package))return [normalizePath([normalizePath(packages[ref.package]),ref.path].filter(Boolean).join('/'))];
    const dirs=ancestors(base),roots=[...dirs.filter(p=>p.split('/').pop()===ref.package),...dirs.map(p=>[p,ref.package].filter(Boolean).join('/'))];
    return [...new Set(roots.map(p=>normalizePath(p+'/'+ref.path)))];
  }
  const direct=(node,name)=>[...node.children].filter(child=>child.localName===name);
  function validateRobot(robot) {
    if(robot?.localName!=='robot')throw Error('文件必须包含 robot 根节点');
    const links=direct(robot,'link'),joints=direct(robot,'joint');
    if(!links.length||links.length>LIMITS.links||joints.length>LIMITS.links)throw Error('机器人连杆数量为空或超出预览预算');
    const named=items=>{const map=new Map();for(const item of items){const name=item.getAttribute('name');if(!name||name in Object.prototype||/["\\]/.test(name)||map.has(name))throw Error('连杆或关节名称缺失／重复');map.set(name,item);}return map;};
    named(direct(robot,'material'));
    const geometryNodes=[...robot.getElementsByTagName('geometry')];let primitiveBudget=0,meshCount=0;
    if(geometryNodes.length>LIMITS.geometries)throw Error('几何元素数量超出预览预算');
    for(const geometry of geometryNodes){const kind=geometry.children[0]?.localName;if(kind==='mesh')meshCount++;else primitiveBudget+=({box:12,cylinder:120,sphere:1800})[kind]||0;}
    if(meshCount>LIMITS.assets||primitiveBudget>LIMITS.triangles)throw Error('几何复杂度超出预览预算');
    const byLink=named(links),byJoint=named(joints),children=new Map(),parents=new Set();
    for(const joint of joints){
      if(direct(joint,'parent').length!==1||direct(joint,'child').length!==1||['origin','axis','limit','mimic'].some(tag=>direct(joint,tag).length>1))throw Error('关节节点存在缺失或重复定义');
      const type=joint.getAttribute('type');if(!['fixed','revolute','continuous','prismatic','planar','floating'].includes(type))throw Error('不支持的关节类型：'+type);
      const parent=direct(joint,'parent')[0]?.getAttribute('link'),child=direct(joint,'child')[0]?.getAttribute('link');
      if(!byLink.has(parent)||!byLink.has(child)||parent===child||parents.has(child))throw Error('关节的父子连杆无效或重复');
      parents.add(child);if(!children.has(parent))children.set(parent,[]);children.get(parent).push(child);
      if(type==='revolute'||type==='prismatic'){const limit=direct(joint,'limit')[0],low=Number(limit?.getAttribute('lower')),high=Number(limit?.getAttribute('upper'));if(!limit||!limit.hasAttribute('lower')||!limit.hasAttribute('upper')||!Number.isFinite(low)||!Number.isFinite(high)||low>high)throw Error('旋转／移动关节需要有效上下限');}
      const axis=direct(joint,'axis')[0];if(axis&&!axis.getAttribute('xyz')?.trim())throw Error('关节轴缺少 xyz');if(axis&&['revolute','continuous','prismatic','planar'].includes(type)&&axis.getAttribute('xyz')?.trim().split(/\s+/).map(Number).every(v=>v===0))throw Error('关节轴不能为零：'+joint.getAttribute('name')+'（'+type+'）');
    }
    const roots=links.filter(link=>!parents.has(link.getAttribute('name')));if(roots.length!==1)throw Error('URDF 必须是一棵连通的机器人树');
    const visited=new Set(),queue=[[roots[0].getAttribute('name'),0]];for(let i=0;i<queue.length;i++){const [name,depth]=queue[i];if(depth>256)throw Error('连杆树深度超出预览预算');if(visited.has(name))throw Error('机器人存在循环关节');visited.add(name);queue.push(...(children.get(name)||[]).map(child=>[child,depth+1]));}if(visited.size!==links.length)throw Error('机器人存在循环或断开的连杆');
    for(const joint of joints){const seen=new Set([joint.getAttribute('name')]);const mimic=direct(joint,'mimic')[0];let target=mimic?.getAttribute('joint');if(mimic&&!target)throw Error('mimic 缺少目标关节');while(target){if(seen.has(target)||!byJoint.has(target))throw Error('mimic 关节引用无效或成环');seen.add(target);if(seen.size>64)throw Error('mimic 关节链超出预览预算');if(!['revolute','continuous','prismatic'].includes(joint.getAttribute('type'))||!['revolute','continuous','prismatic'].includes(byJoint.get(target).getAttribute('type')))throw Error('mimic 仅支持单自由度关节');target=direct(byJoint.get(target),'mimic')[0]?.getAttribute('joint');}}
    for(const node of [robot,...robot.getElementsByTagName('*')]){
      if(node.hasAttribute('name')&&node.getAttribute('name') in Object.prototype)throw Error('模型名称与保留字段冲突');
      if(['visual','collision'].includes(node.localName)&&direct(node,'geometry').length!==1)throw Error('可视／碰撞节点需要一个 geometry');
      const required={box:['size'],sphere:['radius'],cylinder:['radius','length'],color:['rgba']}[node.localName];if(required?.some(attr=>!node.hasAttribute(attr)))throw Error('几何或材质缺少必需属性');
      if(node.localName==='mesh'&&!node.getAttribute('filename')?.trim())throw Error('网格缺少文件名');
      if(node.localName==='geometry'&&([...node.children].length!==1||!['mesh','box','sphere','cylinder'].includes(node.children[0]?.localName)))throw Error('不支持的 URDF 几何类型');
      if(node.prefix==='xacro'||node.localName==='include'||node.getAttribute('quat_xyzw'))throw Error('请先将 Xacro／扩展坐标描述转换为标准 URDF');
      for(const attr of ['xyz','rpy','scale','size','rgba'])if(node.hasAttribute(attr)){const values=node.getAttribute(attr).trim().split(/\s+/).map(Number);if(values.length!==(attr==='rgba'?4:3)||!values.every(Number.isFinite)||(attr==='size'&&values.some(v=>v<=0))||(attr==='scale'&&values.some(v=>v===0))||(attr==='rgba'&&values.some(v=>v<0||v>1)))throw Error('无效的 URDF 数值：'+attr);}
      for(const attr of ['radius','length','lower','upper','multiplier','offset'])if(node.hasAttribute(attr)){const value=Number(node.getAttribute(attr));if(!node.getAttribute(attr).trim()||!Number.isFinite(value)||(['radius','length'].includes(attr)&&value<=0))throw Error('无效的 URDF 数值：'+attr);}
    }
    return {links:links.length,joints:joints.length,packages:[...new Set([...robot.getElementsByTagName('*')].filter(n=>['mesh','texture'].includes(n.localName)&&n.getAttribute('filename')?.trim()).map(n=>reference(n.getAttribute('filename')).package).filter(Boolean))]};
  }
  function parseDocument(text) {
    if(/<!DOCTYPE|<!ENTITY|\$\{|\$\(/i.test(text))throw Error('请先展开 Xacro；预览不执行宏或外部 XML 实体');
    const doc=new DOMParser().parseFromString(text,'application/xml');if(doc.getElementsByTagName('parsererror').length)throw Error('URDF XML 格式错误');validateRobot(doc.documentElement);return doc;
  }
  function applyJointValue(joint,value) {
    if(!Number.isFinite(value))throw Error('关节值必须为有限数字');
    const clamp=(item,v)=>['revolute','prismatic'].includes(item.jointType)?Math.max(item.limit.lower,Math.min(item.limit.upper,v)):v;
    joint.setJointValue(clamp(joint,value));
    // The compatible upstream loader propagates mimic before its own limit clamp.
    // Reconcile descendants from the actual clamped parent position.
    const queue=[joint];for(let i=0;i<queue.length;i++)for(const child of queue[i].mimicJoints||[]){child.setJointValue(clamp(child,queue[i].jointValue[0]*child.multiplier+child.offset));queue.push(child);}
  }
  function prepareStl(data) {
    if(data.byteLength<84)throw Error('STL 数据过短');
    const count=new DataView(data.buffer,data.byteOffset,data.byteLength).getUint32(80,true),expected=84+count*50;
    if(count&&expected<=data.byteLength){if(count>LIMITS.triangles)throw Error('STL 三角面超出预览预算');return expected===data.byteLength?data:data.slice(0,expected);}
    let ascii=false;for(let i=0;i<5;i++)if([115,111,108,105,100].every((b,j)=>data[i+j]===b))ascii=true;
    if(!ascii)throw Error('STL 面数与文件长度不一致');
    const text=new TextDecoder().decode(data);let faces=0,vertices=0;for(const _ of text.matchAll(/\bfacet\b/g)){if(++faces>LIMITS.triangles)throw Error('STL 三角面超出预览预算');}for(const _ of text.matchAll(/\bvertex\b/g))vertices++;
    if(!faces||vertices!==faces*3)throw Error('STL 三角面数据不完整');return data;
  }
  function validateCollada(doc) {
    let total=0;
    const numbers=text=>{const result=[];for(const match of String(text||'').matchAll(/\S+/g)){const n=Number(match[0]);if(!Number.isSafeInteger(n)||n<0)throw Error('DAE 索引无效');result.push(n);if(result.length>LIMITS.triangles*3)throw Error('DAE 索引超出预览预算');}return result;};
    for(const tag of ['triangles','polylist'])for(const primitive of doc.getElementsByTagName(tag)){
      const count=Number(primitive.getAttribute('count'));if(!Number.isSafeInteger(count)||count<0||count>LIMITS.triangles)throw Error('DAE 面数超出预览预算');
      const offsets=direct(primitive,'input').map(n=>Number(n.getAttribute('offset')||0));if(!offsets.length||offsets.some(v=>!Number.isSafeInteger(v)||v<0||v>16))throw Error('DAE 索引步长无效');const stride=Math.max(...offsets)+1;
      let vertices=count*3;
      if(tag==='polylist'){const counts=numbers(direct(primitive,'vcount')[0]?.textContent);if(counts.length!==count||counts.some(v=>v<3))throw Error('DAE 多边形数量不一致');vertices=counts.reduce((n,v)=>n+v,0);total+=counts.reduce((n,v)=>n+v-2,0);}else total+=count;
      if(total>LIMITS.triangles)throw Error('DAE 三角面超出预览预算');
      let actual=0;for(const p of direct(primitive,'p'))for(const match of p.textContent.matchAll(/\S+/g)){const value=Number(match[0]);if(!Number.isSafeInteger(value)||value<0)throw Error('DAE 索引无效');actual++;if(actual>vertices*stride)throw Error('DAE 索引数量不一致');}if(actual!==vertices*stride)throw Error('DAE 面数与索引不一致');
    }
  }
  async function readBounded(response,limit,signal) {
    if(!response.ok)throw Error('HTTP '+response.status);
    if(Number(response.headers.get('Content-Length'))>limit){await response.body?.cancel();throw Error('资源文件超出预览大小预算');}
    if(!response.body) {const bytes=new Uint8Array(await response.arrayBuffer());if(bytes.length>limit)throw Error('资源文件超出预览大小预算');return bytes;}
    const reader=response.body.getReader(),chunks=[];let size=0;
    try{for(;;){if(signal?.aborted)throw new DOMException('Aborted','AbortError');const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>limit)throw Error('资源文件超出预览大小预算');chunks.push(value);}}catch(error){await reader.cancel().catch(()=>{});throw error;}finally{reader.releaseLock();}
    const bytes=new Uint8Array(size);let offset=0;for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.length;}return bytes;
  }
  // URDF RPY uses fixed-axis roll/pitch/yaw: Rz(yaw) Ry(pitch) Rx(roll).
  function readPose(node,reference,THREE) {
    reference.updateWorldMatrix(true,false);node.updateWorldMatrix(true,false);
    const matrix=new THREE.Matrix4().copy(reference.matrixWorld).invert().multiply(node.matrixWorld),position=new THREE.Vector3(),quaternion=new THREE.Quaternion(),scale=new THREE.Vector3();
    matrix.decompose(position,quaternion,scale);const rpy=new THREE.Euler().setFromQuaternion(quaternion,'ZYX');
    return {xyz:position.toArray(),rpy:[rpy.x,rpy.y,rpy.z]};
  }
  async function setup(options) {
    const {container,filePath,root,source,viewerSpec,el,t,fileRawUrl,teardownViewer,bindViewerTeardown,ensureThreeStack,ensureVendorScript,detectWebGLContext}=options;
    teardownViewer(container);container.replaceChildren();
    let disposed=false,renderer=null,controls=null,observer=null,lockObserver=null,contextLost=null,frame=0,robot=null,scene=null,camera=null,triangles=0,bytesUsed=0,finished=0,expected=0;
    const current=()=>!disposed&&options.isCurrent?.()!==false;
    const abort=new AbortController(),blobURLs=new Set(),objects=new Set(),geometries=new Set(),materials=new Set(),textures=new Set(),cache=new Map(),textureCache=new Map(),packageRoots=Object.create(null),pending=[],jobs=[];let active=0;
    const viewer=el('section','urdf-viewer'),toolbar=el('div','urdf-toolbar'),meta=el('span','urdf-meta',t('正在读取 URDF…')),body=el('div','urdf-body'),canvas=el('div','urdf-canvas'),side=el('aside','urdf-side'),jointHost=el('div','urdf-joints'),linkHost=el('details','urdf-links'),warnings=el('details','urdf-warnings'),warningTitle=el('summary','',t('资源问题')),warningList=el('ul'),packageBox=el('details','urdf-packages'),packageFields=el('div');
    meta.setAttribute('role','status');meta.setAttribute('aria-live','polite');canvas.setAttribute('aria-label',t('机器人三维预览'));
    const button=(text,fn)=>{const b=el('button','stl-ctrl',t(text));b.type='button';b.onclick=fn;return b;};
    const reset=button('重置视角',()=>fit()),zero=button('关节归零',()=>{for(const joint of Object.values(robot?.joints||{}))if(joint.type!=='URDFMimicJoint'&&['revolute','continuous','prismatic'].includes(joint.jointType))applyJointValue(joint,0);syncJointValues();draw();}),cancel=button('取消加载',()=>fail(Error(t('已取消加载'))));
    const toggle=(text,fn)=>{const b=button(text,()=>{const value=b.getAttribute('aria-pressed')!=='true';b.setAttribute('aria-pressed',String(value));fn(value);});b.setAttribute('aria-pressed','false');return b;};
    let collisionMode=false,showAxes=false,wireframe=false,showNames=false;const axes=[],nameLabels=[];
    const collision=toggle('碰撞模型',value=>{collisionMode=value;updateVisibility();fit();}),axisButton=toggle('坐标轴',value=>{showAxes=value;axes.forEach(a=>a.visible=value);draw();}),wire=toggle('线框',value=>{wireframe=value;materials.forEach(m=>{if('wireframe'in m)m.wireframe=value;});draw();});
    const posePanel=el('section','urdf-pose'),poseSelect=el('select'),poseFrame=el('select'),poseValues=el('dl','urdf-pose-values'),poseJoint=el('p','urdf-help');posePanel.hidden=true;
    poseSelect.setAttribute('aria-label',t('坐标节点'));poseFrame.setAttribute('aria-label',t('参考坐标系'));
    for(const [value,label]of [['root','机器人根坐标系'],['parent','父节点坐标系']]){const option=el('option','',t(label));option.value=value;poseFrame.append(option);}
    const poseOutputs=[];for(const name of ['X (m)','Y (m)','Z (m)','Roll (rad)','Pitch (rad)','Yaw (rad)']){const value=el('dd','','—');poseValues.append(el('dt','',name),value);poseOutputs.push(value);}
    posePanel.append(el('h3','',t('坐标与角度')),poseSelect,poseFrame,el('p','urdf-help',t('节点原点 · 当前姿态 RPY（固定轴 XYZ），与视角无关')),poseValues,poseJoint);
    const poseButton=toggle('坐标与角度',value=>{posePanel.hidden=!value;updatePose();});poseSelect.onchange=poseFrame.onchange=()=>updatePose();
    const nameLayer=el('div','urdf-name-layer'),nameNotice=el('span','urdf-name-notice',t('仅标注前 80 个连杆，可在坐标面板查看全部节点'));nameLayer.hidden=true;nameNotice.hidden=true;canvas.append(nameLayer);
    const namesButton=toggle('名称标注',value=>{showNames=value;nameLayer.hidden=!value;nameNotice.hidden=!value||Object.keys(robot?.links||{}).length<=80;draw();});
    const download=el('a','stl-download',t('下载 URDF'));download.href=fileRawUrl(filePath,root,source,viewerSpec);download.download=filePath.split('/').pop();
    toolbar.append(el('strong','',filePath.split('/').pop()),reset,zero,collision,axisButton,poseButton,namesButton,wire,cancel,download,nameNotice);warnings.append(warningTitle,warningList);warnings.hidden=true;
    packageBox.append(el('summary','',t('ROS 资源包路径')),el('p','urdf-help',t('自动查找 package.xml 或同名目录；缺失时填写包相对工作区的目录，根目录填写 .')),packageFields);
    linkHost.append(el('summary','',t('连杆层级')));side.append(posePanel,el('h3','',t('关节调节')),el('p','urdf-help',t('长度单位 m · 角度单位 rad')),jointHost,linkHost,packageBox);body.append(canvas,side);viewer.append(toolbar,meta,warnings,body);container.append(viewer);
    const warn=(resource,error)=>{if(disposed)return;const item=el('li');item.textContent=String(resource)+' · '+String(error?.message||error);warningList.append(item);warningTitle.textContent=t('资源问题')+' ('+warningList.children.length+')';warnings.hidden=false;};
    const draw=()=>{if(!frame&&current()&&renderer&&!document.hidden)frame=requestAnimationFrame(()=>{frame=0;if(current()&&!document.hidden&&container.isConnected){updateNameLabels();renderer.render(scene,camera);}});};
    const resume=()=>{if(document.hidden&&frame){cancelAnimationFrame(frame);frame=0;}else draw();};
    const track=object=>{
      const nextGeometries=new Set(),nextMaterials=new Set(),nextTextures=new Set();let count=0;
      object.traverse(node=>{
        if(node.geometry&&!geometries.has(node.geometry)&&!nextGeometries.has(node.geometry)){
          nextGeometries.add(node.geometry);const position=node.geometry.attributes?.position;
          if(node.isMesh&&(!position||position.count<3||(node.geometry.index?.count||position.count)%3!==0))throw Error('网格中没有有效三角面');
          if(position&&node.isMesh){count+=(node.geometry.index?.count||position.count)/3;node.geometry.computeBoundingBox();if(!node.geometry.boundingBox.isEmpty()&&!node.geometry.boundingBox.min.toArray().concat(node.geometry.boundingBox.max.toArray()).every(Number.isFinite))throw Error('模型网格包含无效坐标');}
        }
        for(const material of [].concat(node.material||[])){nextMaterials.add(material);if('wireframe'in material)material.wireframe=wireframe;for(const value of Object.values(material))if(value?.isTexture)nextTextures.add(value);}
      });
      if(triangles+count>LIMITS.triangles)throw Error('三角面数量超出预览预算');
      triangles+=count;objects.add(object);nextGeometries.forEach(g=>geometries.add(g));nextMaterials.forEach(m=>materials.add(m));nextTextures.forEach(x=>textures.add(x));return object;
    };
    function teardown(){if(disposed)return;disposed=true;abort.abort();for(const button of toolbar.querySelectorAll('button'))button.disabled=true;for(const input of side.querySelectorAll('input'))input.disabled=true;for(const item of pending.splice(0))item.reject(new DOMException('Aborted','AbortError'));if(frame)cancelAnimationFrame(frame);observer?.disconnect();lockObserver?.disconnect();document.removeEventListener('visibilitychange',resume);controls?.dispose();if(robot)try{robot.traverse(node=>{if(node.geometry)geometries.add(node.geometry);for(const material of [].concat(node.material||[])){materials.add(material);for(const value of Object.values(material))if(value?.isTexture)textures.add(value);}});}catch{}geometries.forEach(g=>g.dispose());materials.forEach(m=>m.dispose());textures.forEach(x=>x.dispose());blobURLs.forEach(url=>URL.revokeObjectURL(url));if(renderer){if(contextLost)renderer.domElement.removeEventListener('webglcontextlost',contextLost);renderer.dispose();renderer.forceContextLoss();}canvas.replaceChildren();side.replaceChildren();cache.clear();textureCache.clear();objects.clear();geometries.clear();materials.clear();textures.clear();blobURLs.clear();jointInputs.length=0;poseNodes.length=0;nameLabels.length=0;poseSelect.replaceChildren();poseOutputs.length=0;axes.length=0;jobs.length=0;robot=null;scene=null;camera=null;renderer=null;controls=null;}
    bindViewerTeardown(container,teardown);
    if(options.lockElement){lockObserver=new MutationObserver(()=>{if(!current()&&!disposed)fail(Error(t('工作区或锁屏状态已变化，请重新打开预览')));});lockObserver.observe(options.lockElement,{attributes:true,attributeFilter:['hidden','class']});}
    function fail(error){if(disposed)return;teardown();cancel.hidden=true;const notice=el('div','urdf-error');notice.append(el('strong','',t('URDF 预览不可用')),el('p','',String(error?.message||error)),button('重新加载',()=>setup(options)));canvas.replaceChildren(notice);meta.textContent=t('预览已结束');}
    function fit(){if(!robot||!renderer||!current())return;robot.updateMatrixWorld(true);const box=new THREE.Box3();robot.traverseVisible(node=>{if(node.isMesh&&node.geometry){node.geometry.computeBoundingBox();box.union(node.geometry.boundingBox.clone().applyMatrix4(node.matrixWorld));}});if(box.isEmpty())box.set(new THREE.Vector3(-.5,-.5,0),new THREE.Vector3(.5,.5,1));const center=box.getCenter(new THREE.Vector3()),size=box.getSize(new THREE.Vector3()),radius=Math.max(size.length()/2,.01),dist=radius/Math.sin(camera.fov*Math.PI/360)*Math.max(1,1/camera.aspect)*1.25;camera.position.copy(center).add(new THREE.Vector3(1,-1,.75).normalize().multiplyScalar(dist));camera.near=Math.max(dist/10000,.00001);camera.far=Math.max(dist*100,100);camera.updateProjectionMatrix();controls.target.copy(center);controls.update();draw();}
    function updateVisibility(){robot?.traverse(node=>{if(node.isURDFVisual)node.visible=!collisionMode;if(node.isURDFCollider)node.visible=collisionMode;});draw();}
    async function bytes(path,limit=LIMITS.asset){if(!current())throw new DOMException('Aborted','AbortError');path=normalizePath(path);if(!cache.has(path)){if(cache.size>=LIMITS.assets)throw Error('资源文件数量超出预览预算');cache.set(path,(async()=>{const response=await fetch(fileRawUrl(path,root,source,viewerSpec),{signal:abort.signal,credentials:'same-origin',cache:'no-store'}),data=await readBounded(response,limit,abort.signal);if(!current())throw new DOMException('Aborted','AbortError');bytesUsed+=data.length;if(bytesUsed>LIMITS.total)throw Error('模型资源总量超出预览预算');return data;})());}const data=await cache.get(path);if(data.length>limit)throw Error('资源文件超出预览大小预算');return data;}
    if(options.packageRoots)Object.assign(packageRoots,options.packageRoots);
    const manifestCache=new Map();
    async function discoverPackage(pkg,base){if(Object.prototype.hasOwnProperty.call(packageRoots,pkg))return;for(const dir of ancestors(base)){const path=[dir,'package.xml'].filter(Boolean).join('/');if(!manifestCache.has(path))manifestCache.set(path,bytes(path,65536).then(data=>{const doc=new DOMParser().parseFromString(new TextDecoder().decode(data),'application/xml');return doc.documentElement.localName==='package'?direct(doc.documentElement,'name')[0]?.textContent.trim():null;}).catch(()=>null));if(await manifestCache.get(path)===pkg){packageRoots[pkg]=dir;return;}}}
    async function resource(ref,base,limit){const info=reference(ref);if(info.package)await discoverPackage(info.package,base);let last;for(const path of candidates(ref,base,packageRoots)){try{return {path,data:await bytes(path,limit)};}catch(error){if(disposed)throw error;last=error;}}throw Error(t('无法读取资源：')+ref+' · '+String(last?.message||''));}
    async function texture(ref,base){const id=base+'\n'+ref;if(!textureCache.has(id))textureCache.set(id,(async()=>{const {path,data}=await resource(ref,base,LIMITS.texture);if(!current())throw new DOMException('Aborted','AbortError');const ext=path.split('.').pop().toLowerCase(),type={png:'image/png',jpg:'image/jpeg',jpeg:'image/jpeg',webp:'image/webp',gif:'image/gif'}[ext];if(!type)throw Error('仅支持 PNG/JPEG/WebP/GIF 纹理');const url=URL.createObjectURL(new Blob([data],{type}));blobURLs.add(url);return url;})());return textureCache.get(id);}
    const work=task=>new Promise((resolve,reject)=>{pending.push({task,resolve,reject});pump();});
    function pump(){while(active<2&&pending.length&&!disposed){const item=pending.shift();active++;Promise.resolve().then(item.task).then(item.resolve,item.reject).finally(()=>{active--;pump();});}}
    const jointInputs=[],poseNodes=[];
    function updateNameLabels(){if(!showNames||!robot)return;robot.updateMatrixWorld(true);camera.updateMatrixWorld(true);const width=canvas.clientWidth,height=canvas.clientHeight;for(const {node,label}of nameLabels){const point=node.getWorldPosition(new THREE.Vector3()).project(camera),visible=point.z>=-1&&point.z<=1&&Math.abs(point.x)<=1&&Math.abs(point.y)<=1;label.hidden=!visible;if(visible){label.style.left=((point.x+1)*width/2)+'px';label.style.top=((1-point.y)*height/2)+'px';}}}
    function updatePose(){if(disposed||posePanel.hidden||!robot)return;const node=poseNodes[Number(poseSelect.value)];if(!node)return;const pose=readPose(node,poseFrame.value==='parent'?node.parent||robot:robot,THREE),format=value=>(Math.abs(value)<.0000005?0:value).toFixed(6);pose.xyz.concat(pose.rpy).forEach((value,index)=>poseOutputs[index].textContent=format(value));poseJoint.textContent=node.isURDFJoint&&['revolute','continuous','prismatic'].includes(node.jointType)?t('当前关节值')+': '+node.jointValue.map(format).join(', ')+' '+(node.jointType==='prismatic'?'m':'rad'):'';}

    function syncJointValues(){for(const {joint,index,range,value} of jointInputs){range.value=joint.jointValue[index];value.textContent=Number(joint.jointValue[index]).toFixed(3);}updatePose();}
    try{
      const xml=options.xmlText===undefined?new TextDecoder().decode(await bytes(filePath,LIMITS.xml)):String(options.xmlText);if(new TextEncoder().encode(xml).length>LIMITS.xml)throw Error('URDF 文件超出预览大小预算');const doc=parseDocument(xml),info=validateRobot(doc.documentElement);
      await ensureThreeStack();await ensureVendorScript('/vendor/ColladaLoader.js',()=>!!THREE.ColladaLoader);await ensureVendorScript('/vendor/OBJLoader.js',()=>!!THREE.OBJLoader);await ensureVendorScript('/vendor/urdf-loader/URDFLoader.js',()=>!!window.URDFLoader);
      if(!current()||!container.isConnected){teardown();return;}if(!detectWebGLContext())throw Error(t('当前浏览器未提供可用的 WebGL 图形上下文。'));
      scene=new THREE.Scene();camera=new THREE.PerspectiveCamera(42,1,.001,1000);camera.up.set(0,0,1);renderer=new THREE.WebGLRenderer({antialias:true,failIfMajorPerformanceCaveat:false});renderer.setPixelRatio(Math.min(devicePixelRatio||1,1.5));renderer.outputEncoding=THREE.sRGBEncoding;renderer.setClearColor(0x202831);canvas.append(renderer.domElement);contextLost=event=>{event.preventDefault();if(!disposed)fail(Error(t('图形上下文已丢失，请重新加载预览')));};renderer.domElement.addEventListener('webglcontextlost',contextLost);scene.add(new THREE.HemisphereLight(0xffffff,0x556477,1));const light=new THREE.DirectionalLight(0xffffff,.9);light.position.set(3,-4,7);scene.add(light);const grid=track(new THREE.GridHelper(10,20,0x617183,0x354351));grid.rotation.x=Math.PI/2;scene.add(grid);controls=new THREE.OrbitControls(camera,renderer.domElement);controls.enableDamping=false;controls.addEventListener('change',draw);document.addEventListener('visibilitychange',resume);observer=new ResizeObserver(()=>{if(disposed)return;const w=canvas.clientWidth,h=canvas.clientHeight;if(w>0&&h>0){renderer.setSize(w,h);camera.aspect=w/h;camera.updateProjectionMatrix();draw();}});observer.observe(canvas);
      const manager=new THREE.LoadingManager();manager.setURLModifier(url=>{if(!blobURLs.has(url))throw Error('已阻止模型引用外部网址');return url;});manager.onLoad=draw;manager.onError=url=>warn('纹理',Error('纹理加载失败'));const loader=new window.URDFLoader(manager);loader.parseVisual=true;loader.parseCollision=true;loader.workingPath='';
      const assetRefs=new Map(),assetMaterials=new Map(),materialInstances=new Map(),definitions=new Map(direct(doc.documentElement,'material').map(node=>[node.getAttribute('name'),node]));let resourceID=0;
      for(const node of doc.getElementsByTagName('*'))for(const attr of ['xyz','rpy','scale','size','rgba'])if(node.hasAttribute(attr))node.setAttribute(attr,node.getAttribute(attr).trim().split(/\s+/).join(' '));
      for(const node of [...doc.getElementsByTagName('mesh')]){const ref=node.getAttribute('filename');reference(ref);const ext=reference(ref).path.split('.').pop().toLowerCase(),id='aide-mesh-'+(++resourceID)+'.'+ext;assetRefs.set(id,ref);const visual=node.parentElement?.parentElement,declared=visual&&direct(visual,'material')[0];if(declared)assetMaterials.set(id,definitions.get(declared.getAttribute('name'))||declared);node.setAttribute('filename',id);}
      for(const node of [...doc.getElementsByTagName('texture')]){const ref=node.getAttribute('filename');if(!ref)continue;try{node.setAttribute('filename',await texture(ref,filePath));}catch(error){warn(ref,error);node.removeAttribute('filename');}}
      if(!current()){teardown();return;}
      let neutralURL='';
      const neutralImage=()=>{if(!current())throw new DOMException('Aborted','AbortError');if(!neutralURL){neutralURL=URL.createObjectURL(new Blob([Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4//8/AAX+Av4N70a4AAAAAElFTkSuQmCC'),c=>c.charCodeAt(0))],{type:'image/png'}));blobURLs.add(neutralURL);}return neutralURL;};
      const groupMaterial=node=>{
        if(materialInstances.has(node))return materialInstances.get(node);
        const material=new THREE.MeshPhongMaterial(),color=direct(node,'color')[0],image=direct(node,'texture')[0]?.getAttribute('filename');
        if(color){const rgba=color.getAttribute('rgba').trim().split(/\s+/).map(Number);material.color.setRGB(...rgba.slice(0,3)).convertSRGBToLinear();material.opacity=rgba[3];material.transparent=rgba[3]<1;material.depthWrite=!material.transparent;}
        if(image){if(!blobURLs.has(image))throw Error('纹理来源无效');material.map=new THREE.TextureLoader(manager).load(image);material.map.encoding=THREE.sRGBEncoding;textures.add(material.map);}
        materialInstances.set(node,material);materials.add(material);return material;
      };
      const loadMesh=async id=>{const ref=assetRefs.get(id);if(!ref)throw Error('未知的网格引用');const {path,data}=await resource(ref,filePath,LIMITS.asset),ext=path.split('.').pop().toLowerCase();if(!current())throw new DOMException('Aborted','AbortError');let object;
        if(ext==='stl'){const checked=prepareStl(data),geometry=new THREE.STLLoader().parse(checked.buffer);geometry.computeVertexNormals();object=new THREE.Mesh(geometry);}
        else if(ext==='obj'){const text=new TextDecoder().decode(data);if(/^\s*mtllib\s/m.test(text))warn(ref,Error('OBJ 几何已读取；MTL 外部材质未加载'));object=new THREE.OBJLoader().parse(text);}
        else if(ext==='dae'){const text=new TextDecoder().decode(data);if(/<!DOCTYPE|<!ENTITY/i.test(text))throw Error('DAE 不允许外部 XML 实体');const dae=new DOMParser().parseFromString(text,'application/xml');if(dae.getElementsByTagName('parsererror').length)throw Error('DAE XML 格式错误');validateCollada(dae);for(const library of dae.getElementsByTagName('library_images'))for(const image of library.getElementsByTagName('init_from')){const imageRef=image.textContent.trim();try{image.textContent=await texture(imageRef,path);}catch(error){warn(imageRef,error);image.textContent=neutralImage();}}if(!current())throw new DOMException('Aborted','AbortError');object=new THREE.ColladaLoader(manager).parse(new XMLSerializer().serializeToString(dae),'').scene;}
        else throw Error('不支持的网格格式：'+ext+'（支持 STL / DAE / OBJ）');
        let meshCount=0;object.traverse(node=>{if(node.isMesh)meshCount++;});if(!meshCount)throw Error('网格中没有可绘制的三角面');
        if(!object.isMesh&&assetMaterials.has(id)){const material=groupMaterial(assetMaterials.get(id));const replaced=new Set();object.traverse(node=>{if(node.isMesh){[].concat(node.material||[]).forEach(old=>replaced.add(old));node.material=material;}});replaced.forEach(old=>{for(const value of Object.values(old))if(value?.isTexture)value.dispose();old.dispose();});}
        object.traverse(node=>{if(node.isMesh&&node.geometry&&!node.geometry.getAttribute('normal'))node.geometry.computeVertexNormals();});
        if(!current()){object.traverse(n=>{n.geometry?.dispose();for(const m of [].concat(n.material||[]))m.dispose();});throw new DOMException('Aborted','AbortError');}try{return track(object);}catch(error){object.traverse(node=>{if(node.geometry&&!geometries.has(node.geometry))node.geometry.dispose();for(const material of [].concat(node.material||[]))if(!materials.has(material)){for(const value of Object.values(material))if(value?.isTexture&&!textures.has(value))value.dispose();material.dispose();}});throw error;}
      };
      loader.loadMeshCb=(id,_manager,done)=>{expected++;const job=work(()=>loadMesh(id)).then(object=>{if(disposed)return;done(object);track(object);updateVisibility();draw();},error=>{if(!disposed)warn(assetRefs.get(id)||id,error);}).finally(()=>{finished++;if(!disposed)meta.textContent=t('网格加载')+' '+finished+'/'+expected+' · '+info.links+' '+t('连杆')+' · '+info.joints+' '+t('关节');});jobs.push(job);};
      robot=loader.parse(doc);scene.add(robot);track(robot);for(const joint of Object.values(robot.joints))if(joint.type!=='URDFMimicJoint'&&['revolute','continuous','prismatic'].includes(joint.jointType))applyJointValue(joint,0);updateVisibility();
      for(const [name,link]of Object.entries(robot.links)){const row=el('div','urdf-link-row',name);let depth=0;for(let parent=link.parent;parent&&parent!==robot;parent=parent.parent)if(parent.isURDFJoint)depth++;row.style.paddingInlineStart=Math.min(depth*10,120)+'px';linkHost.append(row);const axis=track(new THREE.AxesHelper(.08));axis.visible=showAxes;link.add(axis);axes.push(axis);}
      for(const [name,joint]of Object.entries(robot.joints)){const row=el('div','urdf-joint');row.append(el('strong','',name),el('small','',joint.jointType+(joint.type==='URDFMimicJoint'?' · mimic':'')));jointHost.append(row);if(joint.jointType==='fixed'||joint.type==='URDFMimicJoint')continue;if(['floating','planar'].includes(joint.jointType)){row.append(el('small','',t('仅显示初始姿态，暂不支持调节')));continue;}
        joint.jointValue.forEach((_,index)=>{const angular=['continuous','revolute'].includes(joint.jointType)||(joint.jointType==='floating'&&index>=3),low=['revolute','prismatic'].includes(joint.jointType)?joint.limit.lower:angular?-Math.PI:-1,high=['revolute','prismatic'].includes(joint.jointType)?joint.limit.upper:angular?Math.PI:1;
          const label=el('label','urdf-joint-input'),value=el('output'),range=el('input');range.type='range';range.min=low;range.max=high;range.step='any';range.value=joint.jointValue[index];range.setAttribute('aria-label',name+(joint.jointValue.length>1?' '+index:''));range.oninput=()=>{if(disposed)return;const values=[...joint.jointValue];values[index]=Number(range.value);applyJointValue(joint,values[index]);syncJointValues();draw();};label.append(range,value);row.append(label);jointInputs.push({joint,index,range,value});});}
      for(const [kind,nodes]of [['连杆',robot.links],['关节',robot.joints]]){const group=el('optgroup');group.label=t(kind);for(const [name,node]of Object.entries(nodes)){const option=el('option','',name);option.value=String(poseNodes.length);poseNodes.push(node);group.append(option);}poseSelect.append(group);}
      for(const [name,node]of Object.entries(robot.links).slice(0,80)){const label=button(name,()=>{poseSelect.value=String(poseNodes.indexOf(node));posePanel.hidden=false;poseButton.setAttribute('aria-pressed','true');updatePose();});label.className='urdf-name-label';label.title=t('查看模块坐标')+': '+name;label.setAttribute('aria-label',t('查看模块坐标')+': '+name);nameLayer.append(label);nameLabels.push({node,label});}
      syncJointValues();if(!jointInputs.length)jointHost.append(el('p','urdf-help',t('此模型没有可调节关节')));
      for(const pkg of info.packages){const label=el('label','urdf-package-field'),input=el('input');input.placeholder=t('自动查找');input.value=Object.prototype.hasOwnProperty.call(options.packageRoots||{},pkg)?(options.packageRoots[pkg]||'.'):'';input.setAttribute('aria-label',pkg);label.append(el('span','',pkg),input);packageFields.append(label);}
      const applyPackages=button('应用资源包路径',()=>{try{const next=Object.create(null);for(const input of packageFields.querySelectorAll('input')){if(input.value.trim())next[input.getAttribute('aria-label')]=normalizePath(input.value.trim());}options={...options,packageRoots:next};setup(options);}catch(error){warn(t('ROS 资源包路径'),error);}});packageFields.append(applyPackages);packageBox.hidden=!info.packages.length;
      const w=Math.max(canvas.clientWidth,1),h=Math.max(canvas.clientHeight,1);renderer.setSize(w,h);camera.aspect=w/h;fit();meta.textContent=info.links+' '+t('连杆')+' · '+info.joints+' '+t('关节')+' · '+t('网格加载')+' '+finished+'/'+expected;
      await Promise.all(jobs);cache.clear();textureCache.clear();manifestCache.clear();materialInstances.clear();if(disposed)return;robot.traverse(node=>{for(const m of [].concat(node.material||[]))materials.add(m);});fit();cancel.hidden=true;meta.textContent=info.links+' '+t('连杆')+' · '+info.joints+' '+t('关节')+' · '+Math.round(triangles)+' '+t('三角面')+(warningList.children.length?' · '+t('部分资源未加载'):'');
    }catch(error){if(!disposed)fail(error);}
  }
  window.AideUrdfPreview=Object.freeze({setup,normalizePath,reference,candidates,validateRobot,parseDocument,applyJointValue,readPose,prepareStl,validateCollada,readBounded,LIMITS});
})();
