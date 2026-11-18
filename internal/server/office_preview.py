"""Render a private Office copy with a private, non-interactive LibreOffice profile.

The server owns identity/permissions, byte limits, deadlines and process cleanup.
No document-selected commands, profile, paths or export options are accepted.
"""
import ctypes
import json
import os
from pathlib import Path
import platform
import resource
import subprocess
import sys
import xml.etree.ElementTree as ET
import zipfile


# Use the distribution's Python/UNO ABI, separately from the pinned application
# Python. Only automatic row heights are recalculated; explicit heights stay put.
OFFICE_WORKER = r'''
import json, pathlib, subprocess, sys, time, uuid
import uno
from com.sun.star.beans import PropertyValue
def props(**values):
    return tuple(PropertyValue(Name=k, Value=v) for k,v in values.items())
converter, source, profile, output, spec_path, metadata = sys.argv[1:]
spec = json.loads(pathlib.Path(spec_path).read_text())
name='aide_'+uuid.uuid4().hex
process=subprocess.Popen([converter,'-env:UserInstallation='+profile,'--headless','--nologo','--nodefault','--nofirststartwizard','--norestore','--accept=pipe,name='+name+';urp;StarOffice.ComponentContext'],stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
doc=None;measurement_doc=None
try:
    local=uno.getComponentContext()
    resolver=local.ServiceManager.createInstanceWithContext('com.sun.star.bridge.UnoUrlResolver',local)
    deadline=time.monotonic()+10
    while True:
        try: context=resolver.resolve('uno:pipe,name='+name+';urp;StarOffice.ComponentContext');break
        except Exception:
            if process.poll() is not None or time.monotonic()>deadline:raise
            time.sleep(.05)
    desktop=context.ServiceManager.createInstanceWithContext('com.sun.star.frame.Desktop',context)
    kind = spec['kind']
    doc = desktop.loadComponentFromURL(source, '_blank', 0, props(
        Hidden=True, ReadOnly=kind != 'calc' or not spec['repairXlsx'],
        UpdateDocMode=0, MacroExecutionMode=0))
    services = {'writer': 'com.sun.star.text.TextDocument',
                'calc': 'com.sun.star.sheet.SpreadsheetDocument',
                'impress': 'com.sun.star.presentation.PresentationDocument'}
    if doc is None or not doc.supportsService(services[kind]):
        raise ValueError('document does not match the selected Office format')
    sheets = []
    if kind == 'calc':
        if not 1 <= doc.Sheets.Count <= 500:
            raise ValueError('worksheet count limit')
        for i in range(doc.Sheets.Count):
            sheet = doc.Sheets.getByIndex(i)
            sheets.append({'name': sheet.Name, 'hidden': not sheet.IsVisible})
    if spec['repairXlsx']:
        # Some older OOXML workbooks import string cells but export blank text.
        # A private native-format roundtrip materializes the complete layout,
        # preserving drawings (including WMF) instead of rebuilding the workbook.
        native=pathlib.Path(spec_path).parent/'layout.ods'
        doc.storeToURL(native.as_uri(),props(FilterName='calc8',Overwrite=True))
        doc.close(True)
        doc=desktop.loadComponentFromURL(native.as_uri(),'_blank',0,props(Hidden=True,ReadOnly=False,UpdateDocMode=0,MacroExecutionMode=0))
        if doc is None:raise ValueError('native workbook layout could not be loaded')
        measurement_doc=desktop.loadComponentFromURL('private:factory/scalc','_blank',0,props(Hidden=True,MacroExecutionMode=0))
        scratch=measurement_doc.Sheets.getByIndex(0)
        measurement=scratch.getCellByPosition(0,0)
        font_properties=('CharFontName','CharHeight','CharWeight','CharPosture','CharFontNameAsian','CharHeightAsian','CharWeightAsian','CharPostureAsian','IsTextWrapped','ShrinkToFit','RotateAngle','HoriJustify','VertJustify')
        for sheet_name, layout in spec['rows'].items():
            sheet=doc.Sheets.getByName(sheet_name)
            auto_rows = set(layout['rows'])
            for index in layout['rows']:
                sheet.Rows.getByIndex(index).OptimalHeight=True
            for reference in layout['merges']:
                area=sheet.getCellRangeByName(reference)
                address=area.RangeAddress
                if address.StartRow!=address.EndRow or address.StartRow not in auto_rows:continue
                row=sheet.Rows.getByIndex(address.StartRow)
                anchor=sheet.getCellByPosition(address.StartColumn,address.StartRow)
                # Measure in a disposable sheet. Unmerging the original range would
                # alter continuation fills/borders in LibreOffice's model.
                measurement.String=anchor.String
                for key in font_properties:
                    measurement.setPropertyValue(key,anchor.getPropertyValue(key))
                scratch.Columns.getByIndex(0).Width=area.Size.Width
                scratch.Rows.getByIndex(0).Height=500
                scratch.Rows.getByIndex(0).OptimalHeight=True
                minimum=int((max(anchor.CharHeight,anchor.CharHeightAsian)*1.25+2)*35.2778*(anchor.String.count('\n')+1))
                height=max(row.Height,scratch.Rows.getByIndex(0).Height,minimum)
                row.OptimalHeight=False;row.Height=height
        measurement_doc.close(True);measurement_doc=None
    pdf_options = {'UseLosslessCompression': True, 'ExportBookmarks': True}
    if kind == 'calc':
        pdf_options['SinglePageSheets'] = True
    if kind == 'impress':
        pdf_options['ExportHiddenSlides'] = True
    doc.storeToURL(output, props(FilterName=kind+'_pdf_Export', Overwrite=True,
        FilterData=uno.Any('[]com.sun.star.beans.PropertyValue', props(**pdf_options))))
    pathlib.Path(metadata).write_text(json.dumps(sheets))
    doc.close(True);doc=None;desktop.terminate()
finally:
    if measurement_doc is not None:measurement_doc.dispose()
    if doc is not None:doc.dispose()
    try:process.wait(timeout=3)
    except subprocess.TimeoutExpired:process.terminate();process.wait(timeout=3)
'''


