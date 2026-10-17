package messaging

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptedAtomicStoreBindingAndRestart(t *testing.T) {
	directory := t.TempDir()
	key := bytes.Repeat([]byte{7}, 32)
	s, err := OpenStore(directory, key)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("private message and filename")
	if err = s.Put("state", plain); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, plain) {
		t.Fatal("plaintext on disk")
	}
	if err = os.WriteFile(filepath.Join(directory, "copied"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get("copied", 1024); err == nil {
		t.Fatal("object swapping not detected")
	}
	if _, err = s.Get("state", 2); err == nil {
		t.Fatal("unbounded encrypted read")
	}
	if err = s.Put("../outside", plain); err == nil {
		t.Fatal("unsafe object name")
	}
	s.Close()
	s, err = OpenStore(directory, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Get("state", 1024)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("restart lost data", err)
	}
	raw[len(raw)-1] ^= 1
	if err = os.WriteFile(filepath.Join(directory, "state"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get("state", 1024); err == nil {
		t.Fatal("tampering accepted")
	}
}
