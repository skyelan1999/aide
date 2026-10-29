package localmounts

import (
	"testing"
	"unicode/utf8"
)

func TestIssue84RelativeUsesOriginalUnicodeComponents(t *testing.T) {
	for _, tc := range []struct {
		base, candidate, want string
		ok                    bool
	}{
		{"E:/Ⱥ", "E:/Ⱥ/Projects", "Projects", true},
		{"E:/Ⱥ", "e:/ⱥ/中文/文件.txt", "中文/文件.txt", true},
		{"E:/ⱥ", "E:/Ⱥ/中文", "中文", true},
		{"E:/K", "e:/K/Folder", "Folder", true},
		{"E:/k", "E:/K/Folder", "Folder", true},
		{`e:\Ⱥ\work`, `E:\Ⱥ\WORK\数据.txt`, "数据.txt", true},
		{"E:/Ⱥ/", "E:/Ⱥ", ".", true},
		{"E:/Ⱥ", "E:/Ⱥ/../secret", "", false},
		{"E:/Ⱥ", "E:/Ⱥ/a/../../secret", "", false},
		{"E:/Ⱥ", "E:/Ⱥ2/file", "", false},
		{"/Volume/Ⱥ", "/Volume/ⱥ/file", "", false},
	} {
		got, ok := Relative(tc.base, tc.candidate)
		if got != tc.want || ok != tc.ok || !utf8.ValidString(got) {
			t.Errorf("Relative(%q,%q)=(%q,%v), want(%q,%v)", tc.base, tc.candidate, got, ok, tc.want, tc.ok)
		}
	}
}
