#!/usr/bin/env python3
"""3340S dimensional reconstruction and uncalibrated axial BEMT. Standard library only.
Plugin stdin: JSON -> JSON; no file writes. CLI --output creates a fresh directory.
"""
import argparse, collections, csv, hashlib, html, io, json, math, pathlib, struct, sys
from datetime import datetime, timezone

STORE = 'https://store.dji.com/kr/product/dji-avata-360-propellers'
SUPPORT = 'https://repair.dji.com/help/content?customId=01700006559&lang=en&re=US&spaceId=17'
METHOD = 'https://ntrs.nasa.gov/citations/20160007767'
DEFAULT = dict(diameter_mm=83.1, pitch_mm=101.6, blades=4, hub_radius_mm=7.0,
               chord_scale=1.0, pitch_scale=1.0, lift_slope_scale=1.0,
               mass_g=455.0, rotors=4, altitude_m=0.0, axial_speed_ms=0.0)
CHORD = [(0,8),(.2,11),(.45,12),(.7,10.5),(.9,7), (1,2.5)]

def config(args):
    p = DEFAULT.copy()
    bounds = {'diameter_mm':(30,200), 'pitch_mm':(10,300),'blades':(2,6),
              'hub_radius_mm':(2,20),'chord_scale':(.6,1.4),'pitch_scale':(.6,1.4),
              'lift_slope_scale':(.6,1.4),'mass_g':(100,2000),'rotors':(1,8),
              'altitude_m':(0,6000),'axial_speed_ms':(0,15)}
    for k,(lo,hi) in bounds.items():
        if k in args:
            v=args[k]
            if isinstance(v,bool) or not isinstance(v,(int,float)) or not math.isfinite(v) or not lo<=v<=hi:
                raise ValueError('Invalid bounded parameter: '+k)
            if k in ('blades','rotors') and int(v)!=v: raise ValueError(k+' must be integer')
            p[k]=int(v) if k in ('blades','rotors') else float(v)
    if p['hub_radius_mm']>=p['diameter_mm']*.25: raise ValueError('Hub too large')
    return p

def lerp_chord(s,p):
    for (a,x),(b,y) in zip(CHORD,CHORD[1:]):
        if s<=b: return (x+(y-x)*(s-a)/(b-a))*p['chord_scale']*.001
    return CHORD[-1][1]*p['chord_scale']*.001

def station(s,p):
    R=p['diameter_mm']*.0005; h=p['hub_radius_mm']*.001
    # Leave tangential clearance at the tip; actual maximum swept radius is scaled below.
    r=h+(R-h)*s
    c=lerp_chord(s,p)
    beta=math.atan(p['pitch_mm']*.001*p['pitch_scale']/(2*math.pi*r))
    return r,c,beta

def atmosphere(h):
    temp=288.15-.0065*h
    pressure=101325*(temp/288.15)**5.25588
    rho=pressure/(287.05287*temp)
    mu=1.716e-5*(temp/273.15)**1.5*(273.15+110.4)/(temp+110.4)
    return rho,mu,math.sqrt(1.4*287.05287*temp)

def polar(alpha,p):
    # Assumed low-Re surrogate; NOT measured 3340S polars, no absolute accuracy claim.
    effective=alpha+math.radians(2)
    cl=max(-1.15,min(1.15,2*math.pi*p['lift_slope_scale']*effective))
    stall=max(0,abs(effective)-math.radians(15))
    cd=.025+.055*cl*cl+1.8*math.sin(stall)**2
    return cl,cd

