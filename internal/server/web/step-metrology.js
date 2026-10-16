/* CAD picks are stored in original millimetres, independent of camera centering. */
(function(root) {
  'use strict';
  const sub=(a,b)=>a.map((v,i)=>v-b[i]);
  const dot=(a,b)=>a.reduce((sum,v,i)=>sum+v*b[i],0);
  const length=a=>Math.hypot(...a);
  const unit=a=>{const size=length(a);if(!Number.isFinite(size)||size<=1e-12)throw new Error('标定点不能重合。');return a.map(v=>v/size);};
  const cross=(a,b)=>[a[1]*b[2]-a[2]*b[1],a[2]*b[0]-a[0]*b[2],a[0]*b[1]-a[1]*b[0]];
  const valid=p=>Array.isArray(p)&&p.length===3&&p.every(Number.isFinite);
  function frameFromPoints(o,x,xy) {
    if(![o,x,xy].every(valid))throw new Error('坐标必须是有限数值。');
    const axisX=unit(sub(x,o)),plane=unit(sub(xy,o)),normal=cross(axisX,plane);
    if(length(normal)<1e-7)throw new Error('三个标定点不能共线，请重选 XY 平面点。');
    const axisZ=unit(normal),axisY=unit(cross(axisZ,axisX));
    return {origin:[...o],x:axisX,y:axisY,z:axisZ};
  }
  function coordinates(p,frame) {
    if(!frame)return [...p];const d=sub(p,frame.origin);
    return [dot(d,frame.x),dot(d,frame.y),dot(d,frame.z)];
  }
  function distance(a,b,frame) {
    const delta=sub(coordinates(b,frame),coordinates(a,frame));return {distance:length(sub(b,a)),delta};
  }
  function angle(a,b,c) {
    const u=unit(sub(a,b)),v=unit(sub(c,b));return Math.acos(Math.min(1,Math.max(-1,dot(u,v))))*180/Math.PI;
  }
  function create({THREE,scene,camera,canvas,group,center,scale,host,el,t,redraw,filePath}) {
    const tools=el('div','cad-tools'),panel=el('div','cad-readout'),status=el('div','cad-status');
    status.setAttribute('role','status');status.setAttribute('aria-live','polite');
    const hint=el('p','cad-hint',t('预览网格测量，单位 mm；曲面和曲边结果为近似值。'));
    const list=el('ol','cad-records');panel.append(status,hint,list);host.append(tools,panel);
    const overlay=new THREE.Group();scene.add(overlay);
    let mode='point',frame=null,pending=[],records=[],serial=0,down=null,disposed=false;
    const buttons=[];
    const labels={point:'坐标点',distance:'两点距离',angle:'三点夹角',frame:'三点标定'};
    const hints={point:'点击模型表面或顶点读取坐标。',distance:'依次选择两个点，测量距离与轴向差值。',angle:'依次选择三个点，第二个点为角顶点。',frame:'依次选择原点、X 正方向点、XY 平面点。'};
    function inform(message) {status.textContent=t(message);}
    function clearOverlay() {overlay.traverse(obj=>{obj.geometry?.dispose();if(obj.material){for(const material of (Array.isArray(obj.material)?obj.material:[obj.material]))material.dispose();}});overlay.clear();}
    function mark(point,color=0x60d9ff) {
      const world=new THREE.Vector3(...point).sub(center);
      const marker=new THREE.Mesh(new THREE.SphereGeometry(Math.max(scale*0.004,0.001),8,6),new THREE.MeshBasicMaterial({color,depthTest:false}));
      marker.position.copy(world);marker.renderOrder=5;overlay.add(marker);
    }
    function line(points,color=0xffcf6b) {
      const geo=new THREE.BufferGeometry().setFromPoints(points.map(p=>new THREE.Vector3(...p).sub(center)));
      const object=new THREE.Line(geo,new THREE.LineBasicMaterial({color,depthTest:false}));object.renderOrder=4;overlay.add(object);
    }
    const fmt=v=>(Math.abs(v)<0.0000005?0:v).toFixed(3);
    const xyz=p=>coordinates(p,frame).map((v,i)=>'XYZ'[i]+' '+fmt(v)).join(' · ');
    function refresh() {
      clearOverlay();list.replaceChildren();
      for(const record of records) {
        const row=el('li','cad-record');let value=xyz(record.points[0]);
        record.points.forEach(p=>mark(p));
        if(record.kind==='distance') {const d=distance(...record.points,frame);value=fmt(d.distance)+' mm · Δ '+d.delta.map(fmt).join(' / ');line(record.points);}
        if(record.kind==='angle') {value=fmt(angle(...record.points))+'°';line(record.points);}
        row.append(el('strong','',record.label),el('span','',value));list.append(row);
      }
      pending.forEach(p=>mark(p,0xffaa45));
      if(frame) {
        mark(frame.origin,0xffffff);
        for(const [axis,color] of [[frame.x,0xff7676],[frame.y,0x7fdf92],[frame.z,0x7cbaff]])line([frame.origin,frame.origin.map((v,i)=>v+axis[i]*scale*0.2)],color);
      }
      redraw();
    }
    function setMode(next) {mode=next;pending=[];buttons.forEach(button=>button.setAttribute('aria-pressed',String(button.dataset.mode===next)));inform(hints[mode]);refresh();}
    for(const kind of Object.keys(labels)) {const button=el('button','stl-ctrl',t(labels[kind]));button.type='button';button.dataset.mode=kind;button.onclick=()=>setMode(kind);buttons.push(button);tools.append(button);}
    const snap=el('select','cad-pick-mode');snap.setAttribute('aria-label',t('拾取方式'));
    for(const [value,label] of [['vertex','吸附顶点'],['surface','模型表面']]){const option=el('option','',t(label));option.value=value;snap.append(option);}tools.append(snap);
    const reset=el('button','stl-ctrl',t('原始坐标系'));reset.onclick=()=>{frame=null;pending=[];inform('已恢复模型原始坐标系。');refresh();};tools.append(reset);
    const clear=el('button','stl-ctrl',t('清除标记'));clear.onclick=()=>{records=[];pending=[];refresh();inform(hints[mode]);};tools.append(clear);
    const save=el('button','stl-ctrl',t('导出测量'));save.onclick=()=>{
      const data={format:'aide-cad-metrology-v1',file:filePath,unit:'mm',precision:'preview-mesh',frame,records:records.map(record=>({...record,coordinates:record.points.map(p=>coordinates(p,frame)),...(record.kind==='distance'?{measurement:distance(...record.points,frame)}:{}),...(record.kind==='angle'?{degrees:angle(...record.points)}:{})}))};
      const url=URL.createObjectURL(new Blob([JSON.stringify(data,null,2)],{type:'application/json'}));const link=el('a');link.href=url;link.download=filePath.split('/').pop()+'.measurements.json';link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
    };tools.append(save);
    function accept(point) {
      if(disposed||!valid(point))return;
      if(records.length>=100){inform('最多保留 100 条测量，请导出或清除后继续。');return;}
      pending.push([...point]);
      const wanted={point:1,distance:2,angle:3,frame:3}[mode];
      if(pending.length<wanted){inform(t(hints[mode])+' '+pending.length+'/'+wanted);refresh();return;}
      try {
        if(mode==='frame') {frame=frameFromPoints(...pending);inform('自定义坐标系已建立，已有测量按新坐标显示。');}
        else {if(mode==='angle')angle(...pending);records.push({kind:mode,label:'P'+(++serial),points:pending});inform(mode==='point'?xyz(pending[0]):'测量已记录。');}
      } catch(error){inform(error.message);}finally {pending=[];refresh();}
    }
    function pointerDown(event){if(event.button===0)down={x:event.clientX,y:event.clientY,id:event.pointerId};}
    function pointerUp(event) {
      const start=down;down=null;
      if(!start||start.id!==event.pointerId||event.button!==0||Math.hypot(event.clientX-start.x,event.clientY-start.y)>5)return;
      const bounds=canvas.getBoundingClientRect();if(!bounds.width||!bounds.height)return;
      scene.updateMatrixWorld(true);camera.updateMatrixWorld(true);
      const ray=new THREE.Raycaster();ray.setFromCamera(new THREE.Vector2((event.clientX-bounds.left)/bounds.width*2-1,-(event.clientY-bounds.top)/bounds.height*2+1),camera);
      ray.params.Line.threshold=ray.params.Points.threshold=Math.max(scale/500,.001);
      const hit=ray.intersectObjects(group.children,true)[0];if(!hit){inform('未选中模型，请点击可见几何。');return;}
      let world=hit.point;
      if(snap.value==='vertex'&&hit.face) {
        const position=hit.object.geometry.attributes.position;
        world=[hit.face.a,hit.face.b,hit.face.c].map(index=>new THREE.Vector3().fromBufferAttribute(position,index).applyMatrix4(hit.object.matrixWorld)).sort((a,b)=>a.distanceToSquared(hit.point)-b.distanceToSquared(hit.point))[0];
      }
      if(snap.value==='vertex'&&!hit.face&&Number.isInteger(hit.index)) {
        const position=hit.object.geometry.attributes.position,indices=hit.object.isLineSegments?[hit.index,hit.index+1]:[hit.index];
        world=indices.filter(index=>index<position.count).map(index=>new THREE.Vector3().fromBufferAttribute(position,index).applyMatrix4(hit.object.matrixWorld)).sort((a,b)=>a.distanceToSquared(hit.point)-b.distanceToSquared(hit.point))[0]||world;
      }
      accept(world.clone().add(center).toArray());
    }
    const cancel=()=>{down=null;};canvas.addEventListener('pointerdown',pointerDown);canvas.addEventListener('pointerup',pointerUp);canvas.addEventListener('pointercancel',cancel);
    setMode('point');
    return {dispose(){disposed=true;canvas.removeEventListener('pointerdown',pointerDown);canvas.removeEventListener('pointerup',pointerUp);canvas.removeEventListener('pointercancel',cancel);clearOverlay();scene.remove(overlay);tools.remove();panel.remove();}};
  }
  const api={frameFromPoints,coordinates,distance,angle,create};
  if(typeof module==='object'&&module.exports)module.exports=api;else root.AideStepMetrology=api;
})(typeof globalThis==='object'?globalThis:this);
