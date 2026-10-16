#!/usr/bin/env python3
"""Generate real independent cube solids, not padding. Output must not exist."""
import argparse
import json
from pathlib import Path
import re
import tarfile


def generate(output, mib=204, copies=None, layout='compound'):
    vendor=Path(__file__).resolve().parents[1]/'internal/server/web/vendor/occt-import-js'
    with tarfile.open(vendor/'occt-import-js-0.0.23-source.tgz') as archive:
        original=archive.extractfile('package/test/testfiles/simple-basic-cube/cube.stp').read().decode()
    header,body=original.split('DATA;',1)
    body=body.split('ENDSEC;',1)[0]
    stride=max(map(int,re.findall(r'#(\d+)',body)))+1
    records=re.findall(r'#(\d+)\s*=\s*(.*?);',body,re.S)
    base='\n'.join('#'+ident+'='+value+';' for ident,value in records if int(ident)!=11)
    geometry='\n'.join('#'+ident+'='+value+';' for ident,value in records if 32<=int(ident)<=185)
    total=0;count=0;solids=[]
    with Path(output).open('x',encoding='ascii') as result:
        result.write(header+'DATA;\n')
        while count<(copies or 1<<30) and (copies is not None or total<mib*1024*1024):
            offset=count*stride
            template=body if layout=='roots' else (base if count==0 else geometry)
            part=re.sub(r'#(\d+)',lambda m:'#'+str(int(m[1])+offset),template)
            solids.append('#'+str(32+offset))
            shift=((count%80)*400,((count//80)%80)*400,(count//6400)*400)
            pattern=r"(CARTESIAN_POINT\s*\([^,]*,\s*\()([^()]+)(\)\s*\))"
            def moved(match):
                coordinates=[float(value.strip().replace('D','E')) for value in match[2].split(',')]
                return match[1]+','.join(format(value+shift[i],'.12g')+'.' if float(value+shift[i]).is_integer() else format(value+shift[i],'.12g') for i,value in enumerate(coordinates))+match[3]
            part=re.sub(pattern,moved,part,flags=re.I)
            result.write(part+'\n');total+=len(part)+1;count+=1
        if layout=='compound':
            result.write("\n#11=ADVANCED_BREP_SHAPE_REPRESENTATION('stress',("+','.join(solids)+"),#187);\n")
        result.write('\nENDSEC;\nEND-ISO-10303-21;\n')
    return {'bytes':Path(output).stat().st_size,'solids':count,'expectedTriangles':count*12,'padding':False,'layout':layout,'source':'bundled upstream cube fixture'}


if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('output');parser.add_argument('--mib',type=int,default=204);parser.add_argument('--copies',type=int);parser.add_argument('--layout',choices=['compound','roots'],default='compound')
    args=parser.parse_args();print(json.dumps(generate(args.output,args.mib,args.copies,args.layout)))
