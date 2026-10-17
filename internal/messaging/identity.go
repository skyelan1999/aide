// Package messaging defines transport-independent device identity and consent.
// It deliberately does not treat a discovery code or network address as authority.
package messaging

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Identity belongs to one installation's persisted secret, not a network adapter.
// Seed storage must be supplied by the host encrypted vault. Never serialize it
// in profiles, discovery advertisements, API responses, or ordinary backups.
type Identity struct{ private ed25519.PrivateKey }
type Device struct {
	Fingerprint string `json:"fingerprint"`
	Code        string `json:"code"`
	PublicKey   []byte `json:"publicKey"`
}

func GenerateSeed() ([]byte, error) {
	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	return seed, err
}
func FromSeed(seed []byte) (*Identity, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("invalid device seed")
	}
	return &Identity{private: ed25519.NewKeyFromSeed(seed)}, nil
}
func PublicDevice(key []byte) (Device, error) {
	if len(key) != ed25519.PublicKeySize {
		return Device{}, errors.New("invalid device public key")
	}
	digest := sha256.Sum256(key)
	code := make([]byte, 6)
	// First 30 hash bits, six unambiguous base32 characters. This is an index,
	// never an authentication secret or globally unique identifier.
	bits := uint32(digest[0])<<24 | uint32(digest[1])<<16 | uint32(digest[2])<<8 | uint32(digest[3])
	for i := range code {
		code[i] = codeAlphabet[(bits>>uint(27-5*i))&31]
	}
	return Device{Fingerprint: hex.EncodeToString(digest[:]), Code: string(code), PublicKey: append([]byte(nil), key...)}, nil
}
func (i *Identity) Device() Device {
	d, _ := PublicDevice(i.private.Public().(ed25519.PublicKey))
	return d
}
func (i *Identity) Sign(payload []byte) []byte {
	return ed25519.Sign(i.private, append([]byte("aide-messages/v1\x00"), payload...))
}
func Verify(device Device, payload, signature []byte) bool {
	d, err := PublicDevice(device.PublicKey)
	return err == nil && d.Fingerprint == device.Fingerprint && d.Code == device.Code &&
		ed25519.Verify(ed25519.PublicKey(device.PublicKey), append([]byte("aide-messages/v1\x00"), payload...), signature)
}

// ResolveCode preserves collisions. Callers must show every candidate and bind
// acceptance to a verified full fingerprint, never candidates[0].
func ResolveCode(code string, candidates []Device) []Device {
	result := []Device{}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		actual, err := PublicDevice(candidate.PublicKey)
		if err == nil && actual.Fingerprint == candidate.Fingerprint && actual.Code == candidate.Code && actual.Code == code && !seen[actual.Fingerprint] {
			result = append(result, actual)
			seen[actual.Fingerprint] = true
		}
	}
	return result
}
