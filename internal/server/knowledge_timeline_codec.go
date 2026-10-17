package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
)

// Keep the vector-cache codec unchanged. Timeline envelope v2 compresses before
// encryption; its expanded JSON and physical file have independent hard limits.
func compressKnowledgeTime(plain []byte) ([]byte, error) {
	var b bytes.Buffer
	w, err := gzip.NewWriterLevel(&b, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err = w.Write(plain); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func decompressKnowledgeTime(compressed []byte) ([]byte, error) {
	b := bytes.NewReader(compressed)
	r, err := gzip.NewReader(b)
	if err != nil {
		return nil, fmt.Errorf("知识历史压缩格式损坏；保留原文件")
	}
	r.Multistream(false)
	plain, readErr := io.ReadAll(io.LimitReader(r, knowledgeTimelinePlainBytes+1))
	closeErr := r.Close()
	if readErr != nil || closeErr != nil || b.Len() != 0 || len(plain) > knowledgeTimelinePlainBytes {
		zeroBytes(plain)
		return nil, fmt.Errorf("知识历史解压失败或超出容量；保留原文件")
	}
	return plain, nil
}

func knowledgeTimeEnvelopeSize(compressedBytes int) int {
	// JSON has base64 ciphertext (GCM adds 16 bytes) and a 12-byte nonce.
	header, _ := json.Marshal(knowledgeVectorEnvelope{Version: 2, Ciphertext: []byte{}, Nonce: make([]byte, 12)})
	return len(header) + ((compressedBytes+16+2)/3)*4
}
