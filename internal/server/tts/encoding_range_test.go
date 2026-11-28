package tts

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestClientFrameLengthEncoding(t *testing.T) {
	for _, size := range []int{0, 125, 126, 255, 256, 65535, 65536} {
		payload := bytes.Repeat([]byte{0x42}, size)
		sink := &issue97Sink{}
		conn := &wsConn{conn: sink}
		if err := conn.writeFrame(opBinary, payload); err != nil {
			t.Fatal(err)
		}
		raw := sink.Bytes()
		if raw[0] != 0x82 || raw[1]&0x80 == 0 {
			t.Fatal("expected masked binary frame")
		}
		offset, length := 2, uint64(raw[1]&127)
		switch length {
		case 126:
			length = uint64(binary.BigEndian.Uint16(raw[2:4]))
			offset = 4
		case 127:
			length = binary.BigEndian.Uint64(raw[2:10])
			offset = 10
		}
		if length != uint64(size) || len(raw) != offset+4+size {
			t.Fatalf("length %d encoded as %d", size, length)
		}
		mask := raw[offset : offset+4]
		for i, b := range raw[offset+4:] {
			if b^mask[i%4] != payload[i] {
				t.Fatalf("payload mismatch at %d", i)
			}
		}
	}
}

func TestRealtimeWAVLengthRange(t *testing.T) {
	for _, size := range []int{2, 48000, 16 << 20} {
		pcm := make([]byte, size)
		pcm[0], pcm[size-1] = 0x80, 0xff
		wav := openAIWAV(pcm)
		if len(wav) != size+44 || binary.LittleEndian.Uint32(wav[4:8]) != uint32(size+36) || binary.LittleEndian.Uint32(wav[40:44]) != uint32(size) || !bytes.Equal(wav[44:], pcm) {
			t.Fatalf("WAV length/payload mismatch for %d PCM bytes", size)
		}
	}
}
