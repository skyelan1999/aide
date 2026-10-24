//go:build !linux

package server

import "os"

// Automatic image transitions apply only to the shipped Linux Docker runtime.
func integrityRootOwned(os.FileInfo) bool { return false }