def bemt(rpm,p,details=False):
    rho,mu,sound=atmosphere(p['altitude_m']); R=p['diameter_mm']*.0005
    h=p['hub_radius_mm']*.001; dr=(R-h)/64; omega=rpm*2*math.pi/60
    V=p['axial_speed_ms']; thrust=torque=0; rows=[]; max_residual=0
    for i in range(64):
        r=h+(i+.5)*dr; s=(r-h)/(R-h); _,chord,beta=station(s,p)
        def element(vi):
            phi=math.atan2(V+vi,omega*r); U=math.hypot(omega*r,V+vi)
            cl,cd=polar(beta-phi,p)
            ss=max(abs(math.sin(phi)),1e-6)
            ft=(2/math.pi)*math.acos(min(1,math.exp(-p['blades']*(R-r)/(2*r*ss))))
            fr=(2/math.pi)*math.acos(min(1,math.exp(-p['blades']*(r-h)/(2*h*ss))))
            F=max(.03,ft*fr)
            dt=.5*rho*U*U*chord*p['blades']*(cl*math.cos(phi)-cd*math.sin(phi))
            dq=.5*rho*U*U*chord*p['blades']*(cl*math.sin(phi)+cd*math.cos(phi))*r
            momentum=4*math.pi*r*rho*F*vi*(V+vi)
            return dt-momentum,dt,dq,phi,U,F
        lo=0.; hi=2*omega*R
        if element(lo)[0]<=0: raise ValueError('Unsupported windmill/unloaded annulus; decrease axial speed or increase rpm')
        if element(hi)[0]>=0: raise ValueError('No induced velocity bracket')
        for _ in range(60):
            mid=(lo+hi)/2
            if element(mid)[0]>0: lo=mid
            else: hi=mid
        vi=(lo+hi)/2; res,dt,dq,phi,U,F=element(vi)
        thrust+=dt*dr; torque+=dq*dr; max_residual=max(max_residual,abs(res))
        rows.append(dict(r_mm=r*1000,chord_mm=chord*1000,twist_deg=math.degrees(beta),
                         inflow_deg=math.degrees(phi),alpha_deg=math.degrees(beta-phi),
                         induced_ms=vi,Re=rho*U*chord/mu,dT_N=dt*dr,dQ_Nm=dq*dr,F=F))
    power=torque*omega; n=rpm/60; D=2*R; area=math.pi*R*R
    ideal=thrust**1.5/math.sqrt(2*rho*area)
    result=dict(rpm=rpm,thrust_N=thrust,thrust_gf=thrust/9.80665*1000,torque_Nm=torque,
                shaft_power_W=power,CT=thrust/(rho*n*n*D**4),CP=power/(rho*n**3*D**5),
                hover_FM=ideal/power if V==0 else None,axial_efficiency=thrust*V/power if V else None,
                tip_Mach=omega*R/sound,blade_pass_Hz=p['blades']*n,
                Re_min=min(x['Re'] for x in rows),Re_max=max(x['Re'] for x in rows),
                momentum_residual_N_per_m=max_residual)
    if details: result['sections']=rows
    return result

def operating_point(p):
    target=p['mass_g']*.001*9.80665/p['rotors']; lo=4000.; hi=60000.
    if bemt(hi,p)['thrust_N']<target: raise ValueError('Target outside rpm bracket')
    for _ in range(35):
        mid=(lo+hi)/2
        try: value=bemt(mid,p)['thrust_N']
        except ValueError: value=0
        if value<target: lo=mid
        else: hi=mid
    result=bemt((lo+hi)/2,p,True)
    result['target_per_rotor_N']=target
    result['total_shaft_power_W']=p['rotors']*result['shaft_power_W']
    return result

def section(s,p):
    r,c,beta=station(s,p); points=[]; m=.02; q=.4; thick=.1
    # Unique closed loop: upper TE -> LE -> lower TE, finite trailing-edge thickness.
    xs=[.5*(1-math.cos(math.pi*i/20)) for i in range(21)]
    for x,sign in [(x,1) for x in xs[::-1]]+[(x,-1) for x in xs[1:]]:
        yt=5*thick*(.2969*math.sqrt(x)-.126*x-.3516*x*x+.2843*x**3-.1015*x**4)
        if x<q: yc=m/q**2*(2*q*x-x*x); slope=2*m/q**2*(q-x)
        else: yc=m/(1-q)**2*((1-2*q)+2*q*x-x*x); slope=2*m/(1-q)**2*(q-x)
        theta=math.atan(slope); xx=x-sign*yt*math.sin(theta); zz=yc+sign*yt*math.cos(theta)
        # Span x; chord in y-z plane. Both chord and thickness rotate about span.
        y=(xx-.25)*c; z=zz*c
        points.append((r*1000, (y*math.cos(beta)-z*math.sin(beta)+.003*s**2)*1000,
                       (y*math.sin(beta)+z*math.cos(beta))*1000))
    return points

