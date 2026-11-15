#!/usr/bin/env python3
"""Compile the actual patched DXF quoting function against LibreDWG codepages."""
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve()
source = (root / 'src/out_dxf.c').read_text()
start = source.index('static char *\ncquote (')
end = source.index('\nstatic int\ndxf_write_eed', start)
function = source[start:end]
test = r'''
#include <assert.h>
#include <ctype.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "dwg.h"
#include "codepages.h"
#include "bits.h"
#define LOG_ERROR(...) ((void)0)
#define LOG_TRACE(...) ((void)0)
'''+function+r'''
static void check(const char *input, const char *expected) {
  unsigned char buffer[512];
  size_t capacity = strlen(input)*2+1;
  for (int pass=0; pass<100; pass++) {
    memset(buffer, 0xA5, sizeof(buffer));
    assert(cquote((char*)buffer, capacity, input) == (char*)buffer);
    if (strcmp((char*)buffer, expected)) {
      fprintf(stderr,"DXF quote mismatch: input=%s actual=%s expected=%s\n",
              input, buffer, expected);
      abort();
    }
    assert(buffer[capacity] == 0xA5);
  }
}
int main(void) {
  check("plain", "plain");
  check("", "");
  check("a\nb\rc", "a^Jb^Mc");
  check("\\M+5C50AP", "\\M+5C50AP");
  check("\\M+5D6D0", "\\U+4E2D");
  check("\\M+182A0!", "\\U+3042!");
  check("\\M+2A4A4", "\\U+4E2D");
  check("\\M+3C1DF", "\\U+C911");
  const Dwg_Codepage pages[] = {CP_UNDEFINED, CP_ANSI_932, CP_ANSI_950,
                              CP_ANSI_949, CP_ANSI_1361, CP_ANSI_936};
  for (int index=1; index<=5; index++) {
    char input[32], expected[32];
    snprintf(input,sizeof(input),"\\M+%d1234Z",index);
    unsigned unicode = dwg_codepage_uwc(pages[index],0x1234);
    if (unicode) snprintf(expected,sizeof(expected),"\\U+%04XZ",unicode);
    else snprintf(expected,sizeof(expected),"%s",input);
    check(input,expected);
  }
  check("\\M+5C50", "\\M+5C50");
  check("\\M+5ZZZZ", "\\M+5ZZZZ");
  check("\\M+6C50A", "\\M+6C50A");
  check("\\M+0C50A", "\\M+0C50A");
  char long_text[300];
  memset(long_text,'A',sizeof(long_text));
  long_text[249]=(char)0xE4;long_text[250]=(char)0xB8;long_text[251]=(char)0xAD;
  assert(dxf_utf8_chunk(long_text,270)==249);
  assert(dxf_utf8_chunk(long_text+249,21)==21);
  assert(dxf_utf8_chunk(long_text,250)==250);
  assert(dxf_utf8_chunk(long_text,248)==248);
  memcpy(long_text+247,"\\U+4E2D",7);
  assert(dxf_utf8_chunk(long_text,270)==247);
  Bit_Chain dat = {0};
  dat.version = R_2004; dat.codepage = CP_ANSI_936; dat.fh = tmpfile();
  assert(dat.fh);
  char gbk[] = "\xD6\xD0\xCE\xC4\n\xA1\xC0";
  dxf_fixup_string(&dat, gbk, 1, 1);
  fflush(dat.fh); rewind(dat.fh);
  char output[1024] = {0}; fread(output, 1, sizeof(output)-1, dat.fh); fclose(dat.fh);
  assert(strstr(output, "\\U+4E2D\\U+6587^J\\U+00B1"));
  dat.version = R_2007; dat.fh = tmpfile();
  char utf8[] = "\xE4\xB8\xAD\xE6\x96\x87\n\xC2\xB1";
  dxf_fixup_string(&dat, utf8, 1, 1);
  fflush(dat.fh); rewind(dat.fh); memset(output,0,sizeof(output));
  fread(output,1,sizeof(output)-1,dat.fh); fclose(dat.fh);
  assert(strstr(output,"\\U+4E2D\\U+6587^J\\U+00B1"));
  puts("PASS actual LibreDWG DXF quoting: Asian MIF, bounds, deterministic termination");
}
'''
with tempfile.TemporaryDirectory() as td:
    source_path, binary = Path(td)/'test.c', Path(td)/'test'
    source_path.write_text(test)
    subprocess.run(['cc', '-std=c99', '-I'+str(root/'include'),
                    '-I'+str(root/'src'), str(source_path),
                    str(root/'src/.libs/libredwg.a'), '-lm', '-o', str(binary)], check=True)
    subprocess.run([str(binary)], check=True)
