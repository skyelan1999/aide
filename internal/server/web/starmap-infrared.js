/* Node-powered infrared analogy: effective graph degree is an energy proxy.
 * Softened inverse-square dilution and a dust envelope form a scalar field;
 * overlapping contributions add before exposure/tone mapping. No measured data. */
'use strict';
// Integral of u*K(u), 0..6. Normalize the finite envelope so increasing
// its radius spreads the same energy instead of creating additional energy.
const infraredKernelIntegral=(()=>{let sum=0;const step=6/1024;for(let i=0;i<1024;i++){const u=(i+.5)*step;sum+=u*Math.exp(-u/6)/(1+u*u)*step;}return sum;})();
window.AideInfrared={
 energy(degree,distance=1){const d=Number.isFinite(degree)?Math.max(0,degree):0,z=Number.isFinite(distance)?Math.max(.4,distance):1;return (.06+Math.pow(Math.log2(1+d),2))/(z*z);},
 kernel(r,sigma){return Math.exp(-r/(sigma*6))/(1+(r/sigma)**2);},
 density(r,sigma){if(!Number.isFinite(r)||!Number.isFinite(sigma)||r<0||sigma<=0||r>6*sigma)return 0;return this.kernel(r,sigma)/(2*Math.PI*sigma*sigma*infraredKernelIntegral);},
 create({button,note,redraw,onChange,motionEnabled}){
 let enabled=false,mix=0,from=0,started=0,last=-Infinity,gw=0,gh=0,field;
 const surface=document.createElement('canvas'),c=surface.getContext('2d');
 function set(value,announce=true){enabled=!!value;from=mix;started=performance.now();last=-Infinity;if(!motionEnabled())mix=enabled?1:0;button.setAttribute('aria-pressed',String(enabled));note.hidden=!enabled;button.querySelector('span').textContent=enabled?'红外 · 开':'红外';redraw();if(announce)onChange(enabled);}
 button.onclick=()=>set(!enabled);
 function draw(ctx,points,width,height,now){
  const target=enabled?1:0;if(!motionEnabled())mix=target;else{const t=Math.min(1,Math.max(0,(now-started)/420));mix=from+(target-from)*(t*t*(3-2*t));}
  if(mix<.001)return;
  const w=Math.max(1,Math.min(192,Math.ceil(width/10))),h=Math.max(1,Math.min(144,Math.ceil(height/10)));
  if(w!==gw||h!==gh){gw=w;gh=h;surface.width=w;surface.height=h;field=new Float32Array(w*h);last=-Infinity;}
  // Bound raster size, not the number of energy sources: every real node counts.
  if(!motionEnabled()||now-last>=48){
   last=now;field.fill(0);const sx=width/gw,sy=height/gh;
   for(const p of points){if(!Number.isFinite(p.x)||!Number.isFinite(p.y))continue;
    const degree=Math.max(0,p.degree||0),energy=AideInfrared.energy(degree,p.distance)*(p.birth??1),sigma=Math.max(5,Math.min(45,(6+Math.log2(1+degree)*2)*(p.scale||1))),reach=sigma*6;
    if(p.x+reach<0||p.x-reach>width||p.y+reach<0||p.y-reach>height)continue;
    const x0=Math.max(0,Math.floor((p.x-reach)/sx)),x1=Math.min(gw-1,Math.ceil((p.x+reach)/sx)),y0=Math.max(0,Math.floor((p.y-reach)/sy)),y1=Math.min(gh-1,Math.ceil((p.y+reach)/sy));
    for(let y=y0;y<=y1;y++)for(let x=x0;x<=x1;x++){const r=Math.hypot((x+.5)*sx-p.x,(y+.5)*sy-p.y);if(r<=reach)field[y*gw+x]+=energy*AideInfrared.density(r,sigma);}
   }
   const pixels=c.createImageData(gw,gh);
   // Camera exposure is separate from the normalized source energy field.
   for(let i=0;i<field.length;i++){const intensity=1-Math.exp(-field[i]*720),hot=Math.min(1,intensity*1.4),o=i*4;
    pixels.data[o]=Math.round(105+150*hot);pixels.data[o+1]=Math.round(67+130*hot*hot);pixels.data[o+2]=Math.round(73+45*hot*hot);pixels.data[o+3]=Math.round(205*intensity);
   }c.putImageData(pixels,0,0);
  }
  ctx.save();ctx.globalCompositeOperation='screen';ctx.globalAlpha=mix*.72;ctx.imageSmoothingEnabled=true;ctx.drawImage(surface,0,0,width,height);ctx.restore();
 }
 return {set,draw,get enabled(){return enabled},get transitioning(){return Math.abs(mix-(enabled?1:0))>.001}};
}};
