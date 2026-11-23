package server

import (
	"errors"
	"strings"
	"testing"
)

// #968: filenames in validation code are data, not evidence that conversion ran.
func TestShellFailureDWGMetadataUsesActualError(t *testing.T) {
	command := `python3 -c 'source="source.dwg"; read_report("report.json")'`
	tests := []struct {
		name, output, want string
		code               int
		err                error
	}{
		{"missing_report", "[stderr]\nFileNotFoundError: [Errno 2] No such file or directory: 'report.json'", "路径不存在", 1, nil},
		{"python_syntax", "[stderr]\nSyntaxError: unterminated string literal", "语法错误", 1, nil},
		{"python_indent", "[stderr]\nIndentationError: unindent does not match any outer indentation level", "语法错误", 1, nil},
		{"python_tabs", "[stderr]\nTabError: inconsistent use of tabs and spaces in indentation", "语法错误", 1, nil},
		{"shell_quotes", "[stderr]\nbash: unexpected EOF while looking for matching `\"'", "语法错误", 2, nil},
		{"permission", "[stderr]\nPermissionError: [Errno 13] Permission denied: 'report.json'", "权限不足", 1, nil},
		{"timeout", "", "命令执行超时", -1, errors.New("context deadline exceeded")},
		{"memory", "command memory budget exceeded", "超过作业内存预算", 137, nil},
		{"unrelated", "[stderr]\nValueError: invalid report schema", "命令以非零退出码结束", 1, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := analyzeShellFailure(command, tc.output, tc.code, tc.err)
			if !strings.Contains(got, tc.want) || strings.Contains(got, "闭源二进制") || strings.Contains(got, "不要再探测/安装") {
				t.Fatalf("wrong diagnosis for actual error %q: %s", tc.output, got)
			}
		})
	}
}

func TestShellFailureDWGConverterDoesNotInventCapabilityFailure(t *testing.T) {
	tests := []struct{ command, output, want string }{
		{"/opt/aide/dwg/bin/dwg2dxf source.dwg", "[stderr]\ndwg2dxf: corrupt header", "命令以非零退出码结束"},
		{"dwg2dxf missing.dwg", "[stderr]\nNo such file or directory: missing.dwg", "路径不存在"},
		{"dwgwrite -o new.dwg new.dxf", "[stderr]\nbash: dwgwrite: command not found", "命令不存在"},
		{`python3 -c 'hashes={"libredwg.dwg":"sha"}; open("report.json")'`, "[stderr]\nNo such file or directory: report.json", "路径不存在"},
	}
	for _, tc := range tests {
		got := analyzeShellFailure(tc.command, tc.output, 1, nil)
		if !strings.Contains(got, tc.want) || strings.Contains(got, "无法直接生成或转换") {
			t.Fatalf("invented conversion failure for %q: %s", tc.command, got)
		}
	}
}