def mesh(p,reverse=False):
    rings=[section(i/23,p) for i in range(24)]; count=len(rings[0]); verts=[v for ring in rings for v in ring]; faces=[]
    for i in range(23):
        for j in range(count):
            a=i*count+j; b=i*count+(j+1)%count; c=(i+1)*count+(j+1)%count; d=(i+1)*count+j
            faces.extend([(a,b,c),(a,c,d)])
    # Ear clipping provides non-overlapping caps for the concave cambered profile.
    def cap(indices,flip):
        outline=[((v[1]*math.cos(station(0 if flip else 1,p)[2])+v[2]*math.sin(station(0 if flip else 1,p)[2])),
                  -v[1]*math.sin(station(0 if flip else 1,p)[2])+v[2]*math.cos(station(0 if flip else 1,p)[2])) for v in [verts[k] for k in indices]]
        area=sum(outline[i][0]*outline[(i+1)%count][1]-outline[(i+1)%count][0]*outline[i][1] for i in range(count))
        orient=1 if area>0 else -1; remain=list(range(count)); out=[]
        def cross(a,b,c): return (b[0]-a[0])*(c[1]-a[1])-(b[1]-a[1])*(c[0]-a[0])
        while len(remain)>3:
            found=False
            for z,b in enumerate(remain):
                a=remain[z-1]; c=remain[(z+1)%len(remain)]
                if orient*cross(outline[a],outline[b],outline[c])<=1e-12: continue
                if any(all(orient*cross(outline[x],outline[y],outline[k])>=-1e-12 for x,y in ((a,b),(b,c),(c,a))) for k in remain if k not in (a,b,c)): continue
                out.append((indices[a],indices[b],indices[c])); remain.pop(z); found=True; break
            if not found: raise ValueError('Cap triangulation failed')
        out.append(tuple(indices[k] for k in remain))
        return [(c,b,a) if flip else (a,b,c) for a,b,c in out]
    faces+=cap(list(range(count)),True); faces+=cap(list(range(23*count,24*count)),False)
    # Orient each closed component outward using signed volume.
    vol=sum(dot(verts[a],cross3(verts[b],verts[c]))/6 for a,b,c in faces)
    if vol<0: faces=[(c,b,a) for a,b,c in faces]
    scale=p['diameter_mm']*.5/max(math.hypot(v[0],v[1]) for v in verts)
    verts=[(x*scale,y*scale,z) for x,y,z in verts]
    allv=[]; allf=[]
    for k in range(p['blades']):
        ang=2*math.pi*k/p['blades']; co=math.cos(ang); si=math.sin(ang); offset=len(allv)
        for x,y,z in verts:
            if reverse: y=-y
            allv.append((x*co-y*si,x*si+y*co,z))
        allf.extend(tuple(offset+j for j in ((c,b,a) if reverse else (a,b,c))) for a,b,c in faces)
    return allv,allf

def cross3(a,b): return (a[1]*b[2]-a[2]*b[1],a[2]*b[0]-a[0]*b[2],a[0]*b[1]-a[1]*b[0])
def dot(a,b): return sum(x*y for x,y in zip(a,b))
def normal(a,b,c):
    n=cross3(tuple(y-x for x,y in zip(a,b)),tuple(y-x for x,y in zip(a,c))); length=math.sqrt(dot(n,n))
    return tuple(v/length for v in n) if length else (0,0,0)

