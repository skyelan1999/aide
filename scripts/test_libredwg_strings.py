#!/usr/bin/env python3
"""Compile the actual patched DXF quoting function against LibreDWG codepages."""
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve()
source = (root / 'src/out_dxf.c').read_text()
start = source.index('static char *\ncquote (')
end = source.index('\n/*\n   Splits overlong', start)
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
#define memBEGINc(s, literal) (!memcmp((s), (literal), sizeof(literal)-1))
#define LOG_ERROR(...) ((void)0)
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
