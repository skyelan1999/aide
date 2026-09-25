package server

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAutoGenerateCert：ensureTLSCert 在空目录生成自签证书，
// 校验 SAN 含 DNS:localhost 与 IP:127.0.0.1，私钥权限 0600，且二次调用复用不轮换。
func TestAutoGenerateCert(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, err := ensureTLSCert(dir)
	if err != nil {
		t.Fatalf("ensureTLSCert: %v", err)
	}
	if filepath.Base(certFile) != "cert.pem" || filepath.Base(keyFile) != "key.pem" {
		t.Fatalf("unexpected paths: %s %s", certFile, keyFile)
	}

	raw, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("cert.pem 不是 PEM CERTIFICATE")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if !contains(cert.DNSNames, "localhost") {
		t.Fatalf("SAN DNSNames=%v 缺少 localhost", cert.DNSNames)
	}
	hasIP := false
	for _, ip := range cert.IPAddresses {
		if ip.Equal(net.IPv4(127, 0, 0, 1)) {
			hasIP = true
		}
	}
	if !hasIP {
		t.Fatalf("SAN IPAddresses=%v 缺少 127.0.0.1", cert.IPAddresses)
	}
	if validity := cert.NotAfter.Sub(cert.NotBefore); validity < 3600*24*364 {
		t.Fatalf("证书有效期 %v，应约 3650 天", validity)
	}

	// 私钥必须 0600
	st, err := os.Stat(keyFile)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if mode := st.Mode().Perm(); mode != 0600 {
		t.Fatalf("key.pem 权限 %o，期望 0600", mode)
	}

	// 幂等：再次调用复用既有文件，不重新生成
	certFile2, keyFile2, err := ensureTLSCert(dir)
	if err != nil {
		t.Fatalf("second ensureTLSCert: %v", err)
	}
	if certFile2 != certFile || keyFile2 != keyFile {
		t.Fatal("二次调用应复用同一路径")
	}
}

// TestTLSConfig：最低版本 ≥ TLS1.2，套件仅含 ECDHE 前向保密 + AES-GCM AEAD，无弱套件。
func TestTLSConfig(t *testing.T) {
	c := tlsConfig()
	if c.MinVersion < tls.VersionTLS12 {
		t.Fatalf("MinVersion=%x < TLS1.2", c.MinVersion)
	}
	allowed := map[uint16]bool{
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256: true,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384: true,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:   true,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384:   true,
	}
	if len(c.CipherSuites) == 0 {
		t.Fatal("CipherSuites 不应为空（TLS1.2 需显式列举安全套件）")
	}
	for _, cs := range c.CipherSuites {
		if !allowed[cs] {
			t.Fatalf("套件 %d 不在白名单（非 ECDHE/AES-GCM）", cs)
		}
	}
	if !c.PreferServerCipherSuites {
		t.Fatal("应开启 PreferServerCipherSuites")
	}
}

// TestHTTPRedirect：明文 HTTP 请求被 301 跳转到 https 同主机、保留路径与查询串。
func TestHTTPRedirect(t *testing.T) {
	h := httpsRedirectHandler("8443")
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8081/healthz?x=1", nil)
	req.Host = "localhost:8081"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("code=%d, want 301", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://") {
		t.Fatalf("Location=%q 应以 https:// 开头", loc)
	}
	if !strings.Contains(loc, "localhost:8443") {
		t.Fatalf("Location=%q 应指向 https://localhost:8443", loc)
	}
	if !strings.HasSuffix(loc, "/healthz?x=1") {
		t.Fatalf("Location=%q 未保留路径与查询串", loc)
	}
}

// TestHSTSNotOnLocalhost：回环 HTTPS 不加 HSTS；非回环 HTTPS 加；明文 HTTP 不加。
func TestHSTSNotOnLocalhost(t *testing.T) {
	// 回环 + TLS → 不加
	r := httptest.NewRequest(http.MethodGet, "https://localhost:8097/healthz", nil)
	r.TLS = &tls.ConnectionState{}
	if v := strictTransportSecurity(r); v != "" {
		t.Fatalf("localhost HTTPS 不应有 HSTS，got %q", v)
	}
	r127 := httptest.NewRequest(http.MethodGet, "https://127.0.0.1:8097/healthz", nil)
	r127.TLS = &tls.ConnectionState{}
	if v := strictTransportSecurity(r127); v != "" {
		t.Fatalf("127.0.0.1 HTTPS 不应有 HSTS，got %q", v)
	}
	// 非回环 + TLS → 加
	rExt := httptest.NewRequest(http.MethodGet, "https://aide.example.com/healthz", nil)
	rExt.TLS = &tls.ConnectionState{}
	if v := strictTransportSecurity(rExt); v != "max-age=300" {
		t.Fatalf("非回环 HTTPS 应下发 HSTS，got %q", v)
	}
	// 明文 HTTP（即使非回环）→ 不加
	rHTTP := httptest.NewRequest(http.MethodGet, "http://aide.example.com/healthz", nil)
	if v := strictTransportSecurity(rHTTP); v != "" {
		t.Fatalf("明文 HTTP 不应有 HSTS，got %q", v)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