def mesh_audit(v,f):
    edges=collections.Counter(); oriented=collections.Counter(); degenerate=0; adjacency=collections.defaultdict(set)
    for a,b,c in f:
        if normal(v[a],v[b],v[c])==(0,0,0): degenerate+=1
        for x,y in ((a,b),(b,c),(c,a)):
            edges[tuple(sorted((x,y)))]+=1; oriented[(x,y)]+=1; adjacency[x].add(y); adjacency[y].add(x)
    unseen=set(adjacency); components=0
    while unseen:
        components+=1; stack=[unseen.pop()]
        while stack:
            for neighbour in adjacency[stack.pop()]:
                if neighbour in unseen: unseen.remove(neighbour); stack.append(neighbour)
    return dict(vertices=len(v),triangles=len(f),edge_multiplicity=dict(collections.Counter(edges.values())),
                boundary_edges=sum(x==1 for x in edges.values()),nonmanifold_edges=sum(x!=2 for x in edges.values()),
                inconsistent_edge_orientation=sum(oriented[(a,b)]!=oriented[(b,a)] for a,b in edges),
                degenerate_triangles=degenerate,closed_components=components,
                signed_volume_mm3=sum(dot(v[a],cross3(v[b],v[c]))/6 for a,b,c in f),
                maximum_swept_diameter_mm=2*max(math.hypot(x,y) for x,y,z in v),
                limitation='Four separate closed blade solids. Hub/interface omitted: dimensions unavailable. No whole-rotor CFD-volume or manufacturing validation.')

def stl(v,f):
    out=['solid avata360_3340s_dimensional_reconstruction_mm']
    for a,b,c in f:
        out.append('facet normal '+' '.join('%.8g'%x for x in normal(v[a],v[b],v[c])))
        out.append('outer loop')
        out.extend('vertex '+' '.join('%.8g'%x for x in v[j]) for j in (a,b,c))
        out+=['endloop','endfacet']
    out.append('endsolid avata360_3340s_dimensional_reconstruction_mm')
    return '\n'.join(out)+'\n'

def csv_text(rows):
    s=io.StringIO(); w=csv.DictWriter(s,fieldnames=list(rows[0])); w.writeheader(); w.writerows(rows); return s.getvalue()

def svg_start(title,subtitle):
    return '<svg xmlns="http://www.w3.org/2000/svg" width="960" height="640" viewBox="0 0 960 640"><rect width="960" height="640" fill="#f6f8fc"/><g font-family="Arial,sans-serif" fill="#203451"><text x="40" y="42" font-size="24">'+html.escape(title)+'</text><text x="40" y="70" font-size="13">'+html.escape(subtitle)+'</text>'

def chart(rows):
    out=svg_start('3340S | static BEMT estimate','Assumed chord / airfoil. No duct, motor or measured polar calibration.')
    for left,top,key,label in [(65,115,'thrust_N','Single rotor thrust [N]'),(535,115,'shaft_power_W','Single rotor shaft power [W]'),(65,380,'hover_FM','Hover figure of merit'),(535,380,'blade_pass_Hz','Blade passing frequency [Hz]')]:
        width=355; height=175; xmax=max(r['rpm'] for r in rows); ymin=0; ymax=max(r[key] for r in rows)*1.15
        out+=f'<text x="{left}" y="{top-12}" font-size="16">{label}</text><path d="M{left} {top}v{height}h{width}" stroke="#a6b5c9" fill="none"/>'
        for i in range(5):
            yy=top+height-height*i/4; value=ymax*i/4
            out+=f'<path d="M{left} {yy}h{width}" stroke="#e1e7ef"/><text x="{left-8}" y="{yy+4}" text-anchor="end" font-size="11">{value:.2g}</text>'
        pts=' '.join(f"{left+width*r['rpm']/xmax:.2f},{top+height-height*r[key]/ymax:.2f}" for r in rows)
        out+=f'<polyline points="{pts}" fill="none" stroke="#397ab5" stroke-width="3"/>'
        for n in (8000,16000,24000,32000):
            out+=f'<text x="{left+width*n/xmax}" y="{top+height+20}" text-anchor="middle" font-size="11">{n}</text>'
        out+=f'<text x="{left+width}" y="{top+height+38}" text-anchor="end" font-size="11">RPM (assumed sweep)</text>'
    return out+'</g></svg>'

