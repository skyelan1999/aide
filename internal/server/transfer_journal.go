package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// A constant-size, fsynced acknowledgement record per chunk avoids rewriting a
// multi-gigabyte bitmap on every request. Partial crash tails are unacknowledged.
type transferAck struct {
	Part   int    `json:"p"`
	Chunk  int64  `json:"n"`
	SHA    string `json:"h"`
	Upload bool   `json:"u,omitempty"`
}

func (m *transferManager) acknowledge(j *transferJob, part int, index int64, digest string, upload bool) error {
	f, e := os.OpenFile(filepath.Join(m.jobDir(j), "chunks.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	e = json.NewEncoder(f).Encode(transferAck{part, index, digest, upload})
	if e == nil {
		e = f.Sync()
	}
	_ = f.Close()
	return e
}
func (m *transferManager) recoverChunks(j *transferJob) error {
	if j.Status == "complete" {
		j.Completed = j.Size
		return nil
	}
	j.UploadChunks = map[int64]string{}
	for _, p := range j.Parts {
		p.Chunks = map[int64]string{}
	}
	f, e := os.OpenFile(filepath.Join(m.jobDir(j), "chunks.log"), os.O_RDWR, 0600)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	offset := int64(0)
	for {
		b, e := reader.ReadBytes('\n')
		if e == io.EOF {
			if len(b) > 0 {
				_ = f.Truncate(offset)
			}
			break
		}
		if e != nil {
			return e
		}
		var ack transferAck
		if json.Unmarshal(b, &ack) != nil || ack.Part < 0 || ack.Part >= len(j.Parts) || ack.Chunk < 0 || (len(ack.SHA) != 64 && ack.SHA != "") || ack.Chunk > j.Parts[ack.Part].Size/transferChunkSize {
			_ = f.Truncate(offset)
			break
		}
		offset += int64(len(b))
		if ack.Upload {
			if ack.SHA == "" {
				delete(j.UploadChunks, ack.Chunk)
				delete(j.Parts[ack.Part].Chunks, ack.Chunk)
			} else {
				j.UploadChunks[ack.Chunk] = ack.SHA
				j.Parts[ack.Part].Chunks[ack.Chunk] = ack.SHA
			}
		} else {
			j.Parts[ack.Part].Chunks[ack.Chunk] = ack.SHA
		}
	}
	j.Completed = 0
	if j.Operation == "upload" && !j.UploadCommitted {
		for n := range j.UploadChunks {
			j.Completed += min(transferChunkSize, j.Size-n*transferChunkSize)
		}
	} else {
		for _, p := range j.Parts {
			for n := range p.Chunks {
				j.Completed += min(transferChunkSize, p.Size-n*transferChunkSize)
			}
		}
	}
	if j.Status == "complete" {
		j.Completed = j.Size
	}
	return nil
}
func (m *transferManager) checkpoint(j *transferJob) error {
	if time.Since(j.checkpointAt) < time.Second {
		return nil
	}
	j.checkpointAt = time.Now()
	return m.save(j)
}
func transferSummary(j *transferJob, detail bool) map[string]any {
	tops := make([]transferTop, len(j.Tops))
	for i, t := range j.Tops {
		tops[i] = *t
	}
	out := map[string]any{"id": j.ID, "operation": j.Operation, "workspace": j.Workspace, "status": j.Status, "error": j.Error, "created": j.Created, "updated": j.Updated, "size": j.Size, "completed": j.Completed, "concurrency": j.Concurrency, "chunkSize": j.ChunkSize, "tops": tops, "uploadName": j.UploadName, "uploadCommitted": j.UploadCommitted}
	if detail && j.Operation == "upload" && !j.UploadCommitted {
		chunks := make(map[int64]string, len(j.UploadChunks))
		for n, digest := range j.UploadChunks {
			chunks[n] = digest
		}
		out["uploadChunks"] = chunks
	}
	return out
}
func (m *transferManager) cleanup(j *transferJob) error {
	// Only private staging paths from this manifest may be removed. Never recurse
	// through a destination or delete completed publications on cancel.
	var result error
	if j.Operation != "download" {
		if e := m.check(j); e != nil {
			return fmt.Errorf("无法确认原目标，暂存尚未清理：%w", e)
		}
		ctx, cancel := contextWithTransferCleanup()
		defer cancel()
		fs, e := m.app.streamTransferFS(ctx, j.Destination, true)
		if e != nil {
			result = e
		} else {
			defer fs.Close()
			for i := len(j.Parts) - 1; i >= 0; i-- {
				p := j.Parts[i]
				if m.published(j, p) {
					continue
				}
				if e = fs.Remove(p.Target, p.Dir); e != nil && !os.IsNotExist(e) {
					result = errors.Join(result, e)
				}
			}
		}
	}
	for _, name := range []string{"upload.data", "download.zip.part"} {
		if e := os.Remove(filepath.Join(m.jobDir(j), name)); e != nil && !os.IsNotExist(e) {
			result = errors.Join(result, e)
		}
	}
	return result
}

func contextWithTransferCleanup() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}
