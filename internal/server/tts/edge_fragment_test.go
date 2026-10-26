package tts

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

type issue97Sink struct {
	net.Conn
	bytes.Buffer
}

func (c *issue97Sink) Write(p []byte) (int, error) { return c.Buffer.Write(p) }
func (c *issue97Sink) Read(p []byte) (int, error)  { return 0, io.EOF }

func issue97Frame(fin bool, op byte, payload []byte) []byte {
	first := op
	if fin {
		first |= 0x80
	}
	b := []byte{first}
	switch {
	case len(payload) < 126:
		b = append(b, byte(len(payload)))
	case len(payload) <= 65535:
		b = append(b, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		var l [8]byte
		binary.BigEndian.PutUint64(l[:], uint64(len(payload)))
		b = append(b, 127)
		b = append(b, l[:]...)
	}
	return append(b, payload...)
}

func issue97Reader(raw []byte) (*wsConn, *issue97Sink) {
	sink := &issue97Sink{}
	return &wsConn{conn: sink, rd: bufio.NewReader(bytes.NewReader(raw))}, sink
}

func TestIssue97FragmentedMessageHasAggregateLimit(t *testing.T) {
	input := issue97Frame(false, opBinary, bytes.Repeat([]byte{'a'}, 8<<20))
	input = append(input, issue97Frame(true, opContinuation, bytes.Repeat([]byte{'b'}, 9<<20))...)
	conn, _ := issue97Reader(input)
	if _, b, err := conn.readMessage(); err == nil {
		t.Fatalf("accepted fragmented message with %d bytes", len(b))
	}
}

func TestIssue97FragmentSequenceAndCountAreBounded(t *testing.T) {
	tooMany := issue97Frame(false, opBinary, nil)
	for i := 0; i < 4096; i++ {
		tooMany = append(tooMany, issue97Frame(i == 4095, opContinuation, nil)...)
	}
	for name, raw := range map[string][]byte{
		"stray continuation":        issue97Frame(true, opContinuation, []byte("x")),
		"new data during fragments": append(issue97Frame(false, opBinary, []byte("a")), issue97Frame(true, opText, []byte("b"))...),
		"fragmented control":        append(issue97Frame(false, opPing, []byte("p")), issue97Frame(true, opBinary, []byte("a"))...),
		"oversized control":         append(issue97Frame(true, opPong, bytes.Repeat([]byte{'p'}, 126)), issue97Frame(true, opBinary, nil)...),
		"reserved opcode":           issue97Frame(true, 3, []byte("x")),
		"fragment count":            tooMany,
		"reserved extension bits":   []byte{0xc2, 0},
		"masked server frame":       []byte{0x82, 0x80},
	} {
		t.Run(name, func(t *testing.T) {
			conn, _ := issue97Reader(raw)
			if _, _, err := conn.readMessage(); err == nil {
				t.Fatal("invalid sequence accepted")
			}
		})
	}
}

func TestIssue97FragmentCountBoundaryAndNextMessage(t *testing.T) {
	input := issue97Frame(false, opText, []byte("a"))
	for i := 1; i < 4096; i++ {
		input = append(input, issue97Frame(i == 4095, opContinuation, nil)...)
	}
	input = append(input, issue97Frame(true, opBinary, []byte("next"))...)
	conn, _ := issue97Reader(input)
	op, b, err := conn.readMessage()
	if err != nil || op != opText || string(b) != "a" {
		t.Fatal("fragment count boundary rejected", err)
	}
	op, b, err = conn.readMessage()
	if err != nil || op != opBinary || string(b) != "next" {
		t.Fatal("fragment state leaked into next message", err)
	}
}

func TestIssue97ValidFragmentsWithInterleavedControlsAndExactLimit(t *testing.T) {
	input := issue97Frame(false, opBinary, []byte("a"))
	input = append(input, issue97Frame(true, opPing, []byte("p"))...)
	input = append(input, issue97Frame(true, opPong, nil)...)
	input = append(input, issue97Frame(true, opContinuation, []byte("b"))...)
	conn, sink := issue97Reader(input)
	op, b, err := conn.readMessage()
	if err != nil || op != opBinary || string(b) != "ab" {
		t.Fatal("valid control interleave failed", err)
	}
	if sink.Len() == 0 || sink.Bytes()[0] != 0x80|opPong {
		t.Fatal("ping not answered")
	}
	input = issue97Frame(false, opBinary, bytes.Repeat([]byte{'a'}, 8<<20))
	input = append(input, issue97Frame(true, opContinuation, bytes.Repeat([]byte{'b'}, 8<<20))...)
	conn, _ = issue97Reader(input)
	op, b, err = conn.readMessage()
	if err != nil || op != opBinary || len(b) != 16<<20 {
		t.Fatal("exact aggregate boundary rejected", err)
	}
}

func TestIssue97RejectsOversizedContinuationBeforeReadingPayload(t *testing.T) {
	input := issue97Frame(false, opBinary, bytes.Repeat([]byte{'a'}, 16<<20))
	input = append(input, 0x80|opContinuation, 1) // body intentionally absent
	conn, _ := issue97Reader(input)
	_, _, err := conn.readMessage()
	if err == nil || err == io.EOF || err == io.ErrUnexpectedEOF {
		t.Fatalf("read oversized continuation body instead of rejecting its header: %v", err)
	}
}