def preview(v,f,p):
    out=svg_start('DJI Avata 360 | 3340S dimensional reconstruction','83.1 mm diameter / nominal 101.6 mm pitch / four blades; hub omitted, sections assumed.')
    projected=[(480+5.4*(x*.86-y*.5),340+5.4*(x*.30+y*.52-z*1.2)) for x,y,z in v]
    for a,b,c in sorted(f,key=lambda q:sum(v[j][2]+.2*v[j][1] for j in q)):
        n=normal(v[a],v[b],v[c]); light=max(.15,min(.95,.5+.45*abs(n[2])))
        color=f'rgb({int(35+65*light)},{int(78+82*light)},{int(112+98*light)})'
        pts=' '.join('%.2f,%.2f'%projected[j] for j in (a,b,c))
        out+=f'<polygon points="{pts}" fill="{color}"/>'
    out+='<circle cx="480" cy="340" r="36" fill="#e7ecf3" stroke="#9dabbc" stroke-dasharray="5 4"/><text x="480" y="345" text-anchor="middle" font-size="10">hub TBC</text><text x="40" y="605" font-size="14">Closed blade shells; NOT an exact original CAD or flight replacement.</text>'
    return out+'</g></svg>'

def icon():
    return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="#234766"/><g fill="#a9d9ee">'+''.join(f'<path transform="rotate({a} 32 32)" d="M31 28C25 18 35 6 45 10C45 18 38 25 35 30Z"/>' for a in (0,90,180,270))+'<circle cx="32" cy="32" r="5" fill="#fff"/></g></svg>'

def analyze(p):
    rows=[bemt(rpm,{**p,'axial_speed_ms':0}) for rpm in range(8000,32001,2000)]
    op=operating_point(p); sensitivity=[]
    for key in ('chord_scale','pitch_scale','lift_slope_scale'):
        for scale in (.8,1.2):
            q={**p,key:p[key]*scale}; r=operating_point(q)
            sensitivity.append(dict(parameter=key,factor=scale,rpm=r['rpm'],total_shaft_power_W=r['total_shaft_power_W']))
    return dict(parameters=p,method='Axial annular BEMT with Prandtl tip/root losses; no swirl. Uncalibrated empirical polar.',
                official={'model':'3340S','diameter_mm':83.1,'nominal_pitch_in':4,'nominal_pitch_mm':101.6,
                          'nominal_diameter_in':3.3,'mass_each_g':3.5,'material':'Glass-fiber reinforced nylon','screw':'M2 x 4.8 mm'},
                provenance={'diameter_mass_screw':STORE,'pitch_material':SUPPORT,'blade_count':'Visual count: official product photo (four blades)',
                            'method_reference':METHOD},
                assumptions=['Chord distribution, sweep, NACA 2410-like sections, uniform nominal helical pitch, root radius.',
                             'Isolated rotor, ISA atmosphere; no swirl, duct, fuselage or rotor interference.',
                             'No measured Reynolds-dependent polars, actual RPM, motor efficiency, torque/current limits.',
                             'Official diameter 83.1 mm overrides nominal 3.3 inch = 83.82 mm; pitch 4 inch is nominal.',
                             'Sensitivity cases are parameter variations, NOT a validated confidence interval.',
                             'No absolute dB, endurance or maximum climb prediction. BPF is frequency, not loudness.'],
                operating_point=op,sweep=rows,sensitivity=sensitivity,
                disk_area_m2=math.pi*(p['diameter_mm']*.0005)**2,
                computed_at=datetime.now(timezone.utc).isoformat())

