package server

import (
	"os"
	"syscall"
)

func integrityRootOwned(st os.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && s.Uid == 0
}
