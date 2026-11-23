declare var module: any;
/* Readonly CAD measurements in model coordinates. No drawing data is written. */
(function (host: any) {
  'use strict';
  const NS = 'http://www.w3.org/2000/svg';
  const distance = (a: any, b: any): number => Math.hypot(b[0] - a[0], b[1] - a[1]);
  const apply = (m: any, p: any): any => [m[0]*p[0]+m[2]*p[1]+m[4], m[1]*p[0]+m[3]*p[1]+m[5]];
  function inverse(m: any) {
    if (!Array.isArray(m) || m.length !== 6 || !m.every(Number.isFinite)) throw Error('Invalid CAD transform');
    const d = m[0]*m[3]-m[1]*m[2];
    if (!Number.isFinite(d) || Math.abs(d) < 1e-20) throw Error('Invalid CAD transform');
    return [m[3]/d,-m[1]/d,-m[2]/d,m[0]/d,(m[2]*m[5]-m[3]*m[4])/d,(m[1]*m[4]-m[0]*m[5])/d];
  }
  function calculate(mode: any, p: any) {
    if (!p.length || !p.every((v: any) => v.length === 2 && v.every(Number.isFinite))) return null;
    if (mode === 'coordinate') return {x:p[0][0], y:p[0][1]};
    if (mode === 'distance' && p.length === 2) return {length:distance(p[0],p[1]), dx:Math.abs(p[1][0]-p[0][0]), dy:Math.abs(p[1][1]-p[0][1])};
    if (mode === 'length' && p.length >= 2) return {length:p.slice(1).reduce((sum: number,v: any,i: number) => sum+distance(p[i],v),0)};
    if (mode === 'area' && p.length >= 3) {
      // Translate to the first vertex to avoid cancellation at large WCS origins.
      const q=p.map((v: any)=>[v[0]-p[0][0],v[1]-p[0][1]]);
      let a=0, perimeter=0;
      for (let i=0;i<q.length;i++) {const j=(i+1)%q.length;a+=q[i][0]*q[j][1]-q[j][0]*q[i][1];perimeter+=distance(q[i],q[j]);}
      if (Math.abs(a) < 1e-12) return null;
      // Reject crossed polygons: a shoelace result would otherwise be misleading.
      const cross=(a: any,b: any,c: any): number=>(b[0]-a[0])*(c[1]-a[1])-(b[1]-a[1])*(c[0]-a[0]);
      const on=(a: any,b: any,c: any): boolean=>Math.abs(cross(a,b,c))<1e-10 && c[0]>=Math.min(a[0],b[0])-1e-10 && c[0]<=Math.max(a[0],b[0])+1e-10 && c[1]>=Math.min(a[1],b[1])-1e-10 && c[1]<=Math.max(a[1],b[1])+1e-10;
      for(let i=0;i<q.length;i++)for(let j=i+1;j<q.length;j++) {
        if(j===i+1 || (i===0 && j===q.length-1))continue;
        const a=q[i],b=q[(i+1)%q.length],c=q[j],d=q[(j+1)%q.length];
        if((cross(a,b,c)*cross(a,b,d)<0 && cross(c,d,a)*cross(c,d,b)<0)||on(a,b,c)||on(a,b,d)||on(c,d,a)||on(c,d,b))return null;
      }
      return {area:Math.abs(a)/2, perimeter};
    }
    if (mode === 'angle' && p.length === 3) {
      const a=distance(p[0],p[1]),b=distance(p[2],p[1]);if(a<1e-12||b<1e-12)return null;
      return {angle:Math.acos(Math.max(-1,Math.min(1,((p[0][0]-p[1][0])*(p[2][0]-p[1][0])+(p[0][1]-p[1][1])*(p[2][1]-p[1][1]))/(a*b))))*180/Math.PI};
    }
    if (mode === 'circle' && p.length === 3) {
      const a=p[0],bx=p[1][0]-a[0],by=p[1][1]-a[1],cx=p[2][0]-a[0],cy=p[2][1]-a[1];
      const det=2*(bx*cy-by*cx),scale=Math.max(Math.hypot(bx,by),Math.hypot(cx,cy));
      if(Math.abs(det)<1e-10*scale*scale || scale<1e-12)return null;
      const b=bx*bx+by*by,c=cx*cx+cy*cy,x=(cy*b-by*c)/det,y=(bx*c-cx*b)/det,radius=Math.hypot(x,y);
      return {radius,diameter:2*radius,center:[a[0]+x,a[1]+y]};
    }
    return null;
  }
  function nearest(point: any, candidates: any, tolerance: any): any {
    let found=null,best=tolerance*tolerance;
    for(const p of candidates) {const d=(p[0]-point[0])**2+(p[1]-point[1])**2;if(d<=best){best=d;found=p;}}
    return found;
  }
  // INSUNITS -> millimeters per model unit, including engineering/astronomical units.
  const unitMM: Record<number, number>={1:25.4,2:304.8,3:1609344,4:1,5:10,6:1000,7:1e6,8:0.0000254,9:0.0254,10:914.4,11:1e-7,12:1e-6,13:.001,14:100,15:10000,16:100000,17:1e12,18:149597870700000,19:9.4607304725808e18,20:3.085677581491367e19,21:1200000/3937,22:100000/3937,23:3600000/3937,24:6336000000/3937};
  function mount({svg,canvas,toolbar,modelToSvg,units=0,snapPoints=[],t=(v: any)=>v}: any): any {
    const back=inverse(modelToSvg),snaps=snapPoints.filter((p: any)=>Array.isArray(p)&&p.length===2&&p.every(Number.isFinite)).slice(0,25000).map((p: any)=>apply(modelToSvg,p));
    let mode='pan',points: any[]=[],results: any[]=[],hover: any=null,snapped=false,down: any=null;
    const create=(tag: string,cls?: string,text?: string): any=>{const e=document.createElement(tag);e.className=cls||'';if(text)e.textContent=text;return e;};
    const row=create('div','cad-tools'),tools=create('div','cad-tool-buttons'),unit=create('select','dxf-ctrl');
    const names: Record<string, string>={pan:'平移',distance:'距离',length:'连续长度',angle:'角度',area:'面积 / 周长',circle:'半径 / 直径',coordinate:'坐标'};
    const toolButtons=new Map<any, any>();
    tools.setAttribute('role','toolbar');tools.setAttribute('aria-label',t('测量工具'));
    for(const [key,label]of Object.entries(names)){
      const b=create('button','dxf-ctrl cad-tool',t(label));b.type='button';b.dataset.tool=key;
      b.onclick=()=>chooseTool(key);toolButtons.set(key,b);tools.append(b);
    }
    tools.onkeydown=(e: any): void=>{
      if(!['ArrowLeft','ArrowRight','Home','End'].includes(e.key))return;
      const keys=[...toolButtons.keys()],current=keys.indexOf(mode);
      const index=e.key==='Home'?0:e.key==='End'?keys.length-1:(current+(e.key==='ArrowRight'?1:-1)+keys.length)%keys.length;
      e.preventDefault();chooseTool(keys[index]);toolButtons.get(keys[index]).focus();
    };
    row.append(tools);
    const known=unitMM[units];
    for(const [value,label]of [['model',known?t('图纸单位'):t('图纸单位（未指定）')],['mm','mm'],['cm','cm'],['m','m'],['in','in'],['ft','ft']]){const o=create('option','',label);o.value=value;unit.append(o);}
    unit.setAttribute('aria-label',t('测量单位'));if(units===4)unit.value='mm';
    row.append(unit);
    const snapLabel=create('label','cad-snap'),snap=create('input');snap.type='checkbox';snap.checked=true;snapLabel.append(snap,document.createTextNode(t('捕捉端点 / 圆心')));row.append(snapLabel);
    const button=(label: any,fn: any): any=>{const b=create('button','dxf-ctrl',t(label));b.type='button';b.onclick=fn;row.append(b);return b;};
    const finish=button('完成',()=>commit()),undo=button('撤销',()=>{if(points.length)points.pop();else results.pop();hover=null;draw();}),clear=button('清空测量',()=>{points=[];results=[];hover=null;draw();});
    const status=create('span','cad-status');status.setAttribute('role','status');row.append(status);
    toolbar.after(row);
    const panel=create('ol','cad-results');panel.setAttribute('aria-label',t('测量结果'));canvas.append(panel);
    const overlay=document.createElementNS(NS,'g');overlay.setAttribute('class','cad-overlay');svg.append(overlay);
    canvas.tabIndex=0;canvas.dataset.cadTool='pan';
    const factor=(): any=>unit.value==='model'||!known?1:known/({mm:1,cm:10,m:1000,in:25.4,ft:304.8}[unit.value as 'mm'|'cm'|'m'|'in'|'ft']||1);
    const suffix=(): any=>unit.value==='model'?(known?({1:'in',2:'ft',4:'mm',5:'cm',6:'m'}[units as 1|2|4|5|6]||t('图纸单位')):t('图纸单位')):unit.value;
    const fmt=(v: any): string=>Number(v.toFixed(4)).toLocaleString(undefined,{maximumFractionDigits:4});
    function describe(result: any) {
      const r=result.value,k=factor(),u=suffix(),len=(v: any): string=>fmt(v*k)+' '+u;
      if('angle'in r)return fmt(r.angle)+'°';
      if('area'in r)return t('面积')+' '+fmt(r.area*k*k)+' '+u+'² · '+t('周长')+' '+len(r.perimeter);
      if('radius'in r)return 'R '+len(r.radius)+' · Ø '+len(r.diameter);
      if('x'in r)return 'X '+len(r.x)+' · Y '+len(r.y);
      return len(r.length)+('dx'in r?' · ΔX '+len(r.dx)+' · ΔY '+len(r.dy):'');
    }
    function commit(): void {
      const value=calculate(mode,points);
      if(!value){status.textContent=t('点位不足、重合、共线或多边形交叉，请重新选点。');return;}
      results.push({mode,points:points.map(p=>p.slice()),value});if(results.length>50)results.shift();points=[];hover=null;draw();
    }
    function draw(): void {
      overlay.replaceChildren();panel.replaceChildren();
      const shape=(tag: string,attrs: any): any=>{const e=document.createElementNS(NS,tag);for(const[k,v]of Object.entries(attrs))e.setAttribute(k,v as string);overlay.append(e);return e;};
      const ctm=svg.getScreenCTM(),scale=ctm?Math.hypot(ctm.a,ctm.b):1,rad=3/Math.max(scale,1e-12);
      const paint=(pts: any,closed: any,result?: any): void=>{
        const p=pts.map((v: any)=>apply(modelToSvg,v));
        if(p.length>1)shape(closed?'polygon':'polyline',{points:p.map((v: any)=>v.join(',')).join(' '),class:'cad-line'});
        if(result?.center){const center=apply(modelToSvg,result.center);shape('circle',{cx:center[0],cy:center[1],r:distance(center,apply(modelToSvg,[result.center[0]+result.radius,result.center[1]])),class:'cad-line'});}
        for(const v of p)shape('circle',{cx:v[0],cy:v[1],r:rad,class:'cad-point'});
      };
      results.forEach((r,i)=>{paint(r.points,r.mode==='area',r.value);const li=create('li','',t(names[r.mode])+': '+describe(r));const del=create('button','cad-delete','×');del.title=t('删除此测量');del.setAttribute('aria-label',t('删除此测量'));del.onclick=()=>{results.splice(i,1);draw();};li.append(del);panel.append(li);});
      paint(hover&&points.length?[...points,hover]:points,mode==='area');
      if(hover&&snapped){const p=apply(modelToSvg,hover);shape('rect',{x:p[0]-rad*2,y:p[1]-rad*2,width:rad*4,height:rad*4,class:'cad-snap-mark'});}
      finish.disabled=!['length','area'].includes(mode)||points.length<(mode==='area'?3:2);undo.disabled=!points.length&&!results.length;clear.disabled=!points.length&&!results.length;
      if(mode==='pan')status.textContent='';
      else {const hints: Record<string, string>={distance:'选择两点',length:'逐点选择，点“完成”结束',angle:'选择三点，第二点为顶点',area:'沿边界逐点选择，点“完成”闭合',circle:'选择圆上的三点',coordinate:'选择一点'};status.textContent=t(hints[mode])+' · '+points.length+(known?'':' · '+t('未指定单位；选择单位时按 1:1 解释')) + ' · '+t('二维投影测量');}
      panel.hidden=!results.length;
    }
    function eventPoint(e: any): any {
      const ctm=svg.getScreenCTM();if(!ctm)return null;
      const point=new DOMPoint(e.clientX,e.clientY).matrixTransform(ctm.inverse()),p=[point.x,point.y];
      const close=snap.checked?nearest(p,snaps,10/Math.max(Math.hypot(ctm.a,ctm.b),1e-12)):null;
      snapped=!!close;return apply(back,close||p);
    }
    const onDown=(e: any): void=>{if(mode==='pan'||e.button!==0||e.altKey||e.target.closest('.cad-results'))return;down=[e.clientX,e.clientY];canvas.focus({preventScroll:true});e.preventDefault();};
    const onUp=(e: any): void=>{if(!down)return;const start=down;down=null;if(mode==='pan'||distance(start,[e.clientX,e.clientY])>5)return;const p=eventPoint(e);if(!p||points.length>=256)return;if(points.length&&distance(points[points.length-1],p)<1e-10)return;points.push(p);hover=null;if(points.length===({coordinate:1,distance:2,angle:3,circle:3}[mode as 'coordinate'|'distance'|'angle'|'circle']||Infinity))commit();else draw();};
    const onMove=(e: any): void=>{if(mode==='pan'||e.target.closest('.cad-results'))return;hover=eventPoint(e);draw();};
    const onLeave=(): void=>{hover=null;down=null;draw();};
    const onKey=(e: any): void=>{if(e.target!==canvas)return;if(e.key==='Escape'){points=[];hover=null;draw();e.preventDefault();}else if(e.key==='Backspace'){undo.click();e.preventDefault();}else if(e.key==='Enter'&&!finish.disabled){commit();e.preventDefault();}};
    function chooseTool(key: any): void{
      mode=key;canvas.dataset.cadTool=mode;canvas.classList.toggle('cad-measuring',mode!=='pan');points=[];hover=null;
      for(const [name,b]of toolButtons){const active=name===mode;b.setAttribute('aria-pressed',String(active));b.classList.toggle('is-active',active);b.tabIndex=active?0:-1;}
      draw();
    }
    unit.onchange=draw;
    canvas.addEventListener('pointerdown',onDown);canvas.addEventListener('pointerup',onUp);canvas.addEventListener('pointermove',onMove);canvas.addEventListener('pointerleave',onLeave);canvas.addEventListener('keydown',onKey);
    const observer=new MutationObserver(draw);observer.observe(svg,{attributes:true,attributeFilter:['viewBox']});
    chooseTool('pan');return(): void=>{observer.disconnect();row.remove();panel.remove();overlay.remove();delete canvas.dataset.cadTool;for(const[name,fn]of [['pointerdown',onDown],['pointerup',onUp],['pointermove',onMove],['pointerleave',onLeave],['keydown',onKey]] as any[])canvas.removeEventListener(name,fn);};
  }
  const api={apply,inverse,distance,calculate,nearest,mount,unitMM};
  if(typeof module==='object'&&module.exports)module.exports=api;
  else host.AideCADMeasure=api;
})(typeof window==='object'?window:globalThis);