def report(result,audit):
    op=result['operating_point']; p=result['parameters']; rpm=op['rpm']; power=op['total_shaft_power_W']
    return f'''# DJI Avata 360 / 3340S 尺寸约束复刻与气动模型

## 实际完成与边界

本模型采用官方直径、名义螺距与四叶照片外形建立可编辑的尺寸约束模型。不是原厂 CAD 的精确逆向。
桨毂、轴孔及螺丝孔的位置/尺寸没有资料，故**不建接口**。STL 内为四个独立封闭桨叶，不是带桨毂的整体制造实体。

## 来源与更正

- [DJI 商城]({STORE})：3340S，直径约 83.1 mm，单桨约 3.5 g，M2×4.8 mm 螺丝。
- [DJI 维修支持表]({SUPPORT})：3.3×4.0 inch，玻纤增强尼龙。
- 四叶数由官方高清产品图目视确认，非型号名称推断。
- 3.3 inch 换算 83.82 mm 与商品实际约 83.1 mm 有区别；本次模型采用后者。101.6 mm 是 4 inch 名义螺距。
- 更正此前“官方未公开桨型号、直径、螺距、材质、重量、螺丝规格”的说法：这些官方页面已经公开。

## 气动模型及本次计算

[方法参考]({METHOD})。每个环带解方程 dT/dr = 4πρrF vi(V+vi)，二分求诱导速度。
U²=(Ωr)²+(V+vi)²；φ=atan((V+vi)/(Ωr))；α=β−φ；β=atan(P/(2πr))。
叶素升阻力积分获得单桨 T、Q、P=ΩQ，CT=T/(ρn²D⁴)，CP=P/(ρn³D⁵)。64 个中点环带，避免端点重复积分。
F 为 Prandtl 桨尖/桨根损失乘积（最小值 0.03 是数值正则化假设）；没有尾流旋转。
翼型、升阻极线、弦长、后掠全部是假设。不能用官网整机续航倒推出准确极线。

- 输入：{p['mass_g']:.1f} g、{p['rotors']} 旋翼、ISA {p['altitude_m']:.0f} m、轴向来流 {p['axial_speed_ms']:.1f} m/s。
- 等分重量所需单桨推力：{op['target_per_rotor_N']:.4f} N。
- 此假设模型的平衡转速：**{rpm:.0f} RPM**，总轴功率 **{power:.2f} W**。
- 单桨扭矩：{op['torque_Nm']:.6f} N·m；桨尖马赫数：{op['tip_Mach']:.3f}；叶频：{op['blade_pass_Hz']:.1f} Hz。
- 局部 Re：{op['Re_min']:.0f}–{op['Re_max']:.0f}；最大环带动量残差 {op['momentum_residual_N_per_m']:.3g} N/m。
- 这些是**未校准模型结果**，不是 DJI 测试数据。轴功率不等于电池输入功率。
- ±20% 弦长、螺距及升力斜率的单因素敏感性另列 CSV，不是误差条/可信区间。
- 455 g 来源于已下载整机资料，当前参数表是可编辑输入，不把它当作最大有效载荷。

## 几何输出

单位 mm。坐标：桨轴 Z；径向 X；弦长与厚度在 YZ 平面按桨距旋转。反向模型镜像 Y 并反转面绕序。
弦长控制点：8 / 11 / 12 / 10.5 / 7 / 2.5 mm（估算），厚度比 10%、弯度 2%（假设）。
后掠 3 mm（假设）；尖端径向缩放保证最大扫掠直径 {audit['maximum_swept_diameter_mm']:.3f} mm。
封闭边计数：{audit['edge_multiplicity']}；边界边 {audit['boundary_edges']}；非流形边 {audit['nonmanifold_edges']}；退化面 {audit['degenerate_triangles']}。
网格 XY 缩放因子 {audit['mesh_xy_scale']:.8f}，Z 不缩放；`geometry.csv` 为缩放前设计站位并列出该因子。
BEMT 使用名义半径环带与设计弦长，不精确积分缩放后的三角面；二者是同一参数化概念的近似表示，不宣称 CAD 到 CFD 耦合。
封闭组件 {audit['closed_components']} 个；未做自交、强度、动平衡、切片和适配验证。

## 文件与复现

- `rotor_positive.stl` / `rotor_mirrored.stl`：正手性/镜像四叶网格。旋向标记不宣称原厂 CW/CCW 安装对应关系。
- `preview.svg`：模型投影视图；`aerodynamics.svg`：推力、轴功率、FM 与叶频图。
- `geometry.csv` / `performance.csv` / `sections.csv` / `sensitivity.csv`：几何与气动数据。
- `results.json` / `mesh_audit.json` / `inputs.json`：参数、结果与网格记录；`propeller-icon.svg`：四叶能力图标。
- `viewer.html`：离线可旋转模型及图表；`SHA256SUMS`：实际输出指纹。

`python3 -I worker.py --output NEW_DIRECTORY` 可重建；输出目录必须不存在，不覆盖已有文件。
Aide 插件 `propeller_analyze` 只计算；`propeller_reconstruct` 返回文件提案，批准应用后才落盘。
它使用相同 Python 引擎，不需要 numpy/scipy/matplotlib，也不把 BEMT 称作 CFD。

## 不能据此推断的指标

本次不生成绝对噪声 dB、电池续航、最高爬升速度或最高起飞海拔改善。缺少电机/电调曲线、阻力、护罩与实测校准。
官方全消声室噪声与本模型叶频不能定量对齐。真正原厂几何复刻仍需实物测量/扫描或受控工程图。
'''

