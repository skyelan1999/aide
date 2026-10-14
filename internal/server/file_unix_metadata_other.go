//go:build !linux

package server

import "os"

// Do not synthesize Linux ownership from another operating system's metadata.
func newFileUnixReader() func(os.DirEntry) *fileUnixMetadata {
	return func(os.DirEntry) *fileUnixMetadata { return nil }
}
