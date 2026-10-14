package server

import (
	"os"
	"regexp"
)

type fileUnixMetadata struct {
	Owner       string `json:"owner"`
	Group       string `json:"group"`
	Permissions string `json:"permissions"`
	UID         string `json:"uid,omitempty"`
	GID         string `json:"gid,omitempty"`
}

func fileUnixPermissions(mode os.FileMode) string {
	result := []byte("----------")
	switch {
	case mode.IsDir():
		result[0] = 'd'
	case mode&os.ModeSymlink != 0:
		result[0] = 'l'
	case mode&os.ModeNamedPipe != 0:
		result[0] = 'p'
	case mode&os.ModeSocket != 0:
		result[0] = 's'
	case mode&os.ModeCharDevice != 0:
		result[0] = 'c'
	case mode&os.ModeDevice != 0:
		result[0] = 'b'
	}
	for i, bit := range []os.FileMode{0400, 0200, 0100, 0040, 0020, 0010, 0004, 0002, 0001} {
		if mode&bit != 0 {
			result[i+1] = "rwx"[i%3]
		}
	}
	for _, special := range []struct {
		bit     os.FileMode
		index   int
		on, off byte
	}{{os.ModeSetuid, 3, 's', 'S'}, {os.ModeSetgid, 6, 's', 'S'}, {os.ModeSticky, 9, 't', 'T'}} {
		if mode&special.bit != 0 {
			if result[special.index] == 'x' {
				result[special.index] = special.on
			} else {
				result[special.index] = special.off
			}
		}
	}
	return string(result)
}

var reportedUnixPermissions = regexp.MustCompile(`^[dl-][r-][w-][xsS-][r-][w-][xsS-][r-][w-][xtT-][+@.]?$`)

func reportedFileUnixMetadata(mode, owner, group string) *fileUnixMetadata {
	if !reportedUnixPermissions.MatchString(mode) || owner == "" || group == "" {
		return nil
	}
	return &fileUnixMetadata{Owner: owner, Group: group, Permissions: mode}
}
