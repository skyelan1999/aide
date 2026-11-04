package server

import "strings"

// cappedShellOutput bounds memory used to collect one command output stream.
// Writes beyond the presentation limit are consumed and discarded so the
// command can finish normally without retaining unbounded output in memory.
type cappedShellOutput struct {
	strings.Builder
	truncated bool
}

func (b *cappedShellOutput) Write(p []byte) (int, error) {
	const limit = 64 << 10
	original := len(p)
	remaining := limit - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Builder.Write(p)
	}
	if original > remaining {
		b.truncated = true
	}
	return original, nil
}

func (b *cappedShellOutput) result(stream string) string {
	out := b.String()
	if b.truncated {
		out += "\n…（" + stream + " 已截断，单流上限 64 KiB）"
	}
	return out
}
