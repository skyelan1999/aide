package server

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestIssue89CachedManifestBoundToCatalogIdentity(t *testing.T) {
	for _, field := range []string{"version", "id", "valid"} {
		t.Run(field, func(t *testing.T) {
			a, s, _ := marketFixture(t, "2.0.0")
			entry := s.Catalog.Plugins[0]
			if _, err := a.marketPackage(context.Background(), s, entry); err != nil {
				t.Fatal(err)
			}
			if field == "version" {
				entry.Version = "3.0.0"
			}
			if field == "id" {
				entry.ID = "another-plugin"
			}
			requests := 0
			a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(*http.Request) (*http.Response, error) { requests++; return nil, errors.New("cache-only fixture") })}
			b, err := a.marketPackage(context.Background(), s, entry)
			if field == "valid" {
				if err != nil || len(b) == 0 {
					t.Fatal("valid offline cache rejected", err)
				}
			} else if err == nil {
				t.Fatalf("cached manifest bypassed catalog %s binding", field)
			}
			if requests != 0 {
				t.Fatal("matching hash should be checked locally before any network fallback")
			}
		})
	}
}
