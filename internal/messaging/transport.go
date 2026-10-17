package messaging

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const peerName = "aide-device.invalid"

// Card is a short-lived signed discovery record. Routing hints are mutable;
// identity is the full public-key fingerprint. A relay cannot substitute keys
// without changing that fingerprint, which users confirm when adding contacts.
type Card struct {
	Device      Device   `json:"device"`
	Name        string   `json:"name"`
	Certificate []byte   `json:"certificate"`
	Addresses   []string `json:"addresses"`
	Expires     int64    `json:"expires"`
	Signature   []byte   `json:"signature"`
}

func (c Card) payload() ([]byte, error) { c.Signature = nil; return json.Marshal(c) }
func (i *Identity) Certificate() (tls.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: peerName}, DNSNames: []string{peerName}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.Add(365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, i.private.Public(), i.private)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: i.private, Leaf: leaf}, nil
}
func (i *Identity) Card(cert tls.Certificate, name string, addresses []string) (Card, error) {
	c := Card{Device: i.Device(), Name: name, Certificate: cert.Certificate[0], Addresses: addresses, Expires: time.Now().Add(2 * time.Minute).Unix()}
	payload, err := c.payload()
	if err != nil {
		return Card{}, err
	}
	c.Signature = i.Sign(payload)
	return c, c.Validate(time.Now())
}
func (c Card) Validate(now time.Time) error {
	if len(c.Name) > 100 || len(c.Certificate) > 4096 || len(c.Addresses) > 12 || c.Expires < now.Unix() || c.Expires > now.Add(5*time.Minute).Unix() {
		return errors.New("invalid discovery record bounds")
	}
	payload, err := c.payload()
	if err != nil || !Verify(c.Device, payload, c.Signature) {
		return errors.New("invalid discovery signature")
	}
	cert, err := x509.ParseCertificate(c.Certificate)
	if err != nil {
		return err
	}
	pub, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return errors.New("unexpected device certificate key")
	}
	device, err := PublicDevice(pub)
	if err != nil || device.Fingerprint != c.Device.Fingerprint {
		return errors.New("certificate identity mismatch")
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	if _, err = cert.Verify(x509.VerifyOptions{Roots: roots, DNSName: peerName, CurrentTime: now}); err != nil {
		return err
	}
	if err = cert.CheckSignatureFrom(cert); err != nil {
		return err
	}
	for _, address := range c.Addresses {
		if _, err = ParseAddress(address); err != nil {
			return err
		}
	}
	return nil
}
func ParseAddress(address string) (*url.URL, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("peer addresses must be HTTPS origins")
	}
	return u, nil
}

// ServerTLS validates possession of any self-signed device key. Requests are
// subsequently authorized by the full fingerprint extracted from this verified
// TLS channel; unknown devices get only discovery/contact-request operations.
func ServerTLS(cert tls.Certificate) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAnyClientCert, SessionTicketsDisabled: true, NextProtos: []string{"http/1.1"}, VerifyConnection: func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) != 1 {
			return errors.New("exactly one device certificate required")
		}
		peer := state.PeerCertificates[0]
		if _, ok := peer.PublicKey.(ed25519.PublicKey); !ok {
			return errors.New("unexpected device key")
		}
		if err := peer.CheckSignatureFrom(peer); err != nil {
			return err
		}
		roots := x509.NewCertPool()
		roots.AddCert(peer)
		_, err := peer.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
		return err
	}}
}
func AuthenticatedPeer(r *http.Request) (Device, error) {
	if r.TLS == nil || !r.TLS.HandshakeComplete || r.TLS.Version < tls.VersionTLS13 || len(r.TLS.PeerCertificates) != 1 {
		return Device{}, ErrDenied
	}
	pub, ok := r.TLS.PeerCertificates[0].PublicKey.(ed25519.PublicKey)
	if !ok {
		return Device{}, ErrDenied
	}
	return PublicDevice(pub)
}

// ClientTLS trusts exactly the selected card certificate and performs normal
// chain/hostname verification. No system CA, IP-derived identity, or disabled
// verification can silently change an already selected peer.
func ClientTLS(cert tls.Certificate, card Card) (*tls.Config, error) {
	// A previously authenticated contact remains identifiable after its discovery
	// advertisement expires; normal TLS still checks certificate validity now.
	if err := card.Validate(time.Unix(card.Expires-1, 0)); err != nil {
		return nil, err
	}
	peer, err := x509.ParseCertificate(card.Certificate)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	roots.AddCert(peer)
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: peerName, Certificates: []tls.Certificate{cert}, NextProtos: []string{"http/1.1"}, VerifyConnection: func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) < 1 {
			return ErrDenied
		}
		pub, ok := state.PeerCertificates[0].PublicKey.(ed25519.PublicKey)
		if !ok {
			return ErrDenied
		}
		device, err := PublicDevice(pub)
		if err != nil || device.Fingerprint != card.Device.Fingerprint {
			return ErrDenied
		}
		return nil
	}}, nil
}
func PeerClient(cert tls.Certificate, card Card, dial func(context.Context, string, string) (net.Conn, error)) (*http.Client, error) {
	config, err := ClientTLS(cert, card)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{TLSClientConfig: config, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, Proxy: nil}
	if dial != nil {
		transport.DialContext = dial
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("peer redirects forbidden") }}, nil
}
func ValidCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, c := range code {
		if !strings.ContainsRune(codeAlphabet, c) {
			return false
		}
	}
	return true
}
