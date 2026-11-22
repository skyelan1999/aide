package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Repeated multimodal requests retain identical inline image bytes. Intern
// bounded, losslessly compressed blocks in memory; keep the existing JSON body
// on disk and at the inspection API. Eviction never invalidates an owning
// snapshot, and the shared lookup itself cannot grow with the session corpus.
const snapshotBodyCompressionThreshold = 128 << 10
const snapshotBodyBlockSize = 64 << 10
const snapshotBodyCacheBlocks = 2048

type snapshotBodyChunk struct {
	data       []byte
	size       int
	compressed bool
}
type snapshotBodyChunkCache struct {
	mu      sync.Mutex
	entries map[[32]byte]*snapshotBodyChunk
	order   [][32]byte
	limit   int
}

var snapshotBodyChunks = snapshotBodyChunkCache{limit: snapshotBodyCacheBlocks}
var snapshotBodyWriters = sync.Pool{New: func() any { z, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed); return z }}

func (c *snapshotBodyChunkCache) intern(raw []byte) *snapshotBodyChunk {
	key := sha256.Sum256(raw)
	c.mu.Lock()
	if found := c.entries[key]; found != nil {
		c.mu.Unlock()
		return found
	}
	c.mu.Unlock()
	var out bytes.Buffer
	z := snapshotBodyWriters.Get().(*gzip.Writer)
	z.Reset(&out)
	_, writeErr := z.Write(raw)
	closeErr := z.Close()
	z.Reset(io.Discard)
	snapshotBodyWriters.Put(z)
	chunk := &snapshotBodyChunk{size: len(raw)}
	if writeErr == nil && closeErr == nil && out.Len() < len(raw) {
		// Do not retain bytes.Buffer's geometric spare capacity for image blocks.
		chunk.data = bytes.Clone(out.Bytes())
		chunk.compressed = true
	} else {
		chunk.data = bytes.Clone(raw)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if found := c.entries[key]; found != nil {
		return found
	}
	if c.entries == nil {
		c.entries = make(map[[32]byte]*snapshotBodyChunk)
	}
	if c.limit <= 0 {
		return chunk
	}
	if len(c.order) >= c.limit {
		delete(c.entries, c.order[0])
		copy(c.order, c.order[1:])
		c.order = c.order[:len(c.order)-1]
	}
	if c.entries == nil {
		c.entries = make(map[[32]byte]*snapshotBodyChunk)
	}
	c.entries[key] = chunk
	c.order = append(c.order, key)
	return chunk
}

func (s *RequestSnapshot) compactBody() {
	if len(s.Body) < snapshotBodyCompressionThreshold {
		return
	}
	chunks := make([]*snapshotBodyChunk, 0, (len(s.Body)+snapshotBodyBlockSize-1)/snapshotBodyBlockSize)
	add := func(data []byte) {
		for len(data) > 0 {
			end := snapshotBodyBlockSize
			if end > len(data) {
				end = len(data)
			}
			chunks = append(chunks, snapshotBodyChunks.intern(data[:end]))
			data = data[end:]
		}
	}
	// Inline images repeat while the preceding request text changes length.
	// Align their blocks at the data URL, so that prefix shifts do not retain
	// another complete copy. Boundaries only partition bytes; nothing is parsed
	// away, rewritten or omitted, including malformed/user-authored markers.
	remaining := []byte(s.Body)
	for len(remaining) > 0 {
		start := bytes.Index(remaining, []byte("data:image/"))
		if start < 0 {
			add(remaining)
			break
		}
		add(remaining[:start])
		remaining = remaining[start:]
		end := bytes.IndexByte(remaining, '"')
		if end < 0 {
			add(remaining)
			break
		}
		add(remaining[:end+1])
		remaining = remaining[end+1:]
	}
	s.bodySize = len(s.Body)
	s.bodyChunks = chunks
	s.Body = nil
}

func (s RequestSnapshot) bodyBytes() ([]byte, error) {
	if s.bodyChunks == nil || s.Body != nil {
		return s.Body, nil
	}
	data := make([]byte, 0, s.bodySize)
	for _, chunk := range s.bodyChunks {
		if chunk == nil || chunk.size < 0 || chunk.size > snapshotBodyBlockSize || len(data)+chunk.size > s.bodySize {
			return nil, fmt.Errorf("request snapshot block length mismatch")
		}
		if !chunk.compressed {
			if len(chunk.data) != chunk.size {
				return nil, fmt.Errorf("request snapshot raw block length mismatch")
			}
			data = append(data, chunk.data...)
			continue
		}
		z, err := gzip.NewReader(bytes.NewReader(chunk.data))
		if err != nil {
			return nil, fmt.Errorf("read request snapshot body: %w", err)
		}
		block, err := io.ReadAll(io.LimitReader(z, int64(chunk.size)+1))
		closeErr := z.Close()
		if err != nil {
			return nil, fmt.Errorf("decode request snapshot body: %w", err)
		}
		if closeErr != nil || len(block) != chunk.size {
			return nil, fmt.Errorf("request snapshot compressed block length mismatch")
		}
		data = append(data, block...)
	}
	if len(data) != s.bodySize {
		return nil, fmt.Errorf("request snapshot body length mismatch")
	}
	return data, nil
}
func (s RequestSnapshot) bodyLength() int {
	if s.bodyChunks != nil && s.Body == nil {
		return s.bodySize
	}
	return len(s.Body)
}
func (s RequestSnapshot) MarshalJSON() ([]byte, error) {
	type plain RequestSnapshot
	v := plain(s)
	body, err := s.bodyBytes()
	if err != nil {
		return nil, err
	}
	v.Body = body
	return json.Marshal(v)
}
func (s *RequestSnapshot) UnmarshalJSON(data []byte) error {
	type plain RequestSnapshot
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*s = RequestSnapshot(v)
	s.compactBody()
	return nil
}
