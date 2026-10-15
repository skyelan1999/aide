# Source declarations only. No workspace imports, scripts, or file reads.
import json
import sys
from html.parser import HTMLParser


class Page(HTMLParser):
    def __init__(self, unit):
        super().__init__(convert_charrefs=True)
        self.unit = unit
        self.ids = {}
        self.buttons = []
        self.ignored = None
        self.depth = 0

    def record(self, name, value):
        if len(self.unit[name]) < 256:
            self.unit[name].append(value)
        else:
            self.unit['webReferencesTruncated'] = True

    def diagnostic(self, reason):
        line, column = self.getpos()
        self.record('unresolvedWeb', dict(kind='html', reason=reason, line=line, column=column+1))

    def handle_starttag(self, tag, attrs):
        if self.ignored:
            if tag == self.ignored:
                self.depth += 1
            return
        if tag in ('template', 'noscript', 'textarea', 'title'):
            self.ignored, self.depth = tag, 1
            return
        names = [name for name, _ in attrs]
        if len(set(names)) != len(names):
            for name, value in attrs:
                if name == 'id' and value:
                    self.ids[value] = self.ids.get(value, 0)+1
            self.diagnostic('duplicate_html_attributes')
            return
        attrs = dict(attrs)
        line, column = self.getpos()
        identity = attrs.get('id')
        if identity:
            self.ids[identity] = self.ids.get(identity, 0)+1
        if tag == 'base' and 'href' in attrs:
            self.unit['htmlBase'] = True
            self.diagnostic('html_base_url_unsupported')
        if tag == 'script' and attrs.get('type', '').lower() in ('', 'module', 'text/javascript', 'application/javascript'):
            if 'nomodule' in attrs:
                self.diagnostic('conditional_script_unsupported')
            elif attrs.get('src'):
                self.record('scripts', dict(path=attrs['src'], line=line, column=column+1))
            else:
                self.diagnostic('inline_script_unsupported')
        if not identity or len(identity) > 256:
            return
        if tag == 'button' or tag == 'input' and attrs.get('type', '').lower() in ('button', 'submit', 'reset', 'image') or attrs.get('role', '').lower() == 'button':
            self.record_button(dict(key='button#'+identity, parent='', name='#'+identity, kind='button', line=line, column=column+1, endLine=line, signature=(self.get_starttag_text() or '')[:240]))

    def record_button(self, value):
        if len(self.buttons) < 256:
            self.buttons.append(value)
        else:
            self.unit['webReferencesTruncated'] = True

    def handle_endtag(self, tag):
        if tag == self.ignored:
            self.depth -= 1
            if self.depth == 0:
                self.ignored = None

    def handle_startendtag(self, tag, attrs):
        self.handle_starttag(tag, attrs)
        self.handle_endtag(tag)


out = []
for source in json.load(sys.stdin):
    unit = dict(file=source['id'], language='HTML', package='', symbols=[], calls=[], imports=[], diagnostics=[], scripts=[], unresolvedWeb=[], htmlBase=False, webReferencesTruncated=False)
    try:
        page = Page(unit)
        page.feed(source['text'])
        page.close()
        for button in page.buttons:
            identity = button['name'][1:]
            if page.ids[identity] == 1:
                unit['symbols'].append(button)
            else:
                page.record('unresolvedWeb', dict(kind='html', reason='duplicate_dom_id', line=button['line'], column=1))
    except Exception:
        unit['symbols'] = []
        unit['scripts'] = []
        unit['diagnostics'].append('HTML源解析失败；未建立页面关系')
    out.append(unit)
json.dump(out, sys.stdout, ensure_ascii=False)
