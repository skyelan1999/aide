package server

import "testing"

func TestSFTPListDecodesChineseFilenames(t *testing.T) {
	out := "sftp> ls -l\n-rw-r--r-- 1 user group 12 Sep 29 13:06 \\346\\226\\207\\346\\241\\243.docx\n" +
		"-rw-r--r-- 1 user group 8 Sep 29 13:07 \\057outside.xlsx\n" +
		"drwxr-xr-x 1 user group 0 Sep 29 13:00 docs\n"
	items := parseSFTPList(out, ".")
	if len(items) != 2 {
		t.Fatalf("items: %#v", items)
	}
	if items[0]["name"] != "文档.docx" || items[0]["path"] != "文档.docx" {
		t.Fatalf("name: %#v", items[0])
	}
	if items[1]["name"] != "docs" {
		t.Fatalf("directory: %#v", items[1])
	}
}

func TestSFTPListMarksSymlinksWithoutTreatingThemAsDirectories(t *testing.T) {
	items := parseSFTPList("lrwxrwxrwx 1 user group 8 Sep 29 13:06 link -> outside\n", ".")
	if len(items) != 1 || items[0]["name"] != "link" || items[0]["path"] != "link" || items[0]["symlink"] != true || items[0]["dir"] != false {
		t.Fatalf("symlink should be a non-directory unlinkable entry: %#v", items)
	}
}
