(function(root) {
  'use strict';
  function fitPerspective(size,aspect,fov) {
    const norm=Math.hypot(1,1,.8),direction=[1/norm,-1/norm,.8/norm],right=[Math.SQRT1_2,Math.SQRT1_2,0];
    const up=[-direction[2]*right[1],direction[2]*right[0],direction[0]*right[1]-direction[1]*right[0]],vertical=Math.tan(fov*Math.PI/360),horizontal=vertical*Math.max(aspect,.01);
    let distance=.001;
    for(const x of [-size[0]/2,size[0]/2])for(const y of [-size[1]/2,size[1]/2])for(const z of [-size[2]/2,size[2]/2]) {
      const point=[x,y,z],dot=axis=>point.reduce((n,v,i)=>n+v*axis[i],0),depth=dot(direction);
      distance=Math.max(distance,depth+Math.abs(dot(right))/horizontal,depth+Math.abs(dot(up))/vertical);
    }
    return {direction,distance:distance*1.15};
  }
  async function setup(options) {
    const {container,filePath,root:storageRoot,source,viewerSpec,el,t,fileRawUrl,teardownViewer,bindViewerTeardown,detectWebGLContext,ensureThreeStack,ensureVendorScript}=options;
    teardownViewer(container);container.replaceChildren();
    let disposed=false,worker=null,renderer=null,controls=null,resizeObs=null,frame=0,timer=0,metrology=null,phase='download',grid=null,budget=300000,releaseImport=()=>{};
    let group=null;const materials=[];
    const viewer=el('div','step-viewer'),toolbar=el('div','stl-toolbar');
    const title=el('span','stl-info',filePath.split('/').pop()),meta=el('span','stl-meta',t('准备加载 STEP 模型…'));
    meta.setAttribute('role','status');meta.setAttribute('aria-live','polite');
    const reset=el('button','stl-ctrl',t('重置视角')),cancel=el('button','stl-ctrl',t('取消加载')),close=el('button','stl-ctrl','✕');
    close.setAttribute('aria-label',t('关闭预览'));reset.disabled=true;
    const canvas=el('div','stl-canvas');toolbar.append(title,meta,reset,cancel,close);viewer.append(toolbar,canvas);container.append(viewer);
    const rawUrl=fileRawUrl(filePath,storageRoot,source,viewerSpec),started=Date.now();
    const draw=()=>{frame=0;if(disposed||!renderer||document.hidden||!container.isConnected)return;renderer.render(scene,camera);};
    const redraw=()=>{if(!frame&&!disposed)frame=requestAnimationFrame(draw);};
    const resume=()=>{if(document.hidden&&frame){cancelAnimationFrame(frame);frame=0;}else redraw();};
    function teardown() {
      if(disposed)return;disposed=true;clearInterval(timer);worker?.terminate();worker=null;releaseImport();
      if(frame)cancelAnimationFrame(frame);resizeObs?.disconnect();metrology?.dispose();
      document.removeEventListener('visibilitychange',resume);controls?.removeEventListener('change',redraw);controls?.dispose();
      group?.traverse(obj=>obj.geometry?.dispose());group?.clear();group=null;for(const material of materials)material.dispose();materials.length=0;
      if(grid){grid.geometry.dispose();for(const m of (Array.isArray(grid.material)?grid.material:[grid.material]))m.dispose();}
      renderer?.dispose();renderer?.forceContextLoss();renderer=null;controls=null;grid=null;metrology=null;
    }
    bindViewerTeardown(container,teardown);
    const fail=message=>{
      teardown();reset.disabled=true;cancel.hidden=true;
      const notice=el('div','step-error');notice.append(el('strong','',t('STEP 预览不可用')),el('p','',t(message)));
      const retry=el('button','stl-ctrl',t('重新加载'));retry.onclick=()=>setup(options);
      const link=el('a','stl-download',t('下载原始 STEP 文件'));link.href=rawUrl;link.download=filePath.split('/').pop();notice.append(retry,link);canvas.replaceChildren(notice);meta.textContent=t('预览已结束');
    };
    cancel.onclick=()=>fail('已取消加载，解析与传输资源已释放。');
    close.onclick=()=>{teardown();const dialog=container.closest('dialog');if(dialog)dialog.close();else if(document.body.classList.contains('file-view-mode'))history.back();};
    try {await ensureThreeStack();await ensureVendorScript('/step-metrology.js?v=2',()=>!!root.AideStepMetrology);}catch(error){if(!disposed)fail(error.message);return;}
    if(disposed||!container.isConnected)return;
    if(!detectWebGLContext()){fail('当前浏览器未提供可用的 WebGL 图形上下文。');return;}
    const THREE=root.THREE,scene=new THREE.Scene(),camera=new THREE.PerspectiveCamera(45,1,.01,1e9);
    camera.up.set(0,0,1);scene.background=new THREE.Color(0x1a1a2e);
    // Let temporal-dead-zone errors be impossible during early teardown.
    try {
      renderer=new THREE.WebGLRenderer({antialias:true,failIfMajorPerformanceCaveat:false});renderer.outputEncoding=THREE.sRGBEncoding;renderer.setPixelRatio(Math.min(window.devicePixelRatio||1,1.5));canvas.append(renderer.domElement);
      scene.add(new THREE.AmbientLight(0xffffff,.58));
      for(const [color,intensity,position] of [[0xffffff,.92,[3,-4,7]],[0xbad7ff,.3,[-5,4,2]]]) {const light=new THREE.DirectionalLight(color,intensity);light.position.set(...position);scene.add(light);}
      grid=new THREE.GridHelper(10,10,0x52627e,0x303a51);grid.rotation.x=Math.PI/2;scene.add(grid);
      controls=new THREE.OrbitControls(camera,renderer.domElement);controls.enableDamping=false;controls.addEventListener('change',redraw);document.addEventListener('visibilitychange',resume);
      group=new THREE.Group();scene.add(group);const box=new THREE.Box3();
      const resize=()=>{if(disposed)return;const w=canvas.clientWidth,h=canvas.clientHeight;if(w>0&&h>0){camera.aspect=w/h;camera.updateProjectionMatrix();renderer.setSize(w,h);redraw();}};
      resizeObs=new ResizeObserver(resize);resizeObs.observe(canvas);resize();
      const fit=()=>{if(box.isEmpty())return;const bounds=new THREE.Box3().setFromObject(group),center=bounds.getCenter(new THREE.Vector3()),size=bounds.getSize(new THREE.Vector3());const fitted=fitPerspective(size.toArray(),camera.aspect,camera.fov),dist=fitted.distance;camera.position.copy(center).add(new THREE.Vector3(...fitted.direction).multiplyScalar(dist));camera.near=Math.max(dist/10000,.00001);camera.far=dist*100;camera.updateProjectionMatrix();controls.target.copy(center);controls.update();redraw();};
      reset.onclick=fit;
      let triangles=0,segments=0,points=0,meshes=0;
      function addMesh(item) {
        if(item.primitive==='lines'||item.primitive==='points') {
          const positions=item.attributes?.position?.array,color=item.attributes?.color?.array;
          if(!positions?.length)return;
          const geo=new THREE.BufferGeometry();geo.setAttribute('position',new THREE.BufferAttribute(positions,3));
          if(color)geo.setAttribute('color',new THREE.BufferAttribute(color,3));
          const isLine=item.primitive==='lines',material=isLine?new THREE.LineBasicMaterial({color:0xffffff,vertexColors:!!color}):new THREE.PointsMaterial({color:0xffffff,vertexColors:!!color,size:4,sizeAttenuation:false});
          materials.push(material);
          const object=isLine?new THREE.LineSegments(geo,material):new THREE.Points(geo,material);
          if(item.origin)object.position.set(...item.origin);group.add(object);geo.computeBoundingBox();geo.computeBoundingSphere();box.union(geo.boundingBox.clone().translate(object.position));
          if(isLine)segments+=positions.length/6;else points+=positions.length/3;
          meshes+=item.sourceCount||1;return;
        }

        const positions=item.attributes?.position?.array,indices=item.index?.array;
        if(!positions?.length||!indices?.length)return;
        const geo=new THREE.BufferGeometry();geo.setAttribute('position',new THREE.BufferAttribute(positions,3));geo.setIndex(new THREE.BufferAttribute(indices,1));
        if(item.attributes?.normal?.array?.length)geo.setAttribute('normal',new THREE.BufferAttribute(item.attributes.normal.array,3));else geo.computeVertexNormals();
        const vertexColors=!!item.attributes?.color?.array;
        if(vertexColors)geo.setAttribute('color',new THREE.BufferAttribute(item.attributes.color.array,3));
        const color=vertexColors?new THREE.Color(0xffffff):Array.isArray(item.color)?new THREE.Color(...item.color):new THREE.Color(0x9dbfe3);
        const base=new THREE.MeshPhongMaterial({color,vertexColors,specular:0x202633,shininess:48,side:THREE.DoubleSide}),faceMaterials=[base],faces=Array.isArray(item.brep_faces)?item.brep_faces:[];
        let cursor=0;
        for(const face of faces) {
          const first=Math.max(cursor,Math.min(indices.length/3,Number(face.first)||0)),last=Math.max(first,Math.min(indices.length/3,(Number(face.last)||0)+1));
          if(first>cursor)geo.addGroup(cursor*3,(first-cursor)*3,0);
          let index=0;if(Array.isArray(face.color)){index=faceMaterials.length;faceMaterials.push(new THREE.MeshPhongMaterial({color:new THREE.Color(...face.color),specular:0x202633,shininess:48,side:THREE.DoubleSide}));}
          geo.addGroup(first*3,(last-first)*3,index);cursor=last;
        }
        if(cursor<indices.length/3)geo.addGroup(cursor*3,indices.length-cursor*3,0);
        if(faceMaterials.length===1)geo.clearGroups();materials.push(...faceMaterials);
        const mesh=new THREE.Mesh(geo,faceMaterials.length>1?faceMaterials:base);mesh.name=item.name||'';if(item.origin)mesh.position.set(...item.origin);group.add(mesh);geo.computeBoundingBox();geo.computeBoundingSphere();box.union(geo.boundingBox.clone().translate(mesh.position));triangles+=indices.length/3;meshes+=item.sourceCount||1;
      }
      if(navigator.locks?.request) {
        let ready;
        const acquired=new Promise(resolve=>{ready=resolve;});
        navigator.locks.request('aide-step-import',{ifAvailable:true},async lock=>{
          if(!lock){ready(false);return;}
          await new Promise(resolve=>{releaseImport=resolve;ready(true);});
        }).catch(()=>ready(false));
        if(!await acquired){if(!disposed)fail('已有 STEP 模型正在解析，请关闭或等待后重试。');return;}
        if(disposed){releaseImport();return;}
      }
      timer=setInterval(()=>{if(disposed)return;if(Date.now()-started>budget){fail('STEP 加载超时，请拆分模型后重试。');return;}if(phase==='parse')meta.textContent=t('正在解析 STEP 几何…')+' '+Math.floor((Date.now()-started)/1000)+'s';},1000);
      worker=new Worker('/step-import-worker.js?v=4');
      worker.onmessage=async({data})=>{
        if(disposed)return;
        try {
          if(data.error){fail(data.error);return;}
          if(data.kind==='progress') {
            phase=data.phase;
            if(data.total>64*1048576||data.engine==='occt-native')budget=900000;
            if(phase==='tessellate'){meta.textContent=t('正在生成完整曲面…')+(data.faces?' '+data.faces.toLocaleString()+' '+t('曲面'):'');return;}
            if(phase==='download')meta.textContent=t('正在读取 STEP 文件…')+' '+(data.loaded/1048576).toFixed(1)+(data.total?' / '+(data.total/1048576).toFixed(1):'')+' MiB';
            else if(phase==='initialize')meta.textContent=t('正在初始化几何解析器…');
            else meta.textContent=t('正在解析 STEP 几何…');return;
          }
          if(data.kind==='meshes') {
            phase='build';let last=performance.now();
            for(const item of data.meshes){if(disposed)return;addMesh(item);if(performance.now()-last>10){await new Promise(resolve=>setTimeout(resolve,0));last=performance.now();}}
            if(disposed)return;
            meta.textContent=t('正在构建模型…')+' '+meshes+(data.total?' / '+data.total:'');
            if(group.children.length===data.meshes.length)fit();redraw();worker?.postMessage({kind:'ack',id:data.id});return;
          }
          if(data.kind==='complete') {
            if(!meshes){fail('文件中没有可显示的 STEP 实体。');return;}
            clearInterval(timer);worker?.terminate();worker=null;releaseImport();phase='ready';cancel.hidden=true;reset.disabled=false;
            const center=box.getCenter(new THREE.Vector3()),size=box.getSize(new THREE.Vector3()),scale=Math.max(size.x,size.y,size.z)||1;
            group.position.sub(center);grid.position.z=box.min.z-center.z;grid.scale.setScalar(Math.max(.001,scale/10));fit();
            meta.textContent=triangles.toLocaleString()+' '+t('三角面')+(segments?' · '+segments.toLocaleString()+' '+t('线段'):'')+(points?' · '+points.toLocaleString()+' '+t('点'):'')+' · '+size.toArray().map(n=>n.toFixed(2)).join('×')+' mm'+(data.coarse?' · '+t('大模型快速精度'):'')+(data.collapsedFaces?' · '+t('退化面 {0}，已保留边界',data.collapsedFaces):'');
            metrology=root.AideStepMetrology.create({THREE,scene,camera,canvas:renderer.domElement,group,center,scale,host:viewer,el,t,redraw,filePath});
          }
        }catch(error){if(!disposed)fail(error.message);}
      };
      worker.onerror=event=>{if(!disposed)fail(event.message||'STEP 解析器启动失败。');};
      worker.postMessage({url:rawUrl,native:true});
    }catch(error){if(!disposed)fail(error.message);}
  }
  root.AideStepPreview={setup,fitPerspective};
  if(typeof module==='object'&&module.exports)module.exports=root.AideStepPreview;
})(typeof window==='object'?window:globalThis);
