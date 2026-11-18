#!/usr/bin/env python3
"""Exercise the shipped Office engine offline, as its normal runtime user."""
import argparse
import os
from pathlib import Path
import subprocess
import uuid

PROGRAM = r'''
from pathlib import Path
import hashlib, json, shutil, subprocess, tempfile, runpy, sys
from openpyxl import Workbook
from openpyxl.styles import Font, PatternFill, Alignment, Border, Side
from openpyxl.drawing.image import Image as XLImage
from pptx import Presentation
from pptx.util import Inches
from PIL import Image
from pypdf import PdfReader
sys.path.insert(0,"/probe")

def check_render(source, pages):
    before=hashlib.sha256(source.read_bytes()).hexdigest()
    directory=source.parent/('preview-'+source.name)
    directory.mkdir();target=directory/('input'+source.suffix);shutil.copy2(source,target)
    subprocess.run(['python3','/probe/office_preview.py','/usr/bin/soffice',str(target),str(directory)],check=True,timeout=75)
    assert hashlib.sha256(source.read_bytes()).hexdigest()==before
    meta=json.loads((directory/'meta.json').read_text());assert meta['pages']==pages,meta
    reader=PdfReader(directory/'input.pdf');assert len(reader.pages)==pages
    text='\n'.join(page.extract_text() for page in reader.pages)
    assert 'PREVIEW TITLE' in text,text[:300]
    assert 'LINE ONE' in text and 'LINE TWO' in text,text[:300]
    images=sum(len(page.images)for page in reader.pages)
    assert images>=1,'lost embedded image'
    subprocess.run(['pdftoppm','-f','1','-l','1','-singlefile','-scale-to','1600','-png',str(directory/'input.pdf'),str(directory/'page')],check=True,capture_output=True)
    pixels=Image.open(directory/'page.png').convert('RGB')
    colors={'yellow':0,'red':0,'green':0}
    yellow=[]
    for index,(r,g,b) in enumerate(pixels.get_flattened_data() if hasattr(pixels,'get_flattened_data') else pixels.getdata()):
        if r>210 and g>180 and b<100:yellow.append((index%pixels.width,index//pixels.width))
        colors['yellow']+=r>210 and g>180 and b<100
        colors['red']+=r>180 and g<100 and b<100
        colors['green']+=g>140 and r<120 and b<120
    assert all(n>10 for n in colors.values()),colors
    if source.suffix=='.xlsx':
        assert max(x for x,y in yellow)-min(x for x,y in yellow)>pixels.width*.4,'merged fill lost'
        assert max(y for x,y in yellow)-min(y for x,y in yellow)>pixels.width*.045,'automatic merged-title row clipped'
    profile=(directory/'profile/user/registrymodifications.xcu').read_text()
    assert 'MacroSecurityLevel' in profile and '<value>3</value>' in profile
    return {'format':source.suffix,'pages':pages,'images':images,'colorPixels':colors,'sourceUnchanged':True},text

subprocess.run(['python3','/probe/test_xlsx_patch.py'],check=True)
with tempfile.TemporaryDirectory() as temporary:
    root=Path(temporary);image=root/'image.png'
    bitmap=Image.new('RGB',(100,60),'red');bitmap.paste((0,200,0),(50,0,100,60));bitmap.save(image)
    wb=Workbook();ws=wb.active;ws.title='数据'
    ws.merge_cells('A1:D1');ws['A1']='PREVIEW TITLE'
    ws['A1'].font=Font(name='Liberation Sans',bold=True,size=20,color='FF0000')
    ws['A1'].fill=PatternFill('solid',fgColor='FFFF00')
    ws['A1'].alignment=Alignment(horizontal='center',vertical='center')
    ws['A1'].border=Border(bottom=Side(style='thick',color='0000FF'))
    ws['A4']='LINE ONE\nLINE TWO';ws['A4'].alignment=Alignment(wrap_text=True);ws.row_dimensions[4].height=42
    ws.column_dimensions['A'].width=24
    ws['B6']=1234.5;ws['B6'].number_format='$#,##0.00'
    ws['B7']='=SUM(1,2)';ws.add_image(XLImage(image),'F3')
    hidden=wb.create_sheet('Hidden');hidden['A1']='SHOULD STAY HIDDEN';hidden.sheet_state='hidden'
    wb.create_sheet('Empty');wb.security.lockStructure=True;book=root/'book.xlsx';wb.save(book)
    result,text=check_render(book,2);assert 'SHOULD STAY HIDDEN' not in text
    assert '1,234.50' in text,text
    results=[result]
    helper=runpy.run_path('/probe/office_artifact.py')
    edited=root/'edited.xlsx'
    helper['edit_xlsx'](book,{'changes':[{'sheet':'数据','ref':'B8','value':'EDIT ONE\nEDIT TWO'}]},edited)
    from openpyxl import load_workbook
    changed=load_workbook(edited);sheet=changed['数据']
    assert sheet['B8'].value=='EDIT ONE\nEDIT TWO' and sheet['B8'].alignment.wrap_text
    assert 'A1:D1' in sheet.merged_cells and sheet['A1'].font.bold and sheet['A1'].fill.fgColor.rgb.endswith('FFFF00')
    assert len(sheet._images)==1 and sheet.row_dimensions[4].height==42
    changed.close()
    prs=Presentation();slide=prs.slides.add_slide(prs.slide_layouts[6])
    box=slide.shapes.add_textbox(Inches(.5),Inches(.4),Inches(5),Inches(2));box.text='PREVIEW TITLE\nLINE ONE\nLINE TWO'
    from pptx.dml.color import RGBColor
    from pptx.enum.shapes import MSO_SHAPE
    shape=slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, Inches(.5), Inches(3), Inches(3), Inches(1))
    shape.fill.solid();shape.fill.fore_color.rgb=RGBColor(255,255,0)
    slide.shapes.add_picture(str(image),Inches(4),Inches(3),width=Inches(2))
    slide=prs.slides.add_slide(prs.slide_layouts[1]);slide.shapes.title.text='第二页 中文'
    deck=root/'deck.pptx';prs.save(deck);results.append(check_render(deck,2)[0])
    legacy=root/'legacy';legacy.mkdir()
    subprocess.run(['/usr/bin/soffice','-env:UserInstallation='+(root/'legacy-profile').as_uri(),'--headless','--convert-to','ppt:MS PowerPoint 97','--outdir',str(legacy),str(deck)],check=True,capture_output=True,timeout=30)
    results.append(check_render(legacy/'deck.ppt',2)[0])
    from docx import Document
    doc=Document()
    doc.add_heading('PREVIEW TITLE',0)
    doc.add_paragraph('LINE ONE')
    doc.add_paragraph('LINE TWO')
    doc.add_paragraph('中文排版测试')
    doc.add_picture(str(image))
    from docx.oxml import parse_xml
    from docx.oxml.ns import nsdecls
    cell=doc.add_table(rows=1,cols=1).cell(0,0)
    cell.text='VISIBLE WORD TABLE'
    cell._tc.get_or_add_tcPr().append(parse_xml('<w:shd '+nsdecls('w')+' w:fill="FFFF00"/>'))
    doc.add_page_break()
    doc.add_paragraph('SECOND WORD PAGE')
    word=root/'word.docx';doc.save(word)
    rendered,text=check_render(word,2)
    assert all(token in text for token in ['VISIBLE WORD TABLE','SECOND WORD PAGE','中文排版测试']),text
    results.append(rendered)

    def save_as(source, extension, filter_name):
        out=root/extension;out.mkdir()
        subprocess.run(['/usr/bin/soffice','-env:UserInstallation='+(root/'fixture-profile').as_uri(),
                        '--headless','--convert-to',extension+':'+filter_name,
                        '--outdir',str(out),str(source)],check=True,capture_output=True,timeout=30)
        return out/(source.stem+'.'+extension)

    for ext,source,filter_name,pages in [
        ('doc',word,'MS Word 97',2), ('odt',word,'writer8',2),
        ('xls',book,'MS Excel 97',2), ('ods',book,'calc8',2),
        ('odp',deck,'impress8',2),
    ]:
        rendered,text=check_render(save_as(source,ext,filter_name),pages)
        if ext in ('xls','ods'):assert 'SHOULD STAY HIDDEN' not in text
        if ext in ('doc','odt'):assert 'SECOND WORD PAGE' in text and '中文排版测试' in text,text
        results.append(rendered)
    # A renamed or damaged file must fail without leaving a usable preview.
    bad=root/'bad';bad.mkdir();(bad/'input.pptx').write_bytes(b'not a presentation')
    assert subprocess.run(['python3','/probe/office_preview.py','/usr/bin/soffice',str(bad/'input.pptx'),str(bad)]).returncode!=0
    assert not(bad/'meta.json').exists()
    code="import runpy,socket;d=runpy.run_path('/probe/office_preview.py');d['restrict_process']();s=socket.socket(socket.AF_UNIX);s.close();\ntry: socket.socket(socket.AF_INET);raise AssertionError('network socket allowed')\nexcept PermissionError: pass"
    subprocess.run(['python3','-c',code],check=True)
    print(json.dumps({'status':'pass','formats':results,'hiddenSheetsExcluded':True,'emptySheetRetained':True,'networkSocketsDenied':True,'malformedInputRejected':True}))
'''

if __name__ == '__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('image',nargs='?',default=os.environ.get('AIDE_TEST_IMAGE') or 'aide:local')
    args=parser.parse_args()
    source=Path(__file__).resolve().parents[1]/'internal/server/office_preview.py'
    name='aide-office-assets-'+uuid.uuid4().hex[:12]
    try:
        subprocess.run(['docker','run','--rm','--pull=never','--network','none','--name',name,
                        '--workdir','/tmp','--mount',f'type=bind,source={source},target=/probe/office_preview.py,readonly',
                        '--mount',f'type=bind,source={source.parent.parent.parent / "scripts/office/office_artifact.py"},target=/probe/office_artifact.py,readonly',
                        '--mount',f'type=bind,source={source.parent.parent.parent / "scripts/office/xlsx_patch.py"},target=/probe/xlsx_patch.py,readonly',
                        '--mount',f'type=bind,source={source.parent.parent.parent / "scripts/office/test_xlsx_patch.py"},target=/probe/test_xlsx_patch.py,readonly',
                        '--entrypoint','python3',args.image,'-c',PROGRAM],check=True,timeout=180)
    finally:
        subprocess.run(['docker','rm','-f',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,check=False,timeout=10)