VIEWER = '''<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>3340S reconstruction</title><style>body{font:15px system-ui;background:#f6f8fc;color:#203451;max-width:1120px;margin:30px auto;padding:0 18px}canvas{width:100%;height:460px;background:linear-gradient(#e5edf7,#fafbfd);border-radius:20px;touch-action:none}button{padding:8px 15px;border:1px solid #a6b5c9;border-radius:8px;background:white}img{max-width:100%}.grid{display:grid;grid-template-columns:repeat(3,1fr);gap:12px}.card{padding:16px;border:1px solid #d6dfec;border-radius:12px;background:white}pre{white-space:pre-wrap}@media(max-width:650px){.grid{grid-template-columns:1fr}}</style><h1>DJI Avata 360 · 3340S</h1><p>尺寸约束复刻 / 未校准 BEMT。官方：83.1 mm、4 inch 名义螺距；照片四叶。翼型、弦长、后掠为估算，桨毂未复刻。</p><div class="grid">__CARDS__</div><p><button id="mirror">镜像手性</button> <button id="reset">重置视角</button> <span>拖动旋转 · 滚轮缩放</span></p><canvas id="model" aria-label="可旋转桨叶三维模型"></canvas><p id="status">四个独立闭合桨叶；不是可直接替换真机的生产图。</p><img src="aerodynamics.svg" alt="气动估算图表"><p><a href="report.md">完整报告与方法</a> · <a href="rotor_positive.stl">STL</a> · <a href="performance.csv">性能 CSV</a> · <a href="inputs.json">输入参数</a></p><script>const data=__MESH__;const canvas=document.querySelector('canvas'),ctx=canvas.getContext('2d');let ax=.7,az=.3,zoom=4,mirrored=false,drag=null;function draw(){const dpr=window.devicePixelRatio||1,w=canvas.clientWidth,h=canvas.clientHeight;canvas.width=w*dpr;canvas.height=h*dpr;ctx.setTransform(dpr,0,0,dpr,0,0);ctx.clearRect(0,0,w,h);const pts=data.v.map(([x,y,z])=>{if(mirrored)y=-y;let a=x*Math.cos(az)-y*Math.sin(az),b=x*Math.sin(az)+y*Math.cos(az);return [w/2+a*zoom,h/2+(b*Math.cos(ax)-z*Math.sin(ax))*zoom,b*Math.sin(ax)+z*Math.cos(ax)]});const faces=[...data.f].sort((a,b)=>a.reduce((s,i)=>s+pts[i][2],0)-b.reduce((s,i)=>s+pts[i][2],0));for(const f of faces){ctx.beginPath();f.forEach((i,k)=>k?ctx.lineTo(pts[i][0],pts[i][1]):ctx.moveTo(pts[i][0],pts[i][1]));ctx.closePath();const t=.45+.3*Math.sin(pts[f[0]][2]/30);ctx.fillStyle=`rgb(${35+65*t},${78+82*t},${112+98*t})`;ctx.fill();}ctx.fillStyle='#203451';ctx.fillText('mm · hub / mounting interface TBC',18,h-20)}canvas.onpointerdown=e=>{drag=[e.clientX,e.clientY];canvas.setPointerCapture(e.pointerId)};canvas.onpointermove=e=>{if(!drag)return;az+=(e.clientX-drag[0])*.01;ax+=(e.clientY-drag[1])*.01;drag=[e.clientX,e.clientY];draw()};canvas.onpointerup=canvas.onpointercancel=()=>drag=null;canvas.onwheel=e=>{e.preventDefault();zoom=Math.max(1,Math.min(8,zoom*Math.exp(-e.deltaY*.001)));draw()};document.querySelector('#mirror').onclick=()=>{mirrored=!mirrored;document.querySelector('#status').textContent=mirrored?'镜像手性；原厂安装标记对应关系尚未验证':'正手性；原厂安装标记对应关系尚未验证';draw()};document.querySelector('#reset').onclick=()=>{ax=.7;az=.3;zoom=4;draw()};new ResizeObserver(draw).observe(canvas);draw();</script>'''

