package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func privateFixture(t *testing.T) (*App, marketSnapshot, []byte, signedPluginLicense) {
	t.Helper()
	a, s, raw := marketFixture(t, "2.0.0")
	p := &s.Catalog.Plugins[0]
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	p.Distribution = "private"
	p.Package = ""
	p.SHA256 = ""
	p.Bytes = 0
	p.Commerce = &marketCommerce{Status: "available", Model: "perpetual-major", PublicKeys: map[string]string{"issuer1": base64.StdEncoding.EncodeToString(public)}}
	device, err := a.pluginLicenseDevice()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	claims := pluginLicenseClaims{SchemaVersion: 1, LicenseID: "test-purchase", PluginID: p.ID, Version: p.Version, SHA256: hex.EncodeToString(sum[:]), DeviceID: device, IssuedAt: time.Now().Unix()}
	payload, _ := json.Marshal(claims)
	license := signedPluginLicense{KeyID: "issuer1", Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, append([]byte("aide-plugin-license-v1\n"), payload...)))}
	cacheMarket(t, a, s)
	return a, s, raw, license
}
func privateRequest(a *App, s marketSnapshot, raw []byte, license signedPluginLicense) *httptest.ResponseRecorder {
	in := marketInput(a, s)
	in["license"] = license
	review, _ := json.Marshal(in)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("review", string(review))
	part, _ := form.CreateFormFile("bundle", "private.zip")
	part.Write(raw)
	form.Close()
	req := httptest.NewRequest("POST", "/api/plugin-market/"+s.Catalog.Plugins[0].ID+"/install", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.SetPathValue("id", s.Catalog.Plugins[0].ID)
	w := httptest.NewRecorder()
	a.marketInstall(w, req)
	return w
}
func TestPluginCommercePrivateInstallAndUpdate(t *testing.T) {
	a, s, raw, license := privateFixture(t)
	if err := validateMarket(s); err != nil {
		t.Fatal(err)
	}
	first, err := a.pluginLicenseDevice()
	if err != nil {
		t.Fatal(err)
	}
	next, err := a.pluginLicenseDevice()
	if err != nil || first != next {
		t.Fatal("identity not stable")
	}
	requireStatus(t, request(a, "POST", "/api/plugin-license/device", map[string]any{}), 200)
	result := request(a, "GET", "/api/plugin-market", nil)
	requireStatus(t, result, 200)
	if strings.Contains(result.Body.String(), "downloadURL") {
		t.Fatal("private package exposed download URL")
	}
	requireStatus(t, privateRequest(a, s, raw, license), 201)
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == s.Catalog.Plugins[0].ID {
			a.pluginRegistry.Plugins[i].Version = "1.0.0"
			a.pluginRegistry.Plugins[i].Enabled = false
			a.pluginRegistry.Plugins[i].Settings = map[string]any{"marker": "keep"}
		}
	}
	requireStatus(t, privateRequest(a, s, raw, license), 200)
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == s.Catalog.Plugins[0].ID && (p.Enabled || p.Settings["marker"] != "keep" || p.PackageSHA256 == "") {
			t.Fatal("upgrade lost state or digest")
		}
	}
}
func TestPluginCommerceRejectInvalidLicense(t *testing.T) {
	for _, scenario := range []string{"signature", "key", "device", "bundle", "prelaunch", "locked", "readonly", "workspace", "version"} {
		t.Run(scenario, func(t *testing.T) {
			a, s, raw, license := privateFixture(t)
			p := s.Catalog.Plugins[0]
			device, _ := a.pluginLicenseDevice()
			switch scenario {
			case "signature":
				license.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
			case "key":
				license.KeyID = "other"
			case "device":
				device = strings.Repeat("f", 64)
			case "bundle":
				raw = append(raw, 0)
			case "prelaunch":
				p.Commerce.Status = "coming_soon"
				s.Catalog.Plugins[0] = p
				cacheMarket(t, a, s)
			case "locked":
				if err := atomicJSON(lockStatePath(a.dataPath), LockStateFile{Locked: true, Gen: 2}); err != nil {
					t.Fatal(err)
				}
			case "readonly":
				a.settings.SandboxMode = "read-only"
			case "workspace":
				s.Revision = strings.Repeat("c", 40)
			case "version":
				p.Version = "3.0.0"
			}
			if scenario == "locked" || scenario == "readonly" || scenario == "workspace" {
				if privateRequest(a, s, raw, license).Code < 400 {
					t.Fatal("guard bypass")
				}
				return
			}
			if err := verifyPrivateBundle(p, license, device, raw); err == nil {
				t.Fatal("accepted invalid license")
			}
			if scenario != "device" && scenario != "version" && privateRequest(a, s, raw, license).Code < 400 {
				t.Fatal("invalid bundle installed")
			}
		})
	}
}
func TestPluginCommerceCatalogAndPublicIsolation(t *testing.T) {
	a, s, _, _ := privateFixture(t)
	p := s.Catalog.Plugins[0]
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("private package must never be fetched")
		return nil, nil
	})}
	if _, err := a.marketPackage(t.Context(), s, p); err == nil {
		t.Fatal("private download accepted")
	}
	for _, mutate := range []func(*marketPlugin){func(p *marketPlugin) { p.Package = "packages/market-fixture/private.zip" }, func(p *marketPlugin) { p.Commerce = &marketCommerce{Status: "available", Model: "perpetual-major"} }, func(p *marketPlugin) {
		p.Commerce = &marketCommerce{Status: "coming_soon", Model: "perpetual-major", PurchaseURL: "javascript:alert(1)"}
	}, func(p *marketPlugin) { p.Distribution = "unknown" }} {
		bad := p
		mutate(&bad)
		if validateMarketDistribution(bad) == nil {
			t.Fatal("invalid private catalog accepted")
		}
	}
}

func TestPluginCommerceExtendedCatalogKeepsLegacyPublicEntries(t *testing.T) {
	var shipped marketSnapshot
	if err := json.Unmarshal(shippedMarket, &shipped); err != nil {
		t.Fatal(err)
	}
	if len(shipped.Catalog.Plugins) != 23 || len(shipped.Catalog.PrivateListings) != 1 {
		t.Fatal("public/private catalog partition")
	}
	for _, p := range shipped.Catalog.Plugins {
		if p.Package == "" || p.Bytes == 0 || !marketSHA.MatchString(p.SHA256) || p.Distribution == "private" {
			t.Fatal("old host cannot refresh public catalog")
		}
	}
	if err := validateMarket(shipped); err != nil {
		t.Fatal(err)
	}
	a, s, _, _ := privateFixture(t)
	s.Catalog.PrivateListings = s.Catalog.Plugins
	s.Catalog.Plugins = nil
	if err := validateMarket(s); err != nil {
		t.Fatal(err)
	}
	cacheMarket(t, a, s)
	result := request(a, "GET", "/api/plugin-market", nil)
	requireStatus(t, result, 200)
	if !strings.Contains(result.Body.String(), "market-fixture") {
		t.Fatal("private listing omitted")
	}
	requireStatus(t, request(a, "GET", "/api/plugin-market/market-fixture/download?revision="+s.Revision, nil), 403)
}

func TestPluginCommerceIdentitySurvivesRestart(t *testing.T) {
	root := t.TempDir()
	work, ref, data := filepath.Join(root, "work"), filepath.Join(root, "ref"), filepath.Join(root, "data")
	a, err := New(work, ref, data)
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.pluginLicenseDevice()
	a.Close()
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(work, ref, data)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	second, err := b.pluginLicenseDevice()
	if err != nil || first != second {
		t.Fatal("activation identity changed on server restart")
	}
}
