package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// LAN leaf certificate is independent of the pinned localhost certificate.
func ensureLANCertificate(dir string, addresses []string) (*tls.Certificate, string, error) {
	ips := []net.IP{}
	for _, address := range addresses {
		u, e := url.Parse(address)
		if e != nil || u.Scheme != "https" || u.User != nil || u.Path != "" {
			return nil, "", errors.New("invalid LAN address")
		}
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsPrivate() {
			return nil, "", errors.New("LAN address must be private IP")
		}
		ips = append(ips, ip)
	}
	if len(ips) == 0 || len(ips) > 12 {
		return nil, "", errors.New("no LAN addresses")
	}
	certFile, keyFile := filepath.Join(dir, "lan-cert.pem"), filepath.Join(dir, "lan-key.pem")
	current, e := tls.LoadX509KeyPair(certFile, keyFile)
	valid := e == nil
	if valid {
		leaf, err := x509.ParseCertificate(current.Certificate[0])
		valid = err == nil
		if valid {
			valid = time.Until(leaf.NotAfter) > 24*time.Hour
			for _, ip := range ips {
				if leaf.VerifyHostname(ip.String()) != nil {
					valid = false
				}
			}
		}
	}
	if !valid {
		if e = os.MkdirAll(dir, 0700); e != nil {
			return nil, "", e
		}
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return nil, "", e
		}
		serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if e != nil {
			return nil, "", e
		}
		leaf := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Aide LAN"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(30 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: ips, BasicConstraintsValid: true}
		der, e := x509.CreateCertificate(rand.Reader, &leaf, &leaf, &key.PublicKey, key)
		if e != nil {
			return nil, "", e
		}
		keyDer, e := x509.MarshalECPrivateKey(key)
		if e != nil {
			return nil, "", e
		}
		if e = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer}), 0600); e != nil {
			return nil, "", e
		}
		if e = os.Chmod(keyFile, 0600); e != nil {
			return nil, "", e
		}
		if e = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); e != nil {
			return nil, "", e
		}
		current, e = tls.LoadX509KeyPair(certFile, keyFile)
		if e != nil {
			return nil, "", e
		}
	}
	sum := sha256.Sum256(current.Certificate[0])
	return &current, hex.EncodeToString(sum[:]), nil
}
