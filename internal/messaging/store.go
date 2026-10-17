package messaging

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
)

var objectName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,100}$`)

// Store uses a host-vault supplied AES-256 key; filenames are internal object
// identifiers. User filenames are display metadata and never filesystem paths.
type Store struct {
	root *os.Root
	aead cipher.AEAD
}

func OpenStore(directory string, key []byte) (*Store, error) {
	if len(key) != 32 {
		return nil, errors.New("storage key must be 256 bits")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		root.Close()
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		root.Close()
		return nil, err
	}
	return &Store{root: root, aead: aead}, nil
}
func (s *Store) Close() error { return s.root.Close() }
func (s *Store) Put(name string, plain []byte) error {
	if !objectName.MatchString(name) || len(plain) > 4<<20 {
		return errors.New("invalid storage object")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := s.aead.Seal(append([]byte(nil), nonce...), nonce, plain, []byte("aide-messages/store/v1/"+name))
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	tmp := "tmp-" + hex.EncodeToString(random)
	file, err := s.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer s.root.Remove(tmp)
	if _, err = file.Write(sealed); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return s.root.Rename(tmp, name)
}
func (s *Store) Get(name string, limit int64) ([]byte, error) {
	if !objectName.MatchString(name) || limit < 0 || limit > 4<<20 {
		return nil, errors.New("invalid storage object")
	}
	file, err := s.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	max := limit + int64(s.aead.NonceSize()+s.aead.Overhead())
	data, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max || len(data) < s.aead.NonceSize()+s.aead.Overhead() {
		return nil, errors.New("encrypted object size invalid")
	}
	return s.aead.Open(nil, data[:s.aead.NonceSize()], data[s.aead.NonceSize():], []byte("aide-messages/store/v1/"+name))
}
func (s *Store) Delete(name string) error {
	if !objectName.MatchString(name) {
		return errors.New("invalid storage object")
	}
	err := s.root.Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