def automatic_rows(archive, workbook):
    import posixpath
    relation_ns = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships'
    relationships = ET.fromstring(archive.read('xl/_rels/workbook.xml.rels'))
    targets = {item.attrib['Id']: item.attrib['Target'] for item in relationships
               if item.attrib.get('TargetMode') != 'External'}
    rows, count = {}, 0
    for sheet in workbook.findall('{*}sheets/{*}sheet'):
        target = targets[sheet.attrib['{' + relation_ns + '}id']]
        target = posixpath.normpath(posixpath.join('xl', target)) if not target.startswith('/') else target[1:]
        if not target.startswith('xl/worksheets/'):
            continue
        tree = ET.fromstring(archive.read(target))
        defaults=tree.find('{*}sheetFormatPr')
        fixed_default=defaults is not None and defaults.attrib.get('customHeight') in ('1','true')
        indices = [int(row.attrib['r']) - 1 for row in tree.findall('{*}sheetData/{*}row')
                   if (not fixed_default or row.attrib.get('customHeight') in ('0','false')) and row.attrib.get('customHeight') not in ('1', 'true') and row.attrib.get('hidden') not in ('1', 'true')]
        count += len(indices)
        if any(index < 0 or index >= 1048576 for index in indices):
            raise ValueError('invalid worksheet row')
        if count > 20000:
            return {}  # Preserve imported row geometry when repair is too expensive.
        rows[sheet.attrib['name']] = {'rows': indices, 'merges': [item.attrib['ref'] for item in tree.findall('{*}mergeCells/{*}mergeCell')]}
    return rows


