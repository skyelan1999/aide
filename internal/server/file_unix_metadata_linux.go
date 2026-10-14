//go:build linux

package server

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// Names are resolved once per directory response, not once per file. The
// numeric identity always remains available if this runtime lacks an account.
func newFileUnixReader() func(os.DirEntry) *fileUnixMetadata {
	owners, groups := map[uint32]string{}, map[uint32]string{}
	return func(entry os.DirEntry) *fileUnixMetadata {
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil
		}
		uid, gid := strconv.FormatUint(uint64(stat.Uid), 10), strconv.FormatUint(uint64(stat.Gid), 10)
		owner, ok := owners[stat.Uid]
		if !ok {
			owner = uid
			if account, err := user.LookupId(uid); err == nil && account.Username != "" {
				owner = account.Username
			}
			owners[stat.Uid] = owner
		}
		group, ok := groups[stat.Gid]
		if !ok {
			group = gid
			if account, err := user.LookupGroupId(gid); err == nil && account.Name != "" {
				group = account.Name
			}
			groups[stat.Gid] = group
		}
		return &fileUnixMetadata{Owner: owner, Group: group, Permissions: fileUnixPermissions(info.Mode()), UID: uid, GID: gid}
	}
}