def artifacts(p):
    result=analyze(p); v,f=mesh(p); audit=mesh_audit(v,f)
    vm,fm=mesh(p,True); audit['mirrored']=mesh_audit(vm,fm)
    raw=[v for i in range(24) for v in section(i/23,p)]
    audit['mesh_xy_scale']=p['diameter_mm']*.5/max(math.hypot(x,y) for x,y,z in raw)
    op=result['operating_point']; geometry=[]
    for i in range(24):
        s=i/23; r,c,b=station(s,p); geometry.append(dict(r_mm=r*1000,chord_mm=c*1000,twist_deg=math.degrees(b),sweep_mm=3*s*s,mesh_xy_scale=audit['mesh_xy_scale']))
    js=lambda x: json.dumps(x,ensure_ascii=False,indent=2,allow_nan=False)+'\n'
    cards=''.join(f'<div class="card">{label}<h2>{value}</h2><small>此参数化模型的计算值</small></div>' for label,value in [('单桨平衡推力',f"{op['thrust_N']:.3f} N"),('估算平衡转速',f"{op['rpm']:.0f} RPM"),('总轴功率',f"{op['total_shaft_power_W']:.1f} W")])
    out={'inputs.json':js(p),'results.json':js(result),'mesh_audit.json':js(audit),
         'rotor_positive.stl':stl(v,f),'rotor_mirrored.stl':stl(vm,fm),
         'geometry.csv':csv_text(geometry),'performance.csv':csv_text(result['sweep']),
         'sections.csv':csv_text(op['sections']),'sensitivity.csv':csv_text(result['sensitivity']),
         'aerodynamics.svg':chart(result['sweep']),'preview.svg':preview(v,f,p),
         'propeller-icon.svg':icon(),'report.md':report(result,audit),
         'viewer.html':VIEWER.replace('__CARDS__',cards).replace('__MESH__',json.dumps({'v':v,'f':f},separators=(',',':')))}
    out['SHA256SUMS']=''.join(hashlib.sha256(text.encode()).hexdigest()+'  '+name+'\n' for name,text in sorted(out.items()))
    return result,out

def main():
    parser=argparse.ArgumentParser(); parser.add_argument('--output'); parser.add_argument('--inputs'); opt=parser.parse_args()
    if opt.output:
        args=json.loads(pathlib.Path(opt.inputs).read_text()) if opt.inputs else {}
        p=config(args); result,files=artifacts(p); directory=pathlib.Path(opt.output)
        directory.mkdir(parents=True,exist_ok=False)
        for name,text in files.items(): (directory/name).write_text(text,encoding='utf-8')
        print(json.dumps({'directory':str(directory.resolve()),'files':list(files),'operating_point':{k:v for k,v in result['operating_point'].items() if k!='sections'}},ensure_ascii=False))
    else:
        args=json.load(sys.stdin); p=config(args)
        if args.get('action')=='artifacts':
            result,files=artifacts(p); out={'summary':{k:v for k,v in result.items() if k not in ('sweep','operating_point')},'operating_point':{k:v for k,v in result['operating_point'].items() if k!='sections'},'files':files}
        else: out=analyze(p)
        print(json.dumps(out,ensure_ascii=False,allow_nan=False))
if __name__=='__main__': main()
