package server

import "testing"

// Issue #51: a missing vault entry returns an empty key without an error.
func TestAutoModelRouterEmptyProviderKey(t *testing.T) {
	for _, test := range []struct {
		name       string
		removeKeys []string
		wantModel  string
	}{
		{"configured winner", nil, "write-model"},
		{"missing winning key", []string{"b"}, "code-model"},
		{"all missing preserves manual fallback", []string{"a", "b"}, "code-model"},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := testApp(t)
			routerFixture(t, a)
			cfg := routerSettings(t, a, "http://a.invalid", "http://b.invalid")
			a.mu.Lock()
			defer a.mu.Unlock()
			for _, provider := range test.removeKeys {
				a.vault.Delete(modelProviderVaultID(provider, "fixture"))
			}
			task := &Task{Prompt: "write document", ModelSelection: &SessionModelSelection{ModelRouter: "auto"}}
			chosen, decision := a.routeModelLocked(task, cfg, []Message{{Role: "user", Content: "write document"}}, ProfileParams{MaxTokens: 1024}, nil, "chat")
			if chosen.Model != test.wantModel {
				t.Fatalf("selected %q, want %q", chosen.Model, test.wantModel)
			}
			if len(test.removeKeys) == 2 && (chosen.APIKey != cfg.APIKey || decision.Profile != "") {
				t.Fatal("no eligible candidate must retain the existing manual fallback")
			}
		})
	}
}