def restrict_process():
    # Legacy vector-image imports reserve more virtual address space than their
    # resident memory. Keep a bounded 3 GiB address space for the native roundtrip.
    resource.setrlimit(resource.RLIMIT_AS, (3072 << 20, 3072 << 20))
    resource.setrlimit(resource.RLIMIT_CPU, (240, 240))
    resource.setrlimit(resource.RLIMIT_NOFILE, (256, 256))
    # LibreOffice uses local Unix sockets. Deny all other socket families,
    # including DNS/HTTP external links, in this process and its descendants.
    if sys.platform.startswith('linux'):
        number = {'aarch64': 198, 'x86_64': 41}.get(platform.machine())
        if number is None:
            raise ValueError('unsupported renderer platform')
        class Filter(ctypes.Structure):
            _fields_ = [('code', ctypes.c_ushort), ('jt', ctypes.c_ubyte),
                        ('jf', ctypes.c_ubyte), ('k', ctypes.c_uint)]
        class Program(ctypes.Structure):
            _fields_ = [('len', ctypes.c_ushort), ('filter', ctypes.POINTER(Filter))]
        instructions = (Filter * 7)(
            Filter(0x20, 0, 0, 0),                  # syscall number
            Filter(0x15, 0, 4, number),
            Filter(0x20, 0, 0, 16),                 # socket family
            Filter(0x15, 2, 0, 1),                  # AF_UNIX
            Filter(0x06, 0, 0, 0x00050000 | 13),    # EACCES
            Filter(0x06, 0, 0, 0x00050000 | 13),
            Filter(0x06, 0, 0, 0x7fff0000))          # allow
        libc = ctypes.CDLL(None, use_errno=True)
        if libc.prctl(38, 1, 0, 0, 0) or libc.prctl(22, 2, ctypes.byref(Program(7, instructions)), 0, 0):
            raise OSError(ctypes.get_errno(), 'renderer network isolation unavailable')


def render(converter, source, directory):
    source, directory = Path(source), Path(directory)
    profile = directory / 'profile'
    (profile / 'user').mkdir(parents=True)
    (profile / 'user/registrymodifications.xcu').write_text('''<?xml version="1.0"?>
<oor:items xmlns:oor="http://openoffice.org/2001/registry">
<item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="MacroSecurityLevel" oor:op="fuse"><value>3</value></prop></item>
<item oor:path="/org.openoffice.Office.Common/Load"><prop oor:name="UpdateMode" oor:op="fuse"><value>0</value></prop></item>
<item oor:path="/org.openoffice.Office.Calc/Content/Update"><prop oor:name="Link" oor:op="fuse"><value>2</value></prop></item>
</oor:items>''')
    ext = source.suffix
    kind = {'.doc': 'writer', '.docx': 'writer', '.odt': 'writer',
            '.xls': 'calc', '.xlsx': 'calc', '.ods': 'calc',
            '.ppt': 'impress', '.pptx': 'impress', '.odp': 'impress'}[ext]
    rows = {}
    if ext == '.xlsx':
        with zipfile.ZipFile(source) as archive:
            rows = automatic_rows(archive, ET.fromstring(archive.read('xl/workbook.xml')))
    spec_path = directory / 'render.json'
    spec_path.write_text(json.dumps({'kind': kind, 'repairXlsx': ext == '.xlsx', 'rows': rows}))
    sheets_path = directory / 'sheets.json'
    subprocess.run(['/usr/bin/python3', '-c', OFFICE_WORKER, converter, source.as_uri(),
                    profile.as_uri(), (directory / 'input.pdf').as_uri(),
                    str(spec_path), str(sheets_path)], check=True, timeout=270)
    sheets = json.loads(sheets_path.read_text())
    output = directory / 'input.pdf'
    if not output.is_file() or output.stat().st_size == 0:
        raise ValueError('Office PDF output missing')
    from pypdf import PdfReader
    reader = PdfReader(output)
    pages = len(reader.pages)
    if not 1 <= pages <= 2000 or sheets and pages != len(sheets):
        raise ValueError('Office page count invalid')
    if sheets and any(sheet['hidden'] for sheet in sheets):
        from pypdf import PdfWriter
        visible = [i for i, sheet in enumerate(sheets) if not sheet['hidden']]
        if not visible:
            raise ValueError('no visible worksheets')
        writer = PdfWriter()
        for i in visible:
            writer.add_page(reader.pages[i])
        filtered = directory / 'visible.pdf'
        writer.write(filtered)
        output.unlink()
        filtered.rename(output)
        sheets = [sheets[i] for i in visible]
        pages = len(sheets)
    (directory / 'meta.json').write_text(json.dumps({'pages': pages, 'sheets': sheets}, ensure_ascii=False))


if __name__ == '__main__':
    try:
        restrict_process()
        render(*sys.argv[1:])
    except Exception:
        sys.exit(1)
